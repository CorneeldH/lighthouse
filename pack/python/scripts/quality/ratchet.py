#!/usr/bin/env python3
"""Kwaliteitsratel — telt ruff-regels en beoordeelt diff-coverage.

Aanroepvorm, overal letterlijk identiek (§2.2 van het ontwerp):

    uv run python scripts/quality/ratchet.py ruff
    uv run python scripts/quality/ratchet.py show
    uv run python scripts/quality/ratchet.py coverage --base origin/main
    uv run python scripts/quality/ratchet.py coverage --report coverage.json
    uv run python scripts/quality/ratchet.py nightly --report coverage.json
    uv run python scripts/quality/ratchet.py nightly --report coverage.json --since SHA --min-diff-pct 90 --dry-run

Geen console-script in pyproject.toml: een `ratchet`-executable zou een
installatiestap eisen die een git-hook niet kan garanderen.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
from collections.abc import Callable, Mapping, Sequence
from pathlib import Path
from types import ModuleType

# scripts/ is geen geïnstalleerd pakket. Zelf op sys.path zetten, zodat de
# aanroepvorm hierboven werkt zonder PYTHONPATH van buiten. Dezelfde vorm die
# tests/unit/lane/conftest.py voor scripts/opencode-lane gebruikt.
_SCRIPTS = Path(__file__).resolve().parents[1]
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))

from quality import diffcov
from quality.baseline import (
    COMPLEXIPY_KEY,
    COUNTED_RULES,
    ZERO_RULES,
    Baseline,
    BaselineError,
    load,
)
from quality.gitenv import suite_env

ALL_RULES: tuple[str, ...] = ZERO_RULES + COUNTED_RULES

# Dezelfde signatuur als quality.prcov_issues.Gh, hier los gedefinieerd in
# plaats van geïmporteerd — cmd_nightly importeert prcov_issues pas binnen
# zichzelf (zie de moduledocstring), dus deze alias mag geen module-brede
# afhankelijkheid van src.controller.* toevoegen.
Gh = Callable[[Sequence[str], Mapping[str, str] | None, str | None], str]

# Hoeveel locaties er per regel in de melding komen. Alle 64 C901-locaties
# afdrukken maakt de melding onleesbaar; de eerste handvol wijst de weg.
MAX_LOCATIONS = 20

BASELINE_NAME = "quality-baseline.toml"

# De teller telt functies met een cognitieve complexiteit strikt boven dit getal.
COGNITIVE_COUNTER_THRESHOLD = 15

# De twee harde grenzen per functie (#1192): geen baseline, geen bump, ook niet
# met een noqa-comment. Een score gelijk aan de grens is groen, strikt erboven rood.
CYCLOMATIC_LIMIT = 20
COGNITIVE_LIMIT = 25

# ruff draagt de cyclomatische score in het bericht van elke C901-bevinding:
# `f` is too complex (22 > 10). ruff meldt alleen functies boven de drempel, dus
# de ratel pint die op 10 (`--config`, zie _ruff_findings): een hogere
# max-complexity in pyproject.toml zou anders scores 21 en hoger tot die drempel
# verbergen voor de harde grens, en de C901-teller iets anders laten tellen.
C901_THRESHOLD = 10
C901_SCORE = re.compile(r"is too complex \((\d+) > \d+\)")
C901_NAME = re.compile(r"`([^`]+)`")


def repo_root() -> Path:
    proc = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        capture_output=True,
        text=True,
        check=False,
    )
    if proc.returncode != 0:
        raise SystemExit("ratchet: geen git-repo gevonden vanaf de huidige map")
    return Path(proc.stdout.strip())


def _read_pin(root: Path, name: str) -> str:
    """Lees een versiepin uit een eigen bestand in de repowortel.

    Eigen bestand en geen variabele in drie scripts: één plek betekent dat er
    niets uit elkaar kán lopen. .claude/hooks/ruff-format.sh leest hetzelfde
    bestand; CI leest het transitief via deze module.

    Ongepind was prima zolang ruff alleen autofixte. Zodra dezelfde ruff telt,
    verschuift een release de getallen en maakt CI rood op een PR die niets
    veranderde — hetzelfde probleem dat de workflow-audit oploste door zizmor,
    actionlint en shellcheck te pinnen.
    """
    try:
        pin = (root / name).read_text(encoding="utf-8").strip()
    except OSError as exc:
        raise SystemExit(f"ratchet: {name} onleesbaar: {exc}") from exc
    if not re.fullmatch(r"\d+\.\d+\.\d+", pin):
        raise SystemExit(f"ratchet: {name} bevat geen versie: {pin!r}")
    return pin


def ruff_pin(root: Path) -> str:
    """De ene bron van waarheid voor de ruff-versie: .ruff-version."""
    return _read_pin(root, ".ruff-version")


def complexipy_pin(root: Path) -> str:
    """De ene bron van waarheid voor de complexipy-versie: .complexipy-version."""
    return _read_pin(root, ".complexipy-version")


def _ruff_findings(root: Path) -> list[dict]:
    """De ruff-bevindingen (JSON) over src/ + tests/, of SystemExit als ruff niet draait.

    --ignore-noqa is niet optioneel: zonder die vlag onderdrukt één
    `# noqa: F401` de teller en is de gate met één comment te omzeilen (§3.2).
    Er staan vandaag 23 zulke comments in de repo.

    Geen --isolated: de PT011-instelling uit pyproject.toml moet meedoen. Wel
    een expliciete --select, zodat de getelde set exact deze vijftien blijft,
    ook als extend-select later uitdijt. Om dezelfde reden staat de
    mccabe-drempel vast via --config: wat C901 telt, en welke scores de harde
    cyclomatische grens ziet, hangt dan niet van pyproject.toml af.
    """
    version = ruff_pin(root)
    cmd = [
        "uvx",
        f"ruff@{version}",
        "check",
        "--ignore-noqa",
        "--output-format",
        "json",
        "--select",
        ",".join(ALL_RULES),
        "--config",
        f"lint.mccabe.max-complexity={C901_THRESHOLD}",
        "src/",
        "tests/",
    ]
    proc = subprocess.run(cmd, cwd=root, capture_output=True, text=True, check=False)
    # ruff geeft 0 (schoon) of 1 (bevindingen); al het andere is "de tool kon
    # niet draaien" en dat is rood, niet groen. Fail closed (§7).
    if proc.returncode not in (0, 1):
        raise SystemExit(
            f"ratchet: ruff@{version} kon niet draaien (rc={proc.returncode}) — "
            f"de pin resolvet niet.\n{proc.stderr}"
        )
    try:
        return json.loads(proc.stdout or "[]")
    except ValueError as exc:
        raise SystemExit(
            f"ratchet: ruff-uitvoer niet te lezen: {exc}\n{proc.stderr}"
        ) from exc


def _relative_name(finding: dict, root: Path) -> str:
    filename = finding.get("filename", "?")
    try:
        return str(Path(filename).relative_to(root))
    except ValueError:
        return filename


def _c901_score(finding: dict, location: str) -> tuple[str, int]:
    """(`locatie functie`, score) uit het bericht van één C901-bevinding.

    Een bericht zonder leesbare score is rood: na een ruff-bump zou een stille
    nul de harde cyclomatische grens uitzetten (fail closed, §7).
    """
    message = finding.get("message", "")
    match = C901_SCORE.search(message)
    if match is None:
        raise SystemExit(
            f"ratchet: de C901-score is onleesbaar op {location}: {message!r} — "
            "het berichtformaat van ruff is veranderd, pas C901_SCORE aan."
        )
    name = C901_NAME.search(message)
    return f"{location} {name.group(1) if name else '?'}", int(match.group(1))


def count_ruff(
    root: Path,
) -> tuple[dict[str, int], dict[str, list[str]], list[tuple[str, int]]]:
    """({code: aantal}, {code: [locaties]}, [(locatie functie, C901-score)]).

    Het derde element draagt de cyclomatische score per functie, voor de harde
    grens in `judge_complexity`; de eerste twee voeden de tellers.
    """
    counts = dict.fromkeys(ALL_RULES, 0)
    where: dict[str, list[str]] = {code: [] for code in ALL_RULES}
    cyclomatic: list[tuple[str, int]] = []
    invalid_syntax_files: list[str] = []
    for finding in _ruff_findings(root):
        code = finding.get("code")
        filename = _relative_name(finding, root)
        if code == "invalid-syntax":
            # ruff kon dit bestand niet parsen: elke echte hit erin blijft
            # ongeteld en de arm zou stil groen rapporteren — fail closed (§7)
            # in plaats van "de baseline kan omlaag".
            invalid_syntax_files.append(filename)
            continue
        if code not in counts:
            continue
        counts[code] += 1
        position = finding.get("location") or {}
        location = f"{filename}:{position.get('row')}:{position.get('column')}"
        where[code].append(location)
        if code == "C901":
            cyclomatic.append(_c901_score(finding, location))
    if invalid_syntax_files:
        names = "\n".join(f"  {name}" for name in sorted(set(invalid_syntax_files)))
        raise SystemExit(
            "ratchet: ruff kon deze bestanden niet parsen (invalid-syntax) — de "
            "telling erin is onbetrouwbaar, dus rood in plaats van stil groen:\n"
            f"{names}"
        )
    return counts, where, cyclomatic


def _failed_files(output: str) -> list[str]:
    """De paden uit de `Failed to process <pad>`-regels van complexipy."""
    return sorted(set(re.findall(r"Failed to process (.+)", output)))


def _complexipy_entry(entry: object, version: str) -> tuple[str, int]:
    """(`pad::functie`, score) uit één element, of SystemExit bij een afwijkende vorm."""
    if isinstance(entry, dict):
        path = entry.get("path")
        name = entry.get("function_name")
        score = entry.get("complexity")
        # type() en niet isinstance(): bool erft van int.
        if isinstance(path, str) and isinstance(name, str) and type(score) is int:
            return f"{path}::{name}", score
    raise SystemExit(
        f"ratchet: complexipy@{version} gaf een onleesbaar element: {entry!r}"
    )


def _complexipy_scores(output: Path, version: str) -> list[tuple[str, int]]:
    """(`pad::functie`, score) per functie uit het JSON-bestand van complexipy.

    Een element zonder pad, naam of gehele score is rood: een stil overgeslagen
    functie zou de teller kunstmatig laag houden. Dubbele namen blijven staan;
    de teller telt regels, geen unieke namen.
    """
    try:
        data = json.loads(output.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise SystemExit(
            f"ratchet: complexipy@{version} schreef geen leesbare uitvoer: {exc}"
        ) from exc
    if not isinstance(data, list):
        raise SystemExit(f"ratchet: complexipy@{version} gaf geen lijst: {data!r:.200}")
    return [_complexipy_entry(entry, version) for entry in data]


def count_complexipy(root: Path) -> list[tuple[str, int]]:
    """Per functie (`pad::functie`, cognitieve score) over src/ + tests/.

    --no-ignore is niet optioneel, om dezelfde reden als --ignore-noqa bij ruff:
    één `# noqa: complexipy` achter een `def` zou de functie uit de telling
    halen. Uitvoer en cache gaan naar een tijdelijke map, zodat de werkboom
    schoon blijft en een oude cache de telling niet kan beïnvloeden.

    De drempel staat op een miljoen: complexipy geeft rc 1 bij een functie boven
    die drempel én bij een bestand dat hij niet kon verwerken, en alleen het
    tweede mag hier rood geven. Dat tweede zou stil minder functies tellen, dus
    het is fail closed (§7) en niet "de baseline kan omlaag".

    De cwd is de tijdelijke map, niet de repo: complexipy leest `[tool.complexipy]
    exclude` uit pyproject.toml, of een (.)complexipy.toml, uit zijn cwd, en
    --no-ignore stopt dat niet. Eén configregel zou anders bestanden stil uit de
    teller en de harde grens halen. src/ en tests/ gaan er absoluut in, de paden
    in de uitvoer komen relatief terug; .gitignore blijft gewoon gelden.
    """
    version = complexipy_pin(root)
    with tempfile.TemporaryDirectory(prefix="ratchet-complexipy-") as tmp:
        output = Path(tmp) / "complexipy.json"
        cmd = [
            "uvx",
            f"complexipy@{version}",
            str(root / "src"),
            str(root / "tests"),
            "--no-ignore",
            "--plain",
            "--max-complexity-allowed",
            "1000000",
            "--output",
            str(output),
            "--output-format",
            "json",
            "--cache-dir",
            str(Path(tmp) / "cache"),
        ]
        proc = subprocess.run(cmd, cwd=tmp, capture_output=True, text=True, check=False)
        failed = _failed_files(f"{proc.stdout}\n{proc.stderr}")
        if failed:
            names = "\n".join(f"  {name}" for name in failed)
            raise SystemExit(
                f"ratchet: complexipy@{version} kon deze bestanden niet verwerken — "
                "de telling erin is onbetrouwbaar, dus rood in plaats van stil "
                f"groen:\n{names}"
            )
        if proc.returncode != 0:
            raise SystemExit(
                f"ratchet: complexipy@{version} kon niet draaien "
                f"(rc={proc.returncode}).\n{proc.stderr}"
            )
        return [
            (_relative_to_root(name, root), score)
            for name, score in _complexipy_scores(output, version)
        ]


def _relative_to_root(name: str, root: Path) -> str:
    """`pad::functie` met het pad relatief aan de repo, zoals bij ruff."""
    path, sep, function = name.partition("::")
    try:
        path = str(Path(path).relative_to(root))
    except ValueError:
        pass
    return f"{path}{sep}{function}"


def _over_threshold(scores: list[tuple[str, int]]) -> list[tuple[str, int]]:
    """De functies met een score strikt boven COGNITIVE_COUNTER_THRESHOLD, hoogste eerst."""
    return _over_limit(scores, COGNITIVE_COUNTER_THRESHOLD)


def _over_limit(scores: list[tuple[str, int]], limit: int) -> list[tuple[str, int]]:
    """De functies met een score strikt boven `limit`, hoogste eerst."""
    over = [item for item in scores if item[1] > limit]
    return sorted(over, key=lambda item: (-item[1], item[0]))


def _hard_limit_failures(
    axis: str, scores: list[tuple[str, int]], limit: int
) -> list[str]:
    """Eén foutregel per functie boven de harde grens, hoogstens MAX_LOCATIONS."""
    over = _over_limit(scores, limit)
    lines = [
        f"{axis} {score} > {limit}: {name} — harde grens, geen bump mogelijk"
        for name, score in over[:MAX_LOCATIONS]
    ]
    if len(over) > MAX_LOCATIONS:
        lines.append(f"… en nog {len(over) - MAX_LOCATIONS}")
    return lines


def judge_complexity(
    baseline: Baseline,
    cyclomatic: list[tuple[str, int]],
    cognitive: list[tuple[str, int]],
) -> tuple[list[str], list[str]]:
    """(fouten, meldingen) voor de complexiteit. Een lege fouten-lijst is groen.

    Drie oordelen: de twee harde grenzen per functie (cyclomatisch 20,
    cognitief 25; geen baseline, geen bump) en de teller `cognitive_over_15`,
    die niet mag stijgen.
    """
    failures = _hard_limit_failures("cyclomatisch", cyclomatic, CYCLOMATIC_LIMIT)
    failures += _hard_limit_failures("cognitief", cognitive, COGNITIVE_LIMIT)
    over = _over_threshold(cognitive)
    base = baseline.cognitive_over_15
    now = len(over)
    where = {COMPLEXIPY_KEY: [f"{name} (cognitief {score})" for name, score in over]}
    notes: list[str] = []
    if now > base:
        failures.append(
            f"{COMPLEXIPY_KEY}: {base} -> {now} (+{now - base})\n"
            + _locations(where, COMPLEXIPY_KEY)
        )
    elif now < base:
        notes.append(f"{COMPLEXIPY_KEY}: {base} -> {now}; de baseline kan omlaag")
    return failures, notes


def _locations(where: dict[str, list[str]], rule: str) -> str:
    hits = where.get(rule, [])
    shown = "\n".join(f"    {hit}" for hit in hits[:MAX_LOCATIONS])
    if len(hits) > MAX_LOCATIONS:
        shown += f"\n    … en nog {len(hits) - MAX_LOCATIONS}"
    return shown


def compare(
    baseline: Baseline, counts: dict[str, int], where: dict[str, list[str]]
) -> tuple[list[str], list[str]]:
    """(fouten, meldingen). Een lege fouten-lijst is groen."""
    failures: list[str] = []
    notes: list[str] = []
    for rule in ZERO_RULES:
        if counts.get(rule):
            failures.append(
                f"{rule}: {counts[rule]} hit(s) op een nulregel — geen bump mogelijk\n"
                + _locations(where, rule)
            )
    for rule in COUNTED_RULES:
        base = baseline.counts[rule]
        now = counts.get(rule, 0)
        if now > base:
            failures.append(
                f"{rule}: {base} -> {now} (+{now - base})\n" + _locations(where, rule)
            )
        elif now < base:
            # Zonder deze melding zakt de streep nooit en is de ratel
            # eenrichtingsverkeer dat vastroest op de openingsstand (§3.4).
            notes.append(f"{rule}: {base} -> {now}; de baseline kan omlaag")
    return failures, notes


# Dezelfde selectie als .claude/hooks/pre-commit-tests.sh en als ci.yml, zodat
# "lokaal groen" en "CI groen" hetzelfde betekenen.
COVERAGE_ARGV = [
    "uv",
    "run",
    "pytest",
    "tests/unit",
    "-m",
    "not slow and not hardware_audio",
    "--cov=src",
    "--cov-report=json:coverage.json",
]


def _git(root: Path, args: list[str]) -> str:
    proc = subprocess.run(
        ["git", *args], cwd=root, capture_output=True, text=True, check=False
    )
    if proc.returncode != 0:
        raise SystemExit(f"ratchet: `git {' '.join(args)}` faalde:\n{proc.stderr}")
    return proc.stdout


def _merge_base(root: Path, base_ref: str) -> str:
    proc = subprocess.run(
        ["git", "merge-base", "HEAD", base_ref],
        cwd=root,
        capture_output=True,
        text=True,
        check=False,
    )
    if proc.returncode != 0:
        raise SystemExit(
            f"ratchet: geen merge-base tussen HEAD en {base_ref}. "
            f"Draai `git fetch origin main` en probeer opnieuw.\n{proc.stderr}"
        )
    return proc.stdout.strip()


def judge_coverage(baseline: Baseline, result: diffcov.DiffCoverage) -> list[str]:
    """Lege lijst is groen. De melding noemt de ongedekte regels, niet alleen %."""
    if result.total == 0:
        return []
    if result.pct >= baseline.min_diff_pct:
        return []
    lines = [
        (
            f"diff-coverage {result.pct:.1f}% < {baseline.min_diff_pct}% "
            f"({result.covered}/{result.total} gewijzigde statements gedekt)"
        )
    ]
    for path, numbers in sorted(result.uncovered.items()):
        lines.append(f"  {path}: ongedekt op {', '.join(str(n) for n in numbers)}")
    return lines


def _acquire_report(root: Path, report_arg: str | None) -> tuple[Path | None, int]:
    """(pad, 0) bij succes; (None, rc) als de aanroeper meteen moet stoppen met rc.

    Puur uitgetrokken uit cmd_coverage om diens complexiteit onder de C901-limiet
    te krijgen — zelfde twee vormen, zelfde meldingen, geen gedragswijziging:
    zonder --report draait de suite en levert een vers rapport, mét --report
    wordt een bestaand rapport gelezen.
    """
    if report_arg is None:
        report = root / "coverage.json"
        # Een coverage.json van een eerdere, gecrashte run zou stille groene
        # getallen geven op verouderde data. Weghalen vóór de run.
        report.unlink(missing_ok=True)
        proc = subprocess.run(COVERAGE_ARGV, cwd=root, check=False, env=suite_env())
        if proc.returncode != 0:
            print(
                "ratchet: de suite faalt — een groene ratel op een rode suite is "
                "een leugen.",
                file=sys.stderr,
            )
            return None, proc.returncode
        if not report.exists():
            print(
                "ratchet: pytest gaf 0 maar er is geen coverage.json; het rapport "
                "is niet geschreven.",
                file=sys.stderr,
            )
            return None, 1
        return report, 0

    report = Path(report_arg)
    if not report.is_absolute():
        report = root / report
    if not report.exists():
        print(f"ratchet: {report} ontbreekt", file=sys.stderr)
        return None, 1
    return report, 0


def judge_total(baseline: Baseline, total_pct: float) -> list[str]:
    """Lege lijst is groen. De globale src-dekking mag niet onder de vloer zakken.

    Anders dan de diff-drempel is dit een vastgezette meting: hij staat op wat
    de repo haalde toen de gate aanging en kruipt omhoog zodra nieuwe code de
    dekking optilt (#703). De prijs is bekend en geaccepteerd: een groot
    goed-getest bestand verwijderen laat het gemiddelde zakken en is dan rood.
    """
    if total_pct >= baseline.min_total_pct:
        return []
    return [
        f"globale src-dekking {total_pct:.2f}% < {baseline.min_total_pct}% — "
        "de vloer is een gemeten stand, geen streefgetal; hij mag niet zakken"
    ]


def cmd_coverage(root: Path, base_ref: str, report_arg: str | None) -> int:
    baseline = load(root / BASELINE_NAME, root)
    merge_base = _merge_base(root, base_ref)
    added = diffcov.added_lines(
        _git(root, ["diff", "--unified=0", f"{merge_base}..HEAD", "--", "src/"])
    )

    report, rc = _acquire_report(root, report_arg)
    if report is None:
        return rc

    try:
        data = json.loads(report.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        print(f"ratchet: {report} is onleesbaar: {exc}", file=sys.stderr)
        return 1

    try:
        result = diffcov.diff_coverage(
            added,
            data,
            read_source=lambda rel: (root / rel).read_text(encoding="utf-8"),
            exclude=baseline.exclude,
        )
    except diffcov.CoverageReportError as exc:
        print(f"ratchet: {exc}", file=sys.stderr)
        return 1

    total_pct = float(data.get("totals", {}).get("percent_covered", 0.0))
    failures = judge_coverage(baseline, result) + judge_total(baseline, total_pct)
    if failures:
        print(file=sys.stderr)
        for line in failures:
            print(f"ratchet: {line}", file=sys.stderr)
        print(
            "\nSchrijf een test die deze regels raakt. Kan de code hier echt niet "
            "draaien, gebruik dan `# pragma: no cover` met een reden, per blok.",
            file=sys.stderr,
        )
        return 1
    print(f"ratchet: globale src-dekking {total_pct:.2f}% >= {baseline.min_total_pct}%")
    if result.total == 0:
        print(
            "ratchet: geen uitvoerbare gewijzigde regels — coverage niet van toepassing"
        )
        return 0
    print(
        f"ratchet: diff-coverage {result.pct:.1f}% >= {baseline.min_diff_pct}% "
        f"({result.covered}/{result.total})"
    )
    return 0


def _run_url() -> str | None:
    """De permalink naar deze Actions-run, of None buiten Actions."""
    server = os.environ.get("GITHUB_SERVER_URL")
    repo = os.environ.get("GITHUB_REPOSITORY")
    run_id = os.environ.get("GITHUB_RUN_ID")
    if server and repo and run_id:
        return f"{server}/{repo}/actions/runs/{run_id}"
    return None


def _pr_report_line(pr: int, result: diffcov.DiffCoverage) -> str:
    if result.total == 0:
        return f"ratchet: PR #{pr} geen uitvoerbare src/-regels"
    return f"ratchet: PR #{pr} {result.pct:.1f}% ({result.covered}/{result.total})"


def _remediate_pr(
    prcov_issues: ModuleType,
    gh: Gh,
    pr: int,
    result: diffcov.DiffCoverage,
    threshold: int,
    run_url: str | None,
    dry_run: bool,
) -> bool:
    """Issue, handoff en board voor één PR onder de drempel — stap 5 (#1016).

    Print zijn eigen regels. Geeft True terug als deze PR stap 5 laat falen
    (rc 1 in de aanroeper) — een ontbrekend of leeg board-token telt daarbij
    mee: een groene run zou anders het watermerk optillen (`last_success_sha`),
    en dan reconcilieert de volgende nacht de board-stap nooit meer (review
    ronde 1, bevinding 1; plan-tekst "de run wordt rood met een melding die
    het token noemt").

    Een fout uit `ensure_issue`/`ensure_handoff` gooit door naar de
    aanroeper, die hem generiek meldt. Alleen de `ensure_on_board`-fout wordt
    hier lokaal gevangen, zodat de melding `PAIA_BOARD_TOKEN` met naam noemt
    in plaats van alleen de rauwe gh-foutmelding (bevinding 4).
    """
    if dry_run:
        print(f"ratchet: PR #{pr}: --dry-run, zou issue/handoff/board bijwerken")
        return False
    action = prcov_issues.ensure_issue(gh, pr, result, threshold, run_url)
    if action is None:
        print(f"ratchet: PR #{pr}: al afgehandeld (gesloten issue)")
        return False
    _, issue_number, issue_url = action
    prcov_issues.ensure_handoff(gh, issue_number, pr, threshold)
    token = os.environ.get(prcov_issues.BOARD_TOKEN_ENV, "")
    if not token:
        print(
            f"ratchet: PR #{pr}: {prcov_issues.BOARD_TOKEN_ENV} ontbreekt of is leeg "
            "— geen board-aanroep, issue en handoff staan er wel",
            file=sys.stderr,
        )
        return True
    try:
        line = prcov_issues.ensure_on_board(gh, issue_number, issue_url, token)
    except (
        subprocess.CalledProcessError,
        subprocess.TimeoutExpired,
        OSError,
        ValueError,
        KeyError,
    ) as exc:
        print(
            f"ratchet: PR #{pr}: board-stap faalde "
            f"({prcov_issues.BOARD_TOKEN_ENV} mag niet schrijven?): {exc}",
            file=sys.stderr,
        )
        return True
    print(f"ratchet: PR #{pr}: {line}")
    return False


def cmd_nightly(
    root: Path,
    report: str,
    since: str | None,
    min_diff_pct: int | None,
    dry_run: bool,
    gh: Gh | None = None,
) -> int:
    """Nightly-arm (#1016): diff-coverage per PR sinds het watermerk, plus de
    globale dekkingsvloer op hetzelfde rapport.

    `prcov`/`prcov_issues` komen hier binnen, niet bovenaan de module: zo
    blijft `ratchet.py ruff` — dat alleen `scripts/quality/*.py` kopieert,
    zonder `src/controller/` — draaien (zie de moduledocstring).
    """
    from quality import prcov, prcov_issues

    if gh is None:
        gh = prcov_issues.run_gh

    baseline = load(root / BASELINE_NAME, root)
    threshold = baseline.min_diff_pct if min_diff_pct is None else min_diff_pct

    report_path = Path(report)
    if not report_path.is_absolute():
        report_path = root / report_path
    try:
        data = json.loads(report_path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        print(f"ratchet: {report_path} is onleesbaar: {exc}", file=sys.stderr)
        return 1

    if since is not None:
        watermark = since
    else:
        try:
            watermark = prcov_issues.last_success_sha(gh)
        except (
            subprocess.CalledProcessError,
            subprocess.TimeoutExpired,
            OSError,
            ValueError,
            KeyError,
        ) as exc:
            print(f"ratchet: watermerk niet te bepalen: {exc}", file=sys.stderr)
            return 1
    try:
        commits = prcov.select_commits(root, watermark)
        attributed = prcov.attribute(root, commits)
    except prcov.GitCommandError as exc:
        print(f"ratchet: {exc}", file=sys.stderr)
        return 1

    def read_source(rel: str) -> str:
        return (root / rel).read_text(encoding="utf-8")

    run_url = _run_url()

    step5_failed = False
    for pr, paths in sorted(attributed.items()):
        # Per PR, niet in één keer via `prcov.per_pr`: dat bouwt alle PR's in
        # één dict comprehension, dus één ontbrekende rapport-entry
        # (`CoverageReportError`) zou de hele nacht laten crashen — geen
        # issues voor de andere PR's, geen vloercontrole, watermerk blijft
        # steken (M1, #1016-review).
        try:
            result = diffcov.diff_coverage(paths, data, read_source, baseline.exclude)
        except diffcov.CoverageReportError as exc:
            print(f"ratchet: PR #{pr}: {exc}", file=sys.stderr)
            step5_failed = True
            continue
        print(_pr_report_line(pr, result))
        if result.total == 0 or result.pct >= threshold:
            continue
        try:
            failed = _remediate_pr(
                prcov_issues, gh, pr, result, threshold, run_url, dry_run
            )
        except (
            subprocess.CalledProcessError,
            subprocess.TimeoutExpired,
            OSError,
            ValueError,
            KeyError,
        ) as exc:
            print(f"ratchet: PR #{pr}: {exc}", file=sys.stderr)
            step5_failed = True
            continue
        if failed:
            step5_failed = True

    total_pct = float(data.get("totals", {}).get("percent_covered", 0.0))
    floor_failures = judge_total(baseline, total_pct)
    for line in floor_failures:
        print(f"ratchet: {line}", file=sys.stderr)

    return 1 if step5_failed or floor_failures else 0


def _fit_width(measured: int, width: int) -> str:
    """Het nieuwe getal krijgt de breedte van het oude, zodat een comment
    erachter op zijn kolom blijft staan."""
    return f"{measured:<{width}}" if len(str(measured)) < width else str(measured)


def _lower_cognitive_counter(lines: list[str], measured: int) -> list[str]:
    """Verlaag `cognitive_over_15 = N` in `lines` (op zijn plaats); nooit omhoog."""
    for index, line in enumerate(lines):
        match = re.match(
            rf"^(?P<key>{COMPLEXIPY_KEY})(?P<pad>\s*=\s*)(?P<value>\d+)", line
        )
        if match is None:
            continue
        current = int(match.group("value"))
        if measured >= current:
            return []
        start, end = match.span("value")
        replacement = _fit_width(measured, len(match.group("value")))
        lines[index] = line[:start] + replacement + line[end:]
        return [f"{COMPLEXIPY_KEY}: {current} -> {measured}"]
    return []


def rewrite_baseline(
    text: str,
    counts: Mapping[str, int],
    total_pct: float | None = None,
    cognitive_over_15: int | None = None,
) -> tuple[str, list[str]]:
    """(nieuwe tekst, ["RULE: oud -> nieuw"]). Verlaagt alleen, nooit omhoog.

    Regelgewijs met een regex en niet via een TOML-writer, omdat de comments
    achter de getallen het enige geheugen van de FP-steekproef zijn. tomllib
    kan alleen lezen, en een writer erbij halen zou die comments weggooien.

    Omhoog schrijven is expliciet uitgesloten. Een stijging hoort rood te zijn
    in de gate; zou de verlager hem wegschrijven, dan legt de automatisering
    stil een bump vast die niemand besloten heeft — precies de bypass die dit
    ontwerp nergens wil hebben.
    """
    changes: list[str] = []
    lines = text.splitlines(keepends=True)
    for index, line in enumerate(lines):
        match = re.match(r"^(?P<rule>[A-Z]+[0-9]+)(?P<pad>\s*=\s*)(?P<value>\d+)", line)
        if match is None:
            continue
        rule = match.group("rule")
        if rule not in COUNTED_RULES or rule not in counts:
            continue
        current = int(match.group("value"))
        measured = counts[rule]
        if measured >= current:
            continue
        replacement = _fit_width(measured, len(match.group("value")))
        start, end = match.span("value")
        lines[index] = line[:start] + replacement + line[end:]
        changes.append(f"{rule}: {current} -> {measured}")

    if cognitive_over_15 is not None:
        changes += _lower_cognitive_counter(lines, cognitive_over_15)

    if total_pct is not None:
        # Naar beneden afkappen, niet afronden: 64.98 blijft 64. Zonder die
        # demping opent de wekelijkse job een PR voor elke tiende procent ruis.
        measured_floor = int(total_pct)
        for index, line in enumerate(lines):
            match = re.match(
                r"^(?P<key>min_total_pct)(?P<pad>\s*=\s*)(?P<value>\d+)", line
            )
            if match is None:
                continue
            current = int(match.group("value"))
            if measured_floor <= current:
                # Nooit omlaag. Een dip mag de vloer niet meeslepen, anders is
                # het geen ratel meer maar een thermometer.
                break
            start, end = match.span("value")
            lines[index] = line[:start] + str(measured_floor) + line[end:]
            changes.append(f"min_total_pct: {current} -> {measured_floor}")
            break

    return "".join(lines), changes


def cmd_tighten(root: Path) -> int:
    """Zet de baseline op de gemeten stand: ruff omlaag, de dekkingsvloer omhoog.

    Schrijft het bestand; de PR eromheen is het werk van de workflow. Zo blijft
    de verlaging zichtbaar en reviewbaar in een diff — de ratel past zichzelf
    nooit stilzwijgend aan.
    """
    path = root / BASELINE_NAME
    baseline = load(path, root)
    counts, _, cyclomatic = count_ruff(root)
    cognitive_scores = count_complexipy(root)
    cognitive = len(_over_threshold(cognitive_scores))

    risen = [r for r in COUNTED_RULES if counts.get(r, 0) > baseline.counts[r]]
    if cognitive > baseline.cognitive_over_15:
        risen.append(COMPLEXIPY_KEY)
    zero_hits = [r for r in ZERO_RULES if counts.get(r)]
    hard_hits = len(_over_limit(cyclomatic, CYCLOMATIC_LIMIT)) + len(
        _over_limit(cognitive_scores, COGNITIVE_LIMIT)
    )
    if risen or zero_hits or hard_hits:
        # Fail closed: bij een rode gate is de stand geen "nieuwe streep" maar
        # een probleem. Het bestand aanraken zou de stijging wegpoetsen.
        print(
            "ratchet: de gate staat rood (gestegen: "
            f"{risen or '-'}, nulregelhits: {zero_hits or '-'}, "
            f"boven een harde grens: {hard_hits}) — "
            "eerst oplossen, dan pas verlagen.",
            file=sys.stderr,
        )
        return 1

    # De globale dekking komt uit dezelfde suite-run die de coverage-arm ook
    # gebruikt. Ontbreekt het rapport, dan verschuift alleen de ruff-as; een
    # ontbrekende meting mag de vloer niet raken.
    total_pct: float | None = None
    report = root / "coverage.json"
    if not report.exists():
        proc = subprocess.run(COVERAGE_ARGV, cwd=root, check=False, env=suite_env())
        if proc.returncode != 0:
            print(
                "ratchet: de suite faalt — de dekkingsvloer blijft ongemoeid.",
                file=sys.stderr,
            )
            return proc.returncode
    if report.exists():
        try:
            total_pct = float(
                json.loads(report.read_text(encoding="utf-8"))["totals"][
                    "percent_covered"
                ]
            )
        except (OSError, ValueError, KeyError, TypeError) as exc:
            print(f"ratchet: coverage.json onbruikbaar: {exc}", file=sys.stderr)
            return 1

    text = path.read_text(encoding="utf-8")
    new_text, changes = rewrite_baseline(text, counts, total_pct, cognitive)
    if not changes:
        print("ratchet: niets aan te scherpen — de baseline staat op de meting.")
        return 0
    path.write_text(new_text, encoding="utf-8")
    print("ratchet: baseline aangescherpt:")
    for change in changes:
        print(f"  {change}")
    return 0


def _report_failures(heading: str, failures: list[str]) -> None:
    if not failures:
        return
    print(file=sys.stderr)
    print(f"ratchet: {heading}", file=sys.stderr)
    for failure in failures:
        print(f"  {failure}", file=sys.stderr)


# Een stijgende teller (`S110: 0 -> 1`, `cognitive_over_15: 107 -> 108`) is de
# enige fout die een bump kan oplossen; een nulregel of harde grens niet.
BUMPABLE_FAILURE = re.compile(r"^\w+: \d+ -> \d+")
HARD_LIMIT_MARK = "harde grens, geen bump mogelijk"
COMPLEXITY_SKILL = (
    ".claude/skills/paia-python-schrijven/SKILL.md, sectie "
    "`## Een complexiteitsgrens halen: verdiepen, niet opknippen`"
)


def _failure_footer(failures: list[str]) -> str:
    """De slotregel bij rood: bump-advies alleen als een stijgende teller faalt.

    Een functie boven een harde grens moet in de code omlaag; het bump-advies
    zou daar de verkeerde weg wijzen.
    """
    parts: list[str] = []
    if any(BUMPABLE_FAILURE.match(failure) for failure in failures):
        parts.append(
            f"Los het op in de code, of pas {BASELINE_NAME} aan in dezelfde wijziging "
            "als je de stijging van een teller bewust accepteert."
        )
    if any(HARD_LIMIT_MARK in failure for failure in failures):
        parts.append(
            "Een functie boven een harde complexiteitsgrens moet in de code omlaag; "
            f"een bump bestaat daar niet. Hoe: {COMPLEXITY_SKILL}."
        )
    if not parts:
        parts.append("Los het op in de code: een nulregel kent geen bump.")
    parts.append(
        "Een `# noqa` helpt hier niet: de ratel telt met --ignore-noqa en --no-ignore."
    )
    return " ".join(parts)


def cmd_ruff(root: Path) -> int:
    baseline = load(root / BASELINE_NAME, root)
    counts, where, cyclomatic = count_ruff(root)
    cognitive = count_complexipy(root)
    failures, notes = compare(baseline, counts, where)
    complexity_failures, complexity_notes = judge_complexity(
        baseline, cyclomatic, cognitive
    )
    for note in notes + complexity_notes:
        print(f"ratchet: {note}")
    _report_failures("de ruff-tellers stijgen.", failures)
    _report_failures(
        "complexiteit boven de grens of de teller stijgt.", complexity_failures
    )
    if failures or complexity_failures:
        print(file=sys.stderr)
        print(_failure_footer(failures + complexity_failures), file=sys.stderr)
        return 1
    total = sum(counts[rule] for rule in COUNTED_RULES)
    print(
        f"ratchet: ruff ok ({total} hits op de tellers, geen stijging, nulregels schoon); "
        f"complexipy ok ({len(_over_threshold(cognitive))} functies cognitief > "
        f"{COGNITIVE_COUNTER_THRESHOLD})"
    )
    return 0


def cmd_show(root: Path) -> int:
    counts, _, _ = count_ruff(root)
    print("[ruff]")
    for rule in COUNTED_RULES:
        print(f"{rule:<6} = {counts[rule]}")
    print()
    print("# nulregels — horen NIET in de baseline, moeten 0 zijn:")
    for rule in ZERO_RULES:
        print(f"# {rule:<6} = {counts[rule]}")
    print()
    print("[complexipy]")
    print(f"{COMPLEXIPY_KEY} = {len(_over_threshold(count_complexipy(root)))}")
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="ratchet", description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("ruff", help="tel de vijftien regels en vergelijk met de baseline")
    sub.add_parser("show", help="print de huidige tellers zonder te oordelen")
    sub.add_parser(
        "tighten",
        help="zet quality-baseline.toml op de gemeten stand (ruff omlaag, dekkingsvloer omhoog)",
    )
    coverage = sub.add_parser("coverage", help="beoordeel diff-coverage")
    coverage.add_argument(
        "--base",
        default="origin/main",
        help="ref waartegen de merge-base wordt bepaald (default: origin/main)",
    )
    coverage.add_argument(
        "--report",
        default=None,
        help="bestaand coverage.json consumeren in plaats van de suite te draaien",
    )
    nightly = sub.add_parser(
        "nightly", help="oordeel diff-coverage per PR sinds het watermerk (#1016)"
    )
    nightly.add_argument(
        "--report", required=True, help="coverage.json van de volledige suite"
    )
    nightly.add_argument(
        "--since",
        default=None,
        help="watermerk-SHA; standaard de laatste geslaagde nightly-run",
    )
    nightly.add_argument(
        "--min-diff-pct",
        type=int,
        default=None,
        help="drempel-override in procent (default: baseline.min_diff_pct)",
    )
    nightly.add_argument(
        "--dry-run",
        action="store_true",
        help="alleen melden wat er zou gebeuren, geen gh-mutaties",
    )
    args = parser.parse_args(argv)

    root = repo_root()
    try:
        if args.command == "ruff":
            return cmd_ruff(root)
        if args.command == "tighten":
            return cmd_tighten(root)
        if args.command == "coverage":
            return cmd_coverage(root, args.base, args.report)
        if args.command == "nightly":
            return cmd_nightly(
                root, args.report, args.since, args.min_diff_pct, args.dry_run
            )
        return cmd_show(root)
    except BaselineError as exc:
        print(f"ratchet: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())

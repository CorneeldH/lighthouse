#!/usr/bin/env python3
"""Quality ratchet — counts ruff rules and complexity, and judges diff coverage.

Usage:

    python3 .lighthouse/scripts/quality/ratchet.py ruff
    python3 .lighthouse/scripts/quality/ratchet.py show
    python3 .lighthouse/scripts/quality/ratchet.py tighten
    python3 .lighthouse/scripts/quality/ratchet.py coverage [--base REF] [--report coverage.json]
    python3 .lighthouse/scripts/quality/ratchet.py nightly --report coverage.json [--since SHA] [--min-diff-pct 90] [--dry-run]

The directories come from lighthouse.toml: python.lint_dirs (ruff, complexipy),
python.source_dirs (coverage) and python.coverage_command (the suite). The base of
`coverage` defaults to origin/<forge.default_branch>.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import shlex
import subprocess
import sys
import tempfile
from collections.abc import Callable, Mapping, Sequence
from pathlib import Path
from types import ModuleType

# scripts/ is geen geïnstalleerd pakket. Zelf op sys.path zetten, zodat de
# aanroepvorm hierboven werkt zonder PYTHONPATH van buiten.
_SCRIPTS = Path(__file__).resolve().parents[1]
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))
# `.lighthouse/lib` next to `.lighthouse/scripts` (ontwerpkeuze 6 van (b)).
_LIB = Path(__file__).resolve().parents[2] / "lib"
if _LIB.is_dir() and str(_LIB) not in sys.path:
    sys.path.insert(0, str(_LIB))

from lighthouse_lib import config

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
# zichzelf, dus de andere subcommando's laden die module niet.
Gh = Callable[[Sequence[str], Mapping[str, str] | None, str | None], str]

# Hoeveel locaties er per regel in de melding komen. Alle 64 C901-locaties
# afdrukken maakt de melding onleesbaar; de eerste handvol wijst de weg.
MAX_LOCATIONS = 20

BASELINE_NAME = "quality-baseline.toml"

# De teller telt functies met een cognitieve complexiteit strikt boven dit getal.
COGNITIVE_COUNTER_THRESHOLD = 15

# De twee harde grenzen per functie: geen baseline, geen bump, ook niet
# met een noqa-comment. Een score gelijk aan de grens is groen, strikt erboven rood.
CYCLOMATIC_LIMIT = 20
COGNITIVE_LIMIT = 25
# De markering in de foutregel van een harde grens; `_failure_footer` herkent hem.
HARD_LIMIT_MARK = "hard limit, no bump possible"

# ruff draagt de cyclomatische score in het bericht van elke C901-bevinding:
# `f` is too complex (22 > 10). ruff meldt alleen functies boven de drempel, dus
# de ratel pint die op 10 (`--config`, zie _ruff_findings): een hogere
# max-complexity in pyproject.toml zou anders scores 21 en hoger tot die drempel
# verbergen voor de harde grens, en de C901-teller iets anders laten tellen.
C901_THRESHOLD = 10
C901_SCORE = re.compile(r"is too complex \((\d+) > \d+\)")
C901_NAME = re.compile(r"`([^`]+)`")


def repo_root() -> Path:
    """LIGHTHOUSE_REPO_ROOT, anders `git rev-parse --show-toplevel`."""
    try:
        return config.repo_root()
    except config.ConfigError as exc:
        raise SystemExit(f"ratchet: {exc}") from exc


def setting(key: str) -> object:
    """Een sleutel uit lighthouse.toml, met de default van de configlezer."""
    try:
        return config.lookup(config.load(config.config_path()), key)
    except config.ConfigError as exc:
        raise SystemExit(f"ratchet: {exc}") from exc
    except KeyError as exc:
        raise SystemExit(f"ratchet: {key} is not set in lighthouse.toml") from exc


def _dirs(key: str) -> list[str]:
    """De mappen onder `key`; een lege lijst stopt de ratel. Zonder paden zouden
    ruff, complexipy en `git diff --` de hele repo nemen en pytest geen `--cov`."""
    value = setting(key)
    assert isinstance(value, list)  # de configlezer controleert het type
    if not value:
        raise SystemExit(f"ratchet: {key} is empty; the ratchet needs at least one directory")
    return [str(item) for item in value]


def _read_pin(root: Path, name: str) -> str:
    """Lees een versiepin uit een eigen bestand in de repowortel.

    Eigen bestand en geen variabele in drie scripts: één plek betekent dat er
    niets uit elkaar kán lopen. De hook ruff-format.sh leest hetzelfde
    bestand; CI leest het transitief via deze module.

    Ongepind was prima zolang ruff alleen autofixte. Zodra dezelfde ruff telt,
    verschuift een release de getallen en maakt CI rood op een PR die niets
    veranderde — hetzelfde probleem dat de workflow-audit oploste door zizmor,
    actionlint en shellcheck te pinnen.
    """
    try:
        pin = (root / name).read_text(encoding="utf-8").strip()
    except OSError as exc:
        raise SystemExit(f"ratchet: {name} is unreadable: {exc}") from exc
    if not re.fullmatch(r"\d+\.\d+\.\d+", pin):
        raise SystemExit(f"ratchet: {name} holds no version: {pin!r}")
    return pin


def ruff_pin(root: Path) -> str:
    """De ene bron van waarheid voor de ruff-versie: .ruff-version."""
    return _read_pin(root, ".ruff-version")


def complexipy_pin(root: Path) -> str:
    """De ene bron van waarheid voor de complexipy-versie: .complexipy-version."""
    return _read_pin(root, ".complexipy-version")


def _ruff_findings(root: Path) -> list[dict]:
    """De ruff-bevindingen (JSON) over `python.lint_dirs`, of SystemExit als ruff niet draait.

    --ignore-noqa is niet optioneel: zonder die vlag onderdrukt één
    `# noqa: F401` de teller en is de gate met één comment te omzeilen.

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
        *_dirs("python.lint_dirs"),
    ]
    proc = subprocess.run(cmd, cwd=root, capture_output=True, text=True, check=False)
    # ruff geeft 0 (schoon) of 1 (bevindingen); al het andere is "de tool kon
    # niet draaien" en dat is rood, niet groen. Fail closed.
    if proc.returncode not in (0, 1):
        raise SystemExit(
            f"ratchet: ruff@{version} could not run (rc={proc.returncode}) — the pin "
            f"does not resolve, or a directory in python.lint_dirs does not exist.\n{proc.stderr}"
        )
    try:
        return json.loads(proc.stdout or "[]")
    except ValueError as exc:
        raise SystemExit(
            f"ratchet: ruff output is unreadable: {exc}\n{proc.stderr}"
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
    nul de harde cyclomatische grens uitzetten (fail closed).
    """
    message = finding.get("message", "")
    match = C901_SCORE.search(message)
    if match is None:
        raise SystemExit(
            f"ratchet: the C901 score is unreadable at {location}: {message!r} — "
            "ruff changed its message format; adjust C901_SCORE."
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
            # ongeteld en de arm zou stil groen rapporteren — fail closed
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
            "ratchet: ruff could not parse these files (invalid-syntax) — the "
            "count in them is unreliable, so red instead of silently green:\n"
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
        f"ratchet: complexipy@{version} gave an unreadable element: {entry!r}"
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
            f"ratchet: complexipy@{version} wrote no readable output: {exc}"
        ) from exc
    if not isinstance(data, list):
        raise SystemExit(f"ratchet: complexipy@{version} gave no list: {data!r:.200}")
    return [_complexipy_entry(entry, version) for entry in data]


def count_complexipy(root: Path) -> list[tuple[str, int]]:
    """Per functie (`pad::functie`, cognitieve score) over `python.lint_dirs`.

    --no-ignore is niet optioneel, om dezelfde reden als --ignore-noqa bij ruff:
    één `# noqa: complexipy` achter een `def` zou de functie uit de telling
    halen. Uitvoer en cache gaan naar een tijdelijke map, zodat de werkboom
    schoon blijft en een oude cache de telling niet kan beïnvloeden.

    De drempel staat op een miljoen: complexipy geeft rc 1 bij een functie boven
    die drempel én bij een bestand dat hij niet kon verwerken, en alleen het
    tweede mag hier rood geven. Dat tweede zou stil minder functies tellen, dus
    het is fail closed en niet "de baseline kan omlaag".

    De cwd is de tijdelijke map, niet de repo: complexipy leest `[tool.complexipy]
    exclude` uit pyproject.toml, of een (.)complexipy.toml, uit zijn cwd, en
    --no-ignore stopt dat niet. Eén configregel zou anders bestanden stil uit de
    teller en de harde grens halen. De mappen gaan er absoluut in, de paden
    in de uitvoer komen relatief terug; .gitignore blijft gewoon gelden.
    """
    version = complexipy_pin(root)
    with tempfile.TemporaryDirectory(prefix="ratchet-complexipy-") as tmp:
        output = Path(tmp) / "complexipy.json"
        cmd = [
            "uvx",
            f"complexipy@{version}",
            *(str(root / d) for d in _dirs("python.lint_dirs")),
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
                f"ratchet: complexipy@{version} could not process these files — "
                "the count in them is unreliable, so red instead of silently "
                f"green:\n{names}"
            )
        if proc.returncode != 0:
            raise SystemExit(
                f"ratchet: complexipy@{version} could not run "
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
        f"{axis} {score} > {limit}: {name} — {HARD_LIMIT_MARK}"
        for name, score in over[:MAX_LOCATIONS]
    ]
    if len(over) > MAX_LOCATIONS:
        lines.append(f"… and {len(over) - MAX_LOCATIONS} more")
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
    failures = _hard_limit_failures("cyclomatic", cyclomatic, CYCLOMATIC_LIMIT)
    failures += _hard_limit_failures("cognitive", cognitive, COGNITIVE_LIMIT)
    over = _over_threshold(cognitive)
    base = baseline.cognitive_over_15
    now = len(over)
    where = {COMPLEXIPY_KEY: [f"{name} (cognitive {score})" for name, score in over]}
    notes: list[str] = []
    if now > base:
        failures.append(
            f"{COMPLEXIPY_KEY}: {base} -> {now} (+{now - base})\n"
            + _locations(where, COMPLEXIPY_KEY)
        )
    elif now < base:
        notes.append(f"{COMPLEXIPY_KEY}: {base} -> {now}; the baseline can go down")
    return failures, notes


def _locations(where: dict[str, list[str]], rule: str) -> str:
    hits = where.get(rule, [])
    shown = "\n".join(f"    {hit}" for hit in hits[:MAX_LOCATIONS])
    if len(hits) > MAX_LOCATIONS:
        shown += f"\n    … and {len(hits) - MAX_LOCATIONS} more"
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
                f"{rule}: {counts[rule]} hit(s) on a zero rule — no bump possible\n"
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
            # eenrichtingsverkeer dat vastroest op de openingsstand.
            notes.append(f"{rule}: {base} -> {now}; the baseline can go down")
    return failures, notes


def coverage_argv() -> list[str]:
    """De suite met coverage: python.coverage_command, met de --cov-vlaggen uit
    python.source_dirs erachter. Geen shell: het commando wordt met shlex gesplitst."""
    command = setting("python.coverage_command")
    assert isinstance(command, str)
    return [
        *shlex.split(command),
        *(f"--cov={d}" for d in _dirs("python.source_dirs")),
        "--cov-report=json:coverage.json",
    ]


def _git(root: Path, args: list[str]) -> str:
    proc = subprocess.run(
        ["git", *args], cwd=root, capture_output=True, text=True, check=False
    )
    if proc.returncode != 0:
        raise SystemExit(f"ratchet: `git {' '.join(args)}` failed:\n{proc.stderr}")
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
            f"ratchet: no merge base between HEAD and {base_ref}. "
            f"Fetch {base_ref} (git fetch) and try again.\n{proc.stderr}"
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
            f"diff coverage {result.pct:.1f}% < {baseline.min_diff_pct}% "
            f"({result.covered}/{result.total} changed statements covered)"
        )
    ]
    for path, numbers in sorted(result.uncovered.items()):
        lines.append(f"  {path}: uncovered at {', '.join(str(n) for n in numbers)}")
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
        proc = subprocess.run(coverage_argv(), cwd=root, check=False, env=suite_env())
        if proc.returncode != 0:
            print(
                "ratchet: the suite fails — a green ratchet on a red suite is a lie.",
                file=sys.stderr,
            )
            return None, proc.returncode
        if not report.exists():
            print(
                "ratchet: the suite returned 0 but there is no coverage.json; the "
                "report was not written.",
                file=sys.stderr,
            )
            return None, 1
        return report, 0

    report = Path(report_arg)
    if not report.is_absolute():
        report = root / report
    if not report.exists():
        print(f"ratchet: {report} is missing", file=sys.stderr)
        return None, 1
    return report, 0


def judge_total(baseline: Baseline, total_pct: float) -> list[str]:
    """Lege lijst is groen. De globale src-dekking mag niet onder de vloer zakken.

    Anders dan de diff-drempel is dit een vastgezette meting: hij staat op wat
    de repo haalde toen de gate aanging en kruipt omhoog zodra nieuwe code de
    dekking optilt. De prijs is bekend en geaccepteerd: een groot
    goed-getest bestand verwijderen laat het gemiddelde zakken en is dan rood.
    """
    if total_pct >= baseline.min_total_pct:
        return []
    return [
        f"total coverage {total_pct:.2f}% < {baseline.min_total_pct}% — "
        "the floor is a measured level, not a target; it may not drop"
    ]


def cmd_coverage(root: Path, base_ref: str, report_arg: str | None) -> int:
    baseline = load(root / BASELINE_NAME, root)
    merge_base = _merge_base(root, base_ref)
    added = diffcov.added_lines(
        _git(
            root,
            ["diff", "--unified=0", f"{merge_base}..HEAD", "--", *_dirs("python.source_dirs")],
        )
    )

    report, rc = _acquire_report(root, report_arg)
    if report is None:
        return rc

    try:
        data = json.loads(report.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        print(f"ratchet: {report} is unreadable: {exc}", file=sys.stderr)
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
            "\nWrite a test that hits these lines. If the code really cannot run "
            "here, use `# pragma: no cover` with a reason, per block.",
            file=sys.stderr,
        )
        return 1
    print(f"ratchet: total coverage {total_pct:.2f}% >= {baseline.min_total_pct}%")
    if result.total == 0:
        print(
            "ratchet: no executable changed lines — coverage does not apply"
        )
        return 0
    print(
        f"ratchet: diff coverage {result.pct:.1f}% >= {baseline.min_diff_pct}% "
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
        return f"ratchet: PR #{pr} no executable source lines"
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
    """Issue, handoff en board voor één PR onder de drempel — stap 5.

    Print zijn eigen regels. Geeft True terug als deze PR stap 5 laat falen
    (rc 1 in de aanroeper) — een ontbrekend of leeg board-token telt daarbij
    mee: een groene run zou anders het watermerk optillen (`last_success_sha`),
    en dan reconcilieert de volgende nacht de board-stap nooit meer.
    Zonder `[forge.board]` is er geen board-stap en dus geen token nodig.

    Een fout uit `ensure_issue`/`ensure_handoff` gooit door naar de
    aanroeper, die hem generiek meldt. Alleen de `ensure_on_board`-fout wordt
    hier lokaal gevangen, zodat de melding het board-token met naam noemt
    in plaats van alleen de rauwe gh-foutmelding.
    """
    if dry_run:
        print(f"ratchet: PR #{pr}: --dry-run, would update issue/handoff/board")
        return False
    action = prcov_issues.ensure_issue(gh, pr, result, threshold, run_url)
    if action is None:
        print(f"ratchet: PR #{pr}: already handled (closed issue)")
        return False
    _, issue_number, issue_url = action
    prcov_issues.ensure_handoff(gh, issue_number, pr, threshold)
    if prcov_issues.board() is None:
        print(f"ratchet: PR #{pr}: board: skipped — no [forge.board] configured")
        return False
    token = os.environ.get(prcov_issues.BOARD_TOKEN_ENV, "")
    if not token:
        print(
            f"ratchet: PR #{pr}: {prcov_issues.BOARD_TOKEN_ENV} is missing or empty "
            "— no board call; the issue and the handoff are there",
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
            f"ratchet: PR #{pr}: the board step failed "
            f"(may {prcov_issues.BOARD_TOKEN_ENV} not write?): {exc}",
            file=sys.stderr,
        )
        return True
    print(f"ratchet: PR #{pr}: {line}")
    return False


def _watermark(prcov_issues: ModuleType, gh: Gh) -> str | None:
    """De `headSha` van de laatste geslaagde nightly, of `None` (dan de laatste 24
    uur, met een melding op stderr: een stille terugval verbergt een kapotte
    configuratie)."""
    workflow = prcov_issues.nightly_workflow()
    if workflow is None:
        print(
            "nightly: no python.nightly_workflow; judging the merges of the last 24 hours",
            file=sys.stderr,
        )
        return None
    sha = prcov_issues.last_success_sha(gh, workflow)
    if sha is None:
        print(
            f"nightly: no successful run of {workflow} on the default branch; "
            "judging the merges of the last 24 hours",
            file=sys.stderr,
        )
    return sha


def cmd_nightly(
    root: Path,
    report: str,
    since: str | None,
    min_diff_pct: int | None,
    dry_run: bool,
    gh: Gh | None = None,
) -> int:
    """Nightly-arm: diff-coverage per PR sinds het watermerk, plus de globale
    dekkingsvloer op hetzelfde rapport.

    Het watermerk is de laatste geslaagde run van `python.nightly_workflow`. Zonder
    die sleutel, of zonder geslaagde run, beoordeelt de nightly de merges van de
    laatste 24 uur — en zegt dat. `prcov`/`prcov_issues` komen hier binnen, niet
    bovenaan de module: de andere subcommando's hebben ze niet nodig.
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
        print(f"ratchet: {report_path} is unreadable: {exc}", file=sys.stderr)
        return 1

    if since is not None:
        watermark = since
    else:
        try:
            watermark = _watermark(prcov_issues, gh)
        except (
            subprocess.CalledProcessError,
            subprocess.TimeoutExpired,
            OSError,
            ValueError,
            KeyError,
            config.ConfigError,
        ) as exc:
            print(f"ratchet: the watermark cannot be determined: {exc}", file=sys.stderr)
            return 1
    try:
        commits = prcov.select_commits(root, watermark)
        attributed = prcov.attribute(root, commits, _dirs("python.source_dirs"))
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
        # steken.
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
            config.ConfigError,
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
            "ratchet: the gate is red (risen: "
            f"{risen or '-'}, zero-rule hits: {zero_hits or '-'}, "
            f"above a hard limit: {hard_hits}) — "
            "fix that first, then lower.",
            file=sys.stderr,
        )
        return 1

    # De globale dekking komt uit dezelfde suite-run die de coverage-arm ook
    # gebruikt. Ontbreekt het rapport, dan verschuift alleen de ruff-as; een
    # ontbrekende meting mag de vloer niet raken.
    total_pct: float | None = None
    report = root / "coverage.json"
    if not report.exists():
        proc = subprocess.run(coverage_argv(), cwd=root, check=False, env=suite_env())
        if proc.returncode != 0:
            print(
                "ratchet: the suite fails — the coverage floor stays as it is.",
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
            print(f"ratchet: coverage.json is unusable: {exc}", file=sys.stderr)
            return 1

    text = path.read_text(encoding="utf-8")
    new_text, changes = rewrite_baseline(text, counts, total_pct, cognitive)
    if not changes:
        print("ratchet: nothing to tighten — the baseline is at the measurement.")
        return 0
    path.write_text(new_text, encoding="utf-8")
    print("ratchet: baseline tightened:")
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
COMPLEXITY_SKILL = (
    ".claude/skills/lighthouse-python-writing/SKILL.md, section "
    "`## Reaching a complexity limit: deepen, don't split`"
)


def _failure_footer(failures: list[str]) -> str:
    """De slotregel bij rood: bump-advies alleen als een stijgende teller faalt.

    Een functie boven een harde grens moet in de code omlaag; het bump-advies
    zou daar de verkeerde weg wijzen.
    """
    parts: list[str] = []
    if any(BUMPABLE_FAILURE.match(failure) for failure in failures):
        parts.append(
            f"Fix it in the code, or adjust {BASELINE_NAME} in the same change "
            "if you accept the rise of a counter on purpose."
        )
    if any(HARD_LIMIT_MARK in failure for failure in failures):
        parts.append(
            "A function above a hard complexity limit must come down in the code; "
            f"there is no bump there. How: {COMPLEXITY_SKILL}."
        )
    if not parts:
        parts.append("Fix it in the code: a zero rule has no bump.")
    parts.append(
        "A `# noqa` does not help here: the ratchet counts with --ignore-noqa and --no-ignore."
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
    _report_failures("the ruff counters rise.", failures)
    _report_failures(
        "complexity above the limit or the counter rises.", complexity_failures
    )
    if failures or complexity_failures:
        print(file=sys.stderr)
        print(_failure_footer(failures + complexity_failures), file=sys.stderr)
        return 1
    total = sum(counts[rule] for rule in COUNTED_RULES)
    print(
        f"ratchet: ruff ok ({total} hits on the counters, no rise, zero rules clean); "
        f"complexipy ok ({len(_over_threshold(cognitive))} functions cognitive > "
        f"{COGNITIVE_COUNTER_THRESHOLD})"
    )
    return 0


def cmd_show(root: Path) -> int:
    counts, _, _ = count_ruff(root)
    print("[ruff]")
    for rule in COUNTED_RULES:
        print(f"{rule:<6} = {counts[rule]}")
    print()
    print("# zero rules — do NOT belong in the baseline, must be 0:")
    for rule in ZERO_RULES:
        print(f"# {rule:<6} = {counts[rule]}")
    print()
    print("[complexipy]")
    print(f"{COMPLEXIPY_KEY} = {len(_over_threshold(count_complexipy(root)))}")
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="ratchet", description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("ruff", help="count the fifteen rules and compare with the baseline")
    sub.add_parser("show", help="print the current counters without judging")
    sub.add_parser(
        "tighten",
        help="set quality-baseline.toml to the measured level (ruff down, coverage floor up)",
    )
    coverage = sub.add_parser("coverage", help="judge diff coverage")
    coverage.add_argument(
        "--base",
        default=None,
        help="ref the merge base is taken against (default: origin/<forge.default_branch>)",
    )
    coverage.add_argument(
        "--report",
        default=None,
        help="read an existing coverage.json instead of running the suite",
    )
    nightly = sub.add_parser(
        "nightly", help="judge diff coverage per PR since the watermark"
    )
    nightly.add_argument(
        "--report", required=True, help="coverage.json of the full suite"
    )
    nightly.add_argument(
        "--since",
        default=None,
        help="watermark SHA; by default the last successful nightly run",
    )
    nightly.add_argument(
        "--min-diff-pct",
        type=int,
        default=None,
        help="threshold override in percent (default: baseline.min_diff_pct)",
    )
    nightly.add_argument(
        "--dry-run",
        action="store_true",
        help="only report what would happen, no gh mutations",
    )
    args = parser.parse_args(argv)

    root = repo_root()
    try:
        if args.command == "ruff":
            return cmd_ruff(root)
        if args.command == "tighten":
            return cmd_tighten(root)
        if args.command == "coverage":
            base = args.base or f"origin/{setting('forge.default_branch')}"
            return cmd_coverage(root, base, args.report)
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

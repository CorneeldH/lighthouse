"""Toets een `gh pr create`-body op een geldige vorm (#1091).

Pure logica onder de PreToolUse-hook `pr-create-gate.sh`; ontwerp:
`docs/developer/planning/specs/2026-10-03-pr-verifieert-issue-design.md`,
sectie 3. `evidence.py` (de runner die de Evidence-rijen uitvoert) en
`pr-create-gate.sh` delen deze lib, zodat de vorm op één plek staat.

Een body moet minstens één van drie vormen hebben (mengen mag):

- `Closes #N` met een kop `### Evidence #N` en een tabel `AC | Command | Result`
  met één rij per acceptatiecriterium van het issue;
- `Refs #N — <reden>` (reden minstens `MIN_REDEN` tekens);
- `No issue: <reden>` (idem).

Alleen stdlib: de hook draait onder de host-`python3`. De repo komt uit
`lighthouse.toml` (`src/controller/identity.py`, ook stdlib); ontbreekt of
deugt die toml niet, dan faalt het laden luid met een `IdentityError`.
"""

from __future__ import annotations

import os
import re
import subprocess
import sys
import time
from collections.abc import Callable
from pathlib import Path
from typing import NamedTuple

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
sys.path.append(
    str(Path(__file__).resolve().parents[3])
)  # repo-root, voor `src.controller`

from git_invocations import (
    Step,
    Unparseable,
    basename,
    existing_ancestor,
    resolve,
    walk,
)

from src.controller.identity import identity

# Geen try/except: een `IdentityError` bij het laden is de luide faal die
# `pr-create-gate.sh` als blokkade meldt.
REPO = identity().repo

CLOSE_WORDS = r"(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)"
# Doel: #N, <repo>#N of de volledige issue-URL.
_DOEL = (
    rf"(?:https://github\.com/{re.escape(REPO)}/issues/"
    rf"|{re.escape(REPO)}#|#)(\d+)(?![\w/])"
)
_CLOSES_RE = re.compile(rf"(?<![\w/]){CLOSE_WORDS}:?[ \t]+{_DOEL}", re.IGNORECASE)

REFS_SEP = ("—", "-", ":")  # op dezelfde regel als `Refs #N`
MIN_REDEN = 10  # tekens, na strip(), voor Refs én No issue
_REFS_RE = re.compile(r"(?<![\w/])Refs[ \t]+#(\d+)(?![\w/])(.*)", re.IGNORECASE)
_NO_ISSUE_RE = re.compile(r"^[ \t]*No issue[ \t]*:(.*)$", re.IGNORECASE | re.MULTILINE)

EVIDENCE_HEADER = ("AC", "Command", "Result")
_SCHEIDING_CEL_RE = re.compile(r"^:?-+:?$")
_CEL_SPLIT_RE = re.compile(r"(?<!\\)\|")

_AC_KOP_RE = re.compile(r"^#{2,4}[ \t]+Acceptatiecriteria[ \t]*$", re.IGNORECASE)
_KOP_RE = re.compile(r"^#{1,6}[ \t]")
_CHECKBOX_RE = re.compile(r"^[ \t]*[-*][ \t]+\[[ xX]\]")

AC_TIMEOUT_S = 5
# Totale deadline per `main`-aanroep over alle subprocess-calls (gh én git). Moet
# onder de hook-timeout (15 s in .claude/settings.json) blijven: een command-hook
# die verloopt laat de tool-call door, en dat zou fail-open zijn.
DEADLINE_S = 12
TERUGVAL = (
    "pr-create-gate: AC-telling niet meetbaar voor #{nr} ({oorzaak})"
    " — alleen structuur getoetst"
)


class Row(NamedTuple):
    """Eén datarij van het Evidence-blok; `command` is de ruwe cel, mét backticks."""

    ac: str
    command: str
    result: str


class Verdict(NamedTuple):
    blokkades: list[str]
    meldingen: list[str]


def strip_comments(body: str) -> str:
    """`\\r\\n` naar `\\n` (web-UI-bewerking), dan `<!-- … -->` eruit."""
    body = body.replace("\r\n", "\n")
    return re.sub(r"<!--.*?-->", "", body, flags=re.DOTALL)


def closes_targets(body: str) -> list[int]:
    """Uniek, in volgorde van voorkomen, de issues waarvoor de body een sluitwoord heeft."""
    gezien: list[int] = []
    for m in _CLOSES_RE.finditer(strip_comments(body)):
        nr = int(m.group(1))
        if nr not in gezien:
            gezien.append(nr)
    return gezien


def _reden(rest: str) -> str:
    """De reden achter een scheidingsteken, of "" zonder scheidingsteken."""
    rest = rest.strip()
    if not rest.startswith(REFS_SEP):
        return ""
    return rest[1:].strip()


def _refs_alle(body: str) -> list[tuple[int, str]]:
    """Elke `Refs #N` met zijn reden ("" als het scheidingsteken of de reden ontbreekt)."""
    return [
        (int(m.group(1)), _reden(m.group(2)))
        for m in _REFS_RE.finditer(strip_comments(body))
    ]


def refs_issues(body: str) -> list[int]:
    """Alleen de `Refs #N` met een reden van minstens `MIN_REDEN` tekens."""
    uit: list[int] = []
    for nr, reden in _refs_alle(body):
        if len(reden) >= MIN_REDEN and nr not in uit:
            uit.append(nr)
    return uit


def no_issue(body: str) -> bool:
    return any(
        len(m.group(1).strip()) >= MIN_REDEN
        for m in _NO_ISSUE_RE.finditer(strip_comments(body))
    )


def _cellen(regel: str) -> list[str]:
    """Splits een tabelregel op niet-geëscapete `|`; de buitenste pipes vallen weg."""
    s = regel.strip()
    s = s.removeprefix("|")
    if s.endswith("|") and not s.endswith("\\|"):
        s = s[:-1]
    return [c.strip() for c in _CEL_SPLIT_RE.split(s)]


def _rij(regel: str, volgnr: int) -> Row | str:
    """Eén datarij als `Row`, of de reden waarom hij ongeldig is."""
    cellen = _cellen(regel)
    if len(cellen) != len(EVIDENCE_HEADER):
        return (
            f"rij {volgnr} heeft {len(cellen)} cellen, verwacht {len(EVIDENCE_HEADER)}"
            " (een `|` in een cel schrijf je als `\\|`)"
        )
    if not all(cellen):
        return f"rij {volgnr} heeft een lege cel"
    ac, command, result = cellen
    if len(command) < 3 or not (command.startswith("`") and command.endswith("`")):
        return f"de Command-cel van rij {volgnr} moet tussen backticks staan"
    return Row(ac, command, result)


def evidence_rows(body: str, nr: int) -> list[Row] | str:
    """De rijen van `### Evidence #nr`, of een Nederlandse reden waarom het blok niet klopt."""
    regels = strip_comments(body).split("\n")
    kop = re.compile(rf"^###[ \t]+Evidence[ \t]+#{nr}[ \t]*$")
    start = next((i for i, r in enumerate(regels) if kop.match(r)), None)
    if start is None:
        return f"kop `### Evidence #{nr}` ontbreekt"

    i = start + 1
    while i < len(regels) and not regels[i].strip():
        i += 1
    if i >= len(regels) or not regels[i].strip().startswith("|"):
        return f"onder `### Evidence #{nr}` staat geen tabel"
    if tuple(_cellen(regels[i])) != EVIDENCE_HEADER:
        return f"de kop van de tabel moet `| {' | '.join(EVIDENCE_HEADER)} |` zijn"
    i += 1
    if i >= len(regels) or not all(
        _SCHEIDING_CEL_RE.match(c) for c in _cellen(regels[i])
    ):
        return "de scheidingsregel `|---|---|---|` onder de tabelkop ontbreekt"

    rijen: list[Row] = []
    for regel in regels[i + 1 :]:
        if not regel.strip().startswith("|"):
            break
        rij = _rij(regel, len(rijen) + 1)
        if isinstance(rij, str):
            return rij
        rijen.append(rij)
    return rijen or "het Evidence-blok heeft geen datarij"


def count_acs(issue_body: str) -> int | None:
    """Aantal checkboxregels onder `#{2,4} Acceptatiecriteria`; `None` bij geen kop of nul."""
    regels = issue_body.replace("\r\n", "\n").split("\n")
    start = next((i for i, r in enumerate(regels) if _AC_KOP_RE.match(r)), None)
    if start is None:
        return None
    aantal = 0
    for regel in regels[start + 1 :]:
        if _KOP_RE.match(regel):
            break
        if _CHECKBOX_RE.match(regel):
            aantal += 1
    return aantal or None


_deadline: float | None = None  # `time.monotonic()`-moment; door `main` gezet


def _budget(eigen: float) -> float:
    """`min(eigen timeout, resterende deadline)`; `<= 0` betekent: het budget is op."""
    if _deadline is None:
        return eigen
    return min(eigen, _deadline - time.monotonic())


def fetch_ac_count(nr: int) -> tuple[int | None, str]:
    """`(aantal, "")` of `(None, oorzaak)`; nooit een exception voor een gewone storing."""
    timeout = _budget(AC_TIMEOUT_S)
    if timeout <= 0:
        return None, "deadline bereikt"
    try:
        proc = subprocess.run(
            ["gh", "api", f"repos/{REPO}/issues/{nr}", "--jq", ".body"],
            capture_output=True,
            text=True,
            timeout=timeout,
            check=False,
        )
    except FileNotFoundError:
        return None, "gh ontbreekt"
    except subprocess.TimeoutExpired:
        if timeout < AC_TIMEOUT_S:
            return None, "deadline bereikt"
        return None, f"timeout na {AC_TIMEOUT_S:g} s"
    if proc.returncode != 0:
        return None, f"gh api faalde (rc {proc.returncode})"
    aantal = count_acs(proc.stdout)
    if aantal is None:
        return None, "geen AC-kop"
    return aantal, ""


def check_body(
    body: str,
    ac_count: Callable[[int], tuple[int | None, str]] = fetch_ac_count,
) -> Verdict:
    blokkades: list[str] = []
    meldingen: list[str] = []

    doelen = closes_targets(body)
    for nr in doelen:
        rijen = evidence_rows(body, nr)
        if isinstance(rijen, str):
            blokkades.append(f"Closes #{nr}: {rijen}")
            continue
        aantal, oorzaak = ac_count(nr)
        if aantal is None:
            meldingen.append(TERUGVAL.format(nr=nr, oorzaak=oorzaak))
        elif aantal != len(rijen):
            blokkades.append(
                f"Closes #{nr}: het issue heeft {aantal} acceptatiecriteria,"
                f" het Evidence-blok {len(rijen)} rijen — één rij per AC"
            )

    if not (doelen or refs_issues(body) or no_issue(body)):
        hint = ""
        if _refs_alle(body):
            hint = (
                f" `Refs #N` mist een reden van minstens {MIN_REDEN} tekens"
                f" (`Refs #N — <reden>`; scheiding {', '.join(REFS_SEP)})."
            )
        blokkades.append(
            "geen geldige vorm in de PR-body: `Closes #N` met `### Evidence #N`,"
            " `Refs #N — <reden>` of `No issue: <reden>`." + hint
        )
    return Verdict(blokkades, meldingen)


# --- gh pr create vinden, body-bron, scope ----------------------------------

# Opties met een waarde (los of `=`); `--recover` neemt ook een waarde.
_WAARDE_LANG = frozenset(
    {
        "--base", "--head", "--title", "--body", "--body-file", "--assignee",
        "--label", "--milestone", "--project", "--reviewer", "--repo",
        "--template", "--recover",
    }
)  # fmt: skip
_WAARDE_KORT = frozenset(
    {"-B", "-H", "-t", "-b", "-F", "-a", "-l", "-m", "-p", "-r", "-R", "-T"}
)
# Vlaggen die geen toetsbare body opleveren (de PR-tekst komt dan van elders).
_GEEN_BODY_LANG = frozenset(
    {"--fill", "--fill-first", "--fill-verbose", "--web", "--editor"}
)
_GEEN_BODY_KORT = frozenset("fwe")
GIT_TIMEOUT_S = 10


class PrCreate(NamedTuple):
    """Eén `gh pr create` (of `gh pr new`) met de stand van zijn eigen stap.

    `cwd` is `step.dirs["here"]` op het moment van de stap (None = onbekend);
    `body_file` is daar al tegen opgelost. `probleem` is een reden om te
    blokkeren vóór elke inhoudstoets. `repo_probleem` blokkeert ongeacht de
    cwd: bij een niet-letterlijke repo weten we niet of de PR hierheen gaat.
    """

    cwd: str | None
    repo: str | None
    body: str | None
    body_file: str | None
    probleem: str | None
    repo_probleem: str | None = None


Optie = tuple[str, bool, bool]  # (waarde, letterlijk, tilde)


def _optie(step: Step, k: int) -> tuple[str | None, Optie | None, list[str], int]:
    """Het woord op `k` als optie: `(naam, waarde, geen-body-vlaggen, volgende k)`."""
    words, literal, tildes = step.words, step.literal, step.tildes
    w = words[k]
    if w.startswith("--"):
        kop, eq, rest = w.partition("=")
        if kop in _WAARDE_LANG and eq:
            return kop, (rest, literal[k], False), [], k + 1
        if kop in _WAARDE_LANG:
            naam = kop
        else:
            return None, None, [w] if w in _GEEN_BODY_LANG else [], k + 1
    elif w.startswith("-") and len(w) > 1:
        if w[:2] in _WAARDE_KORT and w not in _WAARDE_KORT:  # `-Fbody.md`, `-F=body.md`
            return w[:2], (w[2:].removeprefix("="), literal[k], False), [], k + 1
        if w not in _WAARDE_KORT:
            vlaggen = [f"-{c}" for c in w[1:]] if set(w[1:]) <= _GEEN_BODY_KORT else []
            return None, None, vlaggen, k + 1
        naam = w
    else:
        return None, None, [], k + 1
    if k + 1 >= len(words):  # optie zonder waarde
        return naam, None, [], k + 1
    return naam, (words[k + 1], literal[k + 1], tildes[k + 1]), [], k + 2


def _opties(step: Step) -> tuple[dict[str, Optie], list[str]]:
    """Optiewaarden per optienaam en de gevonden geen-body-vlaggen."""
    waarden: dict[str, Optie] = {}
    vlaggen: list[str] = []
    k = step.idx + 3
    while k < len(step.words) and step.words[k] != "--":
        naam, waarde, nieuw, k = _optie(step, k)
        vlaggen.extend(nieuw)
        if naam is not None and waarde is not None:
            waarden[naam] = waarde
    return waarden, vlaggen


def _waarde(waarden: dict[str, Optie], *namen: str) -> Optie | None:
    return next((waarden[n] for n in namen if n in waarden), None)


def _body_bron(
    cwd: str | None, body_w: Optie | None, file_w: Optie | None, vlaggen: list[str]
) -> tuple[str | None, str | None, str | None]:
    """`(body, body_file, probleem)` volgens de body-bron-tabel van de spec."""
    if vlaggen:
        return None, None, f"{vlaggen[0]} levert geen toetsbare body"
    if body_w and file_w:
        return None, None, "kies één body-bron"
    bron = file_w or body_w
    if bron is None:
        return None, None, "geen body"
    waarde, lit, til = bron
    if file_w and waarde == "-":
        return None, None, "body via stdin is niet te toetsen"
    if not lit:
        return None, None, "niet-letterlijke body — gebruik --body-file"
    if not file_w:
        return waarde, None, None
    pad = resolve(waarde, lit, til)
    if pad is not None and os.path.isabs(pad):
        return None, os.path.normpath(pad), None
    if pad is None or cwd is None:
        return None, None, "pad van het body-bestand niet te herleiden"
    return None, os.path.normpath(os.path.join(cwd, pad)), None


def _gh_repo_env(step: Step) -> Optie | None:
    """Een `GH_REPO=…` vóór `gh` (ook na `env`), als optiewaarde; de laatste wint."""
    gevonden = None
    for k in range(step.idx):
        naam, gelijk, waarde = step.words[k].partition("=")
        if naam == "GH_REPO" and gelijk:
            gevonden = (waarde, step.literal[k], False)
    return gevonden


def _pr_create(step: Step) -> PrCreate:
    cwd = step.dirs["here"]  # LIVE dict: nu kopiëren, voor de volgende next()
    waarden, vlaggen = _opties(step)
    # gh: `--repo`/`-R` wint van `GH_REPO`.
    vlag = _waarde(waarden, "--repo", "-R")
    repo_w = vlag or _gh_repo_env(step)
    body, body_file, probleem = _body_bron(
        cwd,
        _waarde(waarden, "--body", "-b"),
        _waarde(waarden, "--body-file", "-F"),
        vlaggen,
    )
    if repo_w and not repo_w[1]:
        # Gaat de PR naar deze repo of een andere? Dat zien we niet, ook niet
        # aan de cwd: blokkeren, waar het commando ook draait.
        bron = "--repo" if vlag else "GH_REPO"
        return PrCreate(
            cwd,
            None,
            body,
            body_file,
            probleem,
            f"niet-letterlijke {bron} — schrijf de repo uit",
        )
    return PrCreate(cwd, repo_w[0] if repo_w else None, body, body_file, probleem)


def find_pr_creates(command: str, cwd: str | None) -> list[PrCreate]:
    """Elke `gh pr create`/`gh pr new` in `command`; gooit `Unparseable` bij een open quote."""
    uit: list[PrCreate] = []
    for step in walk(command, cwd):
        w = step.words
        if (
            basename(w[step.idx]) == "gh"
            and step.idx + 2 < len(w)
            and w[step.idx + 1] == "pr"
            and w[step.idx + 2] in ("create", "new")
        ):
            uit.append(_pr_create(step))
    return uit


def _common_dir(pad: str) -> tuple[str | None, str]:
    """`(git-common-dir, "")`, `(None, "geen repo")` of `(None, <git-fout>)`."""
    timeout = _budget(GIT_TIMEOUT_S)
    if timeout <= 0:
        return None, "KON NIET CONTROLEREN: deadline bereikt"
    try:
        proc = subprocess.run(
            [
                "git",
                "-C",
                pad,
                "rev-parse",
                "--path-format=absolute",
                "--git-common-dir",
            ],
            capture_output=True,
            text=True,
            timeout=timeout,
            check=False,
        )
    except subprocess.TimeoutExpired:
        return None, "KON NIET CONTROLEREN: git-timeout of deadline bereikt"
    if proc.returncode == 0:
        return os.path.realpath(proc.stdout.strip()), ""
    if "not a git repository" in proc.stderr:
        return None, "geen repo"
    return None, f"git faalde (rc {proc.returncode}): {proc.stderr.strip()}"


def _normaliseer_repo(waarde: str) -> str | None:
    """`owner/repo` (kleine letters) uit een `--repo`-waarde, of `None` als die vorm ontbreekt.

    gh accepteert `[HOST/]OWNER/REPO` en URL's (`https://…`, `ssh://git@…`, `git@host:o/r`),
    met of zonder `.git` of een slot-`/`.
    """
    rest = re.split(r"[?#]", waarde.strip(), maxsplit=1)[0]  # query en anker weg
    schema = "://" in rest
    rest = rest.split("://", 1)[-1]
    gebruiker = "@" in rest.split("/", 1)[0]
    if gebruiker:
        rest = rest.split("@", 1)[-1]
    heeft_host = schema or gebruiker
    if gebruiker and not schema:
        rest = rest.replace(":", "/", 1)  # scp-vorm `git@host:owner/repo`
    elif schema:
        host, _, pad = rest.partition("/")
        rest = f"{host.split(':', 1)[0]}/{pad}"  # poort weg
    delen = [d for d in rest.split("/") if d]
    if len(delen) > 2:
        heeft_host = True
    if heeft_host:
        delen = delen[1:]
    if len(delen) < 2:
        return None
    owner, naam = delen[0], delen[1].removesuffix(".git")
    if not owner or not naam:
        return None
    return f"{owner}/{naam}".casefold()


def _scope_repo(repo: str) -> tuple[str, str]:
    """Het scopebesluit voor een letterlijke repo (`--repo`/`-R` of `GH_REPO`)."""
    genormaliseerd = _normaliseer_repo(repo)
    if genormaliseerd is None:
        return "blokkeren", f"--repo {repo!r} heeft niet de vorm [HOST/]OWNER/REPO"
    if genormaliseerd == REPO.casefold():
        return "toetsen", ""
    return "overslaan", f"overgeslagen — andere repo {repo}"


def in_scope(pc: PrCreate, project_dir: str) -> tuple[str, str]:
    """`("toetsen"|"overslaan"|"blokkeren", reden)` voor deze `gh pr create`."""
    if pc.repo_probleem is not None:
        return "blokkeren", pc.repo_probleem
    if pc.repo is not None:
        return _scope_repo(pc.repo)
    if pc.cwd is None:
        return "blokkeren", "werkmap van `gh pr create` niet te herleiden"
    # Een `cd` naar een map die het commando zelf nog aanmaakt: de dichtstbijzijnde
    # bestaande voorouder bepaalt de repo.
    pad = existing_ancestor(pc.cwd)
    if pad is None:
        return "blokkeren", f"werkmap {pc.cwd} niet te herleiden"
    eigen, fout = _common_dir(project_dir)
    if eigen is None:
        return "blokkeren", f"repo van het project niet te bepalen ({fout})"
    dir_, fout = _common_dir(pad)
    if dir_ is None:
        if fout == "geen repo":
            return "overslaan", "overgeslagen — buiten deze repo"
        return "blokkeren", f"repo van {pad} niet te bepalen ({fout})"
    if dir_ == eigen:
        return "toetsen", ""
    return "overslaan", "overgeslagen — buiten deze repo"


BLOKKADE = (
    "pr-create-gate: GEBLOKKEERD — {reden}\n\n"
    "Maak de body met /paia-verify-pr <nr> en geef hem mee met --body-file <pad>.\n"
    "Vormen: `Closes #N` + `### Evidence #N` (één rij per AC), `Refs #N — <reden>`,"
    " `No issue: <reden>`.\n"
)


def _vormen(body: str, tellingen: dict[int, int | None]) -> str:
    vormen = []
    for nr in closes_targets(body):
        rijen = evidence_rows(body, nr)
        n_rijen = len(rijen) if isinstance(rijen, list) else 0
        aantal = tellingen.get(nr)
        telling = f"{aantal} AC, " if aantal is not None else "AC niet geteld, "
        vormen.append(f"Closes #{nr} ({telling}{n_rijen} rijen)")
    vormen.extend(f"Refs #{nr}" for nr in refs_issues(body))
    if no_issue(body):
        vormen.append("No issue")
    return ", ".join(vormen)


def _toets(pc: PrCreate) -> tuple[list[str], list[str]]:
    """`(blokkades, regels voor stdout)` voor één in-scope `gh pr create`."""
    if pc.probleem:
        return [pc.probleem], []
    body = pc.body
    if pc.body_file is not None:
        try:
            body = Path(pc.body_file).read_text(encoding="utf-8", errors="replace")
        except FileNotFoundError:
            return [f"body-bestand {pc.body_file} bestaat niet"], []
        except OSError as e:
            return [f"body-bestand {pc.body_file} niet te lezen ({e.strerror})"], []
    assert body is not None  # probleem is None => body of body_file is gezet
    tellingen: dict[int, int | None] = {}

    def telling(nr: int) -> tuple[int | None, str]:
        resultaat = fetch_ac_count(nr)
        tellingen[nr] = resultaat[0]
        return resultaat

    verdict = check_body(body, telling)
    if verdict.blokkades:
        return verdict.blokkades, []
    return [], [
        f"pr-create-gate: body getoetst — {_vormen(body, tellingen)}",
        *verdict.meldingen,
    ]


def main(argv: list[str] | None = None) -> int:
    """CLI: `pr_body_gate.py <cwd>`, commando op stdin.

    Exit 0 (stdout: doorlaatregels), 2 (stderr: blokkade), 3 (open quote).
    Elke andere uitkomst, ook een exception, is voor de hook `KON NIET CONTROLEREN`.
    """
    args = sys.argv[1:] if argv is None else argv
    if len(args) != 1:
        print("gebruik: pr_body_gate.py <cwd>  (commando op stdin)", file=sys.stderr)
        return 64
    cwd = args[0] or None
    global _deadline
    _deadline = time.monotonic() + DEADLINE_S
    project = os.environ.get("CLAUDE_PROJECT_DIR") or str(
        Path(__file__).resolve().parents[3]
    )
    try:
        creates = find_pr_creates(sys.stdin.read(), cwd)
    except Unparseable:
        return 3

    blokkades: list[str] = []
    regels: list[str] = []
    for pc in creates:
        besluit, reden = in_scope(pc, project)
        if besluit == "overslaan":
            regels.append(f"pr-create-gate: {reden}")
        elif besluit == "blokkeren":
            blokkades.append(reden)
        else:
            b, r = _toets(pc)
            blokkades.extend(b)
            regels.extend(r)
    if blokkades:
        sys.stderr.write(BLOKKADE.format(reden="; ".join(blokkades)))
        return 2
    for regel in regels:
        print(regel)
    return 0


if __name__ == "__main__":
    sys.exit(main())

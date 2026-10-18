"""Evidence-allowlist en runner voor de PR-nacontrole (#1091).

Een PR-body is onbetrouwbare tekst. Deze lib voert een Evidence-rij (de
Command-cel van `### Evidence #N`, zie `pr_body_gate.py`) daarom alleen uit als
het argv er letterlijk in de allowlist past: geen shell (nooit `shell=True`),
geen operatoren, geen schrijfopties, geen opties die een ander programma
starten. Wat niet past draait nooit; de rij is dan `ongeldig`.

De allowlist uitbreiden is een wijziging van deze lib én van de tabel in
`paia-verify-pr`, nooit iets wat een PR-body kan. Ontwerp:
`docs/developer/planning/specs/2026-10-03-pr-verifieert-issue-design.md`,
sectie 2 (allowlist) en 4 (vergelijkregel).

CLI (taak 6 en 7 roepen hem aan):

    evidence.py check '<Command-cel>'
    evidence.py run --issue N --body-file PAD --cwd MAP [--voor-pr]

Alleen stdlib: draait onder de host-`python3`.
"""

from __future__ import annotations

import argparse
import os
import posixpath
import re
import select
import shlex
import signal
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from pr_body_gate import Row, evidence_rows

# Opties zonder waarde, per commando; "VAL" = opties die één waarde slikken (los of met '=').
GREP_FLAGS = {
    "-F", "-E", "-G", "-i", "-n", "-c", "-l", "-L", "-q", "-r", "-R", "-w", "-x",
    "-o", "-s", "-h", "-H", "-v",
    "--fixed-strings", "--extended-regexp", "--basic-regexp", "--ignore-case",
    "--line-number", "--count", "--files-with-matches", "--files-without-match",
    "--quiet", "--recursive", "--dereference-recursive", "--word-regexp",
    "--line-regexp", "--only-matching", "--no-messages", "--no-filename",
    "--with-filename", "--invert-match",
}  # fmt: skip
GREP_VAL = {"-e", "--regexp", "--include", "--exclude"}
GIT_GREP_FLAGS = GREP_FLAGS - {
    "-r", "-R", "-s", "--recursive", "--dereference-recursive", "--no-messages",
}  # fmt: skip
GIT_GREP_VAL = {"-e"}
LS_FLAGS = {"-l", "-a", "-A", "-1", "-d", "-R", "-h"}
WC_FLAGS = {"-l", "-w", "-c", "-m"}
CAT_FLAGS = {"-n"}
# test: alle argumenten zijn een expressie; geen ervan schrijft of voert uit.
GIT_LOG_FLAGS = {"--oneline", "--stat", "--name-only", "--"}
GIT_LOG_VAL = {
    "-n",
    "--format",
    "--pretty",
    "--grep",
}  # -n alleen met een getal; -n5 mag ook
GIT_LOG_ALLEEN_GELIJK = {"--format", "--pretty"}
GIT_SUBS = {"log", "show", "ls-files", "grep"}
PYTEST_FLAGS = {"-q", "-v"}  # paden: normpath begint met tests/unit/
GH_API_VAL = {"--jq", "-q"}  # graphql: plus precies één -f query=…
GH_VIEW_FLAGS = {"--comments", "-c"}
GH_VIEW_VAL = {"--json", "--jq", "-q", "--repo", "-R"}

PUNCTUATIE = set("();<>|&")
PYTEST_PAD = "tests/unit/"
NA_PR = "na PR: "
VOOR_PR_UITKOMST = "na PR, niet gemeten"
GH_PROBE_TIMEOUT_S = 10
MAX_UITVOER = 4 * 1024 * 1024  # bytes, stdout en stderr samen
_LEESBLOK = 65536
# gojq (`gh --jq`) kan de omgeving en stdin lezen; een Evidence-rij heeft dat nooit nodig.
_JQ_VERBODEN_RE = re.compile(r"\benv\b|\$ENV|\binputs?\b")
_REF_VERBODEN = ("..", "@{", "~", "^")
_SUB_ENV = {
    "GIT_PAGER": "cat",
    "PAGER": "cat",
    "GH_PAGER": "cat",
    "GH_PROMPT_DISABLED": "1",
    "NO_COLOR": "1",
}


def split_command(cell: str) -> list[str] | str:
    """De Command-cel als argv, of de reden waarom hij geen kaal argv is."""
    s = cell.strip()
    if len(s) >= 2 and s.startswith("`") and s.endswith("`"):
        s = s[1:-1]
    s = s.replace("\\|", "|")
    if "$(" in s:
        return "`$(` (commandosubstitutie) is niet toegestaan"
    if "`" in s:
        return "een backtick in het commando is niet toegestaan"
    lexer = shlex.shlex(s, posix=True, punctuation_chars=True)
    lexer.whitespace_split = True
    lexer.commenters = ""
    try:
        argv = list(lexer)
    except ValueError as fout:
        return f"het commando laat zich niet splitsen ({fout})"
    for token in argv:
        if token and set(token) <= PUNCTUATIE:
            return f"shell-operator `{token}` is niet toegestaan"
    if not argv:
        return "leeg commando"
    return argv


class _Parsed:
    """Uitkomst van `_parse`: positionele args, args na `--` en (optie, waarde)-paren."""

    def __init__(self) -> None:
        self.pos: list[str] = []
        self.after: list[str] = []
        self.vals: list[tuple[str, str]] = []


def _waarde(
    args: list[str], i: int, naam: str, rest: str, *, vast: bool
) -> tuple[str, int] | str:
    """De waarde van optie `naam`; `(waarde, nieuwe index)` of een reden.

    `vast`: de waarde zit aan de optie vast (`--naam=rest`, `-nrest`). Dan is
    dat de hele waarde, ook als `rest` leeg is: git, GNU grep en pflag lezen
    `--naam=` als een lege waarde en parsen het volgende woord als een nieuwe
    optie. Zouden wij dat woord als waarde slikken, dan glipt `--output=x`
    erlangs. Een lege waarde is daarom geweigerd, net als een korte waarde die
    met `=` begint (`-q=x`: pflag strippen de `=`, getopt niet).
    """
    if vast:
        if not rest:
            return f"`{naam}=` met een lege waarde is niet toegestaan"
        if not naam.startswith("--") and rest.startswith("="):
            return f"`{naam}{rest}`: waarde die met `=` begint is niet toegestaan"
        return rest, i
    if i >= len(args):
        return f"`{naam}` mist een waarde"
    if not args[i]:
        return f"`{naam}` met een lege waarde is niet toegestaan"
    return args[i], i + 1


def _parse_lang(
    args: list[str], i: int, flags: set[str], val: set[str], uit: _Parsed
) -> int | str:
    """Eén `--lang`-woord (`args[i-1]`); geeft de nieuwe index of een reden."""
    tok = args[i - 1]
    naam, gelijk, waarde = tok.partition("=")
    if naam not in val:
        return i if tok in flags else f"optie `{tok}` staat niet in de allowlist"
    gekregen = _waarde(args, i, naam, waarde, vast=bool(gelijk))
    if isinstance(gekregen, str):
        return gekregen
    uit.vals.append((naam, gekregen[0]))
    return gekregen[1]


def _parse_kort(
    args: list[str], i: int, flags: set[str], val: set[str], uit: _Parsed
) -> int | str:
    """Eén kort cluster (`-nF`, `-n5`, `-efoo`; `args[i-1]`); de nieuwe index of een reden."""
    tok = args[i - 1]
    for pos, letter in enumerate(tok[1:], start=1):
        kort = f"-{letter}"
        if kort in val:
            aan = tok[pos + 1 :]
            gekregen = _waarde(args, i, kort, aan, vast=bool(aan))
            if isinstance(gekregen, str):
                return gekregen
            uit.vals.append((kort, gekregen[0]))
            return gekregen[1]
        if kort not in flags:
            return f"optie `{kort}` (in `{tok}`) staat niet in de allowlist"
    return i


def _parse(
    args: list[str], flags: set[str], val: set[str], *, dd_ok: bool = False
) -> _Parsed | str:
    """Splits `args` in vlaggen, waarde-opties en positionele args; een reden bij al het andere.

    Een woord dat met `-` begint en niet op de lijsten staat is geweigerd. Korte
    clusters (`-nF`) mogen als elke letter in `flags` staat; een waarde-letter
    (`-n5`, `-efoo`) slikt de rest van het woord, of anders het volgende woord.
    """
    uit = _Parsed()
    i = 0
    while i < len(args):
        tok = args[i]
        i += 1
        if tok == "--":
            if not dd_ok:
                return "`--` is hier niet toegestaan"
            uit.after = args[i:]
            return uit
        if tok == "-" or not tok.startswith("-"):
            uit.pos.append(tok)
            continue
        if tok in flags:
            continue
        stap = (_parse_lang if tok.startswith("--") else _parse_kort)(
            args, i, flags, val, uit
        )
        if isinstance(stap, str):
            return stap
        i = stap
    return uit


def _jq_reden(vals: list[tuple[str, str]]) -> str | None:
    for naam, waarde in vals:
        if naam in {"--jq", "-q"} and _JQ_VERBODEN_RE.search(waarde):
            return f"`{naam}`-expressie leest omgeving of invoer"
    return None


def _ref_reden(args: list[str]) -> str | None:
    for a in args:
        if any(t in a for t in _REF_VERBODEN):
            return f"`{a}` is branch-relatief (`..`, `@{{`, `~`, `^`)"
    return None


def _check_grep(args: list[str]) -> str | None:
    geparsed = _parse(args, GREP_FLAGS, GREP_VAL, dd_ok=True)
    return geparsed if isinstance(geparsed, str) else None


def _check_git(args: list[str]) -> str | None:
    if not args or args[0] not in GIT_SUBS:
        return "git: alleen log, show, ls-files en grep, zonder globale opties"
    sub, rest = args[0], args[1:]
    if sub == "grep":
        geparsed = _parse(rest, GIT_GREP_FLAGS, GIT_GREP_VAL, dd_ok=True)
        if isinstance(geparsed, str):
            return geparsed
        heeft_e = any(naam == "-e" for naam, _ in geparsed.vals)
        # Het eerste positionele woord zonder `-e` is het patroon, geen ref.
        return _ref_reden(geparsed.pos[0 if heeft_e else 1 :] + geparsed.after)
    # Zonder `=` leest git `--pretty` als losse vlag en `--format` als fout: het
    # volgende woord is dan een optie, geen waarde. Alleen de `=`-vorm is eenduidig.
    kaal = next((a for a in rest if a in GIT_LOG_ALLEEN_GELIJK), None)
    if kaal is not None:
        return f"`{kaal}` alleen als `{kaal}=<waarde>`"
    geparsed = _parse(rest, GIT_LOG_FLAGS, GIT_LOG_VAL, dd_ok=True)
    if isinstance(geparsed, str):
        return geparsed
    for naam, waarde in geparsed.vals:
        if naam == "-n" and not waarde.isdigit():
            return "`-n` accepteert alleen een getal"
        if naam in {"--format", "--pretty"} and "%G" in waarde:
            return f"`{naam}` met `%G` start gpg"
    return _ref_reden(geparsed.pos + geparsed.after)


def _check_pytest(args: list[str]) -> str | None:
    if args[:2] != ["run", "pytest"]:
        return "uv: alleen `uv run pytest`, zonder opties tussen `run` en `pytest`"
    geparsed = _parse(args[2:], PYTEST_FLAGS, set())
    if isinstance(geparsed, str):
        return geparsed
    if not geparsed.pos:
        return "uv run pytest zonder pad zou de hele suite draaien"
    for tok in geparsed.pos:
        pad = posixpath.normpath(tok.split("::", 1)[0])
        if not pad.startswith(PYTEST_PAD):
            return f"pad `{tok}` valt niet onder `{PYTEST_PAD}`"
    return None


def _graphql_veld(args: list[str]) -> tuple[list[str], str] | str:
    """Haal het ene `-f query=…` uit `args`; `(rest, veld)` of een reden."""
    velden: list[str] = []
    rest: list[str] = []
    i = 0
    while i < len(args):
        if args[i] != "-f":
            rest.append(args[i])
            i += 1
        elif i + 1 < len(args):
            velden.append(args[i + 1])
            i += 2
        else:
            return "`-f` mist een waarde"
    if len(velden) != 1:
        return "gh api graphql: precies één `-f query=…`"
    if not velden[0].startswith("query="):
        return "gh api graphql: de `-f`-waarde moet met `query=` beginnen"
    if "mutation" in velden[0].lower():
        return "gh api graphql: een mutation is niet toegestaan"
    return rest, velden[0]


def _check_gh_api(args: list[str]) -> str | None:
    # `-f` is alleen voor graphql; zonder `graphql` weigert `_parse` het.
    graphql = "graphql" in args
    if graphql:
        veld = _graphql_veld(args)
        if isinstance(veld, str):
            return veld
        args = veld[0]
    geparsed = _parse(args, set(), GH_API_VAL)
    if isinstance(geparsed, str):
        return geparsed
    if len(geparsed.pos) != 1:
        return "gh api: precies één pad of `graphql`"
    pad = geparsed.pos[0]
    if graphql and pad != "graphql":
        return "gh api graphql: `graphql` moet het enige pad zijn"
    if "://" in pad or pad.startswith("//"):
        return "gh api: alleen een pad, geen URL"
    return _jq_reden(geparsed.vals)


def _check_gh_view(args: list[str]) -> str | None:
    geparsed = _parse(args, GH_VIEW_FLAGS, GH_VIEW_VAL)
    if isinstance(geparsed, str):
        return geparsed
    # Zonder nummer leest gh de branch van de cwd. Ingang C meet in een detached
    # worktree op `origin/main`: daar is geen branch, en de rij faalt na de merge.
    if len(geparsed.pos) != 1:
        return "gh view: precies één issue- of PR-nummer (of github.com-URL)"
    for tok in geparsed.pos:
        if "://" in tok and not tok.startswith("https://github.com/"):
            return "gh view: alleen een github.com-URL"
    return _jq_reden(geparsed.vals)


def _check_gh(args: list[str]) -> str | None:
    if args[:1] == ["api"]:
        return _check_gh_api(args[1:])
    if args[:1] in (["issue"], ["pr"]) and args[1:2] == ["view"]:
        return _check_gh_view(args[2:])
    return "gh: alleen `gh api`, `gh issue view` en `gh pr view`"


def check(argv: list[str]) -> str | None:
    """`None` = toegestaan, anders de reden. `argv[0]` is de kale naam, geen pad of toewijzing."""
    if not argv:
        return "leeg commando"
    naam, args = argv[0], argv[1:]
    if naam == "grep":
        return _check_grep(args)
    if naam == "git":
        return _check_git(args)
    if naam == "ls":
        return _flags_alleen(args, LS_FLAGS)
    if naam == "wc":
        return _flags_alleen(args, WC_FLAGS)
    if naam == "cat":
        return _flags_alleen(args, CAT_FLAGS)
    if naam == "test":
        return None
    if naam == "uv":
        return _check_pytest(args)
    if naam == "gh":
        return _check_gh(args)
    return f"`{naam}` staat niet in de allowlist"


def _flags_alleen(args: list[str], flags: set[str]) -> str | None:
    geparsed = _parse(args, flags, set())
    return geparsed if isinstance(geparsed, str) else None


def _strip_backticks(cel: str) -> str:
    s = cel.strip()
    if len(s) >= 2 and s.startswith("`") and s.endswith("`"):
        s = s[1:-1]
    return s


def expected(result: str) -> tuple[str, bool]:
    """De Result-cel genormaliseerd: `(verwacht fragment, was_na_pr)`."""
    s = _strip_backticks(result).replace("\\|", "|")
    if s.startswith(NA_PR):
        return s[len(NA_PR) :], True
    return s, False


def _stop(proc: subprocess.Popen[bytes]) -> None:
    """Dood het proces en zijn procesgroep (`uv run pytest` start kinderen) en ruim op."""
    try:
        os.killpg(proc.pid, signal.SIGKILL)
    except (ProcessLookupError, PermissionError):
        proc.kill()
    proc.wait()
    if proc.stdout is not None:
        proc.stdout.close()


def _draai(argv: list[str], cwd: str, timeout: int) -> tuple[int, str] | str:
    """Draai `argv` (geen shell) met een tijds- en een uitvoergrens.

    Stdout en stderr gaan door één pipe, die we in blokken lezen: `cat /dev/zero`
    mag de host niet volschrijven. Geeft `(returncode, uitvoer)`, of een reden
    (`niet meetbaar`) bij te veel uitvoer; `TimeoutExpired` bij een verlopen
    deadline.
    """
    proc = subprocess.Popen(
        argv,
        cwd=cwd,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        env=dict(os.environ, **_SUB_ENV),
        start_new_session=True,
    )
    assert proc.stdout is not None
    deadline = time.monotonic() + timeout
    buf = bytearray()
    try:
        fd = proc.stdout.fileno()
        while True:
            over = deadline - time.monotonic()
            if over <= 0:
                raise subprocess.TimeoutExpired(argv, timeout)
            if not select.select([fd], [], [], over)[0]:
                continue
            blok = os.read(fd, _LEESBLOK)
            if not blok:
                break
            buf += blok
            if len(buf) > MAX_UITVOER:
                return f"uitvoer groter dan {MAX_UITVOER} bytes"
        proc.wait(timeout=max(deadline - time.monotonic(), 0.001))
    finally:
        _stop(proc)
    return proc.returncode, buf.decode("utf-8", errors="replace")


def _draai_of_reden(argv: list[str], cwd: str, timeout: int) -> tuple[int, str] | str:
    """`_draai`, maar elke reden om niet te kunnen meten komt terug als tekst."""
    try:
        return _draai(argv, cwd, timeout)
    except FileNotFoundError as fout:
        return f"niet gevonden: {fout.filename or argv[0]}"
    except subprocess.TimeoutExpired:
        return f"timeout na {timeout} s"
    except OSError as fout:
        return f"niet uitvoerbaar: {fout}"


def _rij_argv(row: Row) -> list[str] | str:
    """Het argv van een geldige rij, of de reden waarom de rij `ongeldig` is.

    Eén toets voor vóór en na de PR: een rij die ingang C `ongeldig` noemt
    (buiten de allowlist, of een lege verwachting zoals `na PR: `), mag onder
    `--voor-pr` niet als `na PR, niet gemeten` doorgaan.
    """
    argv = split_command(row.command)
    if isinstance(argv, str):
        return argv
    reden = check(argv)
    if reden is None and not expected(row.result)[0]:
        reden = "lege Result-cel"
    return argv if reden is None else reden


def run_row(row: Row, cwd: str, gh_ok: bool, timeout: int = 300) -> tuple[str, str]:
    """Voer één Evidence-rij uit; `(uitkomst, detail)`.

    Uitkomst is `klopt`, `klopt niet`, `ongeldig` (buiten de allowlist, draait
    niet) of `niet meetbaar` (binary weg, timeout, `gh` onbereikbaar).
    """
    argv = _rij_argv(row)
    if isinstance(argv, str):
        return "ongeldig", f"Evidence buiten allowlist: {argv}"
    fragment, _ = expected(row.result)
    if argv[0] == "gh" and not gh_ok:
        return "niet meetbaar", "gh niet bereikbaar"
    gelezen = _draai_of_reden(argv, cwd, timeout)
    if isinstance(gelezen, str):
        return "niet meetbaar", gelezen
    returncode, uitvoer = gelezen
    delen: list[str] = []
    if returncode != 0:
        delen.append(f"exitcode {returncode}")
    if fragment not in uitvoer:
        delen.append(f"ontbreekt in uitvoer: {fragment!r}")
    if delen:
        return "klopt niet", "; ".join(delen)
    return "klopt", f"exitcode 0, `{fragment}` gevonden"


def _gh_bereikbaar() -> bool:
    try:
        proc = subprocess.run(
            ["gh", "api", "rate_limit", "--jq", ".rate.remaining"],
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
            timeout=GH_PROBE_TIMEOUT_S,
            env=dict(os.environ, **_SUB_ENV),
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        return False
    return proc.returncode == 0


def _cel(tekst: str) -> str:
    return " ".join(tekst.split()).replace("|", "\\|")


def _draait_gh(row: Row, voor_pr: bool) -> bool:
    """Telt deze rij mee voor de `gh`-probe: geldig, een `gh`-rij en niet overgeslagen."""
    argv = _rij_argv(row)
    if isinstance(argv, str) or argv[0] != "gh":
        return False
    return not (voor_pr and expected(row.result)[1])


def _cmd_run(args: argparse.Namespace) -> int:
    try:
        with open(args.body_file, encoding="utf-8") as f:
            body = f.read()
    except OSError as fout:
        print(f"KON NIET CONTROLEREN: body-bestand onleesbaar ({fout})")
        return 3
    if not os.path.isdir(args.cwd):
        print(f"KON NIET CONTROLEREN: --cwd {args.cwd} is geen map")
        return 3
    rijen = evidence_rows(body, args.issue)
    if isinstance(rijen, str):
        print(f"Closes zonder bewijsblok: {rijen}")
        return 2
    gh_ok = any(_draait_gh(r, args.voor_pr) for r in rijen) and _gh_bereikbaar()
    resultaten: list[tuple[Row, str, str]] = []
    for rij in rijen:
        if args.voor_pr and expected(rij.result)[1]:
            argv = _rij_argv(rij)
            if isinstance(argv, str):
                resultaten.append(
                    (rij, "ongeldig", f"Evidence buiten allowlist: {argv}")
                )
            else:
                resultaten.append((rij, VOOR_PR_UITKOMST, "draait pas na de PR"))
            continue
        uitkomst, detail = run_row(rij, args.cwd, gh_ok)
        resultaten.append((rij, uitkomst, detail))
    print("| AC | Uitkomst | Detail |")
    print("|---|---|---|")
    for rij, uitkomst, detail in resultaten:
        print(f"| {_cel(rij.ac)} | {uitkomst} | {_cel(detail)} |")
    uitkomsten = {u for _, u, _ in resultaten}
    if uitkomsten & {"klopt niet", "ongeldig"}:
        return 1
    if "niet meetbaar" in uitkomsten:
        return 4
    return 0


def _cmd_check(args: argparse.Namespace) -> int:
    argv = split_command(args.cell)
    reden = argv if isinstance(argv, str) else check(argv)
    if reden is None:
        return 0
    print(reden)
    return 1


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="cmd", required=True)
    p_check = sub.add_parser("check", help="toets één Command-cel tegen de allowlist")
    p_check.add_argument("cell")
    p_check.set_defaults(func=_cmd_check)
    p_run = sub.add_parser("run", help="voer de Evidence-rijen van een PR-body uit")
    p_run.add_argument("--issue", type=int, required=True)
    p_run.add_argument("--body-file", required=True)
    p_run.add_argument("--cwd", required=True)
    p_run.add_argument("--voor-pr", action="store_true")
    p_run.set_defaults(func=_cmd_run)
    args = parser.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())

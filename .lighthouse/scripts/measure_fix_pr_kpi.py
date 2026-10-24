#!/usr/bin/env python3
# lighthouse: core@dev
"""KPI-meting voor de skill lighthouse-fix-pr.

WAT DIT MEET
------------
De aanloop bij een rode of conflicterende PR. Gemeten op 2026-08-29, na de fix van de
`git branch`/`git reset`/`git switch`/`git stash`/heredoc-classificatie hieronder:

    tool-calls voor de eerste muterende call   n=9    mediaan 4    (min 2, max 8)
    read-only gh/git-diagnosecalls daarvoor    n=10   mediaan 3,0

`n` groeit met de transcriptmap: elke sessie sinds de vorige meting telt mee, dus een latere
herhaling van dit commando leest een hoger `n`. De mediaan zelf staat niet vast -- alleen de
regex-classificatie hierboven is gefixeerd, het getal is een meting, geen doel om naar toe te
tunen. Het issue schatte 5-7. Doel <= 2, wat drie van de negen beurten al haalden.

WAAROM "MUTEREND" EN NIET "Write/Edit"
--------------------------------------
In 6 van de 8 beurten kwam er geen enkele Write/Edit voorbij: het fixwerk liep via `git rebase`,
`sed -i` en heredoc-Python in Bash. Een telling op Write/Edit zou hier 6 van de 8 gevallen als
"geen route" wegschrijven -- precies de blinde vlek die `worktree-first` in
measure_start_issue_kpi.py expliciet benoemt. Daarom telt `leadin` tot de eerste call die de boom
verandert, waarbij een Bash-commando op patroon muterend heet.

WAT DIT NIET KAN
----------------
De promptherkenning ziet het verschil tussen een issue- en een PR-nummer niet. "fix 596" telt mee
of 596 nu een issue of een PR was; in de gemeten acht was het steeds een PR, maar de metriek kan
dat niet garanderen. Ruimer maken zou hem onvergelijkbaar maken met de nulmeting.

Het patroon MUTATING_BASH_RE is een benadering. Een fix via een tool dat hier niet in staat telt
als "geen mutatie" en valt uit de noemer -- de kant die de score nooit gunstiger maakt.

`mistrigger` telt een richting: de skill laadde zonder fixverzoek in de prompt. De omgekeerde fout
-- een fixverzoek waarbij de skill NIET laadde -- wordt indirect zichtbaar in `leadin`, want zo'n
beurt houdt de oude mediaan.

Gebruik:
    python3 .lighthouse/scripts/measure_fix_pr_kpi.py            # de hele tabel
    python3 .lighthouse/scripts/measure_fix_pr_kpi.py leadin --since 2026-08-30

Output keys are fixed English tokens.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

# De transcript-parseerlaag staat al in measure_start_issue_kpi.py en is daar getest. Hem hier
# kopieren zou twee definities van "een beurt" opleveren die stil uiteen kunnen lopen. Hij
# wordt geladen als buur in de eigen map, niet via een repo-root.
sys.path.insert(0, str(Path(__file__).resolve().parent))
import measure_start_issue_kpi as tx  # noqa: E402

SKILL_NAME = "lighthouse-fix-pr"

# De vijf gemeten vormen: `fix pr 585`, `fix 595`, `627 pr heeft errors`,
# `612 heeft conflicts, fix die`, `kun je conflict 408 goed oplossen?`.
# Bewust smal: een prompt die een PR alleen NOEMT ("pr 639 is gemerged") is geen fixverzoek.
FIXPR_RE = re.compile(
    r"""
      (?:fix|repareer|debug|herstel)\s+(?:de\s+)?(?:pr\s*)?\#?(?P<a>\d{2,4})\b
    | \#?(?P<b>\d{2,4})\s+(?:pr\s+)?(?:heeft|is)\s+(?:een\s+)?
      (?:conflict|conflicts|conflictje|error|errors|fout|rood)
    | conflict(?:en|je)?\s+(?:van\s+|in\s+|op\s+)?\#?(?P<c>\d{2,4})
    """,
    re.IGNORECASE | re.VERBOSE,
)

# Alles wat de werkboom, de branch of de PR verandert. `gh pr view/checks/diff` staat er bewust
# NIET in; dat is de diagnose die deze skill juist wil samenvatten.
#
# `git branch` staat hier alleen met een delete/rename/copy-vlag (-d/-D/-m/-M/-c/-C en hun lange
# vorm). De kale aanmaakvorm `git branch <naam>` is ambigu tegen leesvormen als
# `git branch --merged main` -- zonder vlag valt met een woordgrens-match niet te onderscheiden of
# "main" een doelbranch is of een filterargument. Die vorm blijft daarom bij DIAGNOSTIC_BASH_RE
# staan (net als de kale `git branch`, `-a`, `-r`, `-v`, `--list`, `--show-current`), wat de
# diagnose-teller iets te gunstig maakt voor die ene vorm -- de kant die het issue toestaat, zie de
# noot onderaan de moduledocstring.
#
# De heredoc-tak (`<<(?!<) ... `) matcht `<<EOF`, `<<-EOF`, `<<'PATCH'` en `<<"SCRIPT"` -- een
# optionele `-`-indent, een optionele quote, dan een delimiter die met een hoofdletter of
# underscore begint. Die laatste eis staat expliciet in een `(?-i:...)`-groep: de rest van dit
# patroon staat onder `re.IGNORECASE` (voor `git`/`gh`/`sed`), maar zonder die lokale schakelaar
# zou een heredoc met een kleine-letter-delimiter óók matchen, en erger, `echo $((x << y))`
# (bit-shift op kleine-letter-variabelen) zou dan als mutatie tellen. `(?!<)` sluit bovendien de
# here-string `<<<` uit (`cat <<< 'tekst'`) -- die verandert niets, en zonder die lookahead zou een
# hoofdletter-quote erna (`<<< 'HELLO'`) alsnog matchen.
MUTATING_BASH_RE = re.compile(
    r"git\s+(rebase|merge|cherry-pick|add|commit|push|checkout|restore|worktree\s+add"
    r"|reset|switch)\b"
    r"|git\s+branch\s+(?:\S+\s+)*(?:-[dDmMcC]|--delete|--move|--copy)\b"
    r"|git\s+stash\s+(?:pop|apply)\b"
    r"|sed\s+-i"
    r"|<<(?!<)-?\s*(?-i:['\"]?[A-Z_][A-Z0-9_]*['\"]?)"
    r"|gh\s+(run\s+rerun|pr\s+merge|pr\s+edit)\b",
    re.IGNORECASE,
)

DIAGNOSTIC_BASH_RE = re.compile(
    r"gh\s+pr\s+(view|checks|diff)\b"
    r"|gh\s+run\s+(view|list)\b"
    r"|git\s+stash\s+(?:list|show)\b"
    r"|git\s+(worktree\s+list|status|log|branch|diff|show|fetch|rev-parse|merge-tree)\b",
    re.IGNORECASE,
)

WRITE_TOOLS = frozenset({"Write", "Edit", "NotebookEdit"})

# De twee handelingen die per AC 7 NA deze skill horen, niet erin. Een van beide in dezelfde beurt
# betekent dat de skill doorliep voorbij "de PR is groen".
SCOPE_SKILLS = frozenset({"lighthouse-close-issue", "caveman:caveman-review"})


def fix_pr_number(text: str) -> int | None:
    match = FIXPR_RE.search(text)
    if not match:
        return None
    return int(match.group("a") or match.group("b") or match.group("c"))


def is_mutating(name: str, payload: dict) -> bool:
    if name in WRITE_TOOLS:
        return True
    if name != "Bash":
        return False
    return bool(MUTATING_BASH_RE.search(str(payload.get("command", ""))))


def is_diagnostic(name: str, payload: dict) -> bool:
    if name != "Bash":
        return False
    command = str(payload.get("command", ""))
    if MUTATING_BASH_RE.search(command):
        return False
    return bool(DIAGNOSTIC_BASH_RE.search(command))


def count_leadin(turn: tx.Turn) -> int | None:
    """Tool-calls vóór de eerste muterende call, of None als de beurt nooit muteerde.

    Exclusief: de muterende call zelf is de fix, niet de aanloop ernaartoe. De ambient
    `Skill{skill: lighthouse-fix-pr}` telt niet mee -- die call ís het laden van de skill, en meetellen
    zou de metriek per constructie laten slagen.
    """
    seen = 0
    for record in turn.records:
        for name, payload in tx.tool_uses(record):
            if name == "Skill" and payload.get("skill") == SKILL_NAME:
                continue
            if is_mutating(name, payload):
                return seen
            seen += 1
    return None


def count_diagnostics(turn: tx.Turn) -> int:
    """Read-only gh/git-calls tot aan de eerste mutatie (of tot het eind van de beurt)."""
    count = 0
    for record in turn.records:
        for name, payload in tx.tool_uses(record):
            if is_mutating(name, payload):
                return count
            if is_diagnostic(name, payload):
                count += 1
    return count


def overruns_scope(turn: tx.Turn) -> bool:
    """Laadde de skill én daarna een handeling die per AC 7 buiten zijn grens valt?"""
    loaded = tx.loaded_skills(turn)
    if SKILL_NAME not in loaded:
        return False
    return any(name in SCOPE_SKILLS for name in loaded)


def iter_fix_turns(directory: Path, since: str | None):
    for path in sorted(directory.glob("*.jsonl")):
        records = [r for r in tx.read_session(path) if tx._after(r, since)]
        for turn in tx.iter_turns(records):
            number = fix_pr_number(turn.prompt)
            if number is not None:
                yield turn, number


def measure_leadin(directory: Path, since: str | None) -> dict[str, object]:
    counts: list[int] = []
    zonder_mutatie = 0
    for turn, _ in iter_fix_turns(directory, since):
        value = count_leadin(turn)
        if value is None:
            zonder_mutatie += 1
        else:
            counts.append(value)
    return {**tx._summary(counts), "without_mutation": zonder_mutatie}


def measure_diagnose(directory: Path, since: str | None) -> dict[str, object]:
    counts = [count_diagnostics(turn) for turn, _ in iter_fix_turns(directory, since)]
    return tx._summary(counts)


def measure_mistrigger(directory: Path, since: str | None) -> dict[str, object]:
    geladen = misgeschoten = 0
    voorbeelden: list[str] = []
    for path in sorted(directory.glob("*.jsonl")):
        records = [r for r in tx.read_session(path) if tx._after(r, since)]
        for turn in tx.iter_turns(records):
            if SKILL_NAME not in tx.loaded_skills(turn):
                continue
            geladen += 1
            if fix_pr_number(turn.prompt) is None:
                misgeschoten += 1
                voorbeelden.append(turn.prompt[:120])
    return {
        "loaded": geladen,
        "without_fix_request": misgeschoten,
        "examples": voorbeelden[:5],
    }


def measure_scope(directory: Path, since: str | None) -> dict[str, object]:
    geladen = buiten = 0
    for path in sorted(directory.glob("*.jsonl")):
        records = [r for r in tx.read_session(path) if tx._after(r, since)]
        for turn in tx.iter_turns(records):
            if SKILL_NAME not in tx.loaded_skills(turn):
                continue
            geladen += 1
            if overruns_scope(turn):
                buiten += 1
    return {"loaded": geladen, "past_the_boundary": buiten}


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Review measurement for the skill lighthouse-fix-pr, from the Claude Code transcripts."
    )
    parser.add_argument(
        "metric",
        nargs="?",
        choices=["leadin", "diagnose", "mistrigger", "scope"],
        help="leave out for the whole table",
    )
    parser.add_argument("--dir", type=Path, help="default: this repo's Claude Code projects dir")
    parser.add_argument("--since", help="YYYY-MM-DD; only records from this day on")
    args = parser.parse_args(argv)

    try:
        if args.dir is None:
            args.dir = tx.default_transcript_dir()
    except tx.ConfigError as exc:
        print(f"measure: {exc}", file=sys.stderr)
        return 2
    if not args.dir.is_dir():
        print(f"transcript directory does not exist: {args.dir}", file=sys.stderr)
        return 2

    rows = {
        "leadin": lambda: measure_leadin(args.dir, args.since),
        "diagnose": lambda: measure_diagnose(args.dir, args.since),
        "mistrigger": lambda: measure_mistrigger(args.dir, args.since),
        "scope": lambda: measure_scope(args.dir, args.since),
    }
    selected = [args.metric] if args.metric else list(rows)
    print(
        json.dumps(
            {name: rows[name]() for name in selected}, indent=2, ensure_ascii=False
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
# lighthouse: core@dev
"""KPI-meting voor de skill lighthouse-start-issue.

WAT DIT MEET
------------
De skill moet de routering doen die nu op de gebruiker neerkomt. De nulmeting liet zien dat de
spreiding vrijwel volledig verklaard wordt door of de gebruiker de route met de hand meegaf:

    prompt noemt zelf al een skill   n=7    mediaan  1 tool-call lead-in
    prompt noemt geen skill          n=11   mediaan 14 tool-calls lead-in

Beide cijfers zijn wat dit script zelf produceert (`leadin`, cohorten "routed" en "unrouted"),
niet de handmatige telling uit het ontwerp — die kwam op n=9 respectievelijk n=12 / mediaan 11 uit.

Het doel <= 2 is dus geen willekeurig getal maar wat een handgerouteerde prompt al haalt.

MEETOPZET
---------
Bron: de sessie-jsonl's die toch al op schijf staan. Geen synthetische testgevallen, geen
eval-harness — dat is expliciet een no-go van milestone #42. Dit script evalueert de skill, het
is geen onderdeel van wat de skill doet.

Een beurt loopt van een mensprompt tot de volgende mensprompt. De lead-in is het aantal
tool_use-blokken VOOR het eerste blok uit ROUTE_TOOLS; die route-call zelf telt niet mee.
Exclusief, want de gemeten nulmeting kent een minimum van 0.

WAT DIT NIET KAN
----------------
`worktree-first` ziet alleen het file_path-argument van Write/Edit. Een bestand dat via een
heredoc of `sed -i` in een Bash-call ontstaat, is onzichtbaar. Het Bash-commando parseren om er
een pad uit te raden geeft stille valse tellingen; de grens is daarom expliciet en staat ook in
de uitvoer. Een score van 100% betekent "van de zichtbare schrijfacties", niet "van alle".

`mistrigger` telt alleen de ene richting: de skill laadde terwijl er geen issuenummer in de prompt
stond. De omgekeerde fout — een pickup waarbij de skill NIET laadde — wordt hier niet gemeten;
die wordt indirect zichtbaar in rij 1, want zo'n beurt houdt de oude mediaan.

`leadin` slaat de `Skill`-call van lighthouse-start-issue zelf over (noch aanloop, noch route-call):
de skill is ambient, dus bij een pickup is `Skill{skill: "lighthouse-start-issue"}` vaak de EERSTE
tool_use van de beurt, vóór er ook maar iets gerouteerd is. Meetellen als route-call zou de
metric per constructie laten slagen (lead-in 0, ongeacht of de routering daadwerkelijk verbeterde).
Dit raakt de nulmeting hierboven niet: die stamt van vóór het bestaan van de skill, dus geen
nulmetingbeurt bevat deze call. `Skill{skill: "lighthouse-route-issue"}` wordt op dezelfde grond
overgeslagen: sinds de split (#1115) roept start-issue die aan voor de poorten, en de melding
komt pas ná die call.

Dezelfde uitsluiting geldt voor de verplichte scoutcall op 4b/4c
(`Agent{subagent_type: issue-impact-scout}`, issue #927): die call is de voorverkenning zelf,
niet de aanloop ernaartoe. Zonder deze uitsluiting krijgt elke correcte 4b/4c-pickup +1 op deze
rij, en leest dat als een routingregressie terwijl de routering niet trager werd.

`gate` telt of de skill de gekozen triage-poort meldde. Hij kan niet zien of die poort de JUISTE
was — dat is een oordeel, geen patroon. Wat hij wel levert is de verdeling, en die maakt de
omgekeerde vraag beantwoordbaar: blijven poort 1 en 2 op nul, dan is de poortenreeks dood hout.

Gebruik:
    python3 .lighthouse/scripts/measure_start_issue_kpi.py           # de hele tabel
    python3 .lighthouse/scripts/measure_start_issue_kpi.py leadin --since 2026-08-24

Output keys are fixed English tokens.
"""

from __future__ import annotations

import argparse
import functools
import json
import re
import statistics
import subprocess
import sys
from collections.abc import Iterable, Iterator
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path

# Run as a file (`python3 .lighthouse/scripts/<name>.py`): put `.lighthouse/lib` first, so
# `lighthouse_lib` imports from this installation.
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

from lighthouse_lib import config, headings  # noqa: E402
from lighthouse_lib.config import ConfigError  # noqa: E402


def default_transcript_dir() -> Path:
    """The Claude Code project directory of this repo: `~/.claude/projects/<root>`, where
    `<root>` is the repo root with every character outside `[A-Za-z0-9]` replaced by `-`."""
    root = str(config.repo_root().resolve())
    return Path.home() / ".claude" / "projects" / re.sub(r"[^A-Za-z0-9]", "-", root)


SKILL_NAME = "lighthouse-start-issue"
# Sinds #1115 staan de poorten in een eigen read-only skill die start-issue aanroept. Die call is
# het routeren zelf, net als de ambient start-skill: `count_leadin` en `text_before_route` slaan
# beide over. `measure_gate` en de andere noemers blijven op SKILL_NAME filteren.
ROUTE_SKILL_NAME = "lighthouse-route-issue"
ROUTING_SKILLS = frozenset({SKILL_NAME, ROUTE_SKILL_NAME})

# Stap 3 van de skill zet de worktree vóór de route; deze Skill-call staat dus altijd vóór de
# poortmelding. Alleen `text_before_route` slaat hem over (#937) — `count_leadin` niet, want
# rij 1 telt per definitie tot de eerste Skill-call, en dat houdt de nulmeting vergelijkbaar.
WORKTREE_SKILL_NAME = "superpowers:using-git-worktrees"

# Een sessie die in een worktree start, landt in een sibling-map van de hoofdmap:
# `<hoofdmap>--claude-worktrees-<naam>/`. `session_files` leest die mee (#937).
WORKTREE_DIR_INFIX = "--claude-worktrees-"

# De vier handelingen die tellen als "er is een route gekozen". Precies deze vier, omdat de
# nulmeting ze gebruikte; uitbreiden maakt de nameting onvergelijkbaar met de nulmeting.
ROUTE_TOOLS = frozenset({"Skill", "Write", "Edit", "AskUserQuestion"})
WRITE_TOOLS = frozenset({"Write", "Edit"})

WORKTREE_MARKER = ".claude/worktrees/"


def correct_doc_dirs() -> tuple[str, ...]:
    """De spec- en planmap van het project (`[paths] specs`, `plans`), elk met een `/`."""
    cfg = config.load(config.config_path())
    return tuple(
        str(config.lookup(cfg, key)).rstrip("/") + "/" for key in ("paths.specs", "paths.plans")
    )


# De defaultmappen van Superpowers, waar een spec of plan landt als niemand de conventie volgt.
_LEGACY_ROOT = "docs/" + "superpowers/"
LEGACY_DOC_DIRS = (_LEGACY_ROOT + "specs/", _LEGACY_ROOT + "plans/")

# `paths` telt toegevoegde bestanden over de git-historie; zonder ondergrens telt hij dus ook de
# periode waarin de conventie nog niet bestond. Er is geen vaste ondergrens: geef de dag waarop
# het project de conventie invoerde mee met --since.

# "pak 610 op", "begin aan issue 526", "kijk naar 623", "ga verder met #663"; in English
# "pick up 610", "pick 610 up", "start on issue 526", "work on #663", "look at 623".
# Bewust smal: een prompt die een nummer alleen NOEMT ("wat staat er in 610?") is geen pickup.
PICKUP_RE = re.compile(
    r"""
      (?:pak|pik)\s+(?:issue\s*)?\#?(?P<a>\d{2,4})\s+op
    | (?:begin(?:nen)?|start(?:en)?|ga\s+verder|ga\s+aan\s+de\s+slag|werk)\s+
      (?:aan|met)\s+(?:issue\s*)?\#?(?P<b>\d{2,4})
    | kijk\s+(?:eens\s+)?naar\s+(?:issue\s*)?\#?(?P<c>\d{2,4})
    | pick\s+up\s+(?:issue\s*)?\#?(?P<d>\d{2,4})
    | pick\s+(?:issue\s*)?\#?(?P<e>\d{2,4})\s+up
    | (?:start(?:ing)?|continue|get\s+started|work(?:ing)?)\s+
      (?:on|with)\s+(?:issue\s*)?\#?(?P<f>\d{2,4})
    | look\s+at\s+(?:issue\s*)?\#?(?P<g>\d{2,4})
    """,
    re.IGNORECASE | re.VERBOSE,
)

# Een slash-commando, maar geen absoluut pad: `/usr/local/...` wordt door de negatieve
# lookahead op '/' uitgesloten.
SLASH_SKILL_RE = re.compile(r"(?<![\w/])/([a-z][a-z0-9-]*(?::[a-z0-9-]+)?)(?![\w/])")

# Ruim: elk getal van 2-4 cijfers, met of zonder '#'. Conservatief de goede kant op — een ruime
# match levert MINDER gerapporteerde mis-triggers, dus dit getal overdrijft het probleem nooit.
ISSUE_NUMBER_RE = re.compile(r"(?<![\w.])#?\d{2,4}(?![\w.])")


def _flexible(key: str) -> str:
    """Every variant of heading `key` as a regex group; a space matches any whitespace."""
    return "(?:" + "|".join(
        re.escape(v).replace("\\ ", "\\s*") for v in headings.variants(key)
    ) + ")"


# De vaste vorm waarin de skill zijn triage-uitkomst meldt: `Poort 2: systematic-debugging`
# (`heading:gate_line`, elke taal). Vijf poorten (0 t/m 4); een hoger cijfer is per definitie
# geen poortmelding. Poort 4 draagt verplicht een subletter (`Poort 4b: writing-plans`); zonder
# `[abc]?` valt er tussen `4` en `b` geen woordgrens en telt precies de voorgeschreven vorm als
# "niet gemeld".
@functools.cache
def gate_re() -> re.Pattern[str]:
    return re.compile(rf"\b{_flexible('gate_line')}\s*(?P<gate>[0-4])[abc]?\b")


SCOUT_AGENT = "lighthouse-issue-impact-scout"

# De trigger wordt structureel gelezen uit het Agent-blok (`_scout_calls`), niet uit deze regex:
# de meldregel `Scout: issue-impact-scout` uit SKILL.md wordt door niets in dit script gemeten.
# `scout_skip_re` hieronder wordt wél gebruikt — dat contract is `heading:scout_skipped` in
# `.lighthouse/headings.toml`, dezelfde kop die de skill schrijft.
@functools.cache
def scout_skip_re() -> re.Pattern[str]:
    return re.compile(
        rf"^{_flexible('scout_skipped')}\s*—\s*(?P<reden>.+?)\s*$", re.MULTILINE
    )


# Een reden van één teken (of alleen een punt) is geen reden. Twintig tekens is geen diepe
# waarheid; het is de ondergrens waaronder "duidelijk." en "simpel." niet meer doorglippen.
SCOUT_MIN_REASON = 20

# Het zoekbudget uit de persona (`.claude/agents/lighthouse-issue-impact-scout.md`, norm
# "hoogstens 35 zoekacties"). Het staat hier omdat `maxTurns` iets anders begrenst — beurten,
# niet calls — en een grens die niemand telt is decoratie. tests/pack/test_issue_impact_scout.py
# pint dat dit getal en de persona hetzelfde zeggen.
SCOUT_TOOL_CALL_BUDGET = 35

SCOUT_GATES = frozenset({"4b", "4c"})


@functools.cache
def gate_suffix_re() -> re.Pattern[str]:
    return re.compile(rf"\b{_flexible('gate_line')}\s*(?P<gate>[0-4])\s*(?P<sub>[abc])?\b")


@dataclass(frozen=True)
class Turn:
    """Een mensprompt en alles wat erop volgde tot de volgende mensprompt."""

    prompt: str
    records: tuple[dict, ...]


# --- recordherkenning -------------------------------------------------------------------------


def prompt_text(record: dict) -> str:
    """De tekstblokken van een record, samengevoegd. Lege string als er geen tekst in zit."""
    content = record.get("message", {}).get("content")
    if isinstance(content, str):
        return content
    if not isinstance(content, list):
        return ""
    return "\n".join(
        block.get("text", "")
        for block in content
        if isinstance(block, dict) and block.get("type") == "text"
    )


def is_user_prompt(record: dict) -> bool:
    """Een echte mensprompt.

    Drie uitsluitingen die er allemaal toe doen: een tool_result heeft dezelfde `type: user`
    (zonder deze check is elke lead-in 0), `isSidechain` is subagent-verkeer, en `isMeta` zijn
    harness-injecties zoals hook-context.
    """
    if record.get("type") != "user":
        return False
    if record.get("isSidechain") or record.get("isMeta"):
        return False
    if record.get("userType") not in (None, "external"):
        return False
    return bool(prompt_text(record).strip())


def tool_uses(record: dict) -> list[tuple[str, dict]]:
    """De (naam, input)-paren van elk tool_use-blok in een assistant-record."""
    if record.get("type") != "assistant" or record.get("isSidechain"):
        return []
    content = record.get("message", {}).get("content")
    if not isinstance(content, list):
        return []
    return [
        (block.get("name", ""), block.get("input") or {})
        for block in content
        if isinstance(block, dict) and block.get("type") == "tool_use"
    ]


def _content_blocks(record: dict) -> list[dict]:
    content = (record.get("message") or {}).get("content")
    return (
        [b for b in content if isinstance(b, dict)] if isinstance(content, list) else []
    )


def iter_turns(records: Iterable[dict]) -> Iterator[Turn]:
    prompt: str | None = None
    buffer: list[dict] = []
    for record in records:
        if is_user_prompt(record):
            if prompt is not None:
                yield Turn(prompt, tuple(buffer))
            prompt, buffer = prompt_text(record), []
        elif prompt is not None:
            buffer.append(record)
    if prompt is not None:
        yield Turn(prompt, tuple(buffer))


# --- prompt-classificatie ---------------------------------------------------------------------


def pickup_issue_number(text: str) -> int | None:
    match = PICKUP_RE.search(text)
    if not match:
        return None
    return int(next(number for number in match.groups() if number))


def names_a_skill(text: str) -> bool:
    return bool(SLASH_SKILL_RE.search(text))


def has_issue_number(text: str) -> bool:
    return bool(ISSUE_NUMBER_RE.search(text))


# --- metingen per beurt -----------------------------------------------------------------------


def count_leadin(turn: Turn) -> int | None:
    """Tool-calls vóór de eerste route-call, of None als er geen route gekozen werd."""
    seen = 0
    for record in turn.records:
        for name, payload in tool_uses(record):
            if name == "Skill" and payload.get("skill") in ROUTING_SKILLS:
                # De skill die zichzelf ambient laadt, en de poortskill die hij aanroept, zijn
                # het routeren zelf: geen aanloop ervoor én geen route-call om te tellen.
                continue
            if name == "Agent" and payload.get("subagent_type") == SCOUT_AGENT:
                # Zelfde argument: de verplichte scoutcall op 4b/4c is de voorverkenning zelf,
                # geen aanloop ernaartoe (zie de moduledocstring hierboven, issue #927 fix 2).
                continue
            if name in ROUTE_TOOLS:
                return seen
            seen += 1
    return None


def first_write_path(turn: Turn) -> str | None:
    for record in turn.records:
        for name, payload in tool_uses(record):
            if name in WRITE_TOOLS:
                path = payload.get("file_path")
                if isinstance(path, str) and path:
                    return path
    return None


def is_worktree_path(path: str) -> bool:
    return WORKTREE_MARKER in path


def text_before_route(turn: Turn) -> str:
    """Assistant-tekst tot en met het bericht waarin de eerste route-call zit.

    De poortmelding hoort vóór het doorroeren te staan. Tekst en tool_use kunnen in hetzelfde
    assistant-bericht zitten — de tekstblokken staan er dan vóór — dus de grens ligt op het
    tool_use-blok zelf, niet op het bericht. De ambient `Skill{skill: lighthouse-start-issue}` wordt
    overgeslagen op dezelfde grond als in `count_leadin`: die call ís het laden van de skill, niet
    het routeren erna. `Skill{skill: lighthouse-route-issue}` evenzo: die call ís het routeren, en de
    poortmelding volgt er pas op. `Skill{skill: superpowers:using-git-worktrees}` ook: stap 3 zet
    de worktree vóór de route, dus als grens slikte hij elke poortmelding (#937). Vindt de skill geen route (poort 0 stopt), dan is er geen grens en telt de
    hele beurt.
    """
    parts: list[str] = []
    for record in turn.records:
        if record.get("type") != "assistant" or record.get("isSidechain"):
            continue
        content = record.get("message", {}).get("content")
        if not isinstance(content, list):
            continue
        for block in content:
            if not isinstance(block, dict):
                continue
            if block.get("type") == "text":
                parts.append(block.get("text", ""))
            elif block.get("type") == "tool_use":
                payload = block.get("input") or {}
                if block.get("name") == "Skill" and (
                    payload.get("skill") in ROUTING_SKILLS
                    or payload.get("skill") == WORKTREE_SKILL_NAME
                ):
                    continue
                if block.get("name") in ROUTE_TOOLS:
                    return "\n".join(parts)
    return "\n".join(parts)


def announced_gate(turn: Turn) -> int | None:
    """Het poortnummer dat de skill meldde, of None als hij stil routeerde."""
    match = gate_re().search(text_before_route(turn))
    return int(match.group("gate")) if match else None


def loaded_skills(turn: Turn) -> list[str]:
    return [
        payload["skill"]
        for record in turn.records
        for name, payload in tool_uses(record)
        if name == "Skill" and isinstance(payload.get("skill"), str)
    ]


# --- inlezen ----------------------------------------------------------------------------------


def read_session(path: Path) -> list[dict]:
    """Eén sessiebestand. Kapotte regels worden overgeslagen, niet gerepareerd."""
    records: list[dict] = []
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            records.append(json.loads(line))
        except json.JSONDecodeError:
            continue
    return records


def session_files(directory: Path) -> list[Path]:
    """De sessiebestanden van de hoofdmap plus die van elke worktree-sibling.

    Zonder de siblings mist elke meting de sessies die in een worktree begonnen — controller-runs
    en vensters die na een handoff in de worktree openen. De scout-rij zag daardoor 0 van ruim
    dertig echte runs (#937).
    """
    siblings = directory.parent.glob(f"{directory.name}{WORKTREE_DIR_INFIX}*")
    return sorted(
        path
        for folder in (directory, *siblings)
        if folder.is_dir()
        for path in folder.glob("*.jsonl")
    )


def _after(record: dict, since: str | None) -> bool:
    stamp = record.get("timestamp")
    return not since or not isinstance(stamp, str) or stamp[:10] >= since


def iter_pickup_turns(directory: Path, since: str | None) -> Iterator[tuple[Turn, int]]:
    """Elke beurt die met een pickup-prompt begint, met het issuenummer erbij."""
    for path in session_files(directory):
        records = [r for r in read_session(path) if _after(r, since)]
        for turn in iter_turns(records):
            number = pickup_issue_number(turn.prompt)
            if number is not None:
                yield turn, number


# --- de vijf rijen ----------------------------------------------------------------------------


def measure_leadin(directory: Path, since: str | None) -> dict[str, object]:
    routed: list[int] = []
    unrouted: list[int] = []
    no_route = 0
    for turn, _ in iter_pickup_turns(directory, since):
        count = count_leadin(turn)
        if count is None:
            no_route += 1
            continue
        (routed if names_a_skill(turn.prompt) else unrouted).append(count)
    return {
        "routed": _summary(routed),
        "unrouted": _summary(unrouted),
        "no_route": no_route,
    }


def _summary(values: list[int]) -> dict[str, object]:
    if not values:
        return {"n": 0}
    return {
        "n": len(values),
        "min": min(values),
        "median": statistics.median(values),
        "max": max(values),
        "mean": round(statistics.fmean(values), 1),
    }


def measure_worktree_first(directory: Path, since: str | None) -> dict[str, object]:
    inside = outside = invisible = 0
    for turn, _ in iter_pickup_turns(directory, since):
        path = first_write_path(turn)
        if path is None:
            invisible += 1
        elif is_worktree_path(path):
            inside += 1
        else:
            outside += 1
    zichtbaar = inside + outside
    return {
        "in_worktree": inside,
        "outside_worktree": outside,
        "no_visible_write": invisible,
        "share": round(inside / zichtbaar, 3) if zichtbaar else None,
        "note": "Write/Edit only; Bash writes are invisible",
    }


def measure_mistrigger(directory: Path, since: str | None) -> dict[str, object]:
    loaded = mistriggered = 0
    voorbeelden: list[str] = []
    for path in session_files(directory):
        records = [r for r in read_session(path) if _after(r, since)]
        for turn in iter_turns(records):
            if SKILL_NAME not in loaded_skills(turn):
                continue
            loaded += 1
            if not has_issue_number(turn.prompt):
                mistriggered += 1
                voorbeelden.append(turn.prompt[:120])
    return {
        "loaded": loaded,
        "without_issue_number": mistriggered,
        "examples": voorbeelden[:5],
    }


def measure_gate(directory: Path, since: str | None) -> dict[str, object]:
    """Meldde de skill welke poort hij nam?

    Noemer is bewust smal: alleen pickups waarin de skill daadwerkelijk laadde. Een pickup zonder
    skill is de fout die `leadin` en `mistrigger` meten; die hier meetellen zou twee verschillende
    faalmodi in één percentage persen.

    De verdeling is bijvangst, geen doel: hij laat zien of poort 1 en 2 überhaupt vuren. Blijven ze
    op nul terwijl er wel bug- en recept-issues langskwamen, dan is de poortenreeks dood hout.
    """
    gemeld = niet_gemeld = zonder_skill = 0
    verdeling: dict[str, int] = {}
    for turn, _ in iter_pickup_turns(directory, since):
        if SKILL_NAME not in loaded_skills(turn):
            zonder_skill += 1
            continue
        gate = announced_gate(turn)
        if gate is None:
            niet_gemeld += 1
            continue
        gemeld += 1
        key = f"gate_{gate}"
        verdeling[key] = verdeling.get(key, 0) + 1
    totaal = gemeld + niet_gemeld
    return {
        "reported": gemeld,
        "not_reported": niet_gemeld,
        "share": round(gemeld / totaal, 3) if totaal else None,
        "distribution": dict(sorted(verdeling.items())),
        "pickup_without_skill": zonder_skill,
    }


def _scout_calls(turn: Turn) -> list[tuple[dict, dict | None]]:
    """Elke Agent-call naar de scout, met het bijbehorende tool_result of None."""
    calls: list[tuple[str, dict]] = []
    for record in turn.records:
        for block in _content_blocks(record):
            if block.get("type") != "tool_use" or block.get("name") != "Agent":
                continue
            payload = block.get("input") or {}
            if payload.get("subagent_type") == SCOUT_AGENT:
                calls.append((block.get("id") or "", record))
    results = {
        block.get("tool_use_id"): record
        for record in turn.records
        for block in _content_blocks(record)
        if block.get("type") == "tool_result"
    }
    return [(record, results.get(uid)) for uid, record in calls]


def _seconds_between(start: dict, end: dict) -> float | None:
    a, b = start.get("timestamp"), end.get("timestamp")
    if not isinstance(a, str) or not isinstance(b, str):
        return None
    try:
        t0 = datetime.fromisoformat(a)
        t1 = datetime.fromisoformat(b)
    except ValueError:
        return None
    return (t1 - t0).total_seconds()


def _tool_use_count(end: dict) -> int | None:
    """Hoeveel tool-calls de scoutrun deed, uit `toolUseResult.totalToolUseCount`.

    Dit is de meetbare proxy voor het budget. Let op de semantiek: `maxTurns` in de
    agentfrontmatter begrenst **beurten** (tool-use-round-trips), en één beurt kan meerdere
    parallelle calls dragen. Een run met meer tool-calls dan `maxTurns` is dus geen
    overschrijding; het getal zegt hoe breed er gezocht is, niet of een plafond raakte.
    """
    result = end.get("toolUseResult")
    if not isinstance(result, dict):
        return None
    count = result.get("totalToolUseCount")
    return count if isinstance(count, int) else None


def _finished(end: dict) -> bool:
    """Is dit retour het einde van een run?

    Niet bij een achtergrondstart (`status: async_launched`, retour na een paar seconden terwijl
    de run nog loopt) en niet bij een afgewezen call (`toolUseResult` is dan een string). Beide
    trokken de looptijdmediaan omlaag (#937).
    """
    result = end.get("toolUseResult")
    if isinstance(result, str):
        return False
    return not (isinstance(result, dict) and result.get("status") == "async_launched")


def _run_stats(calls: list[tuple[dict, dict | None]]) -> tuple[list[float], list[int]]:
    """Looptijd en tool-call-aantal per afgeronde scoutcall; een call zonder retour telt niet."""
    seconden: list[float] = []
    aantallen: list[int] = []
    for start, end in calls:
        if end is None or not _finished(end):
            continue
        duur = _seconds_between(start, end)
        if duur is not None:
            seconden.append(duur)
        aantal = _tool_use_count(end)
        if aantal is not None:
            aantallen.append(aantal)
    return seconden, aantallen


def announced_subgate(turn: Turn) -> str | None:
    """`4b` of `4c` als de skill de subletter meldde, anders alleen het cijfer als string."""
    match = gate_suffix_re().search(text_before_route(turn))
    if not match:
        return None
    return match.group("gate") + (match.group("sub") or "")


def measure_scout(directory: Path, since: str | None) -> dict[str, object]:
    """Vuurde de scout waar hij hoort, en zweeg hij nergens?

    Noemer is smal, net als bij `gate`: alleen pickups waarin de skill laadde én de gemelde
    poort `4b` of `4c` was. Een pickup zonder skill of zonder gemelde poort is de fout die
    `leadin` en `gate` al meten.

    `mistrigger` staat bewust buiten die noemer: een scoutcall op poort 0, 1, 2 of 4a is geen
    slechte 4b/4c-run, het is een andere fout, en twee faalmodi in één percentage persen maakt
    beide onleesbaar.

    `tool_calls` en `over_budget` meten het zoekbudget uit de persona
    (`SCOUT_TOOL_CALL_BUDGET`), niet `maxTurns`: dat plafond telt beurten, en één beurt draagt
    meerdere parallelle calls. Een run boven het budget is dus een overschrijding van de
    prompt-norm, geen runtimefout.

    Een gemelde poort `4` zonder subletter is **onbekend**, geen mis-trigger: SKILL.md eiste tot
    issue #927 geen subletter bij poort 4, dus zo'n melding kan een correcte 4b/4c-run zijn die
    alleen het cijfer meldde. Zo'n beurt telt niet als mistrigger en niet in de
    noemer (`totaal`) van deze rij. Poort 0/1/2/4a blijven ondubbelzinnig en dus ongemoeid.

    Hetzelfde geldt voor een scoutcall zonder enige poortmelding: stil routeren is geen bewijs
    voor poort 0/1/2/4a (#937). Die runs tellen in `gate_unknown`, zodat ze zichtbaar blijven
    zonder de noemer of `mistrigger` te vervuilen; de poortfout zelf meet rij `gate`.
    """
    getriggerd = met_reden = stil = mistrigger = onbekend = 0
    looptijden: list[float] = []
    tool_calls: list[int] = []
    for turn, _ in iter_pickup_turns(directory, since):
        if SKILL_NAME not in loaded_skills(turn):
            continue
        calls = _scout_calls(turn)
        subgate = announced_subgate(turn)
        if subgate is None or subgate == "4":
            onbekend += 1 if calls else 0
            continue
        if subgate not in SCOUT_GATES:
            mistrigger += len(calls)
            continue
        seconden, aantallen = _run_stats(calls)
        looptijden += seconden
        tool_calls += aantallen
        if calls:
            getriggerd += 1
            continue
        skip = scout_skip_re().search(text_before_route(turn))
        reden = skip.group("reden").strip(" .") if skip else ""
        if len(reden) >= SCOUT_MIN_REASON:
            met_reden += 1
        else:
            stil += 1
    totaal = getriggerd + met_reden + stil
    return {
        "triggered": getriggerd,
        "skipped_with_reason": met_reden,
        "silent": stil,
        "mistrigger": mistrigger,
        "gate_unknown": onbekend,
        "share": round((getriggerd + met_reden) / totaal, 3) if totaal else None,
        "runtime_seconds": _summary_float(looptijden),
        "tool_calls": _summary_int(tool_calls),
        "over_budget": sum(1 for n in tool_calls if n > SCOUT_TOOL_CALL_BUDGET),
    }


def _summary_int(values: list[int]) -> dict[str, object]:
    if not values:
        return {"n": 0, "median": None, "max": None}
    return {
        "n": len(values),
        "median": round(statistics.median(values)),
        "max": max(values),
    }


def _summary_float(values: list[float]) -> dict[str, object]:
    if not values:
        return {"n": 0, "median": None, "max": None}
    return {
        "n": len(values),
        "median": round(statistics.median(values), 1),
        "max": round(max(values), 1),
    }


def measure_paths(since: str | None) -> dict[str, object]:
    """Leest git, geen transcripten: waar landden nieuwe spec- en planbestanden?

    Zonder --since telt de hele historie, ook de periode waarin de conventie nog niet bestond;
    een target van 100% is dan per constructie onhaalbaar. Geef daarom de dag mee waarop de
    conventie begon; `since` in de uitvoer noemt altijd welk venster geteld is.
    """
    argv = [
        "git",
        "-C",
        str(config.repo_root()),
        "log",
        "--diff-filter=A",
        "--name-only",
        "--pretty=format:",
    ]
    if since:
        argv += ["--since", since]
    output = subprocess.run(argv, capture_output=True, text=True, check=True).stdout
    added = [line for line in output.splitlines() if line.strip()]
    correct = [f for f in added if f.startswith(correct_doc_dirs())]
    legacy = [f for f in added if f.startswith(LEGACY_DOC_DIRS)]
    total = len(correct) + len(legacy)
    return {
        "since": since,
        "right_dir": len(correct),
        "superpowers_default_dir": len(legacy),
        "share": round(len(correct) / total, 3) if total else None,
    }


# --- CLI --------------------------------------------------------------------------------------


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Review measurement for the skill lighthouse-start-issue, from the Claude Code transcripts."
    )
    parser.add_argument(
        "metric",
        nargs="?",
        choices=["leadin", "worktree-first", "mistrigger", "paths", "gate", "scout"],
        help="leave out for the whole table",
    )
    parser.add_argument("--dir", type=Path, help="default: this repo's Claude Code projects dir")
    parser.add_argument("--since", help="YYYY-MM-DD; only records from this day on")
    args = parser.parse_args(argv)

    try:
        if args.dir is None:
            args.dir = default_transcript_dir()
    except ConfigError as exc:
        print(f"measure: {exc}", file=sys.stderr)
        return 2
    if not args.dir.is_dir():
        print(f"transcript directory does not exist: {args.dir}", file=sys.stderr)
        return 2

    rows = {
        "leadin": lambda: measure_leadin(args.dir, args.since),
        "worktree-first": lambda: measure_worktree_first(args.dir, args.since),
        "mistrigger": lambda: measure_mistrigger(args.dir, args.since),
        "paths": lambda: measure_paths(args.since),
        "gate": lambda: measure_gate(args.dir, args.since),
        "scout": lambda: measure_scout(args.dir, args.since),
    }
    selected = [args.metric] if args.metric else list(rows)
    try:
        table = {name: rows[name]() for name in selected}
    except ConfigError as exc:
        print(f"measure: {exc}", file=sys.stderr)
        return 2
    except subprocess.CalledProcessError as exc:
        print(f"measure: git log failed: {(exc.stderr or '').strip()}", file=sys.stderr)
        return 2
    print(json.dumps(table, indent=2, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

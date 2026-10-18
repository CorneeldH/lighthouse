"""Contracttest op de persona issue-impact-scout en zijn aanroep vanuit paia-start-issue.

De spec (docs/developer/planning/specs/2026-09-11-issue-impact-scout-design.md) legt twee
dingen vast die geen enkele runtime afdwingt: de toolallowlist (D-5 — zonder Bash is de
read-only claim pas waar) en de acht verplichte secties van het IMPACT PACK. Een vormeis
zonder test is een wens, dus staan ze hier.

De .agents/-spiegel wordt hier op INHOUD nagelopen voor precies de drie markers van dit contract.
tests/unit/test_skill_mirror.py vergelijkt bewust alleen bestandsnamen; zonder deze test zou de
scout-integratie in .agents/ stilletjes kunnen ontbreken.
"""

from __future__ import annotations

import importlib.util
import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
AGENT = REPO / ".claude" / "agents" / "issue-impact-scout.md"

REQUIRED_SECTIONS = (
    "IMPACT PACK",
    "GERAAKTE COMPONENTEN",
    "AANROEPERS EN TELLERS",
    "BESTE ANALOGIE",
    "TOEPASSELIJKE REGELS",
    "TESTS EN BEWIJS",
    "RISICO'S EN ONZEKERHEDEN",
    "BUITEN SCOPE",
)


def frontmatter(text: str) -> dict[str, str]:
    """De YAML-kop als platte dict. Geen yaml-dependency: dit zijn key: value-regels."""
    match = re.match(r"^---\n(.*?)\n---\n", text, re.DOTALL)
    assert match, "bestand mist een frontmatterblok"
    fields: dict[str, str] = {}
    for line in match.group(1).splitlines():
        if ":" in line and not line.startswith((" ", "#", "-")):
            key, _, value = line.partition(":")
            fields[key.strip()] = value.strip()
    return fields


def test_the_agent_file_exists() -> None:
    assert AGENT.exists(), f"persona ontbreekt: {AGENT}"


def test_frontmatter_pins_name_and_model() -> None:
    fields = frontmatter(AGENT.read_text(encoding="utf-8"))
    assert fields["name"] == "issue-impact-scout"
    assert fields["model"] == "sonnet", (
        "D-4: waarde zit in cross-file impact, niet in zoekwerk"
    )
    assert fields["maxTurns"] == "12", "D-6: begrensde tijd en hoofdagentcontext"


def test_toolallowlist_is_read_only() -> None:
    """D-5: Bash is niet werkelijk read-only te begrenzen, dus staat hij er niet in."""
    fields = frontmatter(AGENT.read_text(encoding="utf-8"))
    tools = {t.strip() for t in fields["tools"].split(",")}
    assert tools == {"Read", "Grep", "Glob"}, f"toolallowlist is {sorted(tools)}"


def test_description_bounds_the_use_to_start_issue() -> None:
    fields = frontmatter(AGENT.read_text(encoding="utf-8"))
    description = fields["description"].lower()
    assert "paia-start-issue" in description
    assert "4b" in description and "4c" in description


def test_pack_sections_follow_the_contract_order() -> None:
    """Pilot #847, #819, #912: het pack is een vast contract; volgorde is onderdeel daarvan."""
    body = AGENT.read_text(encoding="utf-8")
    start = body.index("```text\nIMPACT PACK")
    block = body[start : body.index("\n```", start + 1)]
    positions = [block.index(s) for s in REQUIRED_SECTIONS]
    assert positions == sorted(positions)


def test_prompt_demands_the_caller_search() -> None:
    """Pilot #847, #819: de scout miste aanroepers in src/cli/, de bridges en deploy/, en
    tellers die een map opsommen. De zoektocht en een niet-kale lege uitkomst zijn verplicht."""
    body = AGENT.read_text(encoding="utf-8").lower()
    for marker in (
        "aanroepers en tellers — verplicht",
        "relatieve import",
        "src/cli/",
        "src/web/bridges/",
        "src/web/services.py",
        "deploy/",
        "op het pad van die map",
        "één regel per map",
        "- geen gevonden — gezocht in",
    ):
        assert marker in body, marker


def test_the_caller_search_comes_before_the_analogy() -> None:
    """Pilot #912: op de budgetgrens moeten analogie en regels het kosten, niet de aanroepers."""
    body = AGENT.read_text(encoding="utf-8").lower()
    assert body.index("aanroepers en tellers — verplicht") < body.index(
        "dat is je analogie"
    )


def test_prompt_carries_every_output_section() -> None:
    body = AGENT.read_text(encoding="utf-8")
    missing = [s for s in REQUIRED_SECTIONS if s not in body]
    assert not missing, f"ontbrekende outputsecties in de prompt: {missing}"


def test_prompt_demands_the_pack_is_the_whole_answer() -> None:
    """Smoke-run op #912 begon met `Not relevant to Paperless index… Now composing the pack.`
    vóór de regel `IMPACT PACK`. Het pack blijft in de context van de ontvanger staan, dus zulke aanloop leest hij mee."""
    body = AGENT.read_text(encoding="utf-8").lower()
    assert "je eerste teken is de `i` van `impact pack`" in body
    assert "geen aanloop" in body and "geen denkwerk" in body


def test_prompt_binds_a_line_number_to_what_was_actually_read() -> None:
    """Zelfde run citeerde `.claude/rules/beelink.md:23-36` voor een norm op 35-48. Het pad was
    goed, het bereik fout — en het bereik is wat dit pack verkoopt."""
    body = AGENT.read_text(encoding="utf-8").lower()
    assert "daadwerkelijk gelezen" in body
    assert "bestandsnaam zonder regels" in body


def test_prompt_forbids_designing_and_planning() -> None:
    """Niet-doel uit de spec: de scout levert feiten, geen ontwerp of taakvolgorde."""
    body = AGENT.read_text(encoding="utf-8").lower()
    assert "geen ontwerp" in body
    assert "geen plan" in body
    assert "unknown" in body, (
        "niet vaststelbare informatie krijgt UNKNOWN, de agent gokt niet"
    )


def test_prompt_caps_the_number_of_searches() -> None:
    """`maxTurns` begrenst beurten, niet zoekacties (één beurt draagt meerdere parallelle calls),
    dus staat de calls-grens in de prompt zelf. Het getal moet gelijk zijn aan het budget dat
    scripts/measure_start_issue_kpi.py telt, anders meet de KPI een andere norm dan de agent
    gedraagt."""
    spec = importlib.util.spec_from_file_location(
        "measure_start_issue_kpi", REPO / "scripts" / "measure_start_issue_kpi.py"
    )
    assert spec and spec.loader
    kpi = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = kpi
    spec.loader.exec_module(kpi)

    body = AGENT.read_text(encoding="utf-8").lower()
    assert f"hoogstens {kpi.SCOUT_TOOL_CALL_BUDGET} zoekacties" in body


CLAUDE_SKILL = REPO / ".claude" / "skills" / "paia-start-issue" / "SKILL.md"
AGENTS_SKILL = REPO / ".agents" / "skills" / "paia-start-issue" / "SKILL.md"
SKILLS = (CLAUDE_SKILL, AGENTS_SKILL)

# De drie meldregels zijn woordelijk contract: scripts/measure_start_issue_kpi.py telt erop.
TRIGGER_LINE = "Scout: issue-impact-scout"
SKIP_PREFIX = "Scout: overgeslagen — "
FAIL_PREFIX = "Scout: mislukt — "
HANDOVER = "IMPACT PACK: staat in context, niet herhalen"


def test_both_skill_trees_mention_the_scout() -> None:
    for path in SKILLS:
        assert "issue-impact-scout" in path.read_text(encoding="utf-8"), (
            f"{path} mist de scout"
        )


def test_both_trees_carry_the_three_report_lines() -> None:
    for path in SKILLS:
        body = path.read_text(encoding="utf-8")
        for line in (TRIGGER_LINE, SKIP_PREFIX, FAIL_PREFIX):
            assert line in body, (
                f"{path.parent.parent.name}: meldregel ontbreekt: {line!r}"
            )


def test_the_scout_is_bound_to_gate_four_b_and_c() -> None:
    """De trigger hangt aan 4b/4c; poort 0, 1, 2 en 4a noemen hem niet."""
    for path in SKILLS:
        body = path.read_text(encoding="utf-8")
        section = body[body.index("## Scout") :]
        assert "4b" in section and "4c" in section
        assert "4a" in section, "4a moet expliciet als niet-trigger genoemd staan"


def test_the_escape_hatch_demands_a_concrete_reason() -> None:
    """`lijkt eenvoudig` is geen reden; de skill moet dat met zoveel woorden verbieden."""
    for path in SKILLS:
        body = path.read_text(encoding="utf-8").lower()
        assert "lijkt eenvoudig" in body
        assert "geen geldige reden" in body


def test_the_failure_path_is_fail_open() -> None:
    for path in SKILLS:
        body = path.read_text(encoding="utf-8")
        assert "Route vervolgt met normale contextverkenning." in body


def test_the_handover_has_one_unambiguous_demand() -> None:
    """Pilot #937: het pack ging in 1 van 5 runs letterlijk in de Skill-args, in 4 alleen als
    verwijzing, terwijl de skill 'verbatim' eiste. Eén eis, en de oude mag er niet naast staan."""
    for path in SKILLS:
        body = path.read_text(encoding="utf-8")
        assert HANDOVER in body, f"{path.parent.parent.name}: overdrachtszin ontbreekt"
        assert "verbatim" not in body.lower(), (
            f"{path.parent.parent.name}: oude eis staat er nog"
        )


def test_the_opencode_mirror_names_the_missing_persona() -> None:
    """De persona is Claude-only; .agents/ moet zeggen wat daar dan gebeurt."""
    body = AGENTS_SKILL.read_text(encoding="utf-8")
    assert "niet beschikbaar" in body
    assert "simuleert" in body, "geen algemene executor als namaakpersona"

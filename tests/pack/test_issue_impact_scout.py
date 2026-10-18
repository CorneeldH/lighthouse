"""Contracttest op de agents van `pack/core`: de persona issue-impact-scout en spec-adversary.

De bronspec van de scout legt twee dingen vast die geen enkele runtime afdwingt: de
toolallowlist (D-5 — zonder Bash is de read-only claim pas waar) en de acht verplichte
secties van het IMPACT PACK. Een vormeis zonder test is een wens, dus staan ze hier.

De aanroep vanuit `lighthouse-start-issue` (de meldregels, poort 4b/4c, de overdracht)
hoort bij die skill en wordt daar getest (taak 24).
"""

from __future__ import annotations

import importlib.util
import re
import sys
import tomllib

from tests.pack._testkit import CORE, INSTALL_ROOT

AGENT = CORE / "agents" / "issue-impact-scout.md"
ADVERSARY = CORE / "agents" / "spec-adversary.md"

REQUIRED_SECTIONS = (
    "IMPACT PACK",
    "AFFECTED COMPONENTS",
    "CALLERS AND COUNTERS",
    "BEST ANALOGY",
    "APPLICABLE RULES",
    "TESTS AND EVIDENCE",
    "RISKS AND UNCERTAINTIES",
    "OUT OF SCOPE",
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
    """`init` legt hem neer als `.claude/agents/lighthouse-issue-impact-scout.md`; de
    naam is wat `subagent_type` noemt en wat de reviewmeting telt."""
    fields = frontmatter(AGENT.read_text(encoding="utf-8"))
    assert fields["name"] == "lighthouse-issue-impact-scout"
    assert fields["model"] == "sonnet", (
        "D-4: waarde zit in cross-file impact, niet in zoekwerk"
    )
    assert fields["maxTurns"] == "12", "D-6: begrensde tijd en hoofdagentcontext"


def test_name_is_what_the_kpi_counts() -> None:
    kpi = _kpi()
    fields = frontmatter(AGENT.read_text(encoding="utf-8"))
    assert fields["name"] == kpi.SCOUT_AGENT


def test_toolallowlist_is_read_only() -> None:
    """D-5: Bash is niet werkelijk read-only te begrenzen, dus staat hij er niet in."""
    fields = frontmatter(AGENT.read_text(encoding="utf-8"))
    tools = {t.strip() for t in fields["tools"].split(",")}
    assert tools == {"Read", "Grep", "Glob"}, f"toolallowlist is {sorted(tools)}"


def test_description_bounds_the_use_to_start_issue() -> None:
    fields = frontmatter(AGENT.read_text(encoding="utf-8"))
    description = fields["description"].lower()
    assert "lighthouse-start-issue" in description
    assert "4b" in description and "4c" in description


def test_pack_sections_follow_the_contract_order() -> None:
    """Het pack is een vast contract; volgorde is onderdeel daarvan."""
    body = AGENT.read_text(encoding="utf-8")
    start = body.index("```text\nIMPACT PACK")
    block = body[start : body.index("\n```", start + 1)]
    positions = [block.index(s) for s in REQUIRED_SECTIONS]
    assert positions == sorted(positions)


def test_impact_pack_heading_is_the_headings_value() -> None:
    """De kop komt uit `headings.toml` (`impact_pack`); hij is in elke taal gelijk,
    dus de persona mag hem letterlijk noemen."""
    table = tomllib.loads((CORE / "headings.toml").read_text(encoding="utf-8"))
    assert set(table["impact_pack"].values()) == {"IMPACT PACK"}


def test_prompt_demands_the_caller_search() -> None:
    """De scout miste aanroepers in CLI-modules, bridges en deploy-units, en tellers die
    een map opsommen. De zoektocht en een niet-kale lege uitkomst zijn verplicht."""
    body = AGENT.read_text(encoding="utf-8").lower()
    for marker in (
        "callers and counters — mandatory",
        "relative import",
        "execstart=",
        "on the path of that directory",
        "one line per directory",
        "- none found — searched",
    ):
        assert marker in body, marker


def test_the_caller_search_comes_before_the_analogy() -> None:
    """Op de budgetgrens moeten analogie en regels het kosten, niet de aanroepers."""
    body = AGENT.read_text(encoding="utf-8").lower()
    assert body.index("callers and counters — mandatory") < body.index(
        "that is your analogy"
    )


def test_prompt_carries_every_output_section() -> None:
    body = AGENT.read_text(encoding="utf-8")
    missing = [s for s in REQUIRED_SECTIONS if s not in body]
    assert not missing, f"ontbrekende outputsecties in de prompt: {missing}"


def test_prompt_demands_the_pack_is_the_whole_answer() -> None:
    """Een smoke-run begon met `… Now composing the pack.` vóór de regel `IMPACT PACK`.
    Het pack blijft in de context van de ontvanger staan, dus zulke aanloop leest hij mee."""
    body = AGENT.read_text(encoding="utf-8").lower()
    assert "your first character is the `i` of `impact pack`" in body
    assert "no preamble" in body and "no reasoning" in body


def test_prompt_binds_a_line_number_to_what_was_actually_read() -> None:
    """Een run citeerde een rule op 23-36 voor een norm op 35-48. Het pad was goed, het
    bereik fout — en het bereik is wat dit pack verkoopt."""
    body = AGENT.read_text(encoding="utf-8").lower()
    assert "actually read" in body
    assert "file name without lines" in body


def test_prompt_forbids_designing_and_planning() -> None:
    """Niet-doel uit de spec: de scout levert feiten, geen ontwerp of taakvolgorde."""
    body = AGENT.read_text(encoding="utf-8").lower()
    assert "no design" in body
    assert "no plan" in body
    assert "unknown" in body, (
        "niet vaststelbare informatie krijgt UNKNOWN, de agent gokt niet"
    )


def test_prompt_names_no_source_repo_layout() -> None:
    """Paden als `src/web/bridges/` waren de mappen van de bronrepo, niet van elk project."""
    body = AGENT.read_text(encoding="utf-8")
    for path in ("src/cli/", "src/web/", "services.py", "-m src."):
        assert path not in body, path


def _kpi():
    path = INSTALL_ROOT / ".lighthouse" / "scripts" / "measure_start_issue_kpi.py"
    spec = importlib.util.spec_from_file_location("measure_start_issue_kpi", path)
    assert spec and spec.loader
    kpi = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = kpi
    spec.loader.exec_module(kpi)
    return kpi


def test_prompt_caps_the_number_of_searches() -> None:
    """`maxTurns` begrenst beurten, niet zoekacties (één beurt draagt meerdere parallelle calls),
    dus staat de calls-grens in de prompt zelf. Het getal moet gelijk zijn aan het budget dat
    `measure_start_issue_kpi.py` telt, anders meet de KPI een andere norm dan de agent
    gedraagt."""
    body = AGENT.read_text(encoding="utf-8").lower()
    assert f"at most {_kpi().SCOUT_TOOL_CALL_BUDGET} searches" in body


# --- spec-adversary -----------------------------------------------------------


def test_adversary_frontmatter() -> None:
    fields = frontmatter(ADVERSARY.read_text(encoding="utf-8"))
    assert fields["name"] == "lighthouse-spec-adversary"
    assert fields["model"] == "sonnet"
    assert {t.strip() for t in fields["tools"].split(",")} == {"Bash", "Read", "Grep", "Glob"}


def test_adversary_names_the_lighthouse_review_skills() -> None:
    body = ADVERSARY.read_text(encoding="utf-8")
    assert "lighthouse-spec-adversarial-review" in body
    assert "lighthouse-plan-adversarial-review" in body
    assert ".claude/skills/lighthouse-plan-adversarial-review/review.sh" in body
    assert not re.search(r"\b(?!lighthouse-)[a-z]+-(spec|plan)-adversarial-review", body)


def test_adversary_output_contract_is_unchanged() -> None:
    body = ADVERSARY.read_text(encoding="utf-8")
    for token in (
        "ROUND <n> | reviewer:",
        "STATUS: <Blockers found | Majors only | Clean>",
        "VERIFIED:",
        "TOP3:",
        "VERDICT: <one sentence>",
        "UNVERIFIED:",
        "r<ROUND>-f<index>",
    ):
        assert token in body, token


def test_adversary_reads_the_budget_json_contract() -> None:
    """`$OUT.budget.json` is de uitvoer van `codex_usage.py budget`: de reden heet
    `reason`, het tijdstip `reset_at` (taak 17)."""
    body = ADVERSARY.read_text(encoding="utf-8")
    line = next(line for line in body.splitlines() if "CODEX_BUDGET" in line and "|" in line)
    assert "`reason`" in line and "`reset_at`" in line, line
    assert "`reden`" not in body


def test_agents_are_english_and_carry_no_source_issue_numbers() -> None:
    for path in (AGENT, ADVERSARY):
        body = path.read_text(encoding="utf-8")
        assert not re.search(r"\(#\d+\)", body), path.name
        for word in (" je ", " het ", " niet ", " geen ", " een "):
            assert word not in body.lower(), (path.name, word)

"""Contract test on the agents of `pack/core`: the persona issue-impact-scout and spec-adversary.

The scout's source spec fixes two things that no runtime enforces: the
tool allowlist (D-5 — without Bash the read-only claim is only true then) and the eight mandatory
sections of the IMPACT PACK. A form requirement without a test is a wish, so they live here.

The call from `lighthouse-start-issue` (the announcement lines, gate 4b/4c, the handover)
belongs to that skill and is tested there (task 24).
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
    """The YAML header as a flat dict. No yaml dependency: these are key: value lines."""
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
    """`init` puts it down as `.claude/agents/lighthouse-issue-impact-scout.md`; the
    name is what `subagent_type` names and what the review measurement counts."""
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
    """D-5: Bash cannot really be bounded to read-only, so it is not in the list."""
    fields = frontmatter(AGENT.read_text(encoding="utf-8"))
    tools = {t.strip() for t in fields["tools"].split(",")}
    assert tools == {"Read", "Grep", "Glob"}, f"toolallowlist is {sorted(tools)}"


def test_description_bounds_the_use_to_start_issue() -> None:
    fields = frontmatter(AGENT.read_text(encoding="utf-8"))
    description = fields["description"].lower()
    assert "lighthouse-start-issue" in description
    assert "4b" in description and "4c" in description


def test_pack_sections_follow_the_contract_order() -> None:
    """The pack is a fixed contract; order is part of it."""
    body = AGENT.read_text(encoding="utf-8")
    start = body.index("```text\nIMPACT PACK")
    block = body[start : body.index("\n```", start + 1)]
    positions = [block.index(s) for s in REQUIRED_SECTIONS]
    assert positions == sorted(positions)


def test_impact_pack_heading_is_the_headings_value() -> None:
    """The heading comes from `headings.toml` (`impact_pack`); it is the same in every language,
    so the persona may name it literally."""
    table = tomllib.loads((CORE / "headings.toml").read_text(encoding="utf-8"))
    assert set(table["impact_pack"].values()) == {"IMPACT PACK"}


def test_prompt_demands_the_caller_search() -> None:
    """The scout missed callers in CLI modules, bridges and deploy units, and counters that
    enumerate a directory. The search and a non-bare empty outcome are mandatory."""
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
    """At the budget limit the analogy and rules must pay for it, not the callers."""
    body = AGENT.read_text(encoding="utf-8").lower()
    assert body.index("callers and counters — mandatory") < body.index(
        "that is your analogy"
    )


def test_prompt_carries_every_output_section() -> None:
    body = AGENT.read_text(encoding="utf-8")
    missing = [s for s in REQUIRED_SECTIONS if s not in body]
    assert not missing, f"ontbrekende outputsecties in de prompt: {missing}"


def test_prompt_demands_the_pack_is_the_whole_answer() -> None:
    """A smoke run began with `… Now composing the pack.` before the line `IMPACT PACK`.
    The pack stays in the recipient's context, so it reads such a preamble along."""
    body = AGENT.read_text(encoding="utf-8").lower()
    assert "your first character is the `i` of `impact pack`" in body
    assert "no preamble" in body and "no reasoning" in body


def test_prompt_binds_a_line_number_to_what_was_actually_read() -> None:
    """A run cited a rule at 23-36 for a norm at 35-48. The path was right, the
    range wrong — and the range is what this pack sells."""
    body = AGENT.read_text(encoding="utf-8").lower()
    assert "actually read" in body
    assert "file name without lines" in body


def test_prompt_forbids_designing_and_planning() -> None:
    """Non-goal from the spec: the scout delivers facts, no design or task order."""
    body = AGENT.read_text(encoding="utf-8").lower()
    assert "no design" in body
    assert "no plan" in body
    assert "unknown" in body, (
        "niet vaststelbare informatie krijgt UNKNOWN, de agent gokt niet"
    )


def test_prompt_names_no_source_repo_layout() -> None:
    """Paths like `src/web/bridges/` were the directories of the source repo, not of every project."""
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
    """`maxTurns` bounds turns, not search actions (one turn carries several parallel calls),
    so the calls limit is in the prompt itself. The number must equal the budget that
    `measure_start_issue_kpi.py` counts, otherwise the KPI measures a different norm than the agent
    follows."""
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
    """`$OUT.budget.json` is the output of `codex_usage.py budget`: the reason is called
    `reason`, the time `reset_at` (task 17)."""
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

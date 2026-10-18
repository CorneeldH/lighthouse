"""Contract test for the issue skills: route-issue, start-issue and handoff.

What lives only in skill text and no runtime enforces: gate 0 reads the blocker from the
relation, the gates moved to route-issue, start-issue reports one gate line and runs the
scout at 4b/4c, and handoff writes the protocol tokens of `lighthouse_lib.protocol`. Both
variants (Claude and Agents) are checked on content.

From paia's `test_issue_skills_contract.py`, `test_instructielagen.py` (the handoff rows) and
the start-issue half of `test_issue_impact_scout.py`. Tests of skills that stay in paia
(backlog-stand, board-sweep, the controller) are gone.
"""

from __future__ import annotations

import re

from tests.pack._testkit import CORE

SKILLS = CORE / "skills"


def skill(name: str) -> tuple[str, str]:
    """The Claude and the Agents variant of a core skill."""
    shared = SKILLS / name / "SKILL.md"
    if shared.is_file():
        text = shared.read_text(encoding="utf-8")
        return text, text
    return tuple(
        (SKILLS / name / variant / "SKILL.md").read_text(encoding="utf-8")
        for variant in ("claude", "agents")
    )


def frontmatter_value(body: str, key: str) -> str:
    head = body.split("---\n", 2)[1]
    line = next(line for line in head.splitlines() if line.startswith(f"{key}:"))
    return line[len(key) + 1 :].strip()


# --- route-issue -------------------------------------------------------------------


def test_gate_0_reads_the_relation():
    for body in skill("route-issue"):
        assert "dependencies/blocked_by" in body


def test_gate_0_stops_without_blocked_label():
    """An issue without a blocked label but with an open blocked_by must not slip through
    gate 0 unnoticed; otherwise it starts while it waits for another issue."""
    for body in skill("route-issue"):
        gate0 = body[body.index("### Gate 0") : body.index("### Gate 1")]
        assert "issue_dependencies_summary.blocked_by" in gate0
        assert "label missing" in gate0
        assert "unknown" in gate0


def test_gate_0_routes_on_in_a_controller_run():
    """In a controller run the controller stacks the execution: gate 0 does not stop on
    labels.blocked_issue, but labels.blocked_external always stops. The router stays
    read-only."""
    for body in skill("route-issue"):
        gate0 = body[body.index("### Gate 0") : body.index("### Gate 1")]
        assert "controller run" in gate0.lower()
        assert "labels.blocked_external" in gate0
        assert "/lighthouse-handoff" not in gate0  # the router posts nothing


def test_router_invariants_know_the_controller_run():
    """No line in the router contradicts the exception of gate 0."""
    for body in skill("route-issue"):
        row = next(r for r in body.splitlines() if r.startswith("| `labels.blocked_issue`"))
        assert "controller run" in row
        hard_stop = body[body.index("Only the two blocked labels") :]
        hard_stop = hard_stop[: hard_stop.index("## Non-negotiable")]
        assert "controller run" in hard_stop
        rule = next(r for r in body.splitlines() if r.startswith("- ✅ `labels.blocked_*`"))
        assert "controller run" in rule
        external = next(
            r for r in body.splitlines() if r.startswith("| `labels.blocked_external`")
        )
        assert "controller run" not in external  # stays an unconditional stop


def test_route_issue_is_read_only_and_reports_one_line():
    for body in skill("route-issue"):
        assert "#<nr> heading:gate_line <n>[<a|b|c>]: <route> — <reason>" in body
        for gate in ("Gate 0", "Gate 1", "Gate 2", "Gate 3", "Gate 4"):
            assert gate in body
        assert "board_start.py" not in body and "issue-impact-scout" not in body
        assert "using-git-worktrees" not in body
        assert "never calls the route" in body  # classify only
        assert "call that skill, done" not in body  # the imperative did not move here


def test_route_issue_description_survives_the_yaml_parser():
    """An unquoted ` #` in the description ("which route for #610") is a YAML comment: the
    parser cuts there and the NOTE clause silently leaves the skill catalogue."""
    for body in skill("route-issue"):
        value = frontmatter_value(body, "description")
        assert value[0] in "'\"" or " #" not in value
        assert value.rstrip("'\"").endswith(
            "NOTE — actually starting on an issue (worktree, board, execution) is "
            "lighthouse-start-issue."
        )


def test_route_issue_sends_an_open_fog_question_to_4c():
    for body in skill("route-issue"):
        gate4 = body[body.index("### Gate 4") : body.index("## A blocking label")]
        assert "heading:milestone_not_yet_decided" in gate4
        assert 'touches open question "' in gate4
        assert "→ #<nr>" in gate4  # only an open bullet counts
        labels = body[body.index("## A blocking label") : body.index("## Non-negotiable")]
        assert len(re.findall(r"^\| `labels\.", labels, flags=re.MULTILINE)) == 6  # no new label


def test_route_issue_reads_labels_from_config():
    """The labels come from `[labels]` (besluit 28), never as paia's literal names."""
    for body in skill("route-issue"):
        for key in (
            "blocked_issue",
            "blocked_external",
            "needs_shaping",
            "needs_decision",
            "needs_hands",
            "needs_testing",
        ):
            assert f"config.py labels.{key}" in body
        assert "3-blocked" not in body and "3-needs" not in body


def test_route_issue_reads_the_repo_from_config():
    for body in skill("route-issue"):
        assert "python3 .lighthouse/lib/lighthouse_lib/config.py forge.repo" in body
        assert "identity" not in body


def test_route_issue_guards_what_stays_in_paia():
    """backlog-stand and board-sweep stay in paia: named only "if present" (ontwerpkeuze 16)."""
    for body in skill("route-issue"):
        for name in ("backlog-stand", "board-sweep"):
            for para in re.split(r"\n\s*\n", body):
                if name in para:
                    assert "if present" in " ".join(para.split()), para

# --- start-issue (letterlijk uit paia) ---
def test_start_issue_houdt_geblokkeerde_uitvoering_vast():
    for body in skill("paia-start-issue"):
        stap4 = body[body.index("### 4.") : body.index("#### Scout")]
        assert "volgende: /paia-start-issue" in stap4
        assert "stapel:" in stap4
        # I6: een start met `blokkade: verrekend` voert de route uit, geen nieuwe handoff.
        assert "`stapel:` of `blokkade:`" in stap4
        for poort in ("`1`", "`2`", "`4a`"):
            assert poort in stap4


def test_start_issue_delegeert_stap_4():
    for body in skill("paia-start-issue"):
        stap4 = body[body.index("### 4.") : body.index("#### Scout")]
        assert "paia-route-issue" in stap4
        assert "#### Poort 1" not in body  # poorten verhuisd, niet gedupliceerd


def _bash_blok(body: str, kop: str) -> str:
    start = body.index("```bash", body.index(kop))
    return body[start : body.index("```", start + 7) + 3]


def test_start_issue_vraagt_al_af_voor_de_worktree():
    recept = _bash_blok(skill("paia-backlog-stand")[0], "### 7. Al af?")
    for body in skill("paia-start-issue"):
        stap2 = body[body.index("### 2.") : body.index("### 3.")]
        assert recept in stap2  # één mechanisme, letterlijk gedeeld met backlog-stand
        assert "al af? nee — gezocht op " in stap2
        assert "dekt de AC niet" in stap2  # variant van de nee-regel
        assert "toch door" in stap2
        assert "controller-run" in stap2.lower()  # headless: keuze als tekst, stop
        assert "al af? waarschijnlijk — " in stap2
        assert (
            "aantoonbaar dekt" in stap2
        )  # precisie: een losse vermelding is geen treffer
        assert (
            "één Bash-call" in stap2
        )  # KPI leadin: het recept kost één call, niet vier
        assert "/paia-close-issue <nr>" in stap2  # ingang B verifieert, deze stap niet
        assert "gh issue close" not in stap2 and "wontfix" not in stap2
        assert "al af? niet meetbaar — fetch faalde" in stap2  # geen `nee` op oude data


def test_start_issue_leest_fog_van_de_milestone():
    for body in skill("paia-start-issue"):
        stap2 = body[body.index("### 2.") : body.index("### 3.")]
        assert "### Nog niet vastgelegd" in stap2
        assert "fog: " in stap2
        scout = body[body.index("#### Scout") :]
        assert "Nog niet vastgelegd" in scout[: scout.index("Meld de trigger")]


START_ISSUE_SKILL = REPO / ".claude" / "skills" / "paia-start-issue" / "SKILL.md"
START_ISSUE_SKILL_AGENTS = REPO / ".agents" / "skills" / "paia-start-issue" / "SKILL.md"
def test_start_issue_verbiedt_overslaan_zonder_besluit(path):
    text = path.read_text(encoding="utf-8")
    assert "de plan-review overslaan zonder besluit van de gebruiker" in text
    assert (
        "Die laat de gebruiker kiezen: plan-review, controller (SDD) of interactief "
        "(`/superpowers:executing-plans`)" in text
    )


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

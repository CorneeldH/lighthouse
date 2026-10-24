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


# --- start-issue -------------------------------------------------------------------


def _step(body: str, start: str, end: str) -> str:
    return body[body.index(start) : body.index(end)]


def _bash_block(body: str, heading: str) -> str:
    start = body.index("```bash", body.index(heading))
    return body[start : body.index("```", start + 7) + 3]


def test_start_issue_holds_a_blocked_execution():
    for body in skill("start-issue"):
        step4 = _step(body, "### 4.", "#### Scout")
        assert "next: /lighthouse-start-issue" in step4
        # A start with `blocker: accounted` runs the route, no new handoff.
        assert "`stack:` or `blocker:`" in step4
        for gate in ("`1`", "`2`", "`4a`"):
            assert gate in step4


def test_start_issue_reads_legacy_stack():
    """The skill writes and reads `stack:`; an old 🤖 comment on a running issue still says
    `stapel:` or `blokkade:` (Protocoltokens), and counts the same."""
    for body in skill("start-issue"):
        step4 = _step(body, "### 4.", "#### Scout")
        assert "stack:" in step4
        assert "`stapel:`" in step4 and "`blokkade:`" in step4
        assert "heading:kind_started" in step4


def test_start_issue_delegates_step_4():
    for body in skill("start-issue"):
        step4 = _step(body, "### 4.", "#### Scout")
        assert "lighthouse-route-issue" in step4
        assert "#### Gate 1" not in body  # gates moved, not duplicated


def test_start_issue_asks_already_done_before_the_worktree():
    for body in skill("start-issue"):
        step2 = _step(body, "### 2.", "### 3.")
        recipe = _bash_block(body, "**Already done?**")
        assert "config.py forge.default_branch" in recipe
        assert "FETCH FAILED" in recipe
        assert "config.py paths.search_dirs" in recipe
        assert "already done? no — searched for " in step2
        assert "does not cover the AC" in step2  # variant of the no line
        assert "carry on anyway" in step2
        assert "controller run" in step2.lower()  # headless: choice as text, stop
        assert "already done? likely — " in step2
        assert "demonstrably covers" in step2  # precision: a loose mention is no hit
        assert "one Bash call" in step2  # KPI leadin: the recipe costs one call, not four
        assert "/lighthouse-close-issue <nr>" in step2  # entry B verifies, this step does not
        assert "gh issue close" not in step2 and "wontfix" not in step2
        assert "already done? not measurable — fetch failed" in step2


def test_start_issue_reads_fog_from_the_milestone():
    for body in skill("start-issue"):
        step2 = _step(body, "### 2.", "### 3.")
        assert "### heading:milestone_not_yet_decided" in step2
        assert "heading:milestone_dod" in step2 and "heading:milestone_no_gos" in step2
        assert "fog: " in step2
        scout = body[body.index("#### Scout") :]
        assert "heading:milestone_not_yet_decided" in scout[: scout.index("Report the trigger")]


def test_start_issue_reads_axes_from_config():
    for body in skill("start-issue"):
        step2 = _step(body, "### 2.", "### 3.")
        assert "config.py labels.location_prefix" in step2
        assert "config.py labels.area_prefix" in step2
        assert "1-loc" not in body and "2-area" not in body


def test_start_issue_skips_board_without_config():
    """Without `[forge.board]` the board step reports and goes on (besluit 16, 24)."""
    for body in skill("start-issue"):
        step3 = _step(body, "### 3.", "### 4.")
        assert "python3 .lighthouse/scripts/board_start.py <nr>" in step3
        assert "config.py forge.board.project_number" in step3 and "rc 1" in step3
        assert "board: skipped" in step3
        assert "is not a stop" in step3


def test_start_issue_forbids_skipping_without_a_decision():
    for body in skill("start-issue"):
        assert "skip the plan review without a decision of the user" in body
        assert (
            "That lets the user choose: plan review, controller (SDD, if present) or "
            "interactive (`/superpowers:executing-plans`)" in body
        )


def test_start_issue_names_the_project_paths():
    for body in skill("start-issue"):
        assert "config.py paths.specs" in body and "config.py paths.plans" in body


def test_start_issue_keeps_the_measurements_not_the_numbers():
    for body in skill("start-issue"):
        for sub in ("leadin", "worktree-first", "mistrigger", "paths", "gate", "scout"):
            assert f"python3 .lighthouse/scripts/measure_start_issue_kpi.py {sub}" in body
        assert "n=11" not in body and "78,6%" not in body and "2026-08-02" not in body


def test_start_issue_controller_run_env():
    for body in skill("start-issue"):
        assert "PAIA_CONTROLLER_RUN" not in body


# The three report lines are a contract: measure_start_issue_kpi.py counts on them.
TRIGGER_LINE = "Scout: lighthouse-issue-impact-scout"
SKIP_PREFIX = "heading:scout_skipped — "
FAIL_PREFIX = "Scout: failed — "
HANDOVER = "IMPACT PACK: in context, do not repeat"


def test_both_variants_mention_the_scout():
    for body in skill("start-issue"):
        assert "subagent_type: lighthouse-issue-impact-scout" in body


def test_both_variants_carry_the_three_report_lines():
    for body in skill("start-issue"):
        for line in (TRIGGER_LINE, SKIP_PREFIX, FAIL_PREFIX):
            assert line in body, line


def test_the_scout_is_bound_to_gate_four_b_and_c():
    """The trigger hangs on 4b/4c; gates 0, 1, 2 and 4a do not call it."""
    for body in skill("start-issue"):
        section = body[body.index("#### Scout") :]
        assert "4b" in section and "4c" in section
        assert "4a" in section, "4a must be named as a non-trigger"


def test_the_escape_hatch_demands_a_concrete_reason():
    for body in skill("start-issue"):
        lower = body.lower()
        assert "looks simple" in lower
        assert "not a valid reason" in lower


def test_the_failure_path_is_fail_open():
    for body in skill("start-issue"):
        assert "The route continues with the normal context exploration." in body


def test_the_handover_has_one_unambiguous_demand():
    for body in skill("start-issue"):
        assert HANDOVER in body
        assert "verbatim" not in body.lower()


def test_the_agents_variant_names_the_missing_persona():
    """The persona is Claude-only; the Agents variant says what happens there."""
    claude, agents = skill("start-issue")
    assert "##### Outside Claude Code" in agents
    assert "not available" in agents and "simulate" in agents
    assert "##### Outside Claude Code" not in claude


# --- handoff -----------------------------------------------------------------------


def test_handoff_blocked_restart_from_its_own_worktree():
    """The blocked restart from lighthouse-start-issue comes from the issue's own worktree;
    from `main` the controller puts it on hold and never stacks."""
    for body in skill("handoff"):
        step3 = _step(body, "3. **`next:`", "4. **`when:`**")
        assert "main checkout and `branch:` `main`" in step3
        assert "— blocker #<x> open" in step3
        assert "own worktree and branch of the issue" in step3


PLAN_SEAM = [
    "**Exception: a plan, not reviewed.**",
    "no `AskUserQuestion`",
    "[<file name>](<absolute path>)",
    "Review, controller or interactive?",
    "My advice:",
    "end the turn",
    "A controller run does not ask this",
    "Without an issue number",
    "skipped on a decision of the user",
    "`First step: /superpowers:executing-plans <plan>`",
    "| What there is | First step | Controller (if present) |",
    "The Controller column says what controller mode step 2 does",
    "`/lighthouse-spec-adversarial-review` on that spec | `default` |",
    "`/superpowers:writing-plans` on that spec | `no` |",
    "`/lighthouse-plan-adversarial-review` on that plan | `choice` |",
    "`/superpowers:executing-plans` | `offer` |",
    "`/lighthouse-fix-pr <pr>` | `offer` |",
    "At `choice` you print two blocks in step 6 before the question: first *review*, then *interactive*.",
    "Only *controller* still asks for an answer in this window.",
    "then ask the question only with *review* or *interactive*, without *controller*",
    "At `no` you do not read the board status; if the issue is on `Controller` anyway, it stays there until the user moves it.",
    "If the user wants the plan review itself through the controller: `next: /lighthouse-plan-adversarial-review <plan>` and steps 5 and 6.",
    "When in doubt: **review**.",
    "This also holds when the issue is already on `Controller`",
]


def test_handoff_plan_seam_asks_in_chat_text():
    """At a committed plan the user chooses, after inspection."""
    for body in skill("handoff"):
        flat = " ".join(body.split())
        missing = [phrase for phrase in PLAN_SEAM if " ".join(phrase.split()) not in flat]
        assert not missing, missing


def test_handoff_plan_seam_blocks_before_the_question():
    """At *review* or *interactive* the user need not come back to the window: review
    block, interactive block, only then plan link, advice and question."""
    for body in skill("handoff"):
        start = body.index("**Exception: a plan, not reviewed.**")
        para = body[start : body.index("A controller run does not ask this", start)]
        order = [
            "`First step: /lighthouse-plan-adversarial-review <plan>`",
            "`First step: /superpowers:executing-plans <plan>`",
            "Plan is in",
            "My advice:",
            "Review, controller or interactive?",
        ]
        positions = [para.index(piece) for piece in order]
        assert positions == sorted(positions), positions


def test_handoff_plan_seam_exception_before_controller_post():
    for body in skill("handoff"):
        assert body.index("**Exception: a plan, not reviewed.**") < body.index(
            "Only at `offer` (never at `default` above): if it is already on `Controller`, "
            "post at once"
        )


def test_handoff_post_artefact():
    for body in skill("handoff"):
        flat = " ".join(body.split())
        for phrase in (
            "python3 .lighthouse/scripts/post_artefact.py <nr> <absolute path>",
            "with `dangerouslyDisableSandbox: true` (Go TLS of `gh`):",
            "also in controller mode (if present) before the `🤖 heading:kind_handoff`",
            "An error does not block the handoff",
            "artefact: <the line from post_artefact.py>",
        ):
            assert phrase in flat, phrase


def test_handoff_post_artefact_is_in_step_3():
    for body in skill("handoff"):
        assert (
            body.index("3. **Decide the anchor.**")
            < body.index(".lighthouse/scripts/post_artefact.py")
            < body.index("4. **Decide the first step")
        )


def test_handoff_knows_lane_native():
    for body in skill("handoff"):
        assert "lane: native" in body
        assert "the controller starts it on Sonnet" in body
        assert "Yes, native" in body


def test_handoff_writes_new_tokens():
    """The handoff block writes the tokens of `lighthouse_lib.protocol` (Protocoltokens):
    `next:`, `when: now|night`, `anchor:`, under the heading from headings.toml."""
    for body in skill("handoff"):
        post = body[body.index("5. **Post**") : body.index("6. **Status")]
        for token in ("anchor: <anchor>", "next: <command> <argument>", "when: now"):
            assert token in post, token
        assert "headings.py kind_handoff" in post
        for old in ("volgende:", "wanneer:", "anker:", "`nu`", "`nacht`"):
            assert old not in body, old
        assert "`when:`** is `now`, unless the user chooses or asks for `night`" in body


def test_handoff_board_and_host_from_config():
    for body in skill("handoff"):
        for key in (
            "forge.owner",
            "forge.name",
            "forge.board.project_number",
            "forge.board.project_id",
            "forge.board.status_field_id",
            "forge.board.status_options.Controller",
        ):
            assert f"config.py {key}" in body, key
        assert "config.py host-check" in body
        assert "the always-on host (`host.toml`)" in body
        assert "LIGHTHOUSE_CONTROLLER_RUN" in body
        assert "identity" not in body and "controller_tick" not in body

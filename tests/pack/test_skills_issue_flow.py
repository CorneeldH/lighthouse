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

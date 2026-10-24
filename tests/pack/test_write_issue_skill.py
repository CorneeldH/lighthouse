"""Contract test for the skill `lighthouse-write-issue`.

What lives only in skill text: the order relation-before-label when an issue gets blocked, the
duplicate search before the template, and the pitch section "not yet decided". The label
taxonomy of a project does not live in the skill: it comes from the file under `[paths] labels`,
and without that file only the `[labels]` keys and the GitHub default labels apply. Both
variants are checked on content; they differ only in the "Generated with" line.

From paia's `test_issue_skills_contract.py` (the write-issue part).
"""

from __future__ import annotations

import re

from tests.pack._testkit import CORE

SKILL_DIR = CORE / "skills" / "write-issue"
CONFIG = "python3 .lighthouse/lib/lighthouse_lib/config.py"


def skill() -> tuple[str, str]:
    return tuple(
        (SKILL_DIR / variant / "SKILL.md").read_text(encoding="utf-8")
        for variant in ("claude", "agents")
    )


def _between(body: str, start: str, end: str) -> str:
    return body[body.index(start) : body.index(end)]


def test_write_issue_sets_relation_before_label():
    for body in skill():
        section = body[body.index("#### 3e. Set a block") :]
        assert section.index('dependencies/blocked_by" -F issue_id=') < section.index(
            '--add-label "$BLOCKED"'
        )
        assert "**unknown**" in section  # state not readable -> never success
        assert "not from the summary" in section
        assert "created_at" in section  # recovery after create: never an older issue
        for verdict in ("holds", "lapsed", "unlinked", "label missing"):
            assert f"**{verdict}**" in section


def test_write_issue_blocking_invariant():
    """The block rule names its labels by config key, never by a literal label name."""
    for body in skill():
        section = _between(body, "#### 3e. Set a block", "### 4.")
        assert f"BLOCKED=$({CONFIG} labels.blocked_issue)" in section
        for key in ("labels.needs_shaping", "labels.needs_decision", "labels.blocked_external"):
            assert key in body, key
        rank = _between(body, "#### 3c.", "#### 3d.")
        order = [
            "labels.needs_shaping",
            "labels.needs_decision",
            "labels.blocked_issue",
            "labels.blocked_external",
            "labels.needs_hands",
            "labels.needs_testing",
        ]
        positions = [rank.index(f"`{key}`") for key in order]
        assert positions == sorted(positions)


def test_write_issue_searches_duplicates_before_the_template():
    for body in skill():
        step0 = _between(body, "### 0. Does this exist already?", "### 1. Choose the template")
        assert "search/issues" in step0 and ".items[:5]" in step0  # open and closed, bounded
        assert "git grep -l" in step0 and 'origin/$B' in step0
        assert f"B=$({CONFIG} forge.default_branch)" in step0
        assert "duplicate? no — " in step0
        assert "git fetch origin \"$B\" || echo 'FETCH FAILED" in step0
        assert "duplicate? not measurable — fetch failed" in step0
        assert "exit ≥ 2" in step0  # exit 1 = zero hits, no error
        for choice in ("do not create", "add to", "create anyway"):
            assert choice in step0
        assert "gh issue list --json" not in step0 and "wontfix" not in step0


def test_pitch_has_section_not_yet_decided():
    for body in skill():
        pitch = _between(body, "### Pitch (Shape Up)", "## Title Standards")
        assert pitch.index("### heading:milestone_no_gos") < pitch.index(
            "### heading:milestone_not_yet_decided"
        )
        assert "### heading:milestone_dod" in pitch
        assert "→ #<nr>" in pitch  # outflow: an issue of its own, retrievable
        assert "/lighthouse-write-issue" in pitch
        assert "no timebox of its own" in pitch  # appetite stays the gatekeeper
        assert "wayfinder" not in pitch


def test_task_template_carries_the_acceptance_criteria_heading():
    for body in skill():
        task = _between(body, "### Task", "### Pitch (Shape Up)")
        assert "### heading:acceptance_criteria" in task


def test_write_issue_taxonomy_from_paths():
    """The label taxonomy lives in the file under `[paths] labels`; without it only the
    `[labels]` keys and the GitHub default labels."""
    for body in skill():
        assert f"{CONFIG} paths.labels" in body
        assert "only the `[labels]` keys and the GitHub default labels" in " ".join(body.split())
        assert f"{CONFIG} labels.location_prefix" in body
        assert f"{CONFIG} labels.area_prefix" in body
        for gone in ("intake", "nexus", "1-loc", "2-area", "3-blocked", "3-needs", "Corneel"):
            assert gone not in body, gone


def test_repo_and_board_from_config():
    for body in skill():
        assert f"R=$({CONFIG} forge.repo)" in body
        assert "src.controller.identity" not in body
        assert f"{CONFIG} forge.board.priority_field_id" in body
        assert "board: skipped — no [forge.board] configured" in body


def test_variants_differ_only_in_the_generated_line():
    claude, agents = skill()
    diff = [line for line in claude.splitlines() if line not in agents.splitlines()]
    assert diff == ['- Never include "Generated with Claude Code" in issues unless directly relevant']


def test_skill_is_english_and_detached():
    for body in skill():
        lower = body.lower()
        for word in (" je ", " het ", " niet ", " geen ", " een ", " wordt "):
            assert word not in lower, word
        assert not re.search(r"#\d{3,}", body)
        for gone in ("Beelink", "uv run", "docs/developer/"):
            assert gone not in body, gone


def test_write_issue_milestone_prefix_from_config():
    """A milestone title carries the repo prefix from `project.milestone_prefix`: a project board
    over several repos slices on milestone or on repo, never on both. Without the key there is no
    prefix, and the skill says so. The form after the prefix is project content."""
    for body in skill():
        section = _between(body, "## Milestones", "## The board")
        flat = " ".join(section.split())
        assert f"P=$({CONFIG} project.milestone_prefix)" in section
        assert "milestone prefix: none — no [project] milestone_prefix set" in flat
        assert "`<project.milestone_prefix> <name>`" in flat
        assert "[paths] labels" in flat  # the form after the prefix
        assert "at least about five issues" in flat
        assert "a milestone without DoD text in its description does not exist" in flat
        assert "gatekeeper" in flat
        assert "<area>: <descriptive name>" not in flat


def _bash_block(section: str) -> str:
    return re.search(r"```bash\n(.*?)```", section, re.S)[1]


# `gh` as it answers flags it does not know: `gh api --jq` takes one expression, no `--arg`.
_STRICT_GH = """#!/bin/sh
[ "$1" = api ] && [ "$3" = --paginate ] && [ $# -eq 3 ] || { echo "unknown flag: $3" >&2; exit 1; }
cat "$GH_PAGE"
"""


def test_invariant_check_runs_against_gh(tmp_path):
    """Step 6 runs as written: two blocking labels on one issue is reported, a clean one not."""
    from tests.pack._testkit import MINIMAL_TOML, install_core, tool_bin

    install_core(tmp_path, MINIMAL_TOML + '\n[labels]\nlocation_prefix = "loc-"\narea_prefix = "area-"\n')
    page = tmp_path / "page.json"
    page.write_text(
        '[{"number": 1, "labels": [{"name": "loc-a"}, {"name": "area-b"},'
        ' {"name": "needs-shaping"}, {"name": "needs-decision"}]},'
        ' {"number": 2, "labels": [{"name": "loc-a"}, {"name": "area-b"}]}]',
        encoding="utf-8",
    )
    path = tool_bin(tmp_path / "bin", ("python3", "jq", "sh", "cat"), stubs={"gh": _STRICT_GH})
    script = _bash_block(_between(skill()[0], "### 6. Check the invariants", "## Milestones"))
    import subprocess

    out = subprocess.run(
        ["/bin/bash", "-c", script],
        cwd=tmp_path,
        env={"PATH": str(path), "GH_PAGE": str(page), "LIGHTHOUSE_REPO_ROOT": str(tmp_path)},
        capture_output=True,
        text=True,
        check=False,
    )
    assert out.returncode == 0, out.stderr
    assert [line for line in out.stdout.splitlines() if line] == [
        '{"n":1,"loc":1,"area":1,"blk":2}'
    ]


def test_milestone_create_reads_its_own_prefix():
    """Shell variables do not survive between tool calls: the create block reads the prefix
    itself, under a name no other block reuses, and reports a missing one."""
    for body in skill():
        block = _bash_block(body[body.index("Creating goes through the REST API") :])
        assert f"MP=$({CONFIG} project.milestone_prefix)" in block
        assert "milestone prefix: none — no [project] milestone_prefix set" in block
        assert '-f title="${MP:+$MP }<name>"' in block
        assert "${P:+" not in body

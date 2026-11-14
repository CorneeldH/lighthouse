"""Contract test for the skill `lighthouse-close-issue`.

What lives only in skill text: step 0 waits for the merge, entry C runs only the evidence runner
on a fresh default branch and reopens fail-closed, the released issues are checked against the
lane `lighthouse-unblock-dependents.yml` (decision 29), and what stays in paia (the release flow,
backlog-stand, the controller) runs only if present. Both variants are checked on content.

From paia's `test_issue_skills_contract.py` (the close-issue part).
"""

from __future__ import annotations

import re

from tests.pack._testkit import CORE, load_hook_lib

SKILL_DIR = CORE / "skills" / "close-issue"
_STRIP_COMMENTS = 'gsub("<!--[\\\\s\\\\S]*?-->"; "")'


def skill() -> tuple[str, str]:
    return tuple(
        (SKILL_DIR / variant / "SKILL.md").read_text(encoding="utf-8")
        for variant in ("claude", "agents")
    )


def _entry_c(body: str) -> str:
    c = body[body.index("## Entry C") :]
    return c[: c.index("\n## ", 1)]


def _section(body: str, heading: str) -> str:
    s = body[body.index(heading) :]
    return s[: s.index("\n## ", 1)]


def test_close_issue_reports_released_issues():
    for body in skill():
        assert "dependencies/blocking" in body
        assert "--remove-label <labels.blocked_issue>" in body
        assert "lighthouse config labels.blocked_issue" in body


def test_close_issue_finds_lighthouse_workflow():
    """Decision 29: the lane is installed as lighthouse-unblock-dependents.yml, and the skill
    looks for its run under that name."""
    for body in skill():
        assert "--workflow lighthouse-unblock-dependents.yml" in body
        assert "gh workflow run lighthouse-unblock-dependents.yml -f issue=<nr>" in body
        assert re.search(r"(?<!lighthouse-)unblock-dependents\.yml", body) is None
        assert 'startswith("Released lane after this close:")' in body


def test_close_issue_bot_from_config():
    for body in skill():
        released = _section(body, "### 6.")
        assert "lighthouse config ci.bot_login" in released
        assert "skipped" in released


def test_step_zero_waits_for_merge():
    for body in skill():
        assert (
            "waits for merge of #<pr>; run /lighthouse-close-issue <nr> again after the merge"
            in body
        )
        assert body.index("## Step 0") < body.index("## Entry B")


def _b2a(body: str) -> str:
    s = body[body.index("### B2a.") :]
    return s[: s.index("\n### ", 1)]


def test_step_zero_stops_on_an_open_refs_pr():
    for body in skill():
        s0 = _section(body, "## Step 0")
        for needle in (
            "gh pr list",
            "--state open",
            'startswith("issue-<nr>-")',
            _STRIP_COMMENTS,
            "--limit 9999",
            "dangerouslyDisableSandbox: true",
            "waits for merge of #<pr>; run /lighthouse-close-issue <nr> again after the merge",
        ):
            assert needle in s0, needle


def test_step_zero_open_pr_stop_is_scoped_to_an_open_issue():
    """A closed issue with some open PR still goes to entry C, not to `waits for merge`."""
    for body in skill():
        s0 = _section(body, "## Step 0")
        assert (
            "| issue `state` `OPEN` and (a node with `state` `OPEN` **or** a number from the "
            "second query) |"
        ) in s0
        assert "`OPEN` and a node with `state` `OPEN`, **or**" not in s0
        assert "| otherwise (issue `OPEN`, no open `Closes` PR and no open PR from the second query) |" in s0


def test_entry_b_runs_the_evidence_of_merged_refs_prs():
    for body in skill():
        b2a = _b2a(body)
        repo = "R=$(lighthouse config forge.repo)"
        assert repo in b2a
        assert b2a.index(repo) < b2a.index('--repo "$R"')
        assert b2a.count("mktemp -d") == 1
        for needle in (
            "--state merged",
            "mergedAt",
            "sort_by(.mergedAt) | reverse",
            "(?m)^### Evidence #<nr>",
            'startswith("issue-<nr>-")',
            _STRIP_COMMENTS,
            "--limit 9999",
            "### Evidence #<nr>",
            "{ lighthouse evidence run --issue <nr>",
            "git worktree add --detach",
            "lighthouse config forge.default_branch",
            "only the row of the latest merged PR",
            "the number the AC cell starts with",
            "`demonstrably done`",
            "`not`",
            "not measurable from here",
        ):
            assert needle in b2a, needle


def test_b2a_sits_between_b2_and_b3():
    for body in skill():
        assert body.index("### B2.") < body.index("### B2a.") < body.index("### B3.")


def test_b2a_per_pr_body_fetch_runs_outside_the_sandbox():
    for body in skill():
        b2a = _b2a(body)
        fetch = b2a.index("gh pr view <pr> --json body")
        assert "dangerouslyDisableSandbox: true" in " ".join(b2a[fetch - 120 : fetch].split())


def test_b2a_never_pastes_body_text_into_a_command():
    for body in skill():
        b2a = _b2a(body)
        assert "--body-file" in b2a
        assert "never paste" in b2a


def test_b7_includes_b2a():
    for body in skill():
        b7 = body[body.index("### B7.") :]
        b7 = b7[: b7.index("\n## ", 1)]
        assert "B2a" in b7


def test_entry_c_runs_only_the_runner():
    for body in skill():
        assert "{ lighthouse evidence run --issue <nr>" in body
        assert "git worktree add --detach" in body
        assert "lighthouse config forge.default_branch" in _entry_c(body)


def test_entry_c_reopens_fail_closed():
    for body in skill():
        assert "gh issue reopen" in body
        assert "python3 .lighthouse/scripts/board_start.py --reopen <nr>" in body
        assert "issue reopened, board not updated" in body
        assert "heading:evidence_closes_without_block" in body
        assert "Evidence outside the allowlist" in body
        assert "lighthouse headings evidence_holds" in body


def test_close_issue_board_skip():
    """Without `[forge.board]` board_start says `board: skipped` (rc 0, decision 24): that is
    no failure and is never reported as one."""
    for body in skill():
        c = _entry_c(body)
        assert "board: skipped" in c
        assert "is no error" in c
        assert "FOUT" not in body
        skip = next(p for p in re.split(r"\n\s*\n", c) if "board: skipped" in p)
        assert "issue reopened, board not updated" not in skip


def test_entry_c_reports_an_unmeasurable_run_loudly():
    for body in skill():
        c = _entry_c(body)
        assert "COULD NOT CHECK" in c
        assert "unknown" in c


def test_entry_c_never_closes_itself():
    for body in skill():
        assert "gh issue close" not in _entry_c(body)


def test_entry_c_does_not_paste_runner_output_in_a_quoted_line():
    for body in skill():
        c = _entry_c(body)
        assert "run.txt" in c
        assert 'gh issue reopen <nr> --repo "$R" --comment "$(cat <scratch>/run.txt)"' in c
        assert "never paste runner output or body text into a quoted command line" in c


def test_entry_c_catches_the_exit_code_before_the_worktree_remove():
    for body in skill():
        c = _entry_c(body)
        assert "rc=$?" in c
        assert c.index("rc=$?") < c.index("git worktree remove")
        assert "run.err" in c


def test_entry_c_reopens_only_on_a_recognised_outcome():
    for body in skill():
        c = _entry_c(body)
        assert "starts with `heading:evidence_closes_without_block`" in c
        assert "heading:evidence_table_head" in c
        recognised = c[c.index("### C3") :]
        assert "argparse" in recognised
        assert "traceback" in recognised


def test_entry_c_measures_the_pr_that_actually_closed_the_issue():
    for body in skill():
        step0 = body[body.index("## Step 0") : body.index("## Three entries")]
        assert "timelineItems(itemTypes:[CLOSED_EVENT],last:1)" in step0
        assert "closer{__typename ... on PullRequest{number}}" in step0
        c1 = _entry_c(body)
        c1 = c1[c1.index("### C1") : c1.index("### C2")]
        assert "`closer`" in c1
        assert "closed without a closing PR — no after-merge check" in c1


def test_entry_c_scratch_is_fresh_and_chained():
    for body in skill():
        c = _entry_c(body)
        assert "scratch=$(mktemp -d)" in c
        assert 'git worktree add --detach "$scratch/after-merge-<nr>" "origin/$B" &&' in c
        assert c.index("scratch=$(mktemp -d)") < c.index("lighthouse evidence run")
        assert "no `rc=` line" in c


def test_entry_c_exit_4_writes_a_comment_file():
    for body in skill():
        c = _entry_c(body)
        assert "> <scratch>/comment.md" in c
        assert 'gh issue comment <nr> --repo "$R" --body-file <scratch>/comment.md' in c


def test_cleanup_reads_the_audit_token():
    for body in skill():
        cleanup = _section(body, "## Clean up the worktree")
        assert "python3 .lighthouse/scripts/worktree_audit.py --issue <nr>" in cleanup
        assert "`cleanup: yes`" in cleanup and "`cleanup: no — <reason>`" in cleanup


def test_cleanup_asks_no_question():
    for body in skill():
        cleanup = _section(body, "## Clean up the worktree")
        assert "AskUserQuestion" not in cleanup
        assert "after confirmation" not in cleanup


def test_cleanup_runs_headless_too():
    for body in skill():
        assert "never clean up" not in body
        assert "you do not clean up yourself" not in body
        assert "Before that close comment, run *Clean up the worktree*" in body


def test_cleanup_keeps_the_forbidden_lines():
    for body in skill():
        cleanup = _section(body, "## Clean up the worktree")
        assert "❌ `--force`, `git worktree unlock` or `git push origin --delete`" in cleanup


def test_cleanup_goes_on_after_a_refusal():
    for body in skill():
        assert "go on with the next branch" in _section(body, "## Clean up the worktree")


def test_cleanup_shows_evidence_per_branch():
    for body in skill():
        cleanup = _section(body, "## Clean up the worktree")
        assert "git -C <main checkout> branch --list <branch>" in cleanup
        assert "git -C <main checkout> worktree list" in cleanup


def test_cleanup_never_removes_own_cwd():
    for body in skill():
        cleanup = _section(body, "## Clean up the worktree")
        assert "The worktree that holds your own cwd is never cleaned up" in cleanup


def test_commit_urls_from_forge_repo():
    for body in skill():
        assert "https://github.com/<forge.repo>/commit/" in body
        assert "identity" not in body


def test_release_flow_only_if_present():
    for body in skill():
        release = body[body.index("### Step 4.") : body.index("### Step 5.")]
        assert "test -e scripts/release.py" in release
        assert "only if present" in release.splitlines()[0]


def test_variants_differ_in_the_instruction_file():
    claude, agents = skill()
    assert "(README, CLAUDE.md, etc.)" in claude
    assert "(README, AGENTS.md, etc.)" in agents
    assert [line for line in claude.splitlines() if line not in agents.splitlines()] == [
        "- [ ] Documentation updated (README, CLAUDE.md, etc.)"
    ]


def test_milestone_close_keeps_the_title_form():
    """The form of a milestone title is project content (`project.milestone_prefix`, the file
    under `[paths] labels`): closing copies the title and appends ` - DONE`, no fixed form."""
    for body in skill():
        step = _section(body, "### Step 3. Close the milestone")
        assert "title='<title> - DONE'" in step
        assert "<area>: <name>" not in step


def test_area_of_a_milestone_after_the_prefix():
    """Since `project.milestone_prefix`, the first word of a milestone title is the repo prefix:
    the area is the part before `:` after that prefix."""
    for body in skill():
        step = _section(body, "### Step 5. Update the near goal of the area")
        flat = " ".join(step.split())
        assert "the title prefix of the milestone" not in flat
        assert "the part before `:` after `project.milestone_prefix`" in flat


def _refs_regex(section: str) -> re.Pattern[str]:
    """The `Refs` regex of a jq filter in the skill, as Python sees it, for issue 147."""
    m = re.search(r'test\("([^"]*Refs[^"]*)"; "i"\)', section)
    assert m, "no case-insensitive Refs test in the jq filter"
    return re.compile(m.group(1).replace("\\\\", "\\").replace("<nr>", "147"), re.IGNORECASE)


_TEMPLATE = (CORE / "templates" / "pull_request_template.md").read_text(encoding="utf-8")


def test_the_refs_filters_find_what_the_gate_parser_finds():
    """A PR in the jq filters is a PR the gate counts as `Refs #147`: comments out, any case."""
    gate = load_hook_lib("pr_body_gate")
    bodies = (
        _TEMPLATE.replace("#123", "#147"),
        "refs #147 — lowercase reason",
        "Refs\t#147: reason after a tab",
        "Refs #1470 — another issue",
        "Refs #147_x — not this number",
        "see a/Refs #147 in a path",
        "xRefs #147 glued to a word",
        "<!-- Refs #147 — only in a comment -->\nNo issue: nothing here at all",
    )
    for body in skill():
        for section in (_section(body, "## Step 0"), _b2a(body)):
            assert _STRIP_COMMENTS in section
            rx = _refs_regex(section)
            for text in bodies:
                found = bool(rx.search(re.sub(r"<!--[\s\S]*?-->", "", text)))
                assert found == (147 in gate.refs_targets(text)), text

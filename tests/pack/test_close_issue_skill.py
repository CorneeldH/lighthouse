"""Contract test for the skill `lighthouse-close-issue`.

What lives only in skill text: step 0 waits for the merge, entry C runs only the evidence runner
on a fresh default branch and reopens fail-closed, the released issues are checked against the
lane `lighthouse-unblock-dependents.yml` (decision 29), and what stays in paia (the release flow,
backlog-stand, the controller) runs only if present. Both variants are checked on content.

From paia's `test_issue_skills_contract.py` (the close-issue part).
"""

from __future__ import annotations

import re

from tests.pack._testkit import CORE

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
        released = _section(body, "### 6.")
        assert "lighthouse forge issue blocking <nr>" in released
        assert "lighthouse forge issue view <nr> --field closed_at" in released
        assert "lighthouse forge issue view <nr> --comments" in released
        assert "lighthouse forge issue view <m> --field labels" in released
        assert "lighthouse config labels.blocked_issue" in body


def test_without_a_lane_the_session_releases():
    """Spec §1: without the lane (GitLab, or GitHub without a bot) the session runs
    `unblock-dependents` itself, reports its summary literally, and never says "none" on exit 1."""
    for body in skill():
        released = _section(body, "### 6.")
        flat = " ".join(released.split())
        assert "lighthouse unblock-dependents --issue <nr>" in released
        assert "report its summary literally" in flat
        assert "released issues not read" in released
        assert "never \"none\"" in flat
        assert "not after a reopening" in flat
        assert "--remove-label" not in body


def test_without_a_lane_every_branch_that_stays_closed_releases():
    """Spec §1: every branch in which the issue is and stays closed releases, a reopening not."""
    for body in skill():
        c = _entry_c(body)
        c1 = c[c.index("### C1") : c.index("### C2")]
        c3 = c[c.index("### C3") :]
        assert "step 6" in c1
        exit0 = next(line for line in c3.splitlines() if line.startswith("| `0` |"))
        exit4 = next(line for line in c3.splitlines() if line.startswith("| `4` |"))
        assert "step 6" in exit0 and "step 6" in exit4
        could_not = c3[c3.index("**Exit `3`") :]
        assert "step 6" in could_not
        reopen = c3[c3.index("**Exit `1` or `2`") : c3.index("**Exit `3`")]
        assert "step 6" not in reopen
        b7 = _section(body, "### B7.")
        assert "steps 4–6 of entry A" in b7


def test_close_issue_finds_lighthouse_workflow():
    """Decision 29: the lane is installed as lighthouse-unblock-dependents.yml, and the skill
    looks for its run under that name."""
    for body in skill():
        assert "--workflow lighthouse-unblock-dependents.yml" in body
        assert "gh workflow run lighthouse-unblock-dependents.yml -f issue=<nr>" in body
        assert "gh run list --workflow lighthouse-unblock-dependents.yml" in body
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
            "lighthouse forge pr list --state open",
            '.head_ref | startswith("issue-<nr>-")',
            _STRIP_COMMENTS,
            "dangerouslyDisableSandbox: true",
            "waits for merge of #<pr>; run /lighthouse-close-issue <nr> again after the merge",
        ):
            assert needle in s0, needle


def test_step_zero_open_pr_stop_is_scoped_to_an_open_issue():
    """A closed issue with some open PR still goes to entry C, not to `waits for merge`."""
    for body in skill():
        s0 = _section(body, "## Step 0")
        assert (
            "| issue `state` `open` and (a change with `closes` `true` and `state` `open` **or** a "
            "number from the second query) |"
        ) in s0
        assert "| issue `state` `closed` | **entry C** |" in s0
        assert "| otherwise (issue `open`, no open `Closes` PR and no open PR from the second query) |" in s0


def test_entry_b_runs_the_evidence_of_merged_refs_prs():
    for body in skill():
        b2a = _b2a(body)
        assert b2a.count("mktemp -d") == 1
        for needle in (
            "lighthouse forge pr list --state merged",
            "sort_by(.merged_at) | reverse",
            "(?m)^### Evidence #<nr>",
            '.head_ref | startswith("issue-<nr>-")',
            _STRIP_COMMENTS,
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
        fetch = b2a.index("lighthouse forge pr view <pr> --field body")
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
        assert "lighthouse forge issue reopen <nr> --body-file <scratch>/run.txt" in body
        assert "lighthouse board-start --reopen <nr>" in body
        assert 'lighthouse forge issue status <nr> "In progress"' in body
        assert "issue reopened, board not updated" in body
        assert "heading:evidence_closes_without_block" in body
        assert "Evidence outside the allowlist" in body
        assert "lighthouse headings evidence_holds" in body


def test_close_issue_board_skip():
    """Without `[forge.board]` board-start says `board: skipped` (rc 0, decision 24): that is
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
        assert re.search(r"issue close\b", _entry_c(body)) is None


def test_entry_c_does_not_paste_runner_output_in_a_quoted_line():
    for body in skill():
        c = _entry_c(body)
        assert "run.txt" in c
        assert "lighthouse forge issue reopen <nr> --body-file <scratch>/run.txt" in c
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
        assert "lighthouse forge issue changes <nr>" in step0
        c1 = _entry_c(body)
        c1 = c1[c1.index("### C1") : c1.index("### C2")]
        assert "`closer`" in c1
        assert "../lighthouse-forge/<kind>.md §close-issue" in c1
        assert "closed without a closing PR — no after-merge check" in c1


def test_entry_c_verifies_the_closer_in_this_repo():
    """`closer` is a bare number: a closer in another repository would read as a PR here. C1
    checks it with `pr view` before C2 measures it."""
    for body in skill():
        c = _entry_c(body)
        c1 = c[c.index("### C1") : c.index("### C2")]
        flat = " ".join(c1.split())
        assert "lighthouse forge pr view <pr> --field state" in c1
        assert "`merged`" in c1
        assert "another repository" in flat


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
        assert "lighthouse forge issue comment <nr> --body-file <scratch>/comment.md" in c


def test_cleanup_reads_the_audit_token():
    for body in skill():
        cleanup = _section(body, "## Clean up the worktree")
        assert "lighthouse worktree-audit --issue <nr>" in cleanup
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


def test_commit_urls_per_forge():
    """Spec §4: the commit link comes from the forge reference, not a fixed github.com."""
    for body in skill():
        assert "https://github.com/" not in body
        assert "<commit-url>" in body
        for heading in ("### 5. Close with a correct commit link", "## Commit Reference Format"):
            assert "../lighthouse-forge/<kind>.md §close-issue" in _section(body, heading)
        assert "identity" not in body
    forge = CORE / "skills" / "forge"
    github = _section((forge / "github.md").read_text(encoding="utf-8") + "\n## end", "## §close-issue")
    gitlab = _section((forge / "gitlab.md").read_text(encoding="utf-8") + "\n## end", "## §close-issue")
    assert "https://github.com/<forge.repo>/commit/<sha>" in github
    assert "https://<forge.host, else gitlab.com>/<forge.repo>/-/commit/<sha>" in gitlab


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
        assert "lighthouse forge milestone edit '<title>' --state closed --title '<title> - DONE'" in step
        assert "lighthouse forge issue view <number> --field milestone.title" in body
        assert "<area>: <name>" not in step


def test_area_of_a_milestone_after_the_prefix():
    """Since `project.milestone_prefix`, the first word of a milestone title is the repo prefix:
    the area is the part before `:` after that prefix."""
    for body in skill():
        step = _section(body, "### Step 5. Update the near goal of the area")
        flat = " ".join(step.split())
        assert "the title prefix of the milestone" not in flat
        assert "the part before `:` after `project.milestone_prefix`" in flat

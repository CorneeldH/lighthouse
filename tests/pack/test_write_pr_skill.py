"""Contract of the skill `lighthouse-write-pr`.

The skill is the counterpart of `.claude/hooks/lighthouse/pr-create-gate.sh`: it writes the PR
body (`Closes #N` + Evidence table, `Refs #N — <reason>` or `No issue: <reason>`) the hook
enforces. These tests bind the skill text to the hook, to the CLI of `evidence.py` and to its
allowlist constants. The template itself is tested in `test_pr_template.py`.
"""

import re
import shutil

from tests.pack._testkit import CORE, load_hook_lib

SKILL = CORE / "skills" / "write-pr" / "SKILL.md"
TEXT = SKILL.read_text(encoding="utf-8")


def front(text: str) -> str:
    """The frontmatter between the first two `---` lines."""
    return text.split("---\n", 2)[1]


def _section(body: str, heading: str) -> str:
    s = body[body.index(heading) :]
    return s[: s.index("\n## ", 1)]


def test_one_shared_skill_file():
    assert not (CORE / "skills" / "write-pr" / "claude").exists()


def test_binding_hard_under_metadata():
    assert re.search(r"^metadata:\n(?:  .*\n)*  binding: hard$", front(TEXT), re.MULTILINE)


def test_description_not_cut_off():
    """An unquoted ` #N` is a YAML comment and cuts off the NOTE clause."""
    line = next(line for line in front(TEXT).splitlines() if line.startswith("description:"))
    value = line[len("description:") :].strip()
    assert value[0] == "'" and value.endswith("is lighthouse-fix-pr.'")


def test_skill_runs_before_pr():
    assert "--before-pr" in TEXT and "--voor-pr" not in TEXT


def test_names_the_hook():
    """Check 8 binds `binding: hard` to the hook through this name."""
    assert ".claude/hooks/lighthouse/pr-create-gate.sh" in TEXT


def test_after_pr_is_gone():
    """A criterion that waits on the PR is `after merge`; write-pr never measures through the PR."""
    assert "`after merge`" in TEXT
    for gone in ("`after PR`", "evidence_after_pr", "closedByPullRequestsReferences"):
        assert gone not in TEXT, gone


def test_verify_pr_evidence_by_path():
    """The evidence CLI runs as a file on its installed path (design choice 6)."""
    assert "python3 .lighthouse/lib/lighthouse_lib/evidence.py check" in TEXT
    assert "python3 .lighthouse/lib/lighthouse_lib/evidence.py run" in TEXT
    assert ".claude/hooks/lib/" not in TEXT


def test_allowlist_table_covers_the_code():
    """The skill table names every command, every refusal and every flag from the allowlist
    constants of `evidence.py`."""
    ev = load_hook_lib("evidence")
    for cmd in (
        "`grep`",
        "`git grep`",
        "`git log`",
        "`uv run pytest`",
        "`gh api <path>`",
        "`gh api graphql`",
        "`gh pr view`",
    ):
        assert cmd in TEXT
    for refused in ("--web", "--output", "-H", "%G"):
        assert refused in TEXT
    flags = (
        ev.GIT_LOG_FLAGS
        | ev.GIT_LOG_VAL
        | ev.PYTEST_FLAGS
        | ev.GH_API_VAL
        | ev.GH_VIEW_FLAGS
        | ev.GH_VIEW_VAL
        | ev.LS_FLAGS
        | ev.WC_FLAGS
        | ev.CAT_FLAGS
        | ev.GIT_GREP_VAL
    )
    section = TEXT[TEXT.index("## Evidence allowlist") :]
    section = section[: section.index("```")]  # only the table and its prose
    in_code = {re.sub(r"[`=]", "", v) for v in re.findall(r"`[^`]*`", section)}
    tokens = {t for cell in in_code for t in re.split(r"[\s/,]+", cell)}
    missing = sorted(v for v in flags if v not in tokens)
    assert not missing, f"flags from evidence.py missing in the skill table: {missing}"
    for sub in ev.GIT_SUBS:
        assert f"git {sub}" in TEXT


def test_pytest_dir_comes_from_config():
    section = _section(TEXT, "## Evidence allowlist")
    assert "lighthouse config test_gate.evidence_test_dir" in section
    assert "tests/unit" not in TEXT


def test_example_row_holds_against_the_template(tmp_path):
    """Row 1 of the example block, copied, must give `holds` in a repo with the installed
    template (grep, no network)."""
    gate = load_hook_lib("pr_body_gate")
    ev = load_hook_lib("evidence")
    (tmp_path / ".github").mkdir()
    shutil.copyfile(
        CORE / "templates" / "pull_request_template.md",
        tmp_path / ".github" / "pull_request_template.md",
    )
    # The example itself contains `## Related issues`: cut up to the next real heading.
    block = TEXT[TEXT.index("## Shape of the evidence block") : TEXT.index("## Counterpart")]
    rows = gate.evidence_rows(block, 1091)
    assert isinstance(rows, list), rows
    assert ev.run_row(rows[0], str(tmp_path), gh_ok=False)[0] == ev.HOLDS


def test_description_names_refs_not_closes():
    desc = next(l for l in front(TEXT).splitlines() if l.startswith("description:"))
    assert "`Refs #N — <reason>`" in desc
    assert "`Closes #N`" not in desc


def test_v3_always_writes_refs():
    v3 = " ".join(_section(TEXT, "## V3.").split())
    for needle in (
        "always `Refs #N — <reason>`, never `Closes`",
        "/lighthouse-close-issue",
        "one row per AC that is `demonstrably done`",
        "starts with the number of the criterion",
        "no table",
    ):
        assert needle in v3, needle
    assert "Mixing is allowed" not in TEXT


def test_example_block_is_a_valid_refs_body():
    """The example is a body write-pr could produce: `Refs` with a table, and the gate lets it
    through without asking the forge for the criteria count."""
    gate = load_hook_lib("pr_body_gate")
    block = TEXT[TEXT.index("## Shape of the evidence block") : TEXT.index("## Counterpart")]
    assert "Refs #1091 — " in block and "Closes #1091" not in block

    def no_forge(nr):
        raise AssertionError("a Refs body must not count criteria")

    assert gate.check_body(block, ac_count=no_forge).blockers == []


def test_v2b_runs_in_the_build_session():
    v2b = " ".join(_section(TEXT, "## V2b.").split())
    assert "build session" in v2b
    assert "`run` or `classify`" in v2b
    assert 'record --skip "build session ran no review"' in v2b
    # The verdict table itself carries the override (the prose above it is easy to skip).
    table = " ".join(v2b[v2b.index("| `verdict` | Do |") :].split())
    assert "In a run started with `/lighthouse-write-pr <nr>`" in table
    assert 'record --skip "build session ran no review"' in table
    assert "the write-pr run never reviews" in v2b


def test_v5_hands_off_to_close_issue():
    v5 = _section(TEXT, "## V5.")
    for needle in (
        "/lighthouse-write-handoff",
        "`/lighthouse-close-issue <nr>`",
        "after the merge",
        "(issue #<nr>, controller-run)",
        "LIGHTHOUSE_CONTROLLER_RUN",
        "no handoff",
        "with `No issue:` V5 is skipped",
    ):
        assert needle in v5, needle
    assert TEXT.index("## V4.") < TEXT.index("## V5.")


def test_v4_gives_a_title_and_writes_the_body_separately():
    v4 = _section(TEXT, "## V4.")
    assert "gh pr create --repo <forge.repo> --title '<title>' --body-file <path>" in v4
    assert "lighthouse config forge.repo" in v4
    assert "Write tool" in v4
    assert "lighthouse plan-review-record pr-open-points <path>..." in v4


def test_dod_does_not_run_the_suite_by_hand_in_claude_code():
    dod = _section(TEXT, "## DoD")
    assert ".claude/hooks/lighthouse/run.sh commit-test-gate" in dod
    # #80: the gate tests only the files of each commit; CI on the PR is the suite evidence.
    flat = " ".join(dod.split())
    assert "proves only the files of each commit" in flat
    assert "`suite: CI`" in flat
    assert "last outcome of the commit gate" not in flat
    assert "Outside Claude Code" in dod
    assert dod.index("Outside Claude Code") < dod.index("lighthouse config test_gate.command")


def test_gh_view_demands_a_number():
    assert "exactly one number or github.com URL" in _section(TEXT, "## Evidence allowlist")


def test_git_grep_row_names_what_evidence_refuses():
    """`git grep` knows only `-e` as a value option; the rest of grep's value options not."""
    ev = load_hook_lib("evidence")
    [row] = [r for r in TEXT.splitlines() if r.startswith("| `git grep` |")]
    _, allowed, refused = (c.strip() for c in row.strip("|").split(" | "))
    assert "as `grep`" not in allowed
    for option in ev.GREP_VAL - ev.GIT_GREP_VAL:
        assert f"`{option}`" in refused


def test_outcomes_are_headings():
    """The runner writes its outcomes in the project language (headings.toml)."""
    for key in ("evidence_holds", "evidence_table_head"):
        assert f"heading:{key}" in TEXT
    assert "COULD NOT CHECK" in TEXT
    assert "klopt" not in TEXT and "KON NIET" not in TEXT


def test_changelog_fragment_is_opt_in_and_english():
    section = _section(TEXT, "## Changelog fragment")
    for needle in ("if present", "changelog.d/.lighthouse", "changelog.d/<branch>.md", "-<n>",
                   "--diff-filter=A", "writes nothing twice",
                   "added", "changed", "fixed", "removed", "internal", "English", "before `gh pr create`"):
        assert needle in section, needle


def test_roborev_step_sits_between_v2_and_the_verdicts():
    assert TEXT.index("## V2.") < TEXT.index("## V2b. Roborev review") < TEXT.index("## Verdicts")


def test_roborev_step_carries_the_contract():
    s = _section(TEXT, "## V2b.")
    for needle in (
        "roborev_gate.py gate",
        "git fetch origin",
        "timeout 540 roborev ci review --agent codex --ref",
        "600000",
        "dangerouslyDisableSandbox: true",
        "superpowers:receiving-code-review",
        "roborev: <path>:<line> (<severity>)",
        "roborev_gate.py fixed",
        "roborev_gate.py record",
        "classify",
        "No issue",
        "git ls-remote --exit-code origin <branch>",
        "git push origin HEAD",
        "--range <range>",
        "Any other non-zero exit",
    ):
        assert needle in s, needle
    assert "--comment" not in s and "run_in_background" not in s


def test_v4_writes_the_marker_and_posts_the_comment():
    v4 = _section(TEXT, "## V4.")
    for needle in ("roborev_gate.py body-line", "roborev_comment.py render", "--bron lokaal", "gh pr comment"):
        assert needle in v4, needle


def test_documents_of_the_linked_issues():
    s = " ".join(_section(TEXT, "## Documents of the linked issues").split())
    assert "lighthouse config paths.labels" in s
    assert "every `Closes #N` and every `Refs #N`" in s
    assert "gh issue view" in s and "--json labels" in s
    assert "does the branch deliver" in s
    assert "before `gh pr create`" in s
    assert "nothing twice" in s  # re-run
    assert "no note in the body" in s
    assert "With `Closes #N`: yes" in s and "With `Refs #N`: judge" in s
    assert "skip this section" in s
    assert "rc 1 (not set)" in s and "rc 2: the configuration is broken; stop" in s
    assert "planned" not in s
    for named in ("loop.json", "`loop`", "docs/site"):
        assert named not in s, named
    assert TEXT.index("## V3. Choose the form") < TEXT.index("## Documents of the linked issues") \
        < TEXT.index("## Changelog fragment")

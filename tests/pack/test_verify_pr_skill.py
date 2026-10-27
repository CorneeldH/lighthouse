"""Contract of the skill `lighthouse-verify-pr`.

The skill is the counterpart of `.claude/hooks/lighthouse/pr-create-gate.sh`: it writes the PR
body (`Closes #N` + Evidence table, `Refs #N — <reason>` or `No issue: <reason>`) the hook
enforces. These tests bind the skill text to the hook, to the CLI of `evidence.py` and to its
allowlist constants. The template itself is tested in `test_pr_template.py`.
"""

import re
import shutil

from tests.pack._testkit import CORE, load_hook_lib

SKILL = CORE / "skills" / "verify-pr" / "SKILL.md"
TEXT = SKILL.read_text(encoding="utf-8")


def front(text: str) -> str:
    """The frontmatter between the first two `---` lines."""
    return text.split("---\n", 2)[1]


def _section(body: str, heading: str) -> str:
    s = body[body.index(heading) :]
    return s[: s.index("\n## ", 1)]


def test_one_shared_skill_file():
    assert not (CORE / "skills" / "verify-pr" / "claude").exists()


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


def test_names_both_new_verdicts():
    assert "`after merge`" in TEXT and "`after PR`" in TEXT


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
    assert "config.py test_gate.evidence_test_dir" in section
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


def test_v4_gives_a_title_and_writes_the_body_separately():
    v4 = _section(TEXT, "## V4.")
    assert "gh pr create --title '<title>' --body-file <path>" in v4
    assert "Write tool" in v4
    assert "python3 .lighthouse/scripts/plan_review_record.py pr-open-points <path>..." in v4


def test_dod_does_not_run_the_suite_by_hand_in_claude_code():
    dod = _section(TEXT, "## DoD")
    assert ".claude/hooks/lighthouse/commit-tests.sh" in dod
    # #80: the gate tests only the files of each commit; CI on the PR is the suite evidence.
    flat = " ".join(dod.split())
    assert "proves only the files of each commit" in flat
    assert "`suite: CI`" in flat
    assert "last outcome of the commit gate" not in flat
    assert "Outside Claude Code" in dod
    assert dod.index("Outside Claude Code") < dod.index("config.py test_gate.command")


def test_gh_view_demands_a_number_and_after_pr_measures_through_the_issue():
    assert "exactly one number or github.com URL" in _section(TEXT, "## Evidence allowlist")
    v3 = _section(TEXT, "## V3.")
    assert "closedByPullRequestsReferences" in v3
    assert "`gh pr view` without a number" in v3


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
    for key in ("evidence_holds", "evidence_after_pr_unmeasured", "evidence_table_head"):
        assert f"heading:{key}" in TEXT
    assert "COULD NOT CHECK" in TEXT
    assert "klopt" not in TEXT and "KON NIET" not in TEXT

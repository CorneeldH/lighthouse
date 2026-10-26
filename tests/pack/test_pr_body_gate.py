"""`pack/core/lib/pr_body_gate.py` (#1091): forms, Evidence table, AC count.

The lib is the pure logic under `pr-create-gate.sh`: which form (`Closes` with
an Evidence block, `Refs` with a reason, `No issue`) the PR body has, whether the
table is right and whether the number of rows covers the number of AC of the
issue. No network: `fetch_ac_count` gets a stub `gh` first on `PATH` here.
"""

import io
import os
import subprocess
import sys
import time

import pytest

from tests.pack._testkit import (
    INSTALLED_LIB,
    ORIGIN,
    forge_worktree,
    git,
    init_repo,
    load_hook_lib,
    tool_bin,
)

gate = load_hook_lib("pr_body_gate")

ROWS1 = [("1. een", "`grep -F a f`", "a")]
ROWS2 = [*ROWS1, ("2. twee", "`ls`", "f")]
ROWS3 = [*ROWS2, ("3. drie", "`true`", "ok")]

ISSUE_WITH_THREE_AC = (
    "## Context\n\ntekst\n\n"
    "### Acceptatiecriteria\n- [ ] a\n- [x] b\n- [X] c\n\n"
    "### Definition of Done\n- [ ] d\n"
)


def closes_body(nr: int, rows: list[tuple[str, str, str]]) -> str:
    """A body with `Closes #nr` and an Evidence table with the given rows."""
    table = "\n".join(f"| {ac} | {cmd} | {res} |" for ac, cmd, res in rows)
    return (
        f"## Related issues\n\nCloses #{nr}\n\n"
        f"### Evidence #{nr}\n\n"
        f"| AC | Command | Result |\n|---|---|---|\n{table}\n"
    )


# --- Closes ---------------------------------------------------------------


@pytest.mark.parametrize(
    "word",
    [
        "close",
        "Closes",
        "closed",
        "fix",
        "FIXES",
        "fixed",
        "resolve",
        "resolves",
        "Resolved",
        "Closes:",
    ],
)
def test_all_closing_words_count(word):
    assert gate.closes_targets(f"{word} #12") == [12]


@pytest.mark.parametrize(
    "target",
    [
        "#12",
        "Acme/widgets#12",
        "https://github.com/Acme/widgets/issues/12",
    ],
)
def test_all_target_forms_count(target):
    assert gate.closes_targets(f"Closes {target}") == [12]


def test_other_repo_does_not_count():
    assert gate.closes_targets("Closes other/repo#12") == []


@pytest.fixture
def other_identity(monkeypatch, tmp_path):
    toml = tmp_path / "other.toml"
    toml.write_text('[forge]\nkind = "github"\nrepo = "Other/repo"\n')
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(toml))


def test_repo_comes_from_lighthouse_toml(other_identity):
    """The repo comes from `lighthouse.toml`, not from a literal (#1445), on every call."""
    assert gate.repo() == "Other/repo"
    assert gate.closes_targets("Closes Other/repo#12") == [12]
    assert gate.closes_targets("Closes Acme/widgets#12") == []


def test_without_forge_repo(tmp_path, monkeypatch):
    """Without `forge.repo` only `#N` counts; the AC count reports that and the rest is still checked."""
    toml = tmp_path / "lighthouse.toml"
    toml.write_text('[project]\nlanguage = "en"\n')
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(toml))
    assert gate.repo() is None
    assert gate.closes_targets("Closes #4 and Closes x/y#5") == [4]
    assert gate.fetch_ac_count(4) == (None, "no forge.repo in lighthouse.toml")
    assert gate.in_scope(_pc(str(tmp_path), "x/y"), str(tmp_path))[0] == "toetsen"


def test_missing_toml_fails_loud(tmp_path):
    """Without a toml the lib fails loudly on a `gh pr create`; `pr-create-gate.sh` reports that as a blocker."""
    lib = INSTALLED_LIB / "pr_body_gate.py"
    gone = tmp_path / "weg.toml"
    proc = subprocess.run(
        [sys.executable, str(lib), str(tmp_path)],
        env={**os.environ, "LIGHTHOUSE_TOML": str(gone)},
        input="gh pr create --repo Acme/widgets --body 'Closes #1'",
        capture_output=True,
        text=True,
        check=False,
    )
    assert proc.returncode not in (0, 2, 3)
    assert "weg.toml" in proc.stderr
    assert "Traceback" not in proc.stderr


def test_template_comment_does_not_count():
    assert gate.closes_targets("<!-- Closes #123\n -->") == []


def test_closes_unique_and_in_order():
    body = "Closes #9\nfixes #3\nCloses #9\nResolves Acme/widgets#4"
    assert gate.closes_targets(body) == [9, 3, 4]


def test_closes_number_is_not_truncated():
    assert gate.closes_targets("Closes #123abc") == []


def test_strip_comments_crlf_and_comment():
    assert gate.strip_comments("a\r\n<!-- x\r\ny -->b") == "a\nb"


# --- Refs en No issue -----------------------------------------------------


def test_refs_with_reason():
    assert gate.refs_issues("Refs #7 — wacht op deploy Beelink") == [7]


@pytest.mark.parametrize("sep", ["—", "-", ":"])
def test_refs_separators(sep):
    assert gate.refs_issues(f"Refs #7 {sep} meting over zeven dagen") == [7]


def test_refs_reason_too_short():
    assert gate.refs_issues("Refs #7 — kort") == []


def test_refs_without_reason():
    assert gate.refs_issues("Refs #7") == []


def test_refs_in_comment_does_not_count():
    assert gate.refs_issues("<!-- Refs #7 — voorbeeld uit het template -->") == []


def test_refs_targets_without_reason_requirement():
    assert gate.refs_targets("Refs #7") == [7]
    assert gate.refs_targets("Refs #7 — kort\nRefs #8 — iets\nRefs #7") == [7, 8, 7]


def test_refs_targets_ignores_comment_and_other_numbers():
    assert gate.refs_targets("<!-- Refs #7 — x -->\nzie #7") == []
    assert gate.refs_targets("Refs #77") == [77]


def test_no_issue():
    assert gate.no_issue("No issue: alleen een typo in docs")


def test_no_issue_too_short():
    assert not gate.no_issue("No issue: typo")


# --- Evidence table -------------------------------------------------------


def test_evidence_valid():
    rows = gate.evidence_rows(closes_body(5, [("1. x", "`grep -F a f`", "a")]), 5)
    assert rows == [gate.Row("1. x", "`grep -F a f`", "a")]


def test_evidence_empty_cell():
    assert isinstance(gate.evidence_rows(closes_body(5, [("1", "`ls`", "")]), 5), str)


def test_evidence_command_without_backticks():
    assert isinstance(gate.evidence_rows(closes_body(5, [("1", "ls", "x")]), 5), str)


def test_evidence_wrong_header():
    body = (
        "Closes #5\n\n### Evidence #5\n\n"
        "| AC | Cmd | Result |\n|---|---|---|\n| 1 | `ls` | x |\n"
    )
    assert isinstance(gate.evidence_rows(body, 5), str)


def test_evidence_no_data_row():
    body = "Closes #5\n\n### Evidence #5\n\n| AC | Command | Result |\n|---|---|---|\n"
    assert isinstance(gate.evidence_rows(body, 5), str)


def test_evidence_heading_missing():
    assert isinstance(gate.evidence_rows("Closes #5", 5), str)


def test_evidence_heading_of_other_issue():
    assert isinstance(gate.evidence_rows(closes_body(6, ROWS1), 5), str)


def test_evidence_block_stops_at_first_non_pipe_line():
    body = closes_body(5, ROWS2) + "\ntekst\n| 3 | `ls` | x |\n"
    rows = gate.evidence_rows(body, 5)
    assert len(rows) == 2


def test_evidence_crlf():
    body = closes_body(5, ROWS1).replace("\n", "\r\n")
    assert len(gate.evidence_rows(body, 5)) == 1


def test_evidence_escaped_pipe_is_one_cell():  # Review Focus 3
    rows = gate.evidence_rows(
        closes_body(5, [("1", r"`grep -E 'a\|b' f`", "a")]),
        5,
    )
    assert len(rows) == 1
    assert rows[0].command == r"`grep -E 'a\|b' f`"


def test_evidence_too_many_cells():
    assert isinstance(
        gate.evidence_rows(closes_body(5, [("1", "`ls`", "a | b")]), 5), str
    )


# --- AC-telling -----------------------------------------------------------


def test_count_acs_counts_checkboxes():
    body = "### Acceptatiecriteria\n- [ ] a\n- [x] b\n### Definition of Done\n- [ ] c"
    assert gate.count_acs(body) == 2


@pytest.mark.parametrize("heading", ["## Acceptatiecriteria", "#### Acceptatiecriteria"])
def test_count_acs_heading_levels(heading):
    assert gate.count_acs(f"{heading}\n- [ ] a") == 1


def test_count_acs_no_heading():
    assert gate.count_acs("- [ ] a") is None


def test_count_acs_zero_checkboxes_is_none():  # Review Focus 2
    assert gate.count_acs("### Acceptatiecriteria\ntekst") is None


def test_count_acs_crlf():
    assert gate.count_acs(ISSUE_WITH_THREE_AC.replace("\n", "\r\n")) == 3


# --- check_body -----------------------------------------------------------


def test_check_body_closes_matches():
    v = gate.check_body(closes_body(5, ROWS3), lambda n: (3, ""))
    assert v.blockers == []
    assert v.notices == []


def test_check_body_ac_count_differs():
    v = gate.check_body(closes_body(5, ROWS2), lambda n: (3, ""))
    assert v.blockers
    assert "3" in v.blockers[0]
    assert "2" in v.blockers[0]


def test_check_body_fallback_notices_and_passes():
    v = gate.check_body(closes_body(5, ROWS2), lambda n: (None, "timeout na 5 s"))
    assert v.blockers == []
    assert v.notices == [
        (
            "pr-create-gate: AC count not measurable for #5 (timeout na 5 s)"
            " — structure checked only"
        )
    ]


def test_check_body_no_form_blocks():
    assert gate.check_body("## Related issues\n\nzie titel").blockers


def test_check_body_closes_without_block_blocks():
    assert gate.check_body("Closes #5", lambda n: (1, "")).blockers


def test_check_body_two_closes_one_block():  # Review Focus 5
    v = gate.check_body(closes_body(5, ROWS1) + "\nCloses #6", lambda n: (1, ""))
    assert any("#6" in b for b in v.blockers)


def test_check_body_closes_plus_refs():
    body = closes_body(5, ROWS1) + "\nRefs #6 — volgt in vervolgissue"
    assert gate.check_body(body, lambda n: (1, "")).blockers == []


def test_check_body_refs_with_reason_is_valid():
    assert gate.check_body("Refs #6 — wacht op deploy Beelink").blockers == []


def test_check_body_no_issue_is_valid():
    assert gate.check_body("No issue: alleen een typo in docs").blockers == []


def test_check_body_refs_without_reason_hint():
    v = gate.check_body("Refs #6")
    assert v.blockers
    assert "reason" in v.blockers[0]


def test_check_body_comment_form_does_not_count():
    body = "<!-- Closes #1\n Refs #2 — voorbeeld uit het template -->\nzie titel"
    assert gate.check_body(body).blockers


def test_check_body_invalid_block_names_reason():
    v = gate.check_body(closes_body(5, [("1", "ls", "x")]), lambda n: (1, ""))
    assert v.blockers
    assert "#5" in v.blockers[0]


# --- fetch_ac_count -------------------------------------------------------


def test_fetch_ac_count_without_gh(monkeypatch, tmp_path):  # Review Focus 4
    monkeypatch.setenv("PATH", str(tmp_path))
    assert gate.fetch_ac_count(5) == (None, "gh is missing")


def test_fetch_ac_count_gh_fails(monkeypatch, tmp_path):
    bin_dir = tool_bin(tmp_path / "bin", (), stubs={"gh": "#!/bin/sh\nexit 1\n"})
    monkeypatch.setenv("PATH", str(bin_dir))
    assert gate.fetch_ac_count(5) == (None, "gh api failed (rc 1)")


def test_fetch_ac_count_counts(monkeypatch, tmp_path):
    call_log = tmp_path / "aanroep.txt"
    stub = (
        "#!/bin/sh\n"
        f'echo "$@" > "{call_log}"\n'
        "printf '%s\\n' '### Acceptatiecriteria' '- [ ] a' '- [x] b' '- [ ] c' "
        "'### Definition of Done' '- [ ] d'\n"
    )
    bin_dir = tool_bin(tmp_path / "bin", (), stubs={"gh": stub})
    monkeypatch.setenv("PATH", str(bin_dir))
    assert gate.fetch_ac_count(5) == (3, "")
    assert call_log.read_text().split() == [
        "api",
        "repos/Acme/widgets/issues/5",
        "--jq",
        ".body",
    ]


def test_fetch_ac_count_no_ac_heading(monkeypatch, tmp_path):
    stub = "#!/bin/sh\nprintf 'geen kop hier\\n'\n"
    bin_dir = tool_bin(tmp_path / "bin", (), stubs={"gh": stub})
    monkeypatch.setenv("PATH", str(bin_dir))
    assert gate.fetch_ac_count(5) == (None, "no acceptance criteria heading")


def test_fetch_ac_count_timeout(monkeypatch, tmp_path):
    # A FIFO without a writer: `read` blocks without burning CPU.
    fifo = tmp_path / "blok"
    os.mkfifo(fifo)
    monkeypatch.setattr(gate, "AC_TIMEOUT_S", 0.3)
    stub = f'#!/bin/sh\nread _ < "{fifo}"\n'
    bin_dir = tool_bin(tmp_path / "bin", (), stubs={"gh": stub})
    monkeypatch.setenv("PATH", str(bin_dir))
    assert gate.fetch_ac_count(5) == (None, "timeout after 0.3 s")


# --- gh pr create vinden, body-bron, scope -----------------------------------


def test_finds_create_and_new(tmp_path):
    cmd = "gh pr create -F b.md; gh pr new -F b.md"
    assert len(gate.find_pr_creates(cmd, str(tmp_path))) == 2


def test_ignores_other_gh_commands(tmp_path):
    assert gate.find_pr_creates("gh pr view 5; git status", str(tmp_path)) == []


def test_body_file_relative_after_cd(tmp_path):
    (tmp_path / "sub").mkdir()
    [pc] = gate.find_pr_creates("cd sub && gh pr create -F body.md", str(tmp_path))
    assert pc.body_file == str(tmp_path / "sub" / "body.md")


def test_subshell_cd_does_not_leak(tmp_path):  # Review Focus 1
    (tmp_path / "sub").mkdir()
    cmd = "(cd sub && gh pr create -F b.md); gh pr create -F b.md"
    a, b = gate.find_pr_creates(cmd, str(tmp_path))
    assert a.body_file == str(tmp_path / "sub" / "b.md")
    assert b.body_file == str(tmp_path / "b.md")


@pytest.mark.parametrize(
    ("cmd", "word"),
    [
        ("gh pr create --fill", "--fill"),
        ("gh pr create --fill-first", "--fill-first"),
        ("gh pr create -f", "-f"),
        ("gh pr create --web", "--web"),
        ("gh pr create -e", "-e"),
        ("gh pr create -F -", "stdin"),
        ("gh pr create --body-file=-", "stdin"),
        ('gh pr create --body "$(cat b.md)"', "non-literal"),
        ("gh pr create --body $BODY", "non-literal"),
        ("gh pr create -F $F", "non-literal"),
        ("gh pr create -t x", "no body"),
        ("gh pr create --body x -F b.md", "choose one"),
    ],
)
def test_body_source_problems(tmp_path, cmd, word):
    assert word in gate.find_pr_creates(cmd, str(tmp_path))[0].problem


@pytest.mark.parametrize(
    "cmd",
    [
        "gh pr create --body 'No issue: alleen docs gewijzigd'",
        "gh pr create -b 'No issue: alleen docs gewijzigd'",
        "gh pr create --body='No issue: alleen docs gewijzigd'",
    ],
)
def test_body_literal(tmp_path, cmd):
    [pc] = gate.find_pr_creates(cmd, str(tmp_path))
    assert pc.problem is None
    assert pc.body.startswith("No issue")


def test_body_file_is(tmp_path):
    [pc] = gate.find_pr_creates("gh pr create --body-file=/x/b.md", str(tmp_path))
    assert pc.body_file == "/x/b.md"


def test_title_with_b_in_value_is_not_a_body(tmp_path):
    [pc] = gate.find_pr_creates("gh pr create -t -b -F b.md", str(tmp_path))
    assert pc.body_file == str(tmp_path / "b.md")


def test_relative_path_without_known_cwd(tmp_path):
    [pc] = gate.find_pr_creates("gh pr create -F body.md", None)
    assert "cannot be resolved" in pc.problem
    [pc] = gate.find_pr_creates("gh pr create -F /abs/body.md", None)
    assert pc.problem is None


def test_repo_flag(tmp_path):
    cmd = "gh pr create -R Acme/widgets -F b"
    assert gate.find_pr_creates(cmd, str(tmp_path))[0].repo == "Acme/widgets"


@pytest.mark.parametrize(
    ("cmd", "repo"),
    [
        ("GH_REPO=x/y gh pr create --fill", "x/y"),
        ("env GH_REPO=x/y gh pr create --fill", "x/y"),
        # gh: `-R` beats `GH_REPO`.
        ("GH_REPO=x/y gh pr create -R Acme/widgets -F b", "Acme/widgets"),
    ],
)
def test_gh_repo_assignment_counts_as_repo(tmp_path, cmd, repo):
    [pc] = gate.find_pr_creates(cmd, str(tmp_path))
    assert pc.repo == repo
    assert pc.repo_problem is None


@pytest.mark.parametrize(
    "cmd",
    [
        'gh pr create -R "$R" -F b.md',
        "gh pr create --repo=$R -F b.md",
        'GH_REPO="$R" gh pr create -F b.md',
    ],
)
def test_non_literal_repo_is_a_repo_problem(tmp_path, cmd):
    [pc] = gate.find_pr_creates(cmd, str(tmp_path))
    assert pc.repo is None
    assert "non-literal" in pc.repo_problem


def test_open_quote(tmp_path):
    with pytest.raises(gate.Unparseable):
        gate.find_pr_creates("gh pr create --body 'x", str(tmp_path))


def _pc(cwd, repo=None):
    return gate.PrCreate(cwd, repo, "x", None, None)


@pytest.fixture
def project(tmp_path):
    proj = tmp_path / "project"
    init_repo(proj)
    return proj


def test_scope_own_repo(tmp_path, project):
    wt = tmp_path / "wt"
    git(project, "worktree", "add", "-q", str(wt), "-b", "feat")
    assert gate.in_scope(_pc(str(project)), str(project))[0] == "toetsen"
    assert gate.in_scope(_pc(str(wt)), str(project))[0] == "toetsen"
    # A subdirectory that does not exist yet also counts as the repo of its ancestor.
    assert gate.in_scope(_pc(str(wt / "nieuw")), str(project))[0] == "toetsen"


def test_scope_other_repo_flag(project):
    decision, reason = gate.in_scope(_pc(str(project), "x/y"), str(project))
    assert decision == "overslaan"
    assert "other repo x/y" in reason


@pytest.mark.parametrize(
    "repo",
    [
        "Acme/widgets",
        "github.com/Acme/widgets",
        "https://github.com/Acme/widgets",
        "http://github.com/Acme/widgets/",
        "https://github.com/Acme/widgets.git",
        "ssh://git@github.com/Acme/widgets.git",
        "git@github.com:Acme/widgets.git",
        "https://github.example:8443/Acme/widgets",
        "acme/WIDGETS",
    ],
)
def test_scope_repo_spellings_of_this_repo(tmp_path, project, repo):
    outside = tmp_path / "buiten"
    outside.mkdir()
    assert gate.in_scope(_pc(str(outside), repo), str(project))[0] == "toetsen"


@pytest.mark.parametrize(
    "repo", ["github.com/x/y", "https://github.com/x/y.git", "git@github.com:x/y.git"]
)
def test_scope_repo_spellings_of_other_repo(project, repo):
    decision, reason = gate.in_scope(_pc(str(project), repo), str(project))
    assert decision == "overslaan"
    assert f"other repo {repo}" in reason


@pytest.mark.parametrize(
    "repo", ["", "   ", "x", "https://github.com/", "https://github.com/x"]
)
def test_scope_unrecognisable_repo_blocks(project, repo):
    decision, reason = gate.in_scope(_pc(str(project), repo), str(project))
    assert decision == "blokkeren"
    assert "--repo" in reason


def test_scope_outside_repo(tmp_path, project):
    outside = tmp_path / "buiten"
    outside.mkdir()
    assert gate.in_scope(_pc(str(outside)), str(project))[0] == "overslaan"


@pytest.mark.parametrize(
    "repo",
    [
        "https://github.com/Acme/widgets?tab=readme",
        "https://github.com/Acme/widgets#readme",
        "https://github.com/Acme/widgets.git?x=1#y",
    ],
)
def test_scope_repo_query_and_anchor_drop_out(tmp_path, project, repo):
    outside = tmp_path / "buiten"
    outside.mkdir()
    assert gate.in_scope(_pc(str(outside), repo), str(project))[0] == "toetsen"


def test_scope_non_literal_repo_blocks_also_outside_the_repo(tmp_path, project):
    """`-R "$R"` from a directory outside the repo: we cannot tell which repo it becomes."""
    outside = tmp_path / "buiten"
    outside.mkdir()
    [pc] = gate.find_pr_creates('gh pr create -R "$R" -F b.md', str(outside))
    decision, reason = gate.in_scope(pc, str(project))
    assert decision == "blokkeren"
    assert "non-literal" in reason


def test_scope_outside_repo_with_R(tmp_path, project):
    outside = tmp_path / "buiten"
    outside.mkdir()
    pc = _pc(str(outside), "Acme/widgets")
    assert gate.in_scope(pc, str(project))[0] == "toetsen"


def test_scope_other_git_repo(tmp_path, project):
    other = tmp_path / "ander"
    init_repo(other)
    assert gate.in_scope(_pc(str(other)), str(project))[0] == "overslaan"


def test_scope_forge_worktree_of_the_same_repo(tmp_path, project):
    """A worktree of a separate clone with the same `origin` is this repo (#36)."""
    git(project, "remote", "add", "origin", ORIGIN)
    work = forge_worktree(project, tmp_path)
    assert gate.in_scope(_pc(str(work)), str(project)) == ("toetsen", "")


def test_scope_other_origin_names_common_dirs_and_remotes(tmp_path, project):
    git(project, "remote", "add", "origin", ORIGIN)
    work = forge_worktree(project, tmp_path, "https://github.com/Acme/gadgets.git")
    common = os.path.realpath(tmp_path / "clone.git")
    ref = os.path.realpath(project / ".git")
    assert gate.in_scope(_pc(str(work)), str(project)) == (
        "overslaan",
        f"skipped — outside this repo (git-common-dir {common} vs {ref};"
        f" origin https://github.com/Acme/gadgets.git vs {ORIGIN})",
    )


def test_scope_unknown_cwd_blocks(project):
    assert gate.in_scope(_pc(None), str(project))[0] == "blokkeren"


def test_scope_other_git_error_blocks(tmp_path, project, monkeypatch):
    # A git that fails without "not a git repository" is no proof of "outside".
    bin_dir = tool_bin(
        tmp_path / "bin", (), stubs={"git": "#!/bin/sh\necho kapot >&2\nexit 128\n"}
    )
    monkeypatch.setenv("PATH", str(bin_dir))
    assert gate.in_scope(_pc(str(project)), str(project))[0] == "blokkeren"


def _run_main(monkeypatch, project, command, cwd):
    monkeypatch.setenv("CLAUDE_PROJECT_DIR", str(project))
    monkeypatch.setattr("sys.stdin", io.StringIO(command))
    return gate.main([str(cwd)])


def test_main_body_file_does_not_exist(project, monkeypatch, capsys):
    rc = _run_main(monkeypatch, project, "gh pr create -F nope.md", project)
    err = capsys.readouterr().err
    assert rc == 2
    assert str(project / "nope.md") in err
    assert "pr-create-gate: BLOCKED" in err
    assert "/lighthouse-verify-pr" in err


def test_main_no_gh_pr_create(project, monkeypatch, capsys):
    assert _run_main(monkeypatch, project, "git status", project) == 0
    assert capsys.readouterr().out == ""


def test_main_open_quote(project, monkeypatch):
    assert _run_main(monkeypatch, project, "gh pr create --body 'x", project) == 3


def test_main_problem_blocks(project, monkeypatch, capsys):
    assert _run_main(monkeypatch, project, "gh pr create --fill", project) == 2
    assert "--fill gives no body to check" in capsys.readouterr().err


def test_main_no_issue_passes(project, monkeypatch, capsys):
    cmd = "gh pr create --body 'No issue: alleen docs gewijzigd'"
    assert _run_main(monkeypatch, project, cmd, project) == 0
    assert capsys.readouterr().out == "pr-create-gate: body checked — No issue\n"


def test_main_non_literal_repo_outside_repo_blocks(
    tmp_path, project, monkeypatch, capsys
):
    outside = tmp_path / "buiten"
    outside.mkdir()
    cmd = "GH_REPO=\"$R\" gh pr create --body 'No issue: alleen docs gewijzigd'"
    assert _run_main(monkeypatch, project, cmd, outside) == 2
    assert "non-literal" in capsys.readouterr().err


def test_main_other_repo_skipped(project, monkeypatch, capsys):
    assert _run_main(monkeypatch, project, "gh pr create -R x/y --fill", project) == 0
    assert capsys.readouterr().out == "pr-create-gate: skipped — other repo x/y\n"


def test_main_closes_passes_with_count(project, monkeypatch, capsys, tmp_path):
    stub = "#!/bin/sh\nprintf '%s\\n' '### Acceptatiecriteria' '- [ ] a' '- [ ] b'\n"
    bin_dir = tool_bin(tmp_path / "bin", (), stubs={"gh": stub})
    monkeypatch.setenv("PATH", f"{bin_dir}:{os.environ['PATH']}")
    (project / "body.md").write_text(closes_body(9, ROWS2), encoding="utf-8")
    rc = _run_main(monkeypatch, project, "gh pr create -F body.md", project)
    assert rc == 0
    assert capsys.readouterr().out == (
        "pr-create-gate: body checked — Closes #9 (2 AC, 2 rows)\n"
    )


def test_main_closes_ac_mismatch_blocks(project, monkeypatch, capsys, tmp_path):
    stub = "#!/bin/sh\nprintf '%s\\n' '### Acceptatiecriteria' '- [ ] a' '- [ ] b' '- [ ] c'\n"
    bin_dir = tool_bin(tmp_path / "bin", (), stubs={"gh": stub})
    monkeypatch.setenv("PATH", f"{bin_dir}:{os.environ['PATH']}")
    (project / "body.md").write_text(closes_body(9, ROWS2), encoding="utf-8")
    rc = _run_main(monkeypatch, project, "gh pr create -F body.md", project)
    assert rc == 2
    assert "3 acceptance criteria" in capsys.readouterr().err


def test_main_several_creates_one_blocker_blocks(project, monkeypatch, capsys):
    cmd = "gh pr create --body 'No issue: alleen docs gewijzigd'; gh pr create -t x"
    assert _run_main(monkeypatch, project, cmd, project) == 2
    assert "no body" in capsys.readouterr().err


def test_main_fallback_without_gh_notices_and_passes(
    project, monkeypatch, capsys, tmp_path
):
    bin_dir = tool_bin(tmp_path / "bin", ("git",))
    monkeypatch.setenv("PATH", str(bin_dir))
    (project / "body.md").write_text(closes_body(9, ROWS1), encoding="utf-8")
    assert _run_main(monkeypatch, project, "gh pr create -F body.md", project) == 0
    out = capsys.readouterr().out
    assert "body checked — Closes #9 (AC not counted, 1 rows)" in out
    assert "AC count not measurable for #9 (gh is missing)" in out


@pytest.fixture(autouse=True)
def _reset_deadline(monkeypatch):
    """`main` sets the module global `_deadline`; reset it to `None` after every test."""
    monkeypatch.setattr(gate, "_deadline", None)


def _slow_gh(monkeypatch, tmp_path, sleep_s=3):
    stub = f"#!/bin/sh\nsleep {sleep_s}\n"
    bin_dir = tool_bin(tmp_path / "bin", (), stubs={"gh": stub})
    monkeypatch.setenv("PATH", f"{bin_dir}:{os.environ['PATH']}")


def _three_closes(with_evidence: tuple[int, ...]) -> str:
    heading = "Closes #5\nCloses #6\nCloses #7\n\n"
    blocks = "".join(
        closes_body(nr, ROWS1).split("\n\n", 2)[2] + "\n" for nr in with_evidence
    )
    return heading + blocks


def test_main_deadline_bounds_slow_gh_fallback(
    project, monkeypatch, capsys, tmp_path
):
    # Three Closes targets, gh hangs: without a total deadline this costs 3 x timeout.
    monkeypatch.setattr(gate, "DEADLINE_S", 1)
    _slow_gh(monkeypatch, tmp_path)
    (project / "body.md").write_text(_three_closes((5, 6, 7)), encoding="utf-8")
    start = time.monotonic()
    rc = _run_main(monkeypatch, project, "gh pr create -F body.md", project)
    elapsed = time.monotonic() - start
    out = capsys.readouterr().out
    assert rc == 0
    assert elapsed < 2.5
    for nr in (5, 6, 7):
        assert f"AC count not measurable for #{nr} (deadline reached)" in out


def test_main_deadline_keeps_blocker_of_earlier_target(
    project, monkeypatch, capsys, tmp_path
):
    # #5 lacks its Evidence block: that stays a blocker, even if the deadline
    # expires during the AC fetch of #6 and #7.
    monkeypatch.setattr(gate, "DEADLINE_S", 1)
    _slow_gh(monkeypatch, tmp_path)
    (project / "body.md").write_text(_three_closes((6, 7)), encoding="utf-8")
    start = time.monotonic()
    rc = _run_main(monkeypatch, project, "gh pr create -F body.md", project)
    elapsed = time.monotonic() - start
    err = capsys.readouterr().err
    assert rc == 2
    assert elapsed < 2.5
    assert "### Evidence #5" in err


def test_main_deadline_git_blocks_loudly(project, monkeypatch, capsys, tmp_path):
    # A hanging git in the scope check is no proof of "inside" or "outside".
    monkeypatch.setattr(gate, "DEADLINE_S", 1)
    bin_dir = tool_bin(tmp_path / "bin", (), stubs={"git": "#!/bin/sh\nsleep 3\n"})
    monkeypatch.setenv("PATH", f"{bin_dir}:{os.environ['PATH']}")
    (project / "body.md").write_text(closes_body(9, ROWS1), encoding="utf-8")
    start = time.monotonic()
    rc = _run_main(monkeypatch, project, "gh pr create -F body.md", project)
    elapsed = time.monotonic() - start
    assert rc == 2
    assert elapsed < 2.5
    assert "COULD NOT CHECK" in capsys.readouterr().err


def test_main_deadline_expired_before_git_blocks(project, monkeypatch, capsys):
    monkeypatch.setattr(gate, "DEADLINE_S", -1)
    rc = _run_main(monkeypatch, project, "gh pr create --body 'zie titel'", project)
    assert rc == 2
    assert "COULD NOT CHECK: deadline reached" in capsys.readouterr().err


def test_fetch_ac_count_deadline_expired(monkeypatch):
    monkeypatch.setattr(gate, "_deadline", time.monotonic() - 1)
    assert gate.fetch_ac_count(5) == (None, "deadline reached")


def test_cli_as_script(project, tmp_path):
    # As the hook calls it: a process of its own under python3.
    import subprocess
    import sys

    proc = subprocess.run(
        [sys.executable, str(INSTALLED_LIB / "pr_body_gate.py"), str(project)],
        input="gh pr create --body 'No issue: alleen docs gewijzigd'",
        capture_output=True,
        text=True,
        env=dict(os.environ, CLAUDE_PROJECT_DIR=str(project)),
        check=False,
    )
    assert proc.returncode == 0, proc.stderr
    assert proc.stdout == "pr-create-gate: body checked — No issue\n"


# --- lighthouse: headings and locale --------------------------------------------


@pytest.mark.parametrize("heading", ["### Acceptatiecriteria", "### Acceptance criteria"])
def test_ac_heading_any_language(heading, tmp_path, monkeypatch):
    """A Dutch issue in an English repo counts its AC like an English issue."""
    toml = tmp_path / "lighthouse.toml"
    toml.write_text('[project]\nlanguage = "en"\n\n[forge]\nkind = "github"\nrepo = "Acme/widgets"\n')
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(toml))
    assert gate.count_acs(f"{heading}\n- [ ] a\n- [x] b\n### Next\n- [ ] c") == 2


def _git_speaks_german(where) -> bool:
    proc = subprocess.run(
        ["git", "-C", str(where), "rev-parse", "--git-common-dir"],
        capture_output=True,
        text=True,
        env={**os.environ, "LANG": "de_DE.UTF-8", "LC_ALL": "", "LANGUAGE": ""},
        check=False,
    )
    return proc.returncode != 0 and "not a git repository" not in proc.stderr


def test_not_a_git_repository_under_other_locale(tmp_path, project, monkeypatch):
    """Under another language a directory outside git stays `outside this repo` (inventory §2.12).

    Git ships no Dutch translation; German shows the same error.
    """
    outside = tmp_path / "buiten"
    outside.mkdir()
    if not _git_speaks_german(outside):
        pytest.skip("git spreekt hier geen Duits (geen de_DE-locale of vertaling)")
    monkeypatch.setenv("LANG", "de_DE.UTF-8")
    monkeypatch.setenv("LC_ALL", "")
    monkeypatch.setenv("LANGUAGE", "")
    assert gate.in_scope(_pc(str(outside)), str(project)) == (
        "overslaan",
        "skipped — outside this repo",
    )

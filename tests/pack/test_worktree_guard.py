# Bootstrap copy of personal_ai_agent@231f6e84 tests/unit/test_instructie_hooks.py (test_worktree_guard_*); M0 (b) replaces it.
"""worktree-guard.sh: warns at a write into the main checkout on `main`.

The six copied tests pin the behaviour from paia. The tests below them pin
the fix for the fail-open gap (M0 spec, risk *Fail-open guards*): a guard
that cannot look says so instead of staying silent.
"""

import json
import subprocess
from pathlib import Path

from tests.hooks._hook_testkit import HOOKS
from tests.hooks._hook_testkit import git as _git
from tests.hooks._hook_testkit import init_repo as _init_repo
from tests.hooks._hook_testkit import run_hook as _run
from tests.hooks._hook_testkit import tool_bin

WORKTREE_GUARD = HOOKS / "worktree-guard.sh"

# Everything worktree-guard.sh calls, so a test can drop exactly one of them.
_TOOLS = ("jq", "git", "cat", "python3", "realpath", "dirname")


def test_worktree_guard_warns_on_main_checkout_on_main(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        WORKTREE_GUARD,
        {
            "tool_name": "Write",
            "cwd": str(repo),
            "tool_input": {"file_path": str(repo / "src" / "nieuw.py")},
        },
        cwd=repo,
    )

    assert result.returncode == 0, result.stderr
    assert "worktree" in json.loads(result.stdout)["systemMessage"].lower()


def test_worktree_guard_silent_inside_worktree(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))

    result = _run(
        WORKTREE_GUARD,
        {
            "tool_name": "Write",
            "cwd": str(wt),
            "tool_input": {"file_path": str(wt / "src" / "nieuw.py")},
        },
        cwd=wt,
    )

    assert result.returncode == 0
    assert result.stdout.strip() == ""


def test_worktree_guard_silent_for_worktree_branch_literally_named_main(tmp_path):
    """Isoleert de worktree-detectieguard (GIT_DIR != GIT_COMMON) van de
    branchcheck. Elke andere worktree-test hangt aan een fixture waarvan de
    default branch `main` heet en de worktree-branch `issue-1-iets` — dus de
    branchcheck alleen levert daar al stilte, ook als de worktree-detectie
    kapot is. Hier heet de default branch `trunk` en de worktree-branch zelf
    `main`: alleen de worktree-detectieguard kan hier nog stilte opleveren."""
    repo = tmp_path / "repo"
    _init_repo(repo, branch="trunk")
    wt = repo / ".claude" / "worktrees" / "main"
    _git(repo, "worktree", "add", "-q", "-b", "main", str(wt))

    result = _run(
        WORKTREE_GUARD,
        {
            "tool_name": "Write",
            "cwd": str(wt),
            "tool_input": {"file_path": str(wt / "src" / "nieuw.py")},
        },
        cwd=wt,
    )

    assert result.returncode == 0
    assert result.stdout.strip() == ""


def test_worktree_guard_silent_on_feature_branch_in_main_checkout(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "fix-iets")

    result = _run(
        WORKTREE_GUARD,
        {
            "tool_name": "Edit",
            "cwd": str(repo),
            "tool_input": {"file_path": str(repo / "README.md")},
        },
        cwd=repo,
    )

    assert result.returncode == 0
    assert result.stdout.strip() == ""


def test_worktree_guard_warns_when_cwd_is_worktree_but_target_is_main_checkout(
    tmp_path,
):
    """De gedocumenteerde ontsnapping: worktree-cwd, absoluut pad in de hoofdboom."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))

    result = _run(
        WORKTREE_GUARD,
        {
            "tool_name": "Edit",
            "cwd": str(wt),
            "tool_input": {"file_path": str(repo / "README.md")},
        },
        cwd=wt,
    )

    assert result.returncode == 0
    assert json.loads(result.stdout)["systemMessage"].strip()


def test_worktree_guard_silent_outside_git_repo(tmp_path):
    loose = tmp_path / "los"
    loose.mkdir()

    result = _run(
        WORKTREE_GUARD,
        {
            "tool_name": "Write",
            "cwd": str(loose),
            "tool_input": {"file_path": str(loose / "x.txt")},
        },
        cwd=loose,
    )

    assert result.returncode == 0
    assert result.stdout.strip() == ""


def _write_payload(repo: Path) -> dict:
    return {
        "tool_name": "Write",
        "cwd": str(repo),
        "tool_input": {"file_path": str(repo / "x.py")},
    }


def test_worktree_guard_warns_without_jq(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        WORKTREE_GUARD,
        _write_payload(repo),
        cwd=repo,
        env={"PATH": str(tool_bin(tmp_path / "no-jq", _TOOLS, drop="jq"))},
    )

    assert result.returncode == 0, result.stderr
    message = json.loads(result.stdout)["systemMessage"]
    assert "COULD NOT CHECK" in message
    assert "jq" in message


def test_worktree_guard_warns_without_git(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        WORKTREE_GUARD,
        _write_payload(repo),
        cwd=repo,
        env={"PATH": str(tool_bin(tmp_path / "no-git", _TOOLS, drop="git"))},
    )

    assert result.returncode == 0, result.stderr
    message = json.loads(result.stdout)["systemMessage"]
    assert "COULD NOT CHECK" in message
    assert "git" in message


def test_worktree_guard_warns_on_invalid_json_payload(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = subprocess.run(
        [str(WORKTREE_GUARD)],
        input="not json",
        capture_output=True,
        text=True,
        cwd=str(repo),
        check=False,
    )

    assert result.returncode == 0, result.stderr
    assert "COULD NOT CHECK" in json.loads(result.stdout)["systemMessage"]


def test_worktree_guard_warns_when_the_path_cannot_be_resolved(tmp_path):
    """No `realpath` and no `python3`: canon_path has nothing to resolve with."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    bare = tool_bin(tmp_path / "no-canon", ("jq", "git", "cat", "dirname"))

    result = _run(
        WORKTREE_GUARD,
        _write_payload(repo),
        cwd=repo,
        env={"PATH": str(bare)},
    )

    assert result.returncode == 0, result.stderr
    message = json.loads(result.stdout)["systemMessage"]
    assert "COULD NOT CHECK" in message
    assert str(repo / "x.py") in message

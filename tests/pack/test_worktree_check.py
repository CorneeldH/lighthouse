"""pack/core worktree-check.sh, as installed: warns at a write into the main checkout
on the default branch.

The six copied tests pin the behaviour from paia. The tests below them pin
the fix for the fail-open gap (M0 spec, risk *Fail-open guards*): a guard
that cannot look says so instead of staying silent.
"""

import json
import subprocess
from pathlib import Path

import os
import shutil

import pytest

from tests.pack._testkit import INSTALLED_HOOKS
from tests.pack._testkit import git as _git
from tests.pack._testkit import init_repo as _init_repo
from tests.pack._testkit import run_hook as _run
from tests.pack._testkit import tool_bin

WORKTREE_GUARD = INSTALLED_HOOKS / "worktree-check.sh"

# Everything worktree-check.sh calls, so a test can drop exactly one of them.
_TOOLS = ("jq", "git", "cat", "python3", "realpath", "dirname")


def test_worktree_check_warns_on_main_checkout_on_main(tmp_path):
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


def test_worktree_check_silent_inside_worktree(tmp_path):
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


def test_worktree_check_silent_for_worktree_branch_literally_named_main(tmp_path):
    """Isolates the worktree-detection guard (GIT_DIR != GIT_COMMON) from the
    branch check. Every other worktree test hangs on a fixture whose default
    branch is called `main` and whose worktree branch is `issue-1-iets`, so
    the branch check alone already yields silence there, even if the worktree
    detection is broken. Here the default branch is called `trunk` and the
    worktree branch itself `main`: only the worktree-detection guard can still
    yield silence here."""
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


def test_worktree_check_silent_on_feature_branch_in_main_checkout(tmp_path):
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


def test_worktree_check_warns_when_cwd_is_worktree_but_target_is_main_checkout(
    tmp_path,
):
    """The documented escape: worktree cwd, absolute path in the main tree."""
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


def test_worktree_check_silent_outside_git_repo(tmp_path):
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


def test_worktree_check_warns_without_jq(tmp_path):
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


def test_worktree_check_warns_without_git(tmp_path):
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


def test_worktree_check_warns_on_invalid_json_payload(tmp_path):
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


def test_worktree_check_warns_when_the_path_cannot_be_resolved(tmp_path):
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


# --- no silent exits (inventory §6) and the default branch (decision 23) -----


def _write_in(repo: Path) -> dict:
    return {
        "tool_name": "Write",
        "cwd": str(repo),
        "tool_input": {"file_path": str(repo / "src" / "nieuw.py")},
    }


def _git_stub(tmp_path: Path, fails_on: str) -> Path:
    real = shutil.which("git")
    return tool_bin(
        tmp_path / "bin",
        _TOOLS,
        drop="git",
        stubs={
            "git": f'#!/bin/sh\ncase "$*" in *{fails_on}*) echo "fatal: broken" >&2; exit 128;; esac\n'
            f'exec {real} "$@"\n'
        },
    )


def test_guard_speaks_when_git_breaks(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    bin_dir = _git_stub(tmp_path, "rev-parse")
    result = _run(WORKTREE_GUARD, _write_in(repo), cwd=repo, env={"PATH": str(bin_dir)})
    assert result.returncode == 0, result.stderr
    assert "COULD NOT CHECK" in json.loads(result.stdout)["systemMessage"]


def test_guard_speaks_when_branch_fails(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    bin_dir = _git_stub(tmp_path, "branch")
    result = _run(WORKTREE_GUARD, _write_in(repo), cwd=repo, env={"PATH": str(bin_dir)})
    assert result.returncode == 0, result.stderr
    assert "COULD NOT CHECK" in json.loads(result.stdout)["systemMessage"]


def test_guard_silent_outside_git_repo_under_other_locale(tmp_path):
    outside = tmp_path / "outside"
    outside.mkdir()
    env = {"LANG": "de_DE.UTF-8", "LC_ALL": "", "LANGUAGE": ""}
    probe = subprocess.run(
        ["git", "-C", str(outside), "rev-parse", "--git-dir"],
        capture_output=True,
        text=True,
        env=dict(os.environ, **env),
        check=False,
    )
    if "not a git repository" in probe.stderr:
        pytest.skip("git speaks no German here")
    result = _run(WORKTREE_GUARD, _write_in(outside), cwd=outside, env=env)
    assert result.returncode == 0, result.stderr
    assert result.stdout == ""


def test_guard_follows_default_branch(tmp_path):
    toml = tmp_path / "lighthouse.toml"
    toml.write_text('[forge]\ndefault_branch = "trunk"\n')
    env = {"LIGHTHOUSE_TOML": str(toml)}
    trunk = tmp_path / "trunk"
    _init_repo(trunk, branch="trunk")
    on_main = tmp_path / "on-main"
    _init_repo(on_main, branch="main")

    warned = _run(WORKTREE_GUARD, _write_in(trunk), cwd=trunk, env=env)
    assert "'trunk'" in json.loads(warned.stdout)["systemMessage"]
    silent = _run(WORKTREE_GUARD, _write_in(on_main), cwd=on_main, env=env)
    assert silent.returncode == 0 and silent.stdout == ""


def test_guard_with_broken_config_speaks(tmp_path):
    toml = tmp_path / "lighthouse.toml"
    toml.write_text("[forge\n")
    repo = tmp_path / "repo"
    _init_repo(repo)
    result = _run(WORKTREE_GUARD, _write_in(repo), cwd=repo, env={"LIGHTHOUSE_TOML": str(toml)})
    assert result.returncode == 0
    assert "COULD NOT CHECK" in json.loads(result.stdout)["systemMessage"]

"""`pack/core/lib/repo_identity.py` (#36): is a tree a checkout of this repo?

The gates themselves compare the same `--git-common-dir`; this lib compares the
`origin`, for a worktree of a separate clone (forge) of the same repo.
"""

import os
import subprocess
import sys

import pytest

from tests.pack._testkit import (
    INSTALLED_LIB,
    forge_worktree,
    git,
    init_repo,
    load_hook_lib,
)

ri = load_hook_lib("repo_identity")


@pytest.fixture
def project(tmp_path):
    proj = tmp_path / "project"
    init_repo(proj)
    git(proj, "remote", "add", "origin", "git@github.com:Acme/widgets.git")
    return proj


# `remote_key` and `same_origin` are in `fixtures/repo_identity/cases.json`, run
# by `test_repo_identity_fixtures.py`.


def _cli(*args):
    return subprocess.run(
        [sys.executable, str(INSTALLED_LIB / "repo_identity.py"), *args],
        capture_output=True,
        text=True,
        env=dict(os.environ),
        check=False,
    )


def test_cli_exitcodes(tmp_path, project):
    work = forge_worktree(project, tmp_path)
    other = tmp_path / "ander"
    init_repo(other)

    same = _cli(str(project), str(work))
    assert (same.returncode, same.stdout) == (0, "")

    other = _cli(str(project), str(other))
    assert other.returncode == 1
    assert other.stdout == "origin none vs git@github.com:Acme/widgets.git\n"

    broken = tmp_path / "broken"
    init_repo(broken)
    (broken / ".git" / "config").write_text("[kapot\n", encoding="utf-8")
    error = _cli(str(project), str(broken))
    assert error.returncode == 2
    assert error.stderr.strip()

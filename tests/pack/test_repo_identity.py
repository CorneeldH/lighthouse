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
    ORIGIN,
    forge_worktree,
    git,
    init_repo,
    load_hook_lib,
)

ri = load_hook_lib("repo_identity")


@pytest.mark.parametrize(
    "url",
    [
        "https://github.com/Acme/widgets.git",
        "https://github.com/acme/widgets",
        "https://user@github.com:443/Acme/widgets.git/",
        "ssh://git@github.com/Acme/widgets.git",
        "git@github.com:Acme/widgets.git",
    ],
)
def test_remote_key_normalises_the_same_repo(url):
    assert ri.remote_key(url) == "github.com/acme/widgets"


@pytest.mark.parametrize("url", ["/srv/git/widgets.git", "file:///srv/git/widgets.git", "", "https://github.com"])
def test_remote_key_without_host_and_path_is_none(url):
    assert ri.remote_key(url) is None


def test_remote_key_keeps_the_host():
    assert ri.remote_key("https://gitlab.com/Acme/widgets") != ri.remote_key(ORIGIN)


@pytest.fixture
def project(tmp_path):
    proj = tmp_path / "project"
    init_repo(proj)
    git(proj, "remote", "add", "origin", "git@github.com:Acme/widgets.git")
    return proj


def test_same_origin_forge_worktree(tmp_path, project):
    work = forge_worktree(project, tmp_path)
    assert ri.same_origin(str(project), str(work), timeout=5) == (True, "")


def test_same_origin_other_repo_names_both_remotes(tmp_path, project):
    work = forge_worktree(project, tmp_path, "https://github.com/Acme/gadgets.git")
    same, reason = ri.same_origin(str(project), str(work), timeout=5)
    assert not same
    assert reason == "origin https://github.com/Acme/gadgets.git vs git@github.com:Acme/widgets.git"


def test_same_origin_without_origin(tmp_path, project):
    other = tmp_path / "ander"
    init_repo(other)
    assert ri.same_origin(str(project), str(other), timeout=5) == (
        False,
        "origin none vs git@github.com:Acme/widgets.git",
    )


def test_same_origin_two_local_paths_are_not_the_same_repo(tmp_path):
    """A local path says nothing about the upstream: precision over recall."""
    a, b = tmp_path / "a", tmp_path / "b"
    init_repo(a)
    init_repo(b)
    git(a, "remote", "add", "origin", "/srv/git/widgets.git")
    git(b, "remote", "add", "origin", "/srv/git/widgets.git")
    assert ri.same_origin(str(a), str(b), timeout=5)[0] is False


def test_same_origin_broken_repo_raises(tmp_path, project):
    broken = tmp_path / "broken"
    init_repo(broken)
    (broken / ".git" / "config").write_text("[kapot\n", encoding="utf-8")
    with pytest.raises(ri.OriginError):
        ri.same_origin(str(project), str(broken), timeout=5)


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

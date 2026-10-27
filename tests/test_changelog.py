"""ci/changelog.py: the fragment parser and the `check` command."""

import subprocess
from pathlib import Path

import pytest

from tests._changelog import load
from tests.pack._testkit import git

changelog = load()


def _head(root: Path) -> str:
    return subprocess.run(
        ["git", "-C", str(root), "rev-parse", "HEAD"],
        check=True, capture_output=True, text=True,
    ).stdout.strip()


def _commit(root: Path, message: str) -> str:
    git(root, "add", "-A")
    git(root, "commit", "-qm", message)
    return _head(root)


def _check(root: Path, base: str, head: str) -> int:
    return changelog.main(["check", "--base", base, "--head", head, "--root", str(root)])


@pytest.fixture
def git_repo(tmp_path: Path) -> tuple[Path, str]:
    """A repo whose commit 1 holds changelog.d/.lighthouse and changelog.d/old.md."""
    root = tmp_path / "repo"
    (root / "changelog.d").mkdir(parents=True)
    (root / "changelog.d" / ".lighthouse").write_text("marker\n", encoding="utf-8")
    (root / "changelog.d" / "old.md").write_text("---\ntype: fixed\n---\nOld.\n", encoding="utf-8")
    (root / "code.go").write_text("package a\n", encoding="utf-8")
    git(root, "init", "-q", "-b", "main")
    return root, _commit(root, "one")


GOOD = "---\ntype: added\n---\nNew skill.\n"


@pytest.mark.parametrize("body,typ,text", [
    ("---\ntype: fixed\n---\nNo more rewrites.\n", "fixed", "No more rewrites."),
    ("﻿---\r\ntype: added\r\n---\r\nNew skill.\r\n", "added", "New skill."),
])
def test_parse_fragment_accepts(tmp_path, body, typ, text):
    """Fails when a valid fragment (also CRLF with a BOM) is refused or parsed wrongly."""
    path = tmp_path / "x.md"
    path.write_bytes(body.encode("utf-8"))
    assert changelog.parse_fragment(path) == changelog.Fragment(path, typ, text)


@pytest.mark.parametrize("body", [
    "type: fixed\n---\nx\n",
    "---\ntype: fixed\nx\n",
    "---\ntype: fixed\n---\n   \n",
    "---\ntype: bugfix\n---\nx\n",
    "---\ntype: fixed\ntype: added\n---\nx\n",
    "---\ntype: fixed\nscope: cli\n---\nx\n",
    "---\n---\nx\n",
])
def test_parse_fragment_refuses(tmp_path, body):
    """Fails when a malformed fragment is accepted or the error omits the path."""
    path = tmp_path / "broken.md"
    path.write_text(body, encoding="utf-8")
    with pytest.raises(changelog.FragmentError) as err:
        changelog.parse_fragment(path)
    assert "broken.md" in str(err.value)


def test_read_fragments_skips_marker_and_sorts(tmp_path):
    """Fails when the marker is parsed or the order is not by file name."""
    for name in (".lighthouse", "b.md", "a.md"):
        (tmp_path / name).write_text("---\ntype: fixed\n---\nx\n", encoding="utf-8")
    (tmp_path / ".lighthouse").write_text("not a fragment\n", encoding="utf-8")
    assert [f.path.name for f in changelog.read_fragments(tmp_path)] == ["a.md", "b.md"]


def test_read_fragments_missing_directory(tmp_path):
    """Fails when a missing directory raises instead of giving no fragments."""
    assert changelog.read_fragments(tmp_path / "nope") == []


def test_read_fragments_refuses_other_files(tmp_path):
    """Fails when a stray non-markdown file is ignored."""
    (tmp_path / "notes.txt").write_text("x\n", encoding="utf-8")
    with pytest.raises(changelog.FragmentError) as err:
        changelog.read_fragments(tmp_path)
    assert "notes.txt" in str(err.value)


def test_check_requires_an_added_fragment(git_repo, capsys):
    """Fails when a PR without a new fragment passes."""
    root, c1 = git_repo
    (root / "code.go").write_text("package b\n", encoding="utf-8")
    assert _check(root, c1, _commit(root, "two")) == 1
    assert "no new fragment" in capsys.readouterr().err


def test_check_ignores_modified_fragment(git_repo):
    """Fails when editing an existing fragment counts as a new one."""
    root, c1 = git_repo
    (root / "changelog.d" / "old.md").write_text("---\ntype: added\n---\nEdited.\n", encoding="utf-8")
    assert _check(root, c1, _commit(root, "two")) == 1


def test_check_counts_renamed_fragment(git_repo):
    """Fails when a renamed fragment is not counted as added under its new name."""
    root, c1 = git_repo
    git(root, "mv", "changelog.d/old.md", "changelog.d/new.md")
    assert _check(root, c1, _commit(root, "two")) == 0


def test_check_ignores_added_marker(tmp_path):
    """Fails when adding only the marker file counts as a fragment."""
    root = tmp_path / "repo"
    root.mkdir()
    (root / "code.go").write_text("package a\n", encoding="utf-8")
    git(root, "init", "-q", "-b", "main")
    c1 = _commit(root, "one")
    (root / "changelog.d").mkdir()
    (root / "changelog.d" / ".lighthouse").write_text("marker\n", encoding="utf-8")
    assert _check(root, c1, _commit(root, "two")) == 1


def test_check_refuses_bad_fragment_anywhere(git_repo, capsys):
    """Fails when a broken fragment passes because another one is fine."""
    root, c1 = git_repo
    (root / "changelog.d" / "old.md").write_text("garbage\n", encoding="utf-8")
    (root / "changelog.d" / "new.md").write_text(GOOD, encoding="utf-8")
    assert _check(root, c1, _commit(root, "two")) == 1
    assert "old.md" in capsys.readouterr().err


def test_check_passes(git_repo):
    """Fails when a PR with a valid new fragment is refused."""
    root, c1 = git_repo
    (root / "changelog.d" / "new.md").write_text(GOOD, encoding="utf-8")
    assert _check(root, c1, _commit(root, "two")) == 0


def test_check_bad_base_cannot_check(git_repo, capsys):
    """Fails when a git failure is reported as a verdict instead of COULD NOT CHECK."""
    root, c1 = git_repo
    assert _check(root, "deadbeef", c1) == 2
    assert "COULD NOT CHECK" in capsys.readouterr().err

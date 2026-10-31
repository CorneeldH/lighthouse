"""`pack/core/lib/commit_base.py` (#80): record the HEAD before a commit, find it after."""

import json
import os
import subprocess
import sys

import pytest

from tests.pack._testkit import IMPORT_ROOT, LIB, git, init_repo, load_hook_lib

cb = load_hook_lib("commit_base")
SCRIPT = LIB / "commit_base.py"


@pytest.fixture(autouse=True)
def runtime_dir(tmp_path, monkeypatch):
    runtime = tmp_path / "runtime"
    runtime.mkdir()
    monkeypatch.setenv("XDG_RUNTIME_DIR", str(runtime))
    return runtime


@pytest.fixture
def repo(tmp_path):
    path = tmp_path / "repo"
    init_repo(path)
    return path


def payload(session="s1", tool_use_id="t1"):
    return {"session_id": session, "tool_use_id": tool_use_id}


def head(repo):
    out = subprocess.run(
        ["git", "-C", str(repo), "rev-parse", "HEAD"], capture_output=True, text=True, check=True
    )
    return out.stdout.strip()


def commit_file(repo, name, content="x\n"):
    target = repo / name
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(content, encoding="utf-8")
    git(repo, "add", name)
    git(repo, "commit", "-qm", f"add {name}")


def cli(*args, stdin=""):
    return subprocess.run(
        [sys.executable, str(SCRIPT), *args],
        input=stdin,
        capture_output=True,
        text=True,
        check=False,
        env=dict(os.environ, PYTHONPATH=str(IMPORT_ROOT)),
    )


def test_moved_after_commit(repo):
    old = head(repo)
    cb.record(payload(), [str(repo)])
    commit_file(repo, "a.txt")
    assert cb.moved(payload()) == [cb.Moved(str(repo), old, head(repo))]


def test_moved_is_empty_without_new_commit(repo):
    cb.record(payload(), [str(repo)])
    assert cb.moved(payload()) == []


def test_moved_consumes_marker(repo):
    cb.record(payload(), [str(repo)])
    commit_file(repo, "a.txt")
    assert len(cb.moved(payload())) == 1
    assert cb.moved(payload()) == []


def test_moved_without_marker_is_empty():
    assert cb.moved(payload()) == []



def test_moved_does_not_create_the_state_dir(runtime_dir):
    # `post` runs after every Bash call: only `record` makes the state dir.
    assert cb.moved(payload()) == []
    assert not (runtime_dir / "lighthouse-commit-tests").exists()


def test_moved_with_read_only_runtime_dir_is_empty(runtime_dir):
    runtime_dir.chmod(0o500)
    try:
        assert cb.moved(payload()) == []
    finally:
        runtime_dir.chmod(0o700)

def test_unborn_base_is_empty_tree(tmp_path):
    root = tmp_path / "fresh"
    root.mkdir()
    git(root, "init", "-q", "-b", "main")
    cb.record(payload(), [str(root)])
    commit_file(root, "a.txt")
    assert cb.moved(payload()) == [cb.Moved(str(root), cb.EMPTY_TREE, head(root))]


def test_old_markers_are_pruned(repo):
    state = cb.state_dir("s1")
    stale = state / "old.json"
    stale.write_text(json.dumps({"heads": {}}))
    os.utime(stale, (1, 1))
    cb.record(payload(), [str(repo)])
    assert not stale.exists()


def test_state_dir_refuses_path_escape():
    with pytest.raises(ValueError):
        cb.state_dir("a/b")
    with pytest.raises(ValueError):
        cb.state_dir("..")


def test_changed_files_rename_gives_both_sides(repo):
    commit_file(repo, "a.txt")
    base = head(repo)
    git(repo, "mv", "a.txt", "b.txt")
    git(repo, "commit", "-qm", "rename")
    assert cb.changed_files(str(repo), base, head(repo)) == ["a.txt", "b.txt"]


def test_changed_files_path_with_space(repo):
    base = head(repo)
    commit_file(repo, "dir/x y.py")
    assert cb.changed_files(str(repo), base, head(repo)) == ["dir/x y.py"]


def test_cli_moved_prints_rows(repo):
    stdin = json.dumps(payload())
    old = head(repo)
    assert cli("record", str(repo), stdin=stdin).returncode == 0
    commit_file(repo, "a.txt")
    result = cli("moved", stdin=stdin)
    assert result.returncode == 0
    assert result.stdout == f"{repo}\t{old}\t{head(repo)}\n"


def test_cli_files_writes_one_path_per_line(repo, tmp_path):
    base = head(repo)
    commit_file(repo, "b.txt")
    commit_file(repo, "a.txt")
    out = tmp_path / "files.txt"
    result = cli("files", str(repo), base, head(repo), str(out))
    assert result.returncode == 0
    assert out.read_text(encoding="utf-8") == "a.txt\nb.txt\n"


def test_cli_git_failure_is_rc_2(repo, tmp_path):
    out = tmp_path / "files.txt"
    result = cli("files", str(repo), "0" * 40, head(repo), str(out))
    assert result.returncode == 2
    assert result.stderr.strip()


"""`pack/core/lib/turn_roots.py` (#54): in which checkouts did this turn write?

The Stop hooks check those checkouts instead of the payload `cwd`, which is the
shell's directory at Stop: a session in the main checkout that edits a worktree
through absolute paths stops with `cwd` = the main checkout.

The script runs as a subprocess against real repositories and a transcript on
disk, the way the hooks call it.
"""

import io
import json
import os
import subprocess
import sys
import time
from pathlib import Path

import pytest

from tests.pack._testkit import (
    INSTALLED_LIB,
    git,
    init_repo,
    past_this_second,
    prompt_entry,
    result_entry,
    stamp,
    stop_payload,
    tool_entry,
    write_transcript,
)

SCRIPT = INSTALLED_LIB / "turn_roots.py"


@pytest.fixture
def main(tmp_path):
    root = tmp_path / "main"
    init_repo(root)
    return root


@pytest.fixture
def wt(main):
    path = main / ".claude" / "worktrees" / "wt"
    git(main, "worktree", "add", "-q", str(path), "-b", "wt")
    return path


def _run(main: Path, payload: dict | str, *args: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, str(SCRIPT), *args],
        input=payload if isinstance(payload, str) else json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(main),
        env=dict(os.environ, CLAUDE_PROJECT_DIR=str(main)),
        check=False,
    )


def _roots(main: Path, cwd: Path, entries: list[dict], tmp_path: Path) -> list[str]:
    transcript = write_transcript(tmp_path / "t" / "s1.jsonl", entries)
    result = _run(main, stop_payload(cwd, transcript))
    assert result.returncode == 0, result.stdout + result.stderr
    return result.stdout.splitlines()


@pytest.mark.parametrize(
    ("tool", "key"),
    [("Edit", "file_path"), ("Write", "file_path"), ("MultiEdit", "file_path"), ("NotebookEdit", "notebook_path")],
)
def test_an_edit_in_a_worktree_names_the_worktree_not_the_cwd(tmp_path, main, wt, tool, key):
    entries = [
        prompt_entry(main),
        tool_entry(tool, {key: str(wt / "src" / "new.py")}, main),
        result_entry(main),
        # A meta entry (hook context, a caveat) is no prompt.
        {**prompt_entry(main), "isMeta": True},
    ]
    assert _roots(main, main, entries, tmp_path) == [str(wt.resolve())]


def test_a_bash_call_names_its_git_target_and_its_cwd(tmp_path, main, wt):
    entries = [
        prompt_entry(main),
        tool_entry("Bash", {"command": f"git -C {wt} commit -qm x"}, main),
        result_entry(main),
    ]
    assert _roots(main, main, entries, tmp_path) == sorted([str(main.resolve()), str(wt.resolve())])


def test_a_bash_call_uses_the_cwd_of_its_own_entry(tmp_path, main, wt):
    entries = [prompt_entry(main), tool_entry("Bash", {"command": "ls"}, wt), result_entry(wt)]
    assert _roots(main, main, entries, tmp_path) == [str(wt.resolve())]


def test_a_turn_without_writes_falls_back_to_the_cwd(tmp_path, main, wt):
    entries = [
        prompt_entry(main, "2026-10-25T09:00:00.000Z"),
        tool_entry("Edit", {"file_path": str(wt / "a.py")}, main, "2026-10-25T09:00:01.000Z"),
        result_entry(main, "2026-10-25T09:00:02.000Z"),
        # The edit above belongs to the previous turn.
        prompt_entry(main),
        tool_entry("Read", {"file_path": str(wt / "a.py")}, main),
    ]
    assert _roots(main, main, entries, tmp_path) == [str(main.resolve())]


def test_a_path_in_another_repository_does_not_count(tmp_path, main):
    other = tmp_path / "other"
    init_repo(other)
    entries = [prompt_entry(main), tool_entry("Edit", {"file_path": str(other / "a.py")}, main)]
    assert _roots(main, main, entries, tmp_path) == [str(main.resolve())]


def test_a_subagent_edit_since_the_prompt_counts(tmp_path, main, wt):
    old = main / ".claude" / "worktrees" / "old"
    git(main, "worktree", "add", "-q", str(old), "-b", "old")
    write_transcript(
        tmp_path / "t" / "s1" / "subagents" / "agent-a1.jsonl",
        [
            {**tool_entry("Edit", {"file_path": str(old / "a.py")}, old, "2026-10-25T09:59:59.000Z"), "isSidechain": True},
            {**tool_entry("Edit", {"file_path": str(wt / "a.py")}, wt, "2026-10-25T10:00:03.000Z"), "isSidechain": True},
        ],
    )
    entries = [prompt_entry(main), tool_entry("Agent", {"prompt": "do it"}, main), result_entry(main)]
    assert _roots(main, main, entries, tmp_path) == [str(wt.resolve())]


def test_without_a_transcript_path_it_cannot_tell(main):
    result = _run(main, {"cwd": str(main), "stop_hook_active": False})
    assert result.returncode == 1
    assert "transcript_path" in result.stdout


def test_a_missing_transcript_cannot_tell(tmp_path, main):
    result = _run(main, stop_payload(main, tmp_path / "gone.jsonl"))
    assert result.returncode == 1
    assert "gone.jsonl" in result.stdout


def test_a_transcript_without_a_prompt_cannot_tell(tmp_path, main, wt):
    transcript = write_transcript(
        tmp_path / "t" / "s1.jsonl",
        [tool_entry("Edit", {"file_path": str(wt / "a.py")}, main), result_entry(main)],
    )
    result = _run(main, stop_payload(main, transcript))
    assert result.returncode == 1
    assert "no prompt" in result.stdout


def test_invalid_json_cannot_tell(main):
    result = _run(main, "{not json")
    assert result.returncode == 1
    assert result.stdout.strip() != ""


def test_a_cwd_outside_a_repository_cannot_tell(tmp_path, main):
    outside = tmp_path / "plain"
    outside.mkdir()
    transcript = write_transcript(tmp_path / "t" / "s1.jsonl", [prompt_entry(outside)])
    result = _run(main, stop_payload(outside, transcript))
    assert result.returncode == 1
    assert str(outside) in result.stdout


# `--match` (#80, scope 4): per root, did the turn touch a path in a domain?


def _classify(main: Path, cwd: Path, entries: list[dict], tmp_path: Path, *patterns: str) -> list[str]:
    transcript = write_transcript(tmp_path / "t" / "s1.jsonl", entries)
    args = [arg for pattern in patterns for arg in ("--match", pattern)]
    result = _run(main, stop_payload(cwd, transcript), *args)
    assert result.returncode == 0, result.stdout + result.stderr
    return result.stdout.splitlines()


def _edit(path: Path, cwd: Path) -> dict:
    return tool_entry("Edit", {"file_path": str(path)}, cwd)


def _bash(command: str, cwd: Path) -> dict:
    return tool_entry("Bash", {"command": command}, cwd)


def test_match_an_edit_in_the_domain_is_touched(tmp_path, main, wt):
    entries = [prompt_entry(main), _edit(wt / "internal" / "x" / "a.go", main)]
    assert _classify(main, main, entries, tmp_path, "*.go") == [f"touched\t{wt.resolve()}"]


def _compact_summary(cwd: Path) -> dict:
    # The shape Claude Code writes for an auto-compaction: a user entry with a string
    # content and no isMeta.
    return {
        "type": "user",
        "isCompactSummary": True,
        "isVisibleInTranscriptOnly": True,
        "timestamp": "2026-10-25T10:00:05.000Z",
        "cwd": str(cwd),
        "message": {"role": "user", "content": "This session is being continued from a previous conversation."},
    }


def test_match_a_compaction_summary_does_not_restart_the_turn(tmp_path, main, wt):
    entries = [
        prompt_entry(main),
        _edit(wt / "a.go", main),
        _compact_summary(main),
        _edit(wt / "README.md", main),
    ]
    assert _classify(main, main, entries, tmp_path, "*.go") == [f"touched\t{wt.resolve()}"]


def test_a_compaction_summary_keeps_the_roots_before_it(tmp_path, main, wt):
    entries = [prompt_entry(main), _edit(wt / "a.go", main), _compact_summary(main), _edit(main / "b.go", main)]
    assert sorted(_roots(main, main, entries, tmp_path)) == sorted([str(wt.resolve()), str(main.resolve())])


def test_match_an_edit_outside_the_domain_is_untouched(tmp_path, main, wt):
    entries = [prompt_entry(main), _edit(wt / "README.md", main)]
    assert _classify(main, main, entries, tmp_path, "*.go") == [f"untouched\t{wt.resolve()}"]


def test_match_any_of_several_patterns(tmp_path, main):
    entries = [prompt_entry(main), _edit(main / "go.mod", main)]
    assert _classify(main, main, entries, tmp_path, "*.go", "go.mod") == [f"touched\t{main.resolve()}"]


def test_match_a_pattern_with_a_directory(tmp_path, main):
    outside = [prompt_entry(main), _edit(main / "tests" / "test_a.py", main)]
    assert _classify(main, main, outside, tmp_path, "src/*.py") == [f"untouched\t{main.resolve()}"]
    inside = [prompt_entry(main), _edit(main / "src" / "pkg" / "a.py", main)]
    assert _classify(main, main, inside, tmp_path, "src/*.py") == [f"touched\t{main.resolve()}"]


def test_match_a_bash_write_since_the_prompt_is_touched(tmp_path, main):
    past_this_second()
    t = time.time()
    (main / "a.go").write_text("package a\n", encoding="utf-8")
    entries = [prompt_entry(main, stamp(t)), _bash("ls", main)]
    assert _classify(main, main, entries, tmp_path, "*.go") == [f"touched\t{main.resolve()}"]


def test_match_a_dirty_path_from_an_earlier_turn_is_untouched(tmp_path, main):
    t = time.time()
    old = main / "a.go"
    old.write_text("package a\n", encoding="utf-8")
    os.utime(old, (t - 3600, t - 3600))
    entries = [prompt_entry(main, stamp(t)), _edit(main / "README.md", main)]
    assert _classify(main, main, entries, tmp_path, "*.go") == [f"untouched\t{main.resolve()}"]


def test_match_a_deleted_path_counts(tmp_path, main):
    (main / "a.go").write_text("package a\n", encoding="utf-8")
    git(main, "add", "a.go")
    git(main, "commit", "-qm", "a")
    past_this_second()
    t = time.time()
    (main / "a.go").unlink()
    entries = [prompt_entry(main, stamp(t)), _bash("ls", main)]
    assert _classify(main, main, entries, tmp_path, "*.go") == [f"touched\t{main.resolve()}"]


def test_match_a_commit_in_the_turn_is_touched(tmp_path, main):
    past_this_second()
    t = time.time()
    (main / "a.go").write_text("package a\n", encoding="utf-8")
    git(main, "add", "a.go")
    git(main, "commit", "-qm", "a")
    entries = [prompt_entry(main, stamp(t)), _bash("git commit -qm x", main)]
    assert _classify(main, main, entries, tmp_path, "*.go") == [f"touched\t{main.resolve()}"]


def test_match_a_commit_before_the_turn_is_untouched(tmp_path, main):
    (main / "a.go").write_text("package a\n", encoding="utf-8")
    git(main, "add", "a.go")
    git(main, "commit", "-qm", "a")
    past_this_second()
    t = time.time()
    entries = [prompt_entry(main, stamp(t)), _bash("ls", main)]
    assert _classify(main, main, entries, tmp_path, "*.go") == [f"untouched\t{main.resolve()}"]


def test_match_a_turn_without_writes_judges_the_cwd(tmp_path, main):
    entries = [prompt_entry(main, stamp(time.time()))]
    assert _classify(main, main, entries, tmp_path, "*.go") == [f"untouched\t{main.resolve()}"]


def test_match_a_prompt_without_timestamp_is_touched(tmp_path, main):
    prompt = prompt_entry(main)
    del prompt["timestamp"]
    entries = [prompt, _bash("ls", main)]
    assert _classify(main, main, entries, tmp_path, "*.go") == [f"touched\t{main.resolve()}"]


def test_match_an_edit_in_an_unborn_repo_is_touched(tmp_path):
    repo = tmp_path / "unborn"
    repo.mkdir()
    git(repo, "init", "-q", "-b", "main")
    entries = [prompt_entry(repo, stamp(time.time())), _edit(repo / "a.go", repo)]
    assert _classify(repo, repo, entries, tmp_path, "*.go") == [f"touched\t{repo.resolve()}"]


def test_match_an_unborn_repo_without_a_domain_path_is_untouched(tmp_path):
    # An unborn HEAD skips the reflog source; it is no git failure.
    repo = tmp_path / "unborn"
    repo.mkdir()
    git(repo, "init", "-q", "-b", "main")
    entries = [prompt_entry(repo, stamp(time.time())), _edit(repo / "README.md", repo)]
    assert _classify(repo, repo, entries, tmp_path, "*.go") == [f"untouched\t{repo.resolve()}"]


def test_match_judges_each_root(tmp_path, main, wt):
    entries = [prompt_entry(main, stamp(time.time())), _edit(wt / "a.go", main), _bash("ls", main)]
    assert _classify(main, main, entries, tmp_path, "*.go") == [
        f"untouched\t{main.resolve()}",
        f"touched\t{wt.resolve()}",
    ]


def test_match_an_edit_in_a_worktree_does_not_touch_the_main_checkout(tmp_path, main, wt):
    # The worktree lies under the main checkout, at `.claude/worktrees/wt`:
    # its path counts only for its own checkout.
    entries = [
        prompt_entry(main, stamp(time.time())),
        _edit(wt / "a.go", main),
        _edit(main / "README.md", main),
    ]
    assert _classify(main, main, entries, tmp_path, "*.go") == [
        f"untouched\t{main.resolve()}",
        f"touched\t{wt.resolve()}",
    ]


def test_match_a_root_git_cannot_read_is_touched(tmp_path, main):
    # A broken index: `rev-parse` still answers, `git status` fails.
    (main / ".git" / "index").write_bytes(b"garbage")
    entries = [prompt_entry(main, stamp(time.time())), _bash("ls", main)]
    assert _classify(main, main, entries, tmp_path, "*.go") == [f"touched\t{main.resolve()}"]


def test_only_a_json_error_is_reported_as_invalid_json(monkeypatch, capsys):
    # A ValueError from git output (`bash_writes.parse_reflog`) is no JSON error.
    from lighthouse_lib import turn_roots

    def broken(payload, patterns):
        raise ValueError("not enough values to unpack")

    monkeypatch.setattr(turn_roots, "classify", broken)
    monkeypatch.setattr(sys, "stdin", io.StringIO(json.dumps({"cwd": "/x"})))
    with pytest.raises(ValueError, match="unpack"):
        turn_roots.main(["--match", "*.go"])
    assert "not valid JSON" not in capsys.readouterr().out

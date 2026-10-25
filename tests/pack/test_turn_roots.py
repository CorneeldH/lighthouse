"""`pack/core/lib/turn_roots.py` (#54): in which checkouts did this turn write?

The Stop hooks check those checkouts instead of the payload `cwd`, which is the
shell's directory at Stop: a session in the main checkout that edits a worktree
through absolute paths stops with `cwd` = the main checkout.

The script runs as a subprocess against real repositories and a transcript on
disk, the way the hooks call it.
"""

import json
import os
import subprocess
import sys
from pathlib import Path

import pytest

from tests.pack._testkit import (
    INSTALLED_LIB,
    git,
    init_repo,
    prompt_entry,
    result_entry,
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


def _run(main: Path, payload: dict | str) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, str(SCRIPT)],
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

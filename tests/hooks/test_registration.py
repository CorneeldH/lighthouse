"""The guards as Claude Code runs them: the command from `.claude/settings.json`.

These tests do not call the scripts directly. They read the registered
command, expand `$CLAUDE_PROJECT_DIR` through a shell, and feed it a
PreToolUse payload. A wrong path, a missing execute bit or a missing
registration fails here, even when the script itself is fine.
"""

import json
import os
import subprocess
from pathlib import Path

from tests.hooks._hook_testkit import bash_payload, git, init_repo

ROOT = Path(__file__).resolve().parents[2]
SETTINGS = ROOT / ".claude" / "settings.json"


def _pretooluse() -> list[dict]:
    return json.loads(SETTINGS.read_text(encoding="utf-8"))["hooks"]["PreToolUse"]


def _registered(matcher: str, script: str) -> str:
    commands = [
        hook["command"]
        for block in _pretooluse()
        if block.get("matcher") == matcher
        for hook in block["hooks"]
        if script in hook["command"]
    ]
    assert len(commands) == 1, f"{script} under matcher {matcher!r}: {commands}"
    return commands[0]


def _run_registered(command: str, payload: dict, cwd: Path) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["sh", "-c", command],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(cwd),
        env=dict(os.environ, CLAUDE_PROJECT_DIR=str(ROOT)),
        check=False,
    )


def test_registered_bash_hook_blocks_a_commit_on_main(tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo)

    result = _run_registered(
        _registered("Bash", "main-commit-guard.sh"),
        bash_payload('git commit -m "x"', repo),
        cwd=repo,
    )

    assert result.returncode == 2, result.stdout
    assert "Main-commit guard: blocked" in result.stderr


def test_registered_bash_hook_allows_a_commit_on_a_branch(tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo)
    git(repo, "checkout", "-q", "-b", "issue-1-x")

    result = _run_registered(
        _registered("Bash", "main-commit-guard.sh"),
        bash_payload('git commit -m "x"', repo),
        cwd=repo,
    )

    assert result.returncode == 0, result.stderr


def test_registered_edit_hook_warns_in_main_checkout(tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo)
    payload = {
        "tool_name": "Edit",
        "cwd": str(repo),
        "tool_input": {"file_path": str(repo / "README.md")},
    }

    result = _run_registered(
        _registered("Edit|Write", "worktree-guard.sh"), payload, cwd=repo
    )

    assert result.returncode == 0, result.stderr
    assert "Worktree guard:" in json.loads(result.stdout)["systemMessage"]


def test_bash_pretooluse_hooks_live_in_one_block():
    """A second Bash block is how a hook gets doubled or lost by accident."""
    assert len([b for b in _pretooluse() if b.get("matcher") == "Bash"]) == 1


def test_allow_main_sentinel_is_not_tracked():
    """Committed, it would switch the guard off for good."""
    tracked = subprocess.run(
        ["git", "-C", str(ROOT), "ls-files", "--", ".claude/allow-main"],
        capture_output=True,
        text=True,
        check=False,
    )
    assert tracked.stdout.strip() == "", "'.claude/allow-main' is in git"


def test_allow_main_sentinel_is_not_ignored():
    """Ignored, it would vanish from `git status` and be forgotten."""
    ignored = subprocess.run(
        ["git", "-C", str(ROOT), "check-ignore", "-q", ".claude/allow-main"],
        check=False,
    )
    assert ignored.returncode == 1, "'.claude/allow-main' is git-ignored"

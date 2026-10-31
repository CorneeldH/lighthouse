"""Lighthouse runs its own pack/core: the guards as Claude Code runs them in this repo.

These tests do not call the scripts directly. They read the registered command from
lighthouse's own `.claude/settings.json`, expand `$CLAUDE_PROJECT_DIR` through a shell,
and feed it a PreToolUse payload. A wrong path, a missing execute bit or a missing
registration fails here, even when the script itself is fine.
"""

import json
import os
import subprocess
import tomllib
from pathlib import Path

from tests.pack._testkit import bash_payload, git, init_repo

ROOT = Path(__file__).resolve().parents[1]
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
    assert "/.claude/hooks/lighthouse/" in commands[0], commands[0]
    return commands[0]


def _run_registered(command: str, payload: dict, cwd: Path) -> subprocess.CompletedProcess:
    # Without the LIGHTHOUSE_* overrides of the test session, the hook finds its lib
    # the way it does in this repo.
    env = {k: v for k, v in os.environ.items() if not k.startswith("LIGHTHOUSE_")}
    return subprocess.run(
        ["sh", "-c", command],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(cwd),
        env=dict(env, CLAUDE_PROJECT_DIR=str(ROOT)),
        check=False,
    )


def test_registered_bash_hook_blocks_a_commit_on_main(tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo)

    result = _run_registered(
        _registered("Bash", "main-commit-gate.sh"),
        bash_payload('git commit -m "x"', repo),
        cwd=repo,
    )

    assert result.returncode == 2, result.stdout
    assert "Main-commit gate: blocked" in result.stderr
    assert "COULD NOT CHECK" not in result.stderr


def test_registered_bash_hook_allows_a_commit_on_a_branch(tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo)
    git(repo, "checkout", "-q", "-b", "issue-1-x")

    result = _run_registered(
        _registered("Bash", "main-commit-gate.sh"),
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
        _registered("Edit|Write", "worktree-check.sh"), payload, cwd=repo
    )

    assert result.returncode == 0, result.stderr
    assert "Worktree check:" in json.loads(result.stdout)["systemMessage"]


def test_bash_pretooluse_hooks_live_in_one_block():
    """A second Bash block is how a hook gets doubled or lost by accident."""
    assert len([b for b in _pretooluse() if b.get("matcher") == "Bash"]) == 1


def test_no_registration_outside_lighthouse_hooks():
    """The bootstrap of (a0) is gone: every hook registered here is one init manages."""
    settings = json.loads(SETTINGS.read_text(encoding="utf-8"))
    commands = [
        hook["command"]
        for blocks in settings["hooks"].values()
        for block in blocks
        for hook in block["hooks"]
    ]
    assert commands
    for command in commands:
        assert command.startswith('"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/'), command


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


def _registered_in(event: str, matcher: str | None, script: str) -> list[str]:
    settings = json.loads(SETTINGS.read_text(encoding="utf-8"))
    return [
        hook["command"]
        for block in settings["hooks"].get(event, [])
        if matcher is None or block.get("matcher") == matcher
        for hook in block["hooks"]
        if script in hook["command"]
    ]


def test_go_hooks_registered():
    """Fails when init stops registering the pack/go hooks under their events."""
    assert len(_registered_in("PostToolUse", "Edit|Write", "gofmt-check.sh")) == 1
    assert len(_registered_in("Stop", None, "go-lint-gate.sh")) == 1


def test_test_gate_uses_selectors():
    """Fails when the gate stops choosing its Go packages, pytest files and web checks from the commit."""
    result = subprocess.run(
        ["python3", "-m", "lighthouse_lib.config", "test_gate.command"],
        capture_output=True,
        text=True,
        cwd=str(ROOT),
        env=dict(os.environ, PYTHONPATH=str(ROOT / ".lighthouse" / "lib")),
        check=False,
    )
    assert result.stdout.strip() == (
        "python3 .lighthouse/scripts/gate_go.py -- go test -race"
        " && python3 pack/python/scripts/gate_python.py -- uv run pytest -q"
        " && python3 .lighthouse/scripts/gate_node.py --dir web --also .github/workflows/ci.yml"
        " --also lighthouse.toml --drift gen=src/api -- lint typecheck test"
    ), result.stdout + result.stderr


def test_node_pack_installed():
    """Fails when the node pack is not recorded in the manifest or its script drifts from the pack."""
    manifest = tomllib.loads((ROOT / ".lighthouse" / "manifest.toml").read_text())
    assert "node" in manifest["packages"]
    # init stamps the installed copy with a "# lighthouse: node@<version>" line.
    installed = [
        line
        for line in (ROOT / ".lighthouse" / "scripts" / "gate_node.py")
        .read_text(encoding="utf-8")
        .splitlines(True)
        if not line.startswith("# lighthouse: node@")
    ]
    assert "".join(installed) == (ROOT / "pack" / "node" / "scripts" / "gate_node.py").read_text()

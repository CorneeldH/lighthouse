"""Every hook from `hooks.json` under a `lighthouse.toml` with only `[project]` and `[forge]`.

Design choice 8: `pack/core` works in a repo without the optional keys.
Per registration the installed hook runs with the payload that touches it, in
a feature-branch worktree: no block, no `Traceback`.
"""

import json
import os
import shutil
import subprocess
import tomllib
from pathlib import Path

import pytest

from tests.pack._testkit import (
    CORE,
    MINIMAL_TOML,
    bash_payload,
    git,
    guard_env,
    init_repo,
    install_core,
)

HOOKS_JSON = CORE / "hooks" / "hooks.json"
PLUGIN_HOOKS = "${CLAUDE_PLUGIN_ROOT}/hooks/"


def _registrations() -> list[tuple[str, str, str]]:
    registered = json.loads(HOOKS_JSON.read_text(encoding="utf-8"))
    return [
        (event, block.get("matcher", ""), hook["command"].removeprefix(PLUGIN_HOOKS))
        for event, blocks in registered["hooks"].items()
        for block in blocks
        for hook in block["hooks"]
    ]


REGISTRATIONS = _registrations()


def _edit_payload(event: str, path: Path, cwd: Path) -> dict:
    return {
        "hook_event_name": event,
        "tool_name": "Edit",
        "cwd": str(cwd),
        "tool_input": {"file_path": str(path)},
    }


def _payload(event: str, script: str, worktree: Path, project: Path) -> dict:
    """The payload that really touches this hook, up to and including its config read."""
    name = script.split()[0]
    if name == "worktree-check.sh":
        # A write in the main checkout on main: only there does the guard read
        # forge.default_branch, and warn (without blocking).
        doc = project / "doc.md"
        doc.write_text("# doc\n", encoding="utf-8")
        return _edit_payload(event, doc, worktree)
    if name == "md-link-check.sh":
        doc = worktree / "doc.md"
        doc.write_text("# doc\n\nSee [the readme](README.md).\n", encoding="utf-8")
        return _edit_payload(event, doc, worktree)
    if name == "code-language-check.sh":
        # Without `code_language` in MINIMAL_TOML the hook is off: exit 0.
        code = worktree / "mod.py"
        code.write_text("x = 1\n", encoding="utf-8")
        return _edit_payload(event, code, worktree)
    if name == "usage-gate.sh":
        return {
            "hook_event_name": event,
            "session_id": "s1",
            "cwd": str(worktree),
            "tool_name": "Bash",
            "tool_input": {"command": "ls"},
        }
    if name == "main-commit-gate.sh":
        # A commit on a feature branch: the guard reads forge.default_branch.
        cmd = "git commit -m x"
    elif name == "commit-test-gate.sh":
        cmd = "git commit -m x"
    elif name == "pr-create-gate.sh":
        cmd = "gh pr create --body 'Refs #5 — waiting for the deploy'"
    elif name == "bash-write-check.sh":
        cmd = "echo x > a.md"
        if script.endswith(" post"):
            (worktree / "a.md").write_text("x\n", encoding="utf-8")
    else:
        raise AssertionError(f"no payload for {script}: add one to this test")
    return dict(bash_payload(cmd, worktree), hook_event_name=event)


@pytest.mark.parametrize(
    ("event", "matcher", "script"),
    REGISTRATIONS,
    ids=[f"{e}:{m}:{s}" for e, m, s in REGISTRATIONS],
)
def test_hooks_with_minimal_config(
    tmp_path: Path, event: str, matcher: str, script: str
) -> None:
    root = tmp_path / "lighthouse"
    install_core(root, toml=MINIMAL_TOML)
    project = tmp_path / "project"
    init_repo(project)
    worktree = tmp_path / "feature"
    git(project, "worktree", "add", "-q", "-b", "feature", str(worktree))
    payload = _payload(event, script, worktree, project)
    name, *args = script.split()
    env = guard_env(
        tmp_path,
        LIGHTHOUSE_REPO_ROOT=str(root),
        CLAUDE_PROJECT_DIR=str(project),
        LIGHTHOUSE_HOST_TOML=str(tmp_path / "no-host.toml"),
        LIGHTHOUSE_GATE_SLOT_DIR=str(tmp_path / "slots"),
        XDG_RUNTIME_DIR=str(tmp_path),
    )

    result = subprocess.run(
        [str(root / ".claude" / "hooks" / "lighthouse" / name), *args],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(worktree),
        env=env,
        check=False,
    )

    assert result.returncode == 0, (result.stdout, result.stderr)
    assert "Traceback" not in result.stderr + result.stdout
    assert "COULD NOT CHECK" not in result.stderr + result.stdout


def test_every_registration_has_a_payload() -> None:
    assert {s.split()[0] for _, _, s in REGISTRATIONS} == {
        "main-commit-gate.sh",
        "commit-test-gate.sh",
        "bash-write-check.sh",
        "pr-create-gate.sh",
        "worktree-check.sh",
        "md-link-check.sh",
        "code-language-check.sh",
        "usage-gate.sh",
    }



PRE_BASH = [s for e, m, s in REGISTRATIONS if e == "PreToolUse" and m == "Bash"]
# Per PreToolUse-Bash hook: the command it guards. `bash-write-check.sh
# pre` never blocks (a PreToolUse exit 2 blocks the call itself).
GUARDED = {
    "main-commit-gate.sh": "git commit -m x",
    "commit-test-gate.sh pre": "git commit -m x",
    "pr-create-gate.sh": "gh pr create --body x",
}


def _without_lib(tmp_path: Path, script: str, cmd: str) -> subprocess.CompletedProcess:
    """The hook from an installation whose `.lighthouse/lib` is missing."""
    root = tmp_path / "lighthouse"
    install_core(root, toml=MINIMAL_TOML)
    shutil.rmtree(root / ".lighthouse" / "lib")
    project = tmp_path / "project"
    init_repo(project, branch="feature")
    name, *args = script.split()
    return subprocess.run(
        [str(root / ".claude" / "hooks" / "lighthouse" / name), *args],
        input=json.dumps(bash_payload(cmd, project)),
        capture_output=True,
        text=True,
        cwd=str(project),
        env=dict(os.environ, LIGHTHOUSE_REPO_ROOT=str(root), CLAUDE_PROJECT_DIR=str(project)),
        check=False,
    )


@pytest.mark.parametrize("script", PRE_BASH)
def test_without_lib_a_plain_command_passes(tmp_path: Path, script: str) -> None:
    # A gitignore with `lib/` leaves `.lighthouse/lib/` out of a clone: that
    # must not lock up every Bash command.
    result = _without_lib(tmp_path, script, "ls")

    assert result.returncode == 0, (result.stdout, result.stderr)


@pytest.mark.parametrize("script", sorted(GUARDED))
def test_without_lib_the_guarded_command_blocks(tmp_path: Path, script: str) -> None:
    result = _without_lib(tmp_path, script, GUARDED[script])

    assert result.returncode == 2, (result.stdout, result.stderr)
    assert "COULD NOT CHECK" in result.stderr
    assert ".lighthouse/lib" in result.stderr


def test_guarded_covers_every_blocking_bash_hook() -> None:
    blocking = {s for s in PRE_BASH if s != "bash-write-check.sh pre"}
    assert blocking == set(GUARDED)


def test_hooks_that_read_config_require_tomllib() -> None:
    # config.py needs tomllib (Python 3.11+); `lighthouse init` reports a
    # missing tool only for the units that requires.toml names.
    requires = tomllib.loads((CORE / "requires.toml").read_text(encoding="utf-8"))
    readers = {
        hook.stem
        for hook in (CORE / "hooks").glob("*.sh")
        if "config.py" in hook.read_text(encoding="utf-8")
    }
    assert readers
    assert readers <= set(requires["tomllib"]), readers - set(requires["tomllib"])

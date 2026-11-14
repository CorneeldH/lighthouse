"""Every hook from `hooks.json` under a `lighthouse.toml` with only `[project]` and `[forge]`.

Design choice 8: `pack/core` works in a repo without the optional keys.
Per registration the installed hook runs with the payload that touches it, in
a feature-branch worktree: no block, no `Traceback`.
"""

import json
import os
import shutil
import subprocess
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
    shim_dir,
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


def _name(script: str) -> str:
    """The hook a registration runs: `<name>` for `run.sh <name> [stand]`, else the script."""
    words = script.split()
    return words[1] if words[0] == "run.sh" else words[0]


def _edit_payload(event: str, path: Path, cwd: Path) -> dict:
    return {
        "hook_event_name": event,
        "tool_name": "Edit",
        "cwd": str(cwd),
        "tool_input": {"file_path": str(path)},
    }


def _payload(event: str, script: str, worktree: Path, project: Path) -> dict:
    """The payload that really touches this hook, up to and including its config read."""
    name = _name(script)
    if name == "worktree-check":
        # A write in the main checkout on main: only there does the guard read
        # forge.default_branch, and warn (without blocking).
        doc = project / "doc.md"
        doc.write_text("# doc\n", encoding="utf-8")
        return _edit_payload(event, doc, worktree)
    if name == "md-link-check":
        doc = worktree / "doc.md"
        doc.write_text("# doc\n\nSee [the readme](README.md).\n", encoding="utf-8")
        return _edit_payload(event, doc, worktree)
    if name == "code-language-check":
        # Without `code_language` in MINIMAL_TOML the hook is off: exit 0.
        code = worktree / "mod.py"
        code.write_text("x = 1\n", encoding="utf-8")
        return _edit_payload(event, code, worktree)
    if name == "usage-gate":
        return {
            "hook_event_name": event,
            "session_id": "s1",
            "cwd": str(worktree),
            "tool_name": "Bash",
            "tool_input": {"command": "ls"},
        }
    if name == "permission-check":
        return {
            "hook_event_name": event,
            "session_id": "s1",
            "cwd": str(worktree),
            "notification_type": "permission_prompt",
            "message": "Claude needs your permission to use Write",
        }
    if name == "main-commit-gate":
        # A commit on a feature branch: the guard reads forge.default_branch.
        cmd = "git commit -m x"
    elif name == "commit-test-gate":
        cmd = "git commit -m x"
    elif name == "pr-create-gate.sh":
        cmd = "gh pr create --repo Acme/widgets --body 'Refs #5 — waiting for the deploy'"
    elif name == "bash-write-check":
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
    tmp_path: Path, event: str, matcher: str, script: str, lighthouse_bin: Path
) -> None:
    root = tmp_path / "lighthouse"
    install_core(root, toml=MINIMAL_TOML)
    project = tmp_path / "project"
    init_repo(project)
    worktree = tmp_path / "feature"
    git(project, "worktree", "add", "-q", "-b", "feature", str(worktree))
    payload = _payload(event, script, worktree, project)
    name, *args = script.split()
    # guard_env takes every `lighthouse` off the PATH; a Go hook behind run.sh needs
    # the binary of this checkout.
    go_hook = {}
    if name == "run.sh":
        go_hook["PATH"] = f"{lighthouse_bin.parent}{os.pathsep}{os.environ['PATH']}"
    env = guard_env(
        tmp_path,
        **go_hook,
        LIGHTHOUSE_REPO_ROOT=str(root),
        CLAUDE_PROJECT_DIR=str(project),
        LIGHTHOUSE_HOST_TOML=str(tmp_path / "no-host.toml"),
        LIGHTHOUSE_GATE_SLOT_DIR=str(tmp_path / "slots"),
        XDG_RUNTIME_DIR=str(tmp_path),
    )
    if name == "run.sh":
        # guard_env keeps the real `lighthouse` off the PATH; run.sh needs the one of this checkout.
        env["PATH"] = f"{lighthouse_bin.parent}{os.pathsep}{env['PATH']}"

    # `run.sh` and the gates of `bash-write-check` need `lighthouse`: the shim of `_testkit`
    # runs the binary of this checkout and never starts a real `lighthouse usage refresh`.
    env["PATH"] = f"{shim_dir()}{os.pathsep}{env['PATH']}"

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
    assert {_name(s) for _, _, s in REGISTRATIONS} == {
        "main-commit-gate",
        "commit-test-gate",
        "bash-write-check",
        "pr-create-gate.sh",
        "worktree-check",
        "md-link-check",
        "code-language-check",
        "usage-gate",
        "permission-check",
    }



PRE_BASH = [s for e, m, s in REGISTRATIONS if e == "PreToolUse" and m == "Bash"]
# Per PreToolUse-Bash hook: the command it guards. `run.sh bash-write-check
# pre` never blocks (a PreToolUse exit 2 blocks the call itself).
GUARDED = {
    "run.sh main-commit-gate": "git commit -m x",
    "run.sh commit-test-gate pre": "git commit -m x",
    "pr-create-gate.sh": "gh pr create --repo Acme/widgets --body x",
}
# A Go hook behind run.sh reads no `.lighthouse/lib`: only the others can miss it.
LIB_GUARDED = {s: cmd for s, cmd in GUARDED.items() if not s.startswith("run.sh ")}


def _without_lib(
    tmp_path: Path, script: str, cmd: str, branch: str = "feature"
) -> subprocess.CompletedProcess:
    """The hook from an installation whose `.lighthouse/lib` is missing."""
    root = tmp_path / "lighthouse"
    install_core(root, toml=MINIMAL_TOML)
    shutil.rmtree(root / ".lighthouse" / "lib")
    project = tmp_path / "project"
    init_repo(project, branch=branch)
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


@pytest.mark.usefixtures("lighthouse_bin")
@pytest.mark.parametrize("script", PRE_BASH)
def test_without_lib_a_plain_command_passes(tmp_path: Path, script: str) -> None:
    # A gitignore with `lib/` leaves `.lighthouse/lib/` out of a clone: that
    # must not lock up every Bash command.
    result = _without_lib(tmp_path, script, "ls")

    assert result.returncode == 0, (result.stdout, result.stderr)


@pytest.mark.parametrize("script", sorted(LIB_GUARDED))
def test_without_lib_the_guarded_command_blocks(tmp_path: Path, script: str) -> None:
    result = _without_lib(tmp_path, script, LIB_GUARDED[script])

    assert result.returncode == 2, (result.stdout, result.stderr)
    assert "COULD NOT CHECK" in result.stderr
    assert ".lighthouse/lib" in result.stderr


@pytest.mark.usefixtures("lighthouse_bin")
def test_without_lib_the_go_gate_still_blocks_a_commit_on_main(tmp_path: Path) -> None:
    # The Go main-commit-gate needs no `.lighthouse/lib`: a missing one changes nothing.
    script = "run.sh main-commit-gate"
    result = _without_lib(tmp_path, script, GUARDED[script], branch="main")

    assert result.returncode == 2, (result.stdout, result.stderr)
    assert "Main-commit gate: blocked" in result.stderr
    assert "COULD NOT CHECK" not in result.stderr


def test_guarded_covers_every_blocking_bash_hook() -> None:
    blocking = {s for s in PRE_BASH if s != "run.sh bash-write-check pre"}
    assert blocking == set(GUARDED)

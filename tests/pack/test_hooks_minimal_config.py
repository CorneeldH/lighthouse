"""Elke hook uit `hooks.json` onder een `lighthouse.toml` met alleen `[project]` en `[forge]`.

Ontwerpkeuze 8: `pack/core` werkt in een repo zonder de optionele sleutels.
Per registratie draait de geïnstalleerde hook met de payload die hem raakt, in
een featurebranch-worktree: geen blokkade, geen `Traceback`.
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
    """De payload die deze hook echt raakt, tot en met zijn configlezing."""
    name = script.split()[0]
    if name == "worktree-guard.sh":
        # Een write in de hoofdcheckout op main: pas daar leest de guard
        # forge.default_branch, en waarschuwt hij (zonder te blokkeren).
        doc = project / "doc.md"
        doc.write_text("# doc\n", encoding="utf-8")
        return _edit_payload(event, doc, worktree)
    if name == "worktree-linkcheck.sh":
        doc = worktree / "doc.md"
        doc.write_text("# doc\n\nSee [the readme](README.md).\n", encoding="utf-8")
        return _edit_payload(event, doc, worktree)
    if name == "code-language.sh":
        # Without `code_language` in MINIMAL_TOML the hook is off: exit 0.
        code = worktree / "mod.py"
        code.write_text("x = 1\n", encoding="utf-8")
        return _edit_payload(event, code, worktree)
    if name == "usage-guard.sh":
        return {
            "hook_event_name": event,
            "session_id": "s1",
            "cwd": str(worktree),
            "tool_name": "Bash",
            "tool_input": {"command": "ls"},
        }
    if name == "main-commit-guard.sh":
        # Een commit op een featurebranch: de guard leest forge.default_branch.
        cmd = "git commit -m x"
    elif name == "pre-commit-tests.sh":
        cmd = "git commit -m x"
    elif name == "pr-create-gate.sh":
        cmd = "gh pr create --body 'Refs #5 — waiting for the deploy'"
    elif name == "bash-write-gates.sh":
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
        "main-commit-guard.sh",
        "pre-commit-tests.sh",
        "bash-write-gates.sh",
        "pr-create-gate.sh",
        "worktree-guard.sh",
        "worktree-linkcheck.sh",
        "code-language.sh",
        "usage-guard.sh",
    }



PRE_BASH = [s for e, m, s in REGISTRATIONS if e == "PreToolUse" and m == "Bash"]
# Per PreToolUse-Bash-hook: het commando dat hij bewaakt. `bash-write-gates.sh
# pre` blokkeert nooit (een PreToolUse-exit 2 blokkeert de call zelf).
GUARDED = {
    "main-commit-guard.sh": "git commit -m x",
    "pre-commit-tests.sh": "git commit -m x",
    "pr-create-gate.sh": "gh pr create --body x",
}


def _without_lib(tmp_path: Path, script: str, cmd: str) -> subprocess.CompletedProcess:
    """De hook uit een installatie waarvan `.lighthouse/lib` ontbreekt."""
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
    # Een gitignore met `lib/` laat `.lighthouse/lib/` weg bij een clone: dat
    # mag niet elk Bash-commando op slot zetten.
    result = _without_lib(tmp_path, script, "ls")

    assert result.returncode == 0, (result.stdout, result.stderr)


@pytest.mark.parametrize("script", sorted(GUARDED))
def test_without_lib_the_guarded_command_blocks(tmp_path: Path, script: str) -> None:
    result = _without_lib(tmp_path, script, GUARDED[script])

    assert result.returncode == 2, (result.stdout, result.stderr)
    assert "COULD NOT CHECK" in result.stderr
    assert ".lighthouse/lib" in result.stderr


def test_guarded_covers_every_blocking_bash_hook() -> None:
    blocking = {s.split()[0] for s in PRE_BASH if s != "bash-write-gates.sh pre"}
    assert blocking == set(GUARDED)


def test_hooks_that_read_config_require_tomllib() -> None:
    # config.py heeft tomllib nodig (Python 3.11+); `lighthouse init` meldt een
    # ontbrekende tool alleen voor de eenheden die requires.toml noemt.
    requires = tomllib.loads((CORE / "requires.toml").read_text(encoding="utf-8"))
    readers = {
        hook.stem
        for hook in (CORE / "hooks").glob("*.sh")
        if "config.py" in hook.read_text(encoding="utf-8")
    }
    assert readers
    assert readers <= set(requires["tomllib"]), readers - set(requires["tomllib"])

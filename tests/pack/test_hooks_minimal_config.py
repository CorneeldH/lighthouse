"""Elke hook uit `hooks.json` onder een `lighthouse.toml` met alleen `[project]` en `[forge]`.

Ontwerpkeuze 8: `pack/core` werkt in een repo zonder de optionele sleutels.
Per registratie draait de geïnstalleerde hook met de payload die hem raakt, in
een featurebranch-worktree: geen blokkade, geen `Traceback`.
"""

import json
import os
import subprocess
from pathlib import Path

import pytest

from tests.pack._testkit import (
    CORE,
    MINIMAL_TOML,
    bash_payload,
    git,
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


def _payload(event: str, script: str, worktree: Path) -> dict:
    """De payload die deze hook echt raakt, of een fout als er geen is."""
    name = script.split()[0]
    doc = worktree / "doc.md"
    if name in ("worktree-guard.sh", "worktree-linkcheck.sh"):
        doc.write_text("# doc\n\nSee [the readme](README.md).\n", encoding="utf-8")
        return _edit_payload(event, doc, worktree)
    if name == "main-commit-guard.sh":
        cmd = "git status"
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
    payload = _payload(event, script, worktree)
    name, *args = script.split()
    env = dict(
        os.environ,
        LIGHTHOUSE_REPO_ROOT=str(root),
        CLAUDE_PROJECT_DIR=str(project),
        LIGHTHOUSE_HOST_TOML=str(tmp_path / "no-host.toml"),
        LIGHTHOUSE_GATE_SLOT_DIR=str(tmp_path / "slots"),
        XDG_RUNTIME_DIR=str(tmp_path),
    )
    env.pop("LIGHTHOUSE_TOML", None)

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
    assert "Traceback" not in result.stderr
    assert "Traceback" not in result.stdout


def test_every_registration_has_a_payload() -> None:
    assert {s.split()[0] for _, _, s in REGISTRATIONS} == {
        "main-commit-guard.sh",
        "pre-commit-tests.sh",
        "bash-write-gates.sh",
        "pr-create-gate.sh",
        "worktree-guard.sh",
        "worktree-linkcheck.sh",
    }

"""The Stop hook `go-check.sh` of `pack/go`: it runs `golangci-lint run ./...` once per
turn, blocks on findings (a vet finding and a build that fails included), and says so
- without blocking - when it could not check.

The hook is bash. These tests run it as a subprocess in a real git repository with
`go` and `golangci-lint` as stubs on the PATH; the stubs log their argv to STUB_LOG.
"""

import json
import os
import subprocess
from pathlib import Path

import pytest

from tests.pack._testkit import (
    git,
    install_pack,
    prompt_entry,
    result_entry,
    stop_payload,
    tool_bin,
    tool_entry,
    write_transcript,
)
from tests.pack.go.conftest import GO_HOOKS

HOOK = GO_HOOKS / "go-check.sh"
UNCHECKED = "go-check: COULD NOT CHECK — "
NOT_CHECKED = "go-check: not checked — no go.mod in the repository root"
TOOLS = ("bash", "jq", "git", "cat", "tr", "head", "sed", "python3", "mktemp", "rm")
GO_STUB = """#!/bin/bash
echo "go $*" >> "$STUB_LOG"
exit 0
"""
# With STUB_FAIL_IN set, `run` finds STUB_RUN_OUT only in that directory.
LINT_STUB = """#!/bin/bash
echo "$* @ $PWD" >> "$STUB_LOG"
case "$1" in
  version) echo "${STUB_VERSION:-2.15.0}" ;;
  config) exit ${STUB_VERIFY_RC:-0} ;;
  run)
    if [ -n "$STUB_FAIL_IN" ]; then
      [ "$PWD" = "$STUB_FAIL_IN" ] || exit 0
      echo "$STUB_RUN_OUT"; exit 1
    fi
    echo "$STUB_RUN_OUT"; exit ${STUB_RUN_RC:-0} ;;
esac
"""
VET_FINDING = "a.go:3:2: printf: fmt.Printf format %d has arg s of wrong type string (govet)"
BUILD_FAILURE = "a.go:3:17: undefined: x (typecheck)"
LINT_FINDING = "a.go:5:22: Error return value of `os.Remove` is not checked (errcheck)"


def _repo(tmp_path: Path, go_mod: bool = True, pin: str | None = None) -> Path:
    root = tmp_path / "repo"
    root.mkdir()
    subprocess.run(["git", "init", "-q"], cwd=root, check=True)
    install_pack("go", root)
    if go_mod:
        (root / "go.mod").write_text("module example.com/m\n\ngo 1.26\n")
    if pin is not None:
        (root / ".golangci-version").write_text(pin)
    return root


def _env(tmp_path: Path, **extra: str) -> dict:
    bin_dir = tool_bin(
        tmp_path / "bin", TOOLS, stubs={"go": GO_STUB, "golangci-lint": LINT_STUB}
    )
    return {
        "PATH": f"{bin_dir}:{os.environ['PATH']}",
        "STUB_LOG": str(tmp_path / "stub.log"),
        **extra,
    }


def _run(
    root: Path, env: dict | None = None, payload: dict | str | None = None
) -> subprocess.CompletedProcess:
    if payload is None:
        # A turn without tool calls: the hook falls back to the payload `cwd`.
        transcript = write_transcript(root.parent / "transcript.jsonl", [prompt_entry(root)])
        payload = stop_payload(root, transcript)
    return subprocess.run(
        [str(HOOK)],
        input=payload if isinstance(payload, str) else json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(root),
        env=dict(os.environ, CLAUDE_PROJECT_DIR=str(root), **(env or {})),
        check=False,
    )


def _message(result: subprocess.CompletedProcess) -> str:
    assert result.returncode == 0, result.stderr
    return json.loads(result.stdout)["systemMessage"]


def _log(env: dict) -> str:
    log = Path(env["STUB_LOG"])
    return log.read_text() if log.exists() else ""


def test_clean_passes(tmp_path):
    env = _env(tmp_path)
    result = _run(_repo(tmp_path), env)
    assert result.returncode == 0, result.stderr
    assert result.stdout == ""
    log = _log(env)
    assert "run ./..." in log
    assert "vet" not in log


@pytest.mark.parametrize(
    "out", [VET_FINDING, BUILD_FAILURE, LINT_FINDING], ids=["vet", "build", "lint"]
)
def test_finding_blocks(tmp_path, out):
    env = _env(tmp_path, STUB_RUN_RC="1", STUB_RUN_OUT=out)
    result = _run(_repo(tmp_path), env)
    assert result.returncode == 2, result.stdout
    lines = result.stderr.splitlines()
    assert lines[0] == f"go-check: golangci-lint finds problems in {os.path.realpath(tmp_path / 'repo')}."
    assert lines[1] == (
        "Fix them in the code — do not add a //nolint over a finding. "
        "If you really want a linter off, ask."
    )
    assert out in result.stderr
    assert "COULD NOT CHECK" not in result.stderr


def test_lint_failure_could_not_check(tmp_path):
    result = _run(_repo(tmp_path), _env(tmp_path, STUB_RUN_RC="3"))
    assert _message(result).startswith(UNCHECKED)


def test_invalid_config_could_not_check(tmp_path):
    env = _env(tmp_path, STUB_VERIFY_RC="3")
    assert _message(_run(_repo(tmp_path), env)).startswith(UNCHECKED)
    assert "run ./..." not in _log(env)


def test_no_config_file_lints_with_defaults(tmp_path):
    env = _env(tmp_path, STUB_VERIFY_RC="6")
    result = _run(_repo(tmp_path), env)
    assert result.returncode == 0, result.stderr
    assert result.stdout == ""
    assert "run ./..." in _log(env)


def test_pin_matches_with_v_and_newline(tmp_path):
    env = _env(tmp_path, STUB_VERSION="2.15.0")
    result = _run(_repo(tmp_path, pin="v2.15.0\n"), env)
    assert result.returncode == 0, result.stderr
    assert result.stdout == ""
    assert "run ./..." in _log(env)


def test_pin_mismatch_could_not_check(tmp_path):
    env = _env(tmp_path, STUB_VERSION="2.11.4")
    message = _message(_run(_repo(tmp_path, pin="v2.15.0"), env))
    assert message.startswith(UNCHECKED)
    assert "2.11.4" in message and "2.15.0" in message
    assert "run ./..." not in _log(env)


def test_no_pin_runs_unpinned(tmp_path):
    env = _env(tmp_path, STUB_VERSION="9.9.9")
    result = _run(_repo(tmp_path), env)
    assert result.returncode == 0, result.stderr
    assert result.stdout == ""
    assert "run ./..." in _log(env)


def test_no_go_mod_reports_not_checked(tmp_path):
    env = _env(tmp_path)
    message = _message(_run(_repo(tmp_path, go_mod=False), env))
    assert message.startswith(NOT_CHECKED)
    assert not Path(env["STUB_LOG"]).exists()


def test_no_go_mod_without_golangci_lint_reports_not_checked(tmp_path):
    bin_dir = tool_bin(tmp_path / "bin", TOOLS, stubs={"go": GO_STUB})
    message = _message(
        _run(_repo(tmp_path, go_mod=False), {"PATH": str(bin_dir), "STUB_LOG": str(tmp_path / "l")})
    )
    assert message.startswith(NOT_CHECKED)


def test_stop_hook_active_passes(tmp_path):
    env = _env(tmp_path, STUB_RUN_RC="1")
    root = _repo(tmp_path)
    result = _run(root, env, {"stop_hook_active": True, "cwd": str(root)})
    assert result.returncode == 0, result.stderr
    assert result.stdout == ""
    assert not Path(env["STUB_LOG"]).exists()


@pytest.mark.parametrize("tool", ["go", "golangci-lint", "jq", "git", "python3"])
def test_missing_tool_could_not_check(tmp_path, tool):
    stubs = {"go": GO_STUB, "golangci-lint": LINT_STUB}
    bin_dir = tool_bin(tmp_path / "bin", TOOLS, drop=tool, stubs=stubs)
    if tool in stubs:
        (bin_dir / tool).unlink()
    message = _message(_run(_repo(tmp_path), {"PATH": str(bin_dir), "STUB_LOG": str(tmp_path / "l")}))
    assert message.startswith(UNCHECKED)
    assert tool in message


def _worktree(tmp_path: Path) -> tuple[Path, Path]:
    """A main checkout with a committed go.mod, and a worktree of it."""
    root = _repo(tmp_path)
    git(root, "add", "-A")
    git(root, "commit", "-qm", "init")
    wt = root / ".claude" / "worktrees" / "wt"
    git(root, "worktree", "add", "-q", str(wt), "-b", "wt")
    return root, wt


def _edit_turn(tmp_path: Path, root: Path, edited: Path) -> dict:
    """The payload of a turn that edited a file in `edited`, from a session whose cwd is `root`."""
    transcript = write_transcript(
        tmp_path / "transcript.jsonl",
        [
            prompt_entry(root),
            tool_entry("Edit", {"file_path": str(edited / "a.go")}, root),
            result_entry(root),
        ],
    )
    return stop_payload(root, transcript)


def _runs(env: dict) -> list[str]:
    return [line for line in _log(env).splitlines() if line.startswith("run ")]


def test_a_finding_in_the_edited_worktree_blocks_with_the_cwd_on_main(tmp_path):
    root, wt = _worktree(tmp_path)
    env = _env(tmp_path, STUB_FAIL_IN=os.path.realpath(wt), STUB_RUN_OUT=LINT_FINDING)
    result = _run(root, env, _edit_turn(tmp_path, root, wt))
    assert result.returncode == 2, result.stdout
    assert result.stderr.splitlines()[0] == (
        f"go-check: golangci-lint finds problems in {os.path.realpath(wt)}."
    )
    assert LINT_FINDING in result.stderr
    assert _runs(env) == [f"run ./... @ {os.path.realpath(wt)}"]


def test_a_finding_in_an_untouched_worktree_does_not_block(tmp_path):
    root, wt = _worktree(tmp_path)
    env = _env(tmp_path, STUB_FAIL_IN=os.path.realpath(wt), STUB_RUN_OUT=LINT_FINDING)
    result = _run(root, env, _edit_turn(tmp_path, root, root))
    assert (result.returncode, result.stdout) == (0, ""), result.stderr
    assert _runs(env) == [f"run ./... @ {os.path.realpath(root)}"]


def test_every_checkout_of_the_turn_is_linted(tmp_path):
    """A Bash call from main into the worktree names both; the finding in the second blocks."""
    root, wt = _worktree(tmp_path)
    env = _env(tmp_path, STUB_FAIL_IN=os.path.realpath(wt), STUB_RUN_OUT=LINT_FINDING)
    transcript = write_transcript(
        tmp_path / "transcript.jsonl",
        [prompt_entry(root), tool_entry("Bash", {"command": f"git -C {wt} commit -qm x"}, root)],
    )
    result = _run(root, env, stop_payload(root, transcript))
    assert result.returncode == 2, result.stdout
    assert LINT_FINDING in result.stderr
    assert _runs(env) == [
        f"run ./... @ {os.path.realpath(root)}",
        f"run ./... @ {os.path.realpath(wt)}",
    ]


def test_without_a_transcript_could_not_check(tmp_path):
    env = _env(tmp_path, STUB_RUN_RC="1", STUB_RUN_OUT=LINT_FINDING)
    root = _repo(tmp_path)
    message = _message(_run(root, env, {"stop_hook_active": False, "cwd": str(root)}))
    assert message.startswith(UNCHECKED)
    assert "transcript_path" in message
    assert _runs(env) == []


def test_registered_on_stop():
    registered = json.loads(
        (Path(__file__).resolve().parents[3] / "pack/go/hooks/hooks.json").read_text()
    )
    entries = [
        (block.get("matcher"), h["command"], h["timeout"])
        for block in registered["hooks"]["Stop"]
        for h in block["hooks"]
    ]
    assert entries == [("", "${CLAUDE_PLUGIN_ROOT}/hooks/go-check.sh", 120)]

"""End-to-end tests for pack/core `pr-create-gate` (`run.sh pr-create-gate`), as installed (#1091).

The hook runs as a subprocess with a real project repo in `tmp_path` as
`CLAUDE_PROJECT_DIR`. No network: a stub `gh` is always first on
`PATH` (except in `test_gh_missing_passes_loudly`, which needs a PATH
without `gh`), so no test reaches the real `gh`.
"""

import json
import os
import subprocess
import time
from pathlib import Path

import pytest

from tests.pack._testkit import (
    CORE,
    INSTALLED_HOOKS,
    RUN_SH_TOOLS,
    bash_payload,
    init_repo,
    run_hook,
    tool_bin,
)
from tests.pack.test_pr_body_gate import (
    ISSUE_WITH_THREE_AC,
    ROWS1,
    ROWS2,
    ROWS3,
    closes_body,
)

# The hook runs as `lighthouse hook pr-create-gate` behind run.sh: the binary of this checkout.
pytestmark = pytest.mark.usefixtures("lighthouse_bin")

HOOK = INSTALLED_HOOKS / "run.sh"
HOOK_ARGS = ("pr-create-gate",)
HOOKS_JSON = CORE / "hooks" / "hooks.json"
GO_HOOK = CORE.parents[1] / "internal" / "hooks" / "prcreategate.go"
# `forge.repo` of the lighthouse.toml that install_core leaves behind.
REPO = "Acme/widgets"

# Fake `gh`: prints $GH_ISSUE_BODY for the issue endpoint, exits 1 with GH_FAIL.
FAKE_GH = (
    "#!/bin/sh\n"
    'if [ -n "$GH_FAIL" ]; then exit 1; fi\n'
    f'if [ "$1 $2" = "api repos/{REPO}/issues/5" ]; then\n'
    "  printf '%s\\n' \"$GH_ISSUE_BODY\"\n"
    "  exit 0\n"
    "fi\n"
    "exit 1\n"
)
BASE_TOOLS = (*RUN_SH_TOOLS, "git", "bash", "sh", "lighthouse")


class Gate:
    """A temporary project repo plus a stub directory with a fake `gh`."""

    def __init__(self, tmp_path: Path, project: Path) -> None:
        self.tmp = tmp_path
        self.project = project
        init_repo(project)
        self.stub = tmp_path / "stub"
        tool_bin(self.stub, (), stubs={"gh": FAKE_GH})

    def git(self, *args: str) -> None:
        subprocess.run(["git", "-C", str(self.project), *args], check=True, capture_output=True)

    def run(
        self,
        cmd: str,
        cwd: Path | None = None,
        path: str | None = None,
        stub: bool = True,
        **env: str,
    ):
        cwd = self.project if cwd is None else cwd
        tail = os.environ["PATH"] if path is None else path
        full_env = {
            # Stub directory always first, unless the test deliberately runs without `gh`.
            "PATH": f"{self.stub}{os.pathsep}{tail}" if stub else tail,
            "CLAUDE_PROJECT_DIR": str(self.project),
            "GH_ISSUE_BODY": ISSUE_WITH_THREE_AC,
            **env,
        }
        return run_hook(HOOK, bash_payload(cmd, cwd), cwd, full_env, args=HOOK_ARGS)


@pytest.fixture
def gate(tmp_path: Path) -> Gate:
    return Gate(tmp_path, tmp_path / "X")


def _body_with_rows(n: int) -> str:
    return closes_body(5, [ROWS1, ROWS2, ROWS3][n - 1])


def test_valid_closes_passes(gate: Gate) -> None:
    (gate.project / "body.md").write_text(_body_with_rows(3), encoding="utf-8")

    result = gate.run("gh pr create --repo Acme/widgets --title t --body-file body.md")

    assert result.returncode == 0, result.stderr
    out = json.loads(result.stdout)
    assert "body checked" in out["systemMessage"]
    assert out["hookSpecificOutput"]["hookEventName"] == "PreToolUse"
    assert out["hookSpecificOutput"]["additionalContext"] == out["systemMessage"]


def test_valid_refs_passes(gate: Gate) -> None:
    result = gate.run("gh pr create --repo Acme/widgets --body 'Refs #5 — wacht op deploy op de Beelink'")

    assert result.returncode == 0, result.stderr
    assert "body checked" in json.loads(result.stdout)["systemMessage"]


def test_no_form_blocks(gate: Gate) -> None:
    result = gate.run("gh pr create --repo Acme/widgets --body 'zie titel'")

    assert result.returncode == 2, result.stdout
    assert "/lighthouse-write-pr" in result.stderr


def test_cd_prefix_reads_relative(gate: Gate) -> None:
    sub = gate.project / "sub"
    sub.mkdir()
    cmd = "cd sub && gh pr create --repo Acme/widgets -F body.md"

    without = gate.run(cmd)
    assert without.returncode == 2, without.stdout

    (sub / "body.md").write_text(_body_with_rows(3), encoding="utf-8")
    with_file = gate.run(cmd)
    assert with_file.returncode == 0, with_file.stderr


def test_gh_pr_new_is_checked(gate: Gate) -> None:
    result = gate.run("gh pr new --repo Acme/widgets --body 'zie titel'")

    assert result.returncode == 2, result.stdout


def test_other_repo_skipped(gate: Gate) -> None:
    result = gate.run("gh pr create -R other/repo --body 'zie titel'")

    assert result.returncode == 0, result.stderr
    assert "other repo" in json.loads(result.stdout)["systemMessage"]


def test_outside_repo_with_R_is_checked(gate: Gate, tmp_path: Path) -> None:
    outside = tmp_path / "buiten"
    outside.mkdir()

    result = gate.run(f"gh pr create -R {REPO} --body 'zie titel'", cwd=outside)

    assert result.returncode == 2, result.stdout


def test_unparseable_without_gh_pr_passes(gate: Gate) -> None:
    result = gate.run("echo 'x")

    assert result.returncode == 0, result.stderr


def test_unparseable_with_gh_pr_blocks(gate: Gate) -> None:
    result = gate.run("gh pr create --repo Acme/widgets --body 'x")

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "open quote" in result.stderr


def test_ac_count_differs_blocks(gate: Gate) -> None:
    (gate.project / "body.md").write_text(_body_with_rows(2), encoding="utf-8")

    result = gate.run("gh pr create --repo Acme/widgets -F body.md")

    assert result.returncode == 2, result.stdout
    assert "3" in result.stderr and "2" in result.stderr


def test_gh_fails_passes_loudly(gate: Gate) -> None:
    (gate.project / "body.md").write_text(_body_with_rows(3), encoding="utf-8")

    result = gate.run("gh pr create --repo Acme/widgets -F body.md", GH_FAIL="1")

    assert result.returncode == 0, result.stderr
    out = json.loads(result.stdout)
    notice = "AC count not measurable for #5"
    assert notice in out["systemMessage"]
    assert notice in out["hookSpecificOutput"]["additionalContext"]


def test_gh_missing_passes_loudly(gate: Gate, tmp_path: Path) -> None:
    (gate.project / "body.md").write_text(_body_with_rows(3), encoding="utf-8")
    no_gh = tool_bin(tmp_path / "zonder_gh", BASE_TOOLS)

    result = gate.run("gh pr create --repo Acme/widgets -F body.md", path=str(no_gh), stub=False)

    assert result.returncode == 0, result.stderr
    out = json.loads(result.stdout)
    assert "gh is missing" in out["systemMessage"]
    assert "gh is missing" in out["hookSpecificOutput"]["additionalContext"]


def test_invalid_payload_blocks(gate: Gate) -> None:
    result = run_hook_raw(gate)

    assert result.returncode == 2
    assert "COULD NOT CHECK" in result.stderr


def test_empty_command_silent(gate: Gate) -> None:
    result = gate.run("")

    assert result.returncode == 0
    assert result.stdout == ""


def test_no_gh_pr_create_silent(gate: Gate) -> None:
    result = gate.run("git status")

    assert result.returncode == 0, result.stderr
    assert result.stdout == ""


def test_hook_registered_and_executable() -> None:
    registered = json.loads(HOOKS_JSON.read_text(encoding="utf-8"))
    bash_blocks = [
        b for b in registered["hooks"]["PreToolUse"] if b.get("matcher") == "Bash"
    ]
    assert len(bash_blocks) == 1
    entries = [
        h for h in bash_blocks[0]["hooks"] if h["command"].endswith("/run.sh pr-create-gate")
    ]
    assert len(entries) == 1, bash_blocks[0]["hooks"]
    assert entries[0]["timeout"] == 15
    assert os.access(HOOK, os.X_OK)
    assert "Counterpart: /lighthouse-write-pr" in GO_HOOK.read_text(encoding="utf-8")


def run_hook_raw(gate: Gate, payload: str = "geen json {"):
    """The hook with a raw payload, by default one that is not JSON."""
    return subprocess.run(
        [str(HOOK), *HOOK_ARGS],
        input=payload,
        capture_output=True,
        text=True,
        cwd=str(gate.project),
        env=dict(os.environ, PATH=f"{gate.stub}{os.pathsep}{os.environ['PATH']}"),
        check=False,
    )


# Review focus 5: payloads of a strange shape. The verdicts are those of the bash hook
# (`jq -r '.tool_input.command // empty'`), the Go hook must give the same.
PAYLOAD_SHAPES = {
    "array": ("[1]", 2),
    "null": ("null", 0),
    "tool_input_string": ('{"tool_input": "gh pr create"}', 2),
    "command_number": ('{"tool_input": {"command": 5}}', 0),
}


@pytest.mark.parametrize("shape", sorted(PAYLOAD_SHAPES))
def test_payload_shapes(gate: Gate, shape: str) -> None:
    payload, code = PAYLOAD_SHAPES[shape]

    result = run_hook_raw(gate, payload)

    assert result.returncode == code, (result.stdout, result.stderr)
    if code == 2:
        assert "COULD NOT CHECK — the payload is not valid JSON." in result.stderr
    else:
        assert (result.stdout, result.stderr) == ("", "")


def test_slow_gh_passes_loudly_within_the_deadline(gate: Gate) -> None:
    """Review focus 2: a `gh` that hangs at the AC count passes loudly, well before the hook timeout."""
    tool_bin(gate.stub, (), stubs={"gh": "#!/bin/sh\nexec sleep 30\n"})
    (gate.project / "body.md").write_text(_body_with_rows(3), encoding="utf-8")

    start = time.monotonic()
    result = gate.run("gh pr create --repo Acme/widgets -F body.md")
    elapsed = time.monotonic() - start

    assert elapsed < 10, elapsed
    assert result.returncode == 0, result.stderr
    out = json.loads(result.stdout)
    assert "AC count not measurable for #5 (timeout after 5 s)" in out["systemMessage"]
    assert out["hookSpecificOutput"]["additionalContext"] == out["systemMessage"]


def test_fork_clone_without_repo_blocks_with_the_exact_flag(gate: Gate) -> None:
    """#224: in a clone of a fork gh would pick the upstream repo; the gate demands `--repo`."""
    gate.git("remote", "add", "origin", "https://github.com/Acme/widgets.git")
    gate.git("remote", "add", "upstream", "https://github.com/Upstream/widgets.git")
    (gate.project / "body.md").write_text(_body_with_rows(3), encoding="utf-8")

    without = gate.run("gh pr create --title t --body-file body.md")
    assert without.returncode == 2, without.stdout
    assert f"--repo {REPO}" in without.stderr

    with_repo = gate.run(f"gh pr create --repo {REPO} --title t --body-file body.md")
    assert with_repo.returncode == 0, with_repo.stderr
    assert "body checked" in json.loads(with_repo.stdout)["systemMessage"]

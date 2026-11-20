"""End-to-end tests for pack/core `pr-create-gate` (`run.sh pr-create-gate`), as installed (#1091).

The hook runs as a subprocess with a real project repo in `tmp_path` as
`CLAUDE_PROJECT_DIR`. No network: the AC count reads the issue from a fake
GitHub API on localhost (`LIGHTHOUSE_GITHUB_API_URL`) with a fake token, and a
stub `gh` is always first on `PATH` (except in `test_token_missing_passes_loudly`,
which needs a PATH without `gh`), so no test reaches the real `gh` or GitHub.
"""

import json
import os
import subprocess
import threading
import time
from collections.abc import Iterator
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import pytest

from tests.pack.python._testkit import (
    CORE,
    INSTALLED_HOOKS,
    ISSUE_WITH_THREE_AC,
    ROWS1,
    ROWS2,
    ROWS3,
    RUN_SH_TOOLS,
    bash_payload,
    closes_body,
    init_repo,
    run_hook,
    tool_bin,
)

# The hook runs as `lighthouse hook pr-create-gate` behind run.sh: the binary of this checkout.
pytestmark = pytest.mark.usefixtures("lighthouse_bin")

HOOK = INSTALLED_HOOKS / "run.sh"
HOOK_ARGS = ("pr-create-gate",)
HOOKS_JSON = CORE / "hooks" / "hooks.json"
GO_HOOK = CORE.parents[1] / "internal" / "hooks" / "prcreategate.go"
# `forge.repo` of the lighthouse.toml that install_core leaves behind.
REPO = "Acme/widgets"

# Fake `gh`: knows no command, so `gh auth token` gives no token.
FAKE_GH = "#!/bin/sh\nexit 1\n"
BASE_TOOLS = (*RUN_SH_TOOLS, "git", "bash", "sh", "lighthouse")
TOKEN = "fake-token"


class FakeGitHub:
    """A GitHub REST API on localhost that knows issue 5 of REPO.

    `mode` is "ok" (the issue, with `issue_body`), "fail" (500) or "slow" (an
    answer only after `release` is set, at teardown).
    """

    def __init__(self) -> None:
        self.mode = "ok"
        self.issue_body = ISSUE_WITH_THREE_AC
        self.release = threading.Event()
        fake = self

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self) -> None:  # noqa: N802 — the name http.server calls
                if fake.mode == "slow":
                    fake.release.wait(120)
                if fake.mode == "fail":
                    self._send(500, {"message": "Server Error"})
                elif self.path != f"/repos/{REPO}/issues/5":
                    self._send(404, {"message": "Not Found"})
                elif self.headers.get("Authorization") != f"Bearer {TOKEN}":
                    self._send(401, {"message": "Bad credentials"})
                else:
                    self._send(200, {"number": 5, "title": "t", "state": "open", "body": fake.issue_body})

            def _send(self, code: int, doc: dict) -> None:
                data = json.dumps(doc).encode()
                self.send_response(code)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            def log_message(self, *args: object) -> None:
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.daemon_threads = True
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def close(self) -> None:
        self.release.set()
        self.server.shutdown()
        self.server.server_close()


class Gate:
    """A temporary project repo, a stub directory with a fake `gh` and a fake GitHub API."""

    def __init__(self, tmp_path: Path, project: Path, api: FakeGitHub) -> None:
        self.tmp = tmp_path
        self.project = project
        self.api = api
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
            "LIGHTHOUSE_GITHUB_API_URL": self.api.url,
            "GH_TOKEN": TOKEN,
            "GITHUB_TOKEN": "",
            **env,
        }
        return run_hook(HOOK, bash_payload(cmd, cwd), cwd, full_env, args=HOOK_ARGS)


@pytest.fixture
def gate(tmp_path: Path) -> Iterator[Gate]:
    api = FakeGitHub()
    try:
        yield Gate(tmp_path, tmp_path / "X", api)
    finally:
        api.close()


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
    assert "Closes #5: the issue has 3 acceptance criteria, the Evidence block 2 rows" in result.stderr


def test_forge_fails_passes_loudly(gate: Gate) -> None:
    (gate.project / "body.md").write_text(_body_with_rows(3), encoding="utf-8")
    gate.api.mode = "fail"

    result = gate.run("gh pr create --repo Acme/widgets -F body.md")

    assert result.returncode == 0, result.stderr
    out = json.loads(result.stdout)
    notice = "AC count not measurable for #5 (forge error:"
    assert notice in out["systemMessage"]
    assert "status 500" in out["systemMessage"]
    assert notice in out["hookSpecificOutput"]["additionalContext"]


def test_token_missing_passes_loudly(gate: Gate, tmp_path: Path) -> None:
    (gate.project / "body.md").write_text(_body_with_rows(3), encoding="utf-8")
    no_gh = tool_bin(tmp_path / "zonder_gh", BASE_TOOLS)

    result = gate.run("gh pr create --repo Acme/widgets -F body.md", path=str(no_gh), stub=False, GH_TOKEN="")

    assert result.returncode == 0, result.stderr
    out = json.loads(result.stdout)
    notice = "AC count not measurable for #5 (no GitHub token: set GH_TOKEN or GITHUB_TOKEN)"
    assert notice in out["systemMessage"]
    assert notice in out["hookSpecificOutput"]["additionalContext"]


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


def test_slow_forge_passes_loudly_within_the_deadline(gate: Gate) -> None:
    """Review focus 2: a forge that hangs at the AC count passes loudly, well before the hook timeout."""
    # The hook's own message proves the 5 s clip; the bound only that the hook did not wait for
    # the forge. The fake answers only at teardown, 120 s at most: a hook that waits for it is far
    # above the 60 s bound, and 60 s is ten times a slow run (#338).
    gate.api.mode = "slow"
    (gate.project / "body.md").write_text(_body_with_rows(3), encoding="utf-8")

    start = time.monotonic()
    result = gate.run("gh pr create --repo Acme/widgets -F body.md")
    elapsed = time.monotonic() - start

    assert elapsed < 60, elapsed
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

"""End-to-end tests voor pack/core `pr-create-gate.sh`, as installed (#1091).

De hook draait als subprocess met een echte projectrepo in `tmp_path` als
`CLAUDE_PROJECT_DIR`. Netwerkverbod: een stub-`gh` staat altijd vooraan op
`PATH` (behalve in `test_gh_ontbreekt_laat_luid_door`, dat juist een PATH
zonder `gh` nodig heeft), zodat geen enkele test de echte `gh` bereikt.
"""

import json
import os
import subprocess
from pathlib import Path

import pytest

from tests.pack._testkit import (
    CORE,
    INSTALLED_HOOKS,
    bash_payload,
    init_repo,
    run_hook,
    tool_bin,
)
from tests.pack.test_pr_body_gate import (
    ISSUE_MET_DRIE_AC,
    ROWS1,
    ROWS2,
    ROWS3,
    closes_body,
)

HOOK = INSTALLED_HOOKS / "pr-create-gate.sh"
HOOKS_JSON = CORE / "hooks" / "hooks.json"
# `forge.repo` of the lighthouse.toml that install_core leaves behind.
REPO = "Acme/widgets"

# Nep-`gh`: print $GH_ISSUE_BODY voor het issue-endpoint, exit 1 met GH_FAIL.
FAKE_GH = (
    "#!/bin/sh\n"
    'if [ -n "$GH_FAIL" ]; then exit 1; fi\n'
    f'if [ "$1 $2" = "api repos/{REPO}/issues/5" ]; then\n'
    "  printf '%s\\n' \"$GH_ISSUE_BODY\"\n"
    "  exit 0\n"
    "fi\n"
    "exit 1\n"
)
BASIS_TOOLS = ("jq", "python3", "git", "cat", "dirname", "bash")


class Gate:
    """Een tijdelijke projectrepo plus een stubmap met nep-`gh`."""

    def __init__(self, tmp_path: Path, project: Path) -> None:
        self.tmp = tmp_path
        self.project = project
        init_repo(project)
        self.stub = tmp_path / "stub"
        tool_bin(self.stub, (), stubs={"gh": FAKE_GH})

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
            # Stubmap altijd eerst, tenzij de test bewust zonder `gh` draait.
            "PATH": f"{self.stub}{os.pathsep}{tail}" if stub else tail,
            "CLAUDE_PROJECT_DIR": str(self.project),
            "GH_ISSUE_BODY": ISSUE_MET_DRIE_AC,
            **env,
        }
        return run_hook(HOOK, bash_payload(cmd, cwd), cwd, full_env)


@pytest.fixture
def gate(tmp_path: Path) -> Gate:
    return Gate(tmp_path, tmp_path / "X")


def _rijen(n: int) -> str:
    return closes_body(5, [ROWS1, ROWS2, ROWS3][n - 1])


def test_geldige_closes_gaat_door(gate: Gate) -> None:
    (gate.project / "body.md").write_text(_rijen(3), encoding="utf-8")

    result = gate.run("gh pr create --title t --body-file body.md")

    assert result.returncode == 0, result.stderr
    out = json.loads(result.stdout)
    assert "body checked" in out["systemMessage"]
    assert out["hookSpecificOutput"]["hookEventName"] == "PreToolUse"
    assert out["hookSpecificOutput"]["additionalContext"] == out["systemMessage"]


def test_geldige_refs_gaat_door(gate: Gate) -> None:
    result = gate.run("gh pr create --body 'Refs #5 — wacht op deploy op de Beelink'")

    assert result.returncode == 0, result.stderr
    assert "body checked" in json.loads(result.stdout)["systemMessage"]


def test_geen_vorm_blokkeert(gate: Gate) -> None:
    result = gate.run("gh pr create --body 'zie titel'")

    assert result.returncode == 2, result.stdout
    assert "/lighthouse-verify-pr" in result.stderr


def test_cd_prefix_leest_relatief(gate: Gate) -> None:
    sub = gate.project / "sub"
    sub.mkdir()
    cmd = "cd sub && gh pr create -F body.md"

    zonder = gate.run(cmd)
    assert zonder.returncode == 2, zonder.stdout

    (sub / "body.md").write_text(_rijen(3), encoding="utf-8")
    met = gate.run(cmd)
    assert met.returncode == 0, met.stderr


def test_gh_pr_new_wordt_getoetst(gate: Gate) -> None:
    result = gate.run("gh pr new --body 'zie titel'")

    assert result.returncode == 2, result.stdout


def test_andere_repo_overgeslagen(gate: Gate) -> None:
    result = gate.run("gh pr create -R other/repo --body 'zie titel'")

    assert result.returncode == 0, result.stderr
    assert "other repo" in json.loads(result.stdout)["systemMessage"]


def test_buiten_repo_met_R_getoetst(gate: Gate, tmp_path: Path) -> None:
    buiten = tmp_path / "buiten"
    buiten.mkdir()

    result = gate.run(f"gh pr create -R {REPO} --body 'zie titel'", cwd=buiten)

    assert result.returncode == 2, result.stdout


def test_onparseerbaar_zonder_gh_pr_door(gate: Gate) -> None:
    result = gate.run("echo 'x")

    assert result.returncode == 0, result.stderr


def test_onparseerbaar_met_gh_pr_blokkeert(gate: Gate) -> None:
    result = gate.run("gh pr create --body 'x")

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "open quote" in result.stderr


def test_ac_telling_wijkt_af_blokkeert(gate: Gate) -> None:
    (gate.project / "body.md").write_text(_rijen(2), encoding="utf-8")

    result = gate.run("gh pr create -F body.md")

    assert result.returncode == 2, result.stdout
    assert "3" in result.stderr and "2" in result.stderr


def test_gh_faalt_laat_luid_door(gate: Gate) -> None:
    (gate.project / "body.md").write_text(_rijen(3), encoding="utf-8")

    result = gate.run("gh pr create -F body.md", GH_FAIL="1")

    assert result.returncode == 0, result.stderr
    out = json.loads(result.stdout)
    melding = "AC count not measurable for #5"
    assert melding in out["systemMessage"]
    assert melding in out["hookSpecificOutput"]["additionalContext"]


def test_gh_ontbreekt_laat_luid_door(gate: Gate, tmp_path: Path) -> None:
    (gate.project / "body.md").write_text(_rijen(3), encoding="utf-8")
    zonder_gh = tool_bin(tmp_path / "zonder_gh", BASIS_TOOLS)

    result = gate.run("gh pr create -F body.md", path=str(zonder_gh), stub=False)

    assert result.returncode == 0, result.stderr
    out = json.loads(result.stdout)
    assert "gh is missing" in out["systemMessage"]
    assert "gh is missing" in out["hookSpecificOutput"]["additionalContext"]


@pytest.mark.parametrize("drop", ["jq", "python3"])
def test_zonder_tooling_blokkeert(gate: Gate, tmp_path: Path, drop: str) -> None:
    kaal = tool_bin(tmp_path / "kaal", BASIS_TOOLS, drop=drop)

    result = gate.run("gh pr create --body 'zie titel'", path=str(kaal), stub=False)

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert drop in result.stderr


def test_parser_met_onverwachte_rc_blokkeert(gate: Gate) -> None:
    # Een lib die met een andere code dan 0/2/3 eindigt (traceback: rc 1) is
    # geen groen licht. Een nep-python3 vooraan op PATH forceert dat.
    tool_bin(gate.stub, (), stubs={"python3": "#!/bin/sh\nexit 1\n"})

    result = gate.run("gh pr create --body 'Refs #5 — wacht op deploy op de Beelink'")

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "the body parser failed (rc 1)" in result.stderr


def test_ongeldige_payload_blokkeert(gate: Gate) -> None:
    result = run_hook_raw(gate)

    assert result.returncode == 2
    assert "COULD NOT CHECK" in result.stderr


def test_leeg_commando_stil(gate: Gate) -> None:
    result = gate.run("")

    assert result.returncode == 0
    assert result.stdout == ""


def test_geen_gh_pr_create_stil(gate: Gate) -> None:
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
        h for h in bash_blocks[0]["hooks"] if "pr-create-gate.sh" in h["command"]
    ]
    assert len(entries) == 1, bash_blocks[0]["hooks"]
    assert entries[0]["timeout"] == 15
    assert os.access(HOOK, os.X_OK)
    assert "# Counterpart: /lighthouse-verify-pr" in HOOK.read_text(encoding="utf-8")


def run_hook_raw(gate: Gate):
    """De hook met een payload die geen JSON is."""
    return subprocess.run(
        [str(HOOK)],
        input="geen json {",
        capture_output=True,
        text=True,
        cwd=str(gate.project),
        env=dict(os.environ, PATH=f"{gate.stub}{os.pathsep}{os.environ['PATH']}"),
        check=False,
    )

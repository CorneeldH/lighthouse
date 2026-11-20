"""The Stop hooks `imports-gate-{python,go,node}.sh` (#383): one per pack, each running
`lighthouse imports check --lang <lang>` for the checkouts a turn touched a relevant path in.

The hooks are bash. These tests run each as a subprocess in a real git repository, with a fake
`lighthouse` for `imports` and `usage` (`turn-roots` and `config` go to the binary built from
this checkout, via `lighthouse_stub`); the fake logs its argv to STUB_LOG.
"""

import json
import os
import re
import subprocess
from datetime import timedelta
from pathlib import Path

import pytest

from tests.pack._testkit import (
    alarm_line,
    git,
    install_pack,
    lighthouse_stub,
    path_with_lighthouse,
    prompt_entry,
    stop_payload,
    tool_bin,
    tool_entry,
    write_transcript,
)

pytestmark = pytest.mark.usefixtures("lighthouse_bin")

# pack -> (relevant path, irrelevant path, rule IDs the hook declares, a finding line)
CASES = {
    "python": ("a.py", "notes.txt", "DEP001,DEP003", "a.py:1: DEP003 numpy — not declared"),
    "go": ("a.go", "notes.txt", "indirect-import,missing-require", "a.go:3: missing-require x/y — not in go.mod"),
    "node": ("a.ts", "notes.txt", "unlisted", "a.ts:1: unlisted left-pad — not in package.json"),
}
PACKS = list(CASES)
TOOLS = ("bash", "jq", "git", "cat", "tr", "head", "sed", "grep", "mktemp", "rm", "read")

# `usage alarm` prints $STUB_ALARM; `imports check` prints $STUB_OUT, $STUB_ERR and exits
# $STUB_RC. Everything is logged.
STUB = """\
echo "$*" >> "$STUB_LOG"
case "$1 $2" in
  "usage alarm") [ -n "$STUB_ALARM" ] && echo "$STUB_ALARM"; exit 0 ;;
  "imports check")
    [ -n "$STUB_OUT" ] && echo "$STUB_OUT"
    [ -n "$STUB_ERR" ] && echo "$STUB_ERR" >&2
    exit "${STUB_RC:-0}" ;;
esac
exit 0
"""


def _hook(pack: str, root: Path) -> Path:
    return root / ".claude" / "hooks" / "lighthouse" / f"imports-gate-{pack}.sh"


def _repo(tmp_path: Path, pack: str, toml_extra: str = "") -> Path:
    root = tmp_path / "repo"
    root.mkdir()
    subprocess.run(["git", "init", "-q"], cwd=root, check=True)
    from tests.pack._testkit import MINIMAL_TOML

    hooks = install_pack(pack, root, MINIMAL_TOML + toml_extra)
    assert (hooks / f"imports-gate-{pack}.sh").exists()
    # The installation is committed, so only what the turn writes counts as touched.
    git(root, "add", "-A")
    git(root, "commit", "-q", "-m", "install")
    return root


def _env(tmp_path: Path, **extra: str) -> dict:
    bin_dir = tool_bin(tmp_path / "bin", TOOLS, stubs={"lighthouse": lighthouse_stub(STUB)})
    return {
        "PATH": f"{bin_dir}:{path_with_lighthouse()}",
        "STUB_LOG": str(tmp_path / "stub.log"),
        **extra,
    }


def _run(root: Path, pack: str, env: dict, touched: str | None = None, active: bool = False):
    """Run the hook for a turn that edited `touched` (default: the relevant path)."""
    touched = touched or CASES[pack][0]
    transcript = write_transcript(
        root.parent / "transcript.jsonl",
        [prompt_entry(root), tool_entry("Edit", {"file_path": str(root / touched)}, root)],
    )
    return subprocess.run(
        [str(_hook(pack, root))],
        input=json.dumps(stop_payload(root, transcript, active)),
        capture_output=True,
        text=True,
        cwd=str(root),
        env={
            **os.environ,
            "CLAUDE_PROJECT_DIR": str(root),
            "LIGHTHOUSE_REPO_ROOT": str(root),
            "XDG_CACHE_HOME": str(root.parent / "xdg-cache"),
            **env,
        },
        check=False,
    )


def _log(env: dict) -> str:
    log = Path(env["STUB_LOG"])
    return log.read_text() if log.exists() else ""


def _message(result) -> str:
    assert result.returncode == 0, result.stderr
    return json.loads(result.stdout)["systemMessage"]


@pytest.mark.parametrize("pack", PACKS)
def test_clean_passes_and_scopes_to_the_touched_path(tmp_path, pack):
    env = _env(tmp_path)
    result = _run(_repo(tmp_path, pack), pack, env)
    assert result.returncode == 0, result.stderr
    assert result.stdout == ""
    assert re.search(rf"imports check --lang {pack} --files \S*{CASES[pack][0]}", _log(env))


@pytest.mark.parametrize("pack", PACKS)
def test_blocks_on_exit1_with_finding(tmp_path, pack):
    env = _env(tmp_path, STUB_RC="1", STUB_OUT=CASES[pack][3])
    result = _run(_repo(tmp_path, pack), pack, env)
    assert result.returncode == 2
    rule = CASES[pack][3].split()[1]
    assert rule in result.stderr


@pytest.mark.parametrize("pack", PACKS)
def test_could_not_check_on_exit3(tmp_path, pack):
    env = _env(tmp_path, STUB_RC="3", STUB_ERR="imports: COULD NOT CHECK — no tool")
    result = _run(_repo(tmp_path, pack), pack, env)
    assert f"imports-gate-{pack}: COULD NOT CHECK — " in _message(result)


@pytest.mark.parametrize("pack", PACKS)
def test_could_not_check_on_exit1_without_finding(tmp_path, pack):
    env = _env(tmp_path, STUB_RC="1", STUB_OUT="something odd")
    result = _run(_repo(tmp_path, pack), pack, env)
    assert "COULD NOT CHECK" in _message(result)


@pytest.mark.parametrize("pack", PACKS)
def test_could_not_check_on_unknown_command(tmp_path, pack):
    env = _env(tmp_path, STUB_RC="1", STUB_ERR="unknown command")
    result = _run(_repo(tmp_path, pack), pack, env)
    message = _message(result)
    assert "COULD NOT CHECK" in message
    assert "unknown command" in message


@pytest.mark.parametrize("pack", PACKS)
def test_not_checked_during_usage_alarm(tmp_path, pack):
    env = _env(tmp_path, STUB_ALARM=alarm_line(local="14_30", delta=timedelta(hours=1)), STUB_RC="1", STUB_OUT=CASES[pack][3])
    result = _run(_repo(tmp_path, pack), pack, env)
    assert "not checked — the usage alarm is up until 14 30" in _message(result)
    assert "imports check" not in _log(env)


@pytest.mark.parametrize("pack", PACKS)
def test_not_checked_when_turn_touched_no_relevant_path(tmp_path, pack):
    env = _env(tmp_path, STUB_RC="1", STUB_OUT=CASES[pack][3])
    result = _run(_repo(tmp_path, pack), pack, env, touched=CASES[pack][1])
    assert "not checked — the turn touched no" in _message(result)
    assert "imports check" not in _log(env)


@pytest.mark.parametrize("pack", PACKS)
def test_no_block_when_stop_hook_active(tmp_path, pack):
    env = _env(tmp_path, STUB_RC="1", STUB_OUT=CASES[pack][3])
    result = _run(_repo(tmp_path, pack), pack, env, active=True)
    assert result.returncode == 0, result.stderr
    assert "imports check" not in _log(env)


@pytest.mark.parametrize("pack", PACKS)
def test_opt_out_does_nothing(tmp_path, pack):
    env = _env(tmp_path, STUB_RC="1", STUB_OUT=CASES[pack][3])
    root = _repo(tmp_path, pack, toml_extra="\n[imports]\ncheck = false\n")
    result = _run(root, pack, env)
    assert result.returncode == 0, result.stderr
    assert result.stdout == ""
    assert "imports check" not in _log(env)


@pytest.mark.parametrize("pack", PACKS)
def test_path_with_a_space_is_passed_whole(tmp_path, pack):
    env = _env(tmp_path)
    name = CASES[pack][0].replace("a.", "my file.")
    root = _repo(tmp_path, pack)
    result = _run(root, pack, env, touched=name)
    assert result.returncode == 0, result.stderr
    assert f"--files {name}" in _log(env) or f"{root}/{name}" in _log(env)


@pytest.mark.parametrize("pack", PACKS)
def test_blocking_rules_declared(pack):
    script = Path(__file__).resolve().parents[3] / "pack" / pack / "hooks" / f"imports-gate-{pack}.sh"
    lines = [l for l in script.read_text().splitlines() if l.startswith("BLOCKING_RULES=")]
    assert lines == [f'BLOCKING_RULES="{CASES[pack][2]}"']

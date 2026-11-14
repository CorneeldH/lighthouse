"""The usage-gate hook (#66): `run.sh usage-gate <stand>` reads the alarm, refreshes
the meter at `prompt`, and speaks on a valid alarm.

Every run goes through `guard_env`: a tmp HOME and XDG dirs, no controller or
refresh variables, no real `lighthouse` on PATH; `run_guard` puts the shim of
`_testkit` first, which records a `lighthouse usage refresh` in `refresh_trace`.
"""

import json
import os
import subprocess
import time
from datetime import datetime, timedelta, timezone
from pathlib import Path

import pytest

from tests.pack._testkit import (
    CORE,
    alarm_line,
    guard_env,
    install_core,
    refresh_trace,
    run_guard,
    write_alarm,
)

CHECKOUT = CORE.parents[1]
RUN = "0b9e2c4a-1f00-4c8e-9d7e-5a1b2c3d4e5f"
EVENTS = {
    "prompt": "UserPromptSubmit",
    "tool": "PostToolUse",
    "subagent": "SubagentStop",
    "stop": "Stop",
}
STANDS = tuple(EVENTS)


def _payload(tmp_path: Path, stand: str, **extra: object) -> dict:
    return dict(
        {"session_id": "s1", "cwd": str(tmp_path), "hook_event_name": EVENTS[stand]},
        **extra,
    )


def _speaks(stand: str, stdout: str) -> bool:
    """True when the output is the stand's own speech; fails on any other JSON."""
    if stdout == "":
        return False
    out = json.loads(stdout)
    if stand == "prompt":
        assert out["hookSpecificOutput"]["additionalContext"]
    else:
        assert out["decision"] == "block", out
    return True


# The alarm cases live in a fixture the Go reader of the alarm reads too
# (`internal/usage`); the former `alarm.sh` was the reference for `valid` and `fields`.
ALARM_CASES = json.loads(
    (Path(__file__).resolve().parent / "fixtures" / "usage_alarm" / "cases.json").read_text(
        encoding="utf-8"
    )
)
ALARM_IDS = [case["name"] for case in ALARM_CASES]


def _expand(text: str, now: datetime) -> str:
    """Fill `{future}` (now + 1 hour) and `{past}` (now - 1 minute) in a fixture text."""
    stamp = "%Y-%m-%dT%H:%M:%SZ"
    return text.replace("{future}", (now + timedelta(hours=1)).strftime(stamp)).replace(
        "{past}", (now - timedelta(minutes=1)).strftime(stamp)
    )


def _write_case(env: dict, case: dict, now: datetime) -> None:
    """Build the alarm file a fixture case describes."""
    line = _expand(case["line"], now)
    if case["setup"] == "absent":
        return
    if case["setup"] == "symlink":
        real = Path(env["HOME"]) / "elsewhere.alarm"
        real.write_text(line + "\n", encoding="utf-8")
        link = write_alarm(env, "")
        link.unlink()
        link.symlink_to(real)
        return
    # Bytes, not `write_alarm`: a `\r` in the line must reach the file unchanged.
    path = write_alarm(env, "")
    path.write_bytes((line + "\n").encode("utf-8"))
    if case["setup"] == "unreadable":
        path.chmod(0o000)


@pytest.mark.parametrize("case", ALARM_CASES, ids=ALARM_IDS)
@pytest.mark.parametrize("stand", STANDS)
def test_alarm_cases(tmp_path: Path, stand: str, case: dict) -> None:
    if case["setup"] == "unreadable" and os.geteuid() == 0:
        pytest.skip("root reads a mode-000 file")
    root = tmp_path / "root"
    install_core(root)
    env = guard_env(tmp_path)
    _write_case(env, case, datetime.now(timezone.utc))

    result = run_guard(root, stand, _payload(tmp_path, stand), env)

    assert result.returncode == 0, result.stderr
    assert result.stderr == ""
    assert _speaks(stand, result.stdout) is case["valid"], result.stdout
    if case["valid"]:
        _, pct, local, _ = case["fields"]
        assert f"at {int(pct)}% of" in result.stdout
        assert local in result.stdout


def test_unreadable_pause_flag_is_silent(tmp_path: Path) -> None:
    if os.geteuid() == 0:
        pytest.skip("root reads a mode-000 file")
    root = tmp_path / "root"
    install_core(root)
    state = tmp_path / "ctl"
    state.mkdir()
    flag = state / "PAUSE"
    flag.write_text("2099-01-01T00:00:00+02:00\n", encoding="utf-8")
    flag.chmod(0o000)
    env = guard_env(
        tmp_path, LIGHTHOUSE_CONTROLLER_RUN=RUN, LIGHTHOUSE_CONTROLLER_STATE=str(state)
    )

    result = run_guard(root, "tool", _payload(tmp_path, "tool"), env)

    assert result.returncode == 0
    assert result.stdout == ""
    assert result.stderr == ""


def test_alarm_fields_are_not_globbed(tmp_path: Path) -> None:
    # The meter writes `?` for an unknown reset_local. Two one-letter files in the
    # cwd would turn it into two fields, and the valid alarm into five.
    root = tmp_path / "root"
    install_core(root)
    (root / "a").touch()
    (root / "b").touch()
    env = guard_env(tmp_path)
    write_alarm(env, alarm_line(local="?"))

    result = run_guard(root, "tool", _payload(tmp_path, "tool"), env)

    assert _speaks("tool", result.stdout)
    assert "resets at ?." in result.stdout


def test_refresh_is_detached(tmp_path: Path) -> None:
    root = tmp_path / "root"
    install_core(root)
    env = guard_env(tmp_path, LIGHTHOUSE_SHIM_DELAY="2")
    trace = refresh_trace(env)
    write_alarm(env, alarm_line())

    start = time.monotonic()
    result = run_guard(root, "prompt", _payload(tmp_path, "prompt"), env)
    elapsed = time.monotonic() - start

    assert result.returncode == 0
    assert elapsed < 1.5, elapsed
    # The payload still reached the hook: the handoff path carries the session id.
    context = json.loads(result.stdout)["hookSpecificOutput"]["additionalContext"]
    assert "s1.handoff.md" in context
    # `> trace` creates the file before printf writes: poll for the content.
    deadline = time.monotonic() + 5
    seen = ""
    while seen != "usage refresh|1" and time.monotonic() < deadline:
        time.sleep(0.05)
        seen = trace.read_text(encoding="utf-8") if trace.exists() else ""
    assert seen == "usage refresh|1"


def test_refresh_only_at_prompt(tmp_path: Path) -> None:
    root = tmp_path / "root"
    install_core(root)
    env = guard_env(tmp_path)
    trace = refresh_trace(env)
    write_alarm(env, alarm_line())

    for stand in ("tool", "subagent", "stop"):
        assert run_guard(root, stand, _payload(tmp_path, stand), env).returncode == 0
    time.sleep(0.5)

    assert not trace.exists()


@pytest.mark.parametrize("stand", STANDS)
def test_usage_refresh_env_silences(tmp_path: Path, stand: str) -> None:
    root = tmp_path / "root"
    install_core(root)
    env = guard_env(tmp_path, LIGHTHOUSE_USAGE_REFRESH="1")
    trace = refresh_trace(env)
    write_alarm(env, alarm_line())

    result = run_guard(root, stand, _payload(tmp_path, stand), env)
    time.sleep(1)

    assert result.returncode == 0
    assert result.stdout == ""
    assert not trace.exists()


def test_once_per_window(tmp_path: Path) -> None:
    root = tmp_path / "root"
    install_core(root)
    env = guard_env(tmp_path)
    write_alarm(env, alarm_line())

    def run(stand: str, **extra: object) -> str:
        result = run_guard(root, stand, _payload(tmp_path, stand, **extra), env)
        assert result.returncode == 0, result.stderr
        return result.stdout

    assert _speaks("tool", run("tool"))
    assert run("tool") == ""
    assert _speaks("subagent", run("subagent"))
    assert run("stop", stop_hook_active=True) == ""
    assert _speaks("stop", run("stop"))
    assert run("stop") == ""
    assert _speaks("tool", run("tool", session_id="s2"))


def test_no_table_no_marker(tmp_path: Path) -> None:
    root = tmp_path / "root"
    install_core(root)
    env = guard_env(tmp_path)
    write_alarm(env, alarm_line())

    result = run_guard(root, "tool", _payload(tmp_path, "tool"), env)

    assert _speaks("tool", result.stdout)
    assert not (Path(env["XDG_DATA_HOME"]) / "lighthouse" / "resume").exists()


def test_unknown_stand(tmp_path: Path) -> None:
    root = tmp_path / "root"
    install_core(root)
    env = guard_env(tmp_path)
    write_alarm(env, alarm_line())

    result = run_guard(root, "nonsense", _payload(tmp_path, "tool"), env)

    assert result.returncode == 0
    assert result.stdout == ""
    assert result.stderr == ""


def test_no_paia_in_guard_files() -> None:
    result = subprocess.run(
        [
            "git",
            "grep",
            "-n",
            "-i",
            "paia",
            "--",
            "internal/usageguard",
            "internal/hooks/usagegate.go",
            "docs/usage-gate.md",
        ],
        capture_output=True,
        text=True,
        cwd=str(CHECKOUT),
        check=False,
    )

    assert result.returncode == 1, result.stdout + result.stderr

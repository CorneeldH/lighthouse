"""The usage-guard hook (#66): `usage-guard.sh` checks the alarm in shell, refreshes
the meter at `prompt`, and hands a valid alarm to `usage_guard.py`.

Every run goes through `guard_env`: a tmp HOME and XDG dirs, no controller or
refresh variables, no real `lighthouse` on PATH.
"""

import json
import shutil
import subprocess
import time
from datetime import timedelta
from pathlib import Path

import pytest

from tests.pack._testkit import (
    BSD_DATE_STUB,
    CORE,
    alarm_line,
    guard_env,
    install_core,
    run_guard,
    tool_bin,
    trace_bin,
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


def _hook(root: Path) -> Path:
    return root / ".claude" / "hooks" / "lighthouse" / "usage-guard.sh"


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


def _symlink_alarm(env: dict) -> None:
    real = Path(env["HOME"]) / "elsewhere.alarm"
    real.write_text(alarm_line() + "\n", encoding="utf-8")
    link = write_alarm(env, "")
    link.unlink()
    link.symlink_to(real)


VALID = "valid"
ALARMS = {
    "no file": None,
    VALID: lambda env: write_alarm(env, alarm_line(local="Tue_14:30")),
    "expired": lambda env: write_alarm(env, alarm_line(delta=-timedelta(minutes=1))),
    "three fields": lambda env: write_alarm(env, "venster 92 14:30"),
    "question marks": lambda env: write_alarm(env, "venster 92 ? ?"),
    "five fields": lambda env: write_alarm(env, alarm_line() + " extra"),
    "carriage return": lambda env: write_alarm(env, alarm_line() + "\r"),
    "kind dag": lambda env: write_alarm(env, alarm_line(kind="dag")),
    "pct 9x": lambda env: write_alarm(env, alarm_line().replace(" 92 ", " 9x ")),
    "symlink": _symlink_alarm,
}


@pytest.mark.parametrize("case", ALARMS)
@pytest.mark.parametrize("stand", STANDS)
def test_alarm_cases(tmp_path: Path, stand: str, case: str) -> None:
    root = tmp_path / "root"
    install_core(root)
    env = guard_env(tmp_path)
    if ALARMS[case] is not None:
        ALARMS[case](env)

    result = run_guard(root, stand, _payload(tmp_path, stand), env)

    assert result.returncode == 0, result.stderr
    assert result.stderr == ""
    assert _speaks(stand, result.stdout) is (case == VALID), result.stdout
    if case == VALID:
        assert "92%" in result.stdout
        assert "Tue 14:30" in result.stdout


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


@pytest.mark.parametrize("run", ["", RUN], ids=["plain", "controller-run"])
@pytest.mark.parametrize("stand", ["tool", "subagent"])
def test_fast_path_runs_nothing(tmp_path: Path, stand: str, run: str) -> None:
    root = tmp_path / "root"
    install_core(root)
    trace = tmp_path / "trace"
    stubs = trace_bin(
        tmp_path / "stubs", trace, ("python3", "jq", "date", "git", "cat", "dirname")
    )
    env = guard_env(tmp_path, PATH=str(stubs), LIGHTHOUSE_REPO_ROOT=str(root))
    if run:
        env["LIGHTHOUSE_CONTROLLER_RUN"] = run

    result = subprocess.run(
        [shutil.which("bash"), str(_hook(root)), stand],
        input=json.dumps(_payload(tmp_path, stand)),
        capture_output=True,
        text=True,
        env=env,
        check=False,
    )

    assert result.returncode == 0
    assert result.stderr == ""
    assert result.stdout == ""
    assert not trace.exists(), trace.read_text(encoding="utf-8")


@pytest.mark.parametrize(
    ("delta", "speaks"),
    [(timedelta(hours=1), True), (-timedelta(minutes=1), False)],
    ids=["valid", "expired"],
)
def test_bsd_date(tmp_path: Path, delta: timedelta, speaks: bool) -> None:
    root = tmp_path / "root"
    install_core(root)
    stubs = tool_bin(tmp_path / "stubs", (), stubs={"date": BSD_DATE_STUB})
    env = guard_env(tmp_path)
    env["PATH"] = f"{stubs}:{env['PATH']}"
    write_alarm(env, alarm_line(delta=delta))

    result = run_guard(root, "tool", _payload(tmp_path, "tool"), env)

    assert result.returncode == 0
    assert result.stderr == ""
    assert _speaks("tool", result.stdout) is speaks


def _lighthouse_stub(tmp_path: Path, trace: Path, delay: str = "") -> Path:
    script = (
        "#!/bin/bash\n"
        f"{delay}"
        f"printf '%s|%s' \"$*\" \"${{LIGHTHOUSE_USAGE_REFRESH:-}}\" > {trace}\n"
    )
    return tool_bin(tmp_path / "lh", (), stubs={"lighthouse": script})


def test_refresh_is_detached(tmp_path: Path) -> None:
    root = tmp_path / "root"
    install_core(root)
    trace = tmp_path / "refresh"
    stub = _lighthouse_stub(tmp_path, trace, delay="sleep 2\n")
    env = guard_env(tmp_path)
    env["PATH"] = f"{stub}:{env['PATH']}"
    write_alarm(env, alarm_line())

    start = time.monotonic()
    result = run_guard(root, "prompt", _payload(tmp_path, "prompt"), env)
    elapsed = time.monotonic() - start

    assert result.returncode == 0
    assert elapsed < 1.5, elapsed
    # The payload still reached the lib: the handoff path carries the session id.
    context = json.loads(result.stdout)["hookSpecificOutput"]["additionalContext"]
    assert "s1.handoff.md" in context
    deadline = time.monotonic() + 5
    while not trace.exists() and time.monotonic() < deadline:
        time.sleep(0.05)
    time.sleep(0.1)  # the stub writes in one printf; give it time to land
    assert trace.read_text(encoding="utf-8") == "usage refresh|1"


def test_refresh_only_at_prompt(tmp_path: Path) -> None:
    root = tmp_path / "root"
    install_core(root)
    trace = tmp_path / "refresh"
    stub = _lighthouse_stub(tmp_path, trace)
    env = guard_env(tmp_path)
    env["PATH"] = f"{stub}:{env['PATH']}"
    write_alarm(env, alarm_line())

    for stand in ("tool", "subagent", "stop"):
        assert run_guard(root, stand, _payload(tmp_path, stand), env).returncode == 0
    time.sleep(0.5)

    assert not trace.exists()


def test_refresh_without_lighthouse(tmp_path: Path) -> None:
    root = tmp_path / "root"
    install_core(root)
    env = guard_env(tmp_path)

    result = run_guard(root, "prompt", _payload(tmp_path, "prompt"), env)

    assert result.returncode == 0
    assert result.stderr == ""
    assert result.stdout == ""


@pytest.mark.parametrize("stand", STANDS)
def test_usage_refresh_env_silences(tmp_path: Path, stand: str) -> None:
    root = tmp_path / "root"
    install_core(root)
    trace = tmp_path / "refresh"
    stub = _lighthouse_stub(tmp_path, trace)
    env = guard_env(tmp_path, LIGHTHOUSE_USAGE_REFRESH="1")
    env["PATH"] = f"{stub}:{env['PATH']}"
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


def test_missing_python(tmp_path: Path) -> None:
    root = tmp_path / "root"
    install_core(root)
    env = guard_env(tmp_path)
    write_alarm(env, alarm_line())
    env["PATH"] = str(tool_bin(tmp_path / "bin", ("date", "git", "cat")))

    result = subprocess.run(
        [shutil.which("bash"), str(_hook(root)), "tool"],
        input=json.dumps(_payload(tmp_path, "tool")),
        capture_output=True,
        text=True,
        env=dict(env, LIGHTHOUSE_REPO_ROOT=str(root)),
        check=False,
    )

    assert result.returncode == 0
    assert result.stdout == ""
    assert result.stderr.startswith("usage-guard: "), result.stderr
    assert "COULD NOT CHECK" in result.stderr


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
            "pack/core/hooks/usage-guard.sh",
            "pack/core/hooks/usage-guard",
            "pack/core/lib/usage_guard.py",
        ],
        capture_output=True,
        text=True,
        cwd=str(CHECKOUT),
        check=False,
    )

    assert result.returncode == 1, result.stdout + result.stderr

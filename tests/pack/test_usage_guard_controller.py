"""The usage-guard module `controller` (#66): parking of a controller run at an alarm or a
PAUSE flag, with a marker the controller reads.

Every run goes through `guard_env`.
"""

import json
import os
import shutil
import subprocess
from datetime import datetime, timedelta, timezone
from pathlib import Path

import pytest

from tests.pack._testkit import (
    BSD_DATE_STUB,
    MINIMAL_TOML,
    alarm_line,
    guard_env,
    install_core,
    run_guard,
    tool_bin,
    trace_bin,
    write_alarm,
)

RUN = "0b9e2c4a-1f00-4c8e-9d7e-5a1b2c3d4e5f"
TOML = MINIMAL_TOML + "\n[usage_guard]\ncontroller = true\n"
EVENTS = {"prompt": "UserPromptSubmit", "tool": "PostToolUse", "stop": "Stop"}
BASE_FIELDS = {"session_id", "cwd", "window", "reset_at", "created_at", "source"}


def _payload(tmp_path: Path, stand: str, **extra: object) -> dict:
    return dict(
        {"session_id": "s1", "cwd": str(tmp_path), "hook_event_name": EVENTS.get(stand, "")},
        **extra,
    )


def _flag_text(delta: timedelta = timedelta(hours=1)) -> str:
    """`now + delta` with offset +02:00."""
    tz = timezone(timedelta(hours=2))
    return (datetime.now(tz) + delta).strftime("%Y-%m-%dT%H:%M:%S+02:00")


def _setup(tmp_path: Path, toml: str = TOML, run: str = RUN, flag=None, alarm=None, **env_extra):
    """Install, write the optional flag text and alarm line; return (root, env)."""
    root = tmp_path / "root"
    install_core(root, toml=toml)
    state = tmp_path / "ctl"
    state.mkdir(exist_ok=True)
    env = guard_env(
        tmp_path,
        LIGHTHOUSE_CONTROLLER_RUN=run,
        LIGHTHOUSE_CONTROLLER_STATE=str(state),
        **env_extra,
    )
    if flag is not None:
        (state / "PAUSE").write_text(flag + "\n", encoding="utf-8")
    if alarm is not None:
        write_alarm(env, alarm)
    return root, env


def _run(root: Path, env: dict, stand: str, payload: dict):
    result = run_guard(root, stand, payload, env)
    assert result.returncode == 0, result.stderr
    assert result.stderr == ""
    return result


def _resume(env: dict) -> Path:
    return Path(env["XDG_DATA_HOME"]) / "lighthouse" / "resume"


def _keys(env: dict) -> list[str]:
    state = Path(env["XDG_STATE_HOME"]) / "lighthouse" / "usage-guard"
    return sorted(p.name for p in state.glob("*.done")) if state.exists() else []


def _reason(result) -> str:
    out = json.loads(result.stdout)
    if "decision" in out:
        assert out["decision"] == "block"
        return out["reason"]
    return out["hookSpecificOutput"]["additionalContext"]


def _marker(env: dict, name: str = "s1") -> dict:
    return json.loads((_resume(env) / f"{name}.json").read_text(encoding="utf-8"))


def _is_controller_reason(reason: str) -> bool:
    return (
        "controller" in reason
        and "/lighthouse-handoff" not in reason
        and "CronCreate" not in reason
    )


@pytest.mark.parametrize("stand", ["tool", "stop"])
@pytest.mark.parametrize("resume", [False, True], ids=["controller", "with-resume"])
def test_alarm_parks_with_controller_reason(tmp_path: Path, stand: str, resume: bool) -> None:
    toml = TOML + ("resume = true\n" if resume else "")
    line = alarm_line()
    root, env = _setup(tmp_path, toml=toml, alarm=line)

    result = _run(root, env, stand, _payload(tmp_path, stand))

    assert _is_controller_reason(_reason(result))
    fields = _marker(env)
    assert set(fields) == BASE_FIELDS | {"controller"}
    assert fields["controller"] is True
    assert fields["window"] == "venster"
    assert fields["source"] == stand
    assert fields["reset_at"] == line.split()[3]
    assert fields["cwd"] == str(tmp_path)
    assert [k for k in _keys(env) if k.endswith(f"{stand}.s1.{RUN}.done")]
    assert list(_resume(env).glob("*.handoff.md")) == []


def test_without_session_id_run_id_is_session(tmp_path: Path) -> None:
    root, env = _setup(tmp_path, alarm=alarm_line())
    payload = {"cwd": str(tmp_path), "hook_event_name": "PostToolUse"}

    result = _run(root, env, "tool", payload)

    assert _is_controller_reason(_reason(result))
    assert _marker(env, RUN)["session_id"] == RUN
    assert [p.name for p in _resume(env).glob("*.json")] == [f"{RUN}.json"]


def test_three_runs_each_once(tmp_path: Path) -> None:
    root, env = _setup(tmp_path, alarm=alarm_line())
    runs = ["aaaa-1", "bbbb-2", "cccc-3"]

    for run in runs:
        run_env = dict(env, LIGHTHOUSE_CONTROLLER_RUN=run)
        first = _run(root, run_env, "tool", _payload(tmp_path, "tool"))
        second = _run(root, run_env, "tool", _payload(tmp_path, "tool"))
        assert _is_controller_reason(_reason(first))
        assert second.stdout == ""

    assert len(_keys(env)) == 3


def test_option_off_is_plain_session(tmp_path: Path) -> None:
    off = TOML.replace("controller = true", "controller = false")
    root, env = _setup(tmp_path, toml=off, alarm=alarm_line())
    result = _run(root, env, "tool", _payload(tmp_path, "tool"))
    assert "/lighthouse-handoff" in _reason(result)
    assert not _resume(env).exists()


def test_invalid_run_id_is_plain_session(tmp_path: Path) -> None:
    root, env = _setup(tmp_path, run="XYZ", alarm=alarm_line())
    result = _run(root, env, "tool", _payload(tmp_path, "tool"))
    assert "/lighthouse-handoff" in _reason(result)
    assert not _resume(env).exists()


def test_prompt_warning(tmp_path: Path) -> None:
    root, env = _setup(tmp_path, alarm=alarm_line())

    result = _run(root, env, "prompt", _payload(tmp_path, "prompt"))

    reason = _reason(result)
    assert "92%" in reason
    assert "CronCreate" not in reason
    assert "/lighthouse-handoff" not in reason
    assert not _resume(env).exists()


def test_subagent_ignores_controller(tmp_path: Path) -> None:
    root, env = _setup(tmp_path, alarm=alarm_line())

    result = _run(root, env, "tool", _payload(tmp_path, "tool", agent_id="a1"))

    assert "Stop now and report" in _reason(result)
    assert not _resume(env).exists()


def test_pause_parks_once(tmp_path: Path) -> None:
    flag_text = _flag_text()
    root, env = _setup(tmp_path, flag=flag_text)
    payload = _payload(tmp_path, "tool")

    first = _run(root, env, "tool", payload)

    reason = _reason(first)
    assert _is_controller_reason(reason)
    assert flag_text[11:16] in reason
    assert flag_text not in reason  # shown as HH:MM
    fields = _marker(env)
    assert set(fields) == BASE_FIELDS | {"controller", "pause"}
    assert fields["window"] == "pause"
    assert fields["pause"] is True
    assert fields["controller"] is True
    assert fields["reset_at"] == flag_text
    assert fields["source"] == "tool"
    assert _keys(env) == [f"pause-{flag_text}.{RUN}.done"]

    assert _run(root, env, "tool", payload).stdout == ""

    fields["created_at"] = "2000-01-01T00:00:00Z"
    (_resume(env) / "s1.json").write_text(json.dumps(fields), encoding="utf-8")
    flag = Path(env["LIGHTHOUSE_CONTROLLER_STATE"]) / "PAUSE"
    later = flag.stat().st_mtime + 10
    os.utime(flag, (later, later))
    assert _is_controller_reason(_reason(_run(root, env, "tool", payload)))
    assert _marker(env)["created_at"] != "2000-01-01T00:00:00Z"  # refreshed


def test_pause_bsd_date(tmp_path: Path) -> None:
    root, env = _setup(tmp_path, flag=_flag_text())
    stubs = tool_bin(tmp_path / "stubs", (), stubs={"date": BSD_DATE_STUB})
    env["PATH"] = f"{stubs}:{env['PATH']}"

    result = _run(root, env, "tool", _payload(tmp_path, "tool"))

    assert _is_controller_reason(_reason(result))


def _invalid_flag(case: str, state: Path) -> None:
    flag = state / "PAUSE"
    if case == "expired":
        flag.write_text(_flag_text(-timedelta(minutes=1)) + "\n", encoding="utf-8")
    elif case == "garbage":
        flag.write_text("morgen\n", encoding="utf-8")
    elif case == "naive":
        flag.write_text("2099-10-03T21:00:00\n", encoding="utf-8")
    else:
        target = state / "real"
        target.write_text(_flag_text() + "\n", encoding="utf-8")
        flag.symlink_to(target)


INVALID = ["expired", "garbage", "naive", "symlink"]


@pytest.mark.parametrize("case", INVALID)
def test_pause_invalid_flag_without_alarm_runs_nothing(tmp_path: Path, case: str) -> None:
    root, env = _setup(tmp_path)
    _invalid_flag(case, Path(env["LIGHTHOUSE_CONTROLLER_STATE"]))
    trace = tmp_path / "trace"
    stubs = trace_bin(
        tmp_path / "trace-stubs", trace, ("python3", "jq", "date", "git", "cat", "dirname")
    )
    env["PATH"] = str(stubs)
    env["LIGHTHOUSE_REPO_ROOT"] = str(root)
    hook = root / ".claude" / "hooks" / "lighthouse" / "usage-guard.sh"

    result = subprocess.run(
        [shutil.which("bash"), str(hook), "tool"],
        input=json.dumps(_payload(tmp_path, "tool")),
        capture_output=True,
        text=True,
        env=env,
        check=False,
    )

    assert result.returncode == 0
    assert result.stderr == ""
    assert result.stdout == ""
    assert "python3" not in (trace.read_text(encoding="utf-8") if trace.exists() else "")


@pytest.mark.parametrize("case", INVALID)
def test_pause_invalid_flag_with_alarm_speaks_alarm(tmp_path: Path, case: str) -> None:
    root, env = _setup(tmp_path, alarm=alarm_line())
    _invalid_flag(case, Path(env["LIGHTHOUSE_CONTROLLER_STATE"]))

    result = _run(root, env, "tool", _payload(tmp_path, "tool"))

    assert "92%" in _reason(result)
    assert _is_controller_reason(_reason(result))
    assert _marker(env)["window"] == "venster"
    assert not [k for k in _keys(env) if k.startswith("pause-")]


def test_pause_with_agent_id_does_not_park(tmp_path: Path) -> None:
    root, env = _setup(tmp_path, flag=_flag_text())
    payload = _payload(tmp_path, "tool", agent_id="a1")

    assert _run(root, env, "tool", payload).stdout == ""

    write_alarm(env, alarm_line())
    assert "Stop now and report" in _reason(_run(root, env, "tool", payload))
    assert not _resume(env).exists()


def test_pause_option_off_falls_through(tmp_path: Path) -> None:
    off = TOML.replace("controller = true", "controller = false")
    root, env = _setup(tmp_path, toml=off, flag=_flag_text(), alarm=alarm_line())

    result = _run(root, env, "tool", _payload(tmp_path, "tool"))

    assert "/lighthouse-handoff" in _reason(result)
    assert not [k for k in _keys(env) if k.startswith("pause-")]
    assert not _resume(env).exists()


@pytest.mark.parametrize("stand", ["prompt", "stop"])
def test_pause_only_in_tool(tmp_path: Path, stand: str) -> None:
    root, env = _setup(tmp_path, flag=_flag_text())

    result = _run(root, env, stand, _payload(tmp_path, stand))

    assert result.stdout == ""
    assert _keys(env) == []


def test_pause_then_alarm_keeps_the_later_pause_end(tmp_path: Path) -> None:
    flag_text = _flag_text(timedelta(hours=3))
    root, env = _setup(tmp_path, flag=flag_text, alarm=alarm_line())
    payload = _payload(tmp_path, "tool")

    assert flag_text[11:16] in _reason(_run(root, env, "tool", payload))
    second = _run(root, env, "tool", payload)  # the alarm path, a pause marker exists

    assert f"resumes this session after {flag_text[11:16]}." in _reason(second)
    for stand in ("tool", "stop"):
        if stand == "stop":
            result = _run(root, env, "stop", _payload(tmp_path, "stop"))
            assert f"resumes this session after {flag_text[11:16]}." in _reason(result)
        fields = _marker(env)
        assert fields["window"] == "pause"
        assert fields["pause"] is True
        assert fields["reset_at"] == flag_text
        assert fields["controller"] is True


def test_alarm_then_earlier_pause_keeps_the_alarm_end(tmp_path: Path) -> None:
    line = alarm_line(delta=timedelta(hours=3))
    root, env = _setup(tmp_path, alarm=line)
    payload = _payload(tmp_path, "tool")
    _run(root, env, "tool", payload)
    assert _marker(env)["window"] == "venster"

    flag_text = _flag_text(timedelta(hours=1))
    (Path(env["LIGHTHOUSE_CONTROLLER_STATE"]) / "PAUSE").write_text(flag_text + "\n")
    result = _run(root, env, "tool", payload)

    fields = _marker(env)
    assert fields["window"] == "venster"
    assert fields["reset_at"] == line.split()[3]
    assert "pause" not in fields
    assert fields["controller"] is True
    at = datetime.strptime(line.split()[3], "%Y-%m-%dT%H:%M:%SZ")
    assert f"resumes this session after {at.strftime('%H:%M')}." in _reason(result)  # TZ=UTC


def test_unreadable_marker_is_overwritten(tmp_path: Path) -> None:
    root, env = _setup(tmp_path, alarm=alarm_line())
    _resume(env).mkdir(parents=True)
    (_resume(env) / "s1.json").write_text("not json", encoding="utf-8")

    _run(root, env, "tool", _payload(tmp_path, "tool"))

    assert _marker(env)["window"] == "venster"

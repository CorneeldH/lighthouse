"""The usage-guard module `resume` (#66): wording, marker and wake-up of an ordinary session."""

import importlib.util
import json
import re
from datetime import timezone
from pathlib import Path
from zoneinfo import ZoneInfo

from tests.pack._testkit import (
    CORE,
    MINIMAL_TOML,
    alarm_line,
    guard_env,
    install_core,
    run_guard,
    write_alarm,
)

TOML = MINIMAL_TOML + "\n[usage_guard]\nresume = true\n"
EVENTS = {"prompt": "UserPromptSubmit", "tool": "PostToolUse", "stop": "Stop"}


def _load():
    # The lib is on sys.path through the conftest; register for dataclasses like load_module.
    path = CORE / "hooks" / "usage-guard" / "resume.py"
    spec = importlib.util.spec_from_file_location("usage_guard_resume_test", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _payload(tmp_path: Path, stand: str, **extra: object) -> dict:
    return dict(
        {"session_id": "s1", "cwd": str(tmp_path), "hook_event_name": EVENTS.get(stand, "")},
        **extra,
    )


def _run(tmp_path: Path, stand: str, line: str, payload=None, toml: str = TOML):
    root = tmp_path / "root"
    install_core(root, toml=toml)
    env = guard_env(tmp_path)
    write_alarm(env, line)
    payload = _payload(tmp_path, stand) if payload is None else payload
    result = run_guard(root, stand, payload, env)
    assert result.returncode == 0, result.stderr
    resume = Path(env["XDG_DATA_HOME"]) / "lighthouse" / "resume"
    return result, resume


def _reason(result) -> str:
    out = json.loads(result.stdout)
    if "decision" in out:
        return out["reason"]
    return out["hookSpecificOutput"]["additionalContext"]


def test_wake_cron() -> None:
    resume = _load()
    assert (
        resume.wake_cron("2026-09-06T12:30:00Z", ZoneInfo("Europe/Amsterdam")) == "32 14 6 9 *"
    )
    assert resume.wake_cron("2026-12-31T23:59:00Z", timezone.utc) == "1 0 1 1 *"


def test_tool_venster_writes_marker(tmp_path: Path) -> None:
    line = alarm_line()
    at = line.split()[3]
    result, resume = _run(tmp_path, "tool", line)

    reason = _reason(result)
    fields = json.loads((resume / "s1.json").read_text(encoding="utf-8"))
    assert set(fields) == {
        "session_id",
        "cwd",
        "window",
        "reset_at",
        "handoff_path",
        "created_at",
        "source",
    }
    assert fields["session_id"] == "s1"
    assert fields["window"] == "venster"
    assert fields["source"] == "tool"
    assert fields["reset_at"] == at
    assert fields["handoff_path"] == str(resume / "s1.handoff.md")
    assert re.fullmatch(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ", fields["created_at"])
    cron = _load().wake_cron(at, timezone.utc)
    assert f'cron "{cron}"' in reason
    assert "recurring false" in reason
    assert "CronList" in reason
    assert f"/lighthouse-handoff {resume / 's1.handoff.md'}" in reason
    assert list(resume.glob("*.tmp")) == []


def test_week_has_no_wake(tmp_path: Path) -> None:
    result, resume = _run(tmp_path, "tool", alarm_line(kind="week"))

    assert "CronCreate" not in _reason(result)
    assert json.loads((resume / "s1.json").read_text(encoding="utf-8"))["window"] == "week"


def test_stop_notes_uncommitted(tmp_path: Path) -> None:
    result, resume = _run(tmp_path, "stop", alarm_line())

    reason = _reason(result)
    assert "uncommitted" in reason
    assert "CronList" in reason
    assert json.loads((resume / "s1.json").read_text(encoding="utf-8"))["source"] == "stop"


def test_prompt_names_steps_without_marker(tmp_path: Path) -> None:
    result, resume = _run(tmp_path, "prompt", alarm_line())

    reason = _reason(result)
    assert "CronCreate" in reason
    assert "/lighthouse-handoff" in reason
    assert not resume.exists() or list(resume.iterdir()) == []


def test_no_session_no_marker(tmp_path: Path) -> None:
    payload = {"cwd": str(tmp_path), "hook_event_name": "PostToolUse"}
    result, resume = _run(tmp_path, "tool", alarm_line(), payload=payload)

    assert "no-session-" in _reason(result)
    assert not resume.exists() or list(resume.glob("*.json")) == []


def test_subagent_ignores_resume(tmp_path: Path) -> None:
    payload = _payload(tmp_path, "tool", agent_id="a1")
    result, resume = _run(tmp_path, "tool", alarm_line(), payload=payload)

    reason = _reason(result)
    assert "Stop now and report" in reason
    assert "CronCreate" not in reason
    assert not resume.exists() or list(resume.iterdir()) == []


def test_config_error_falls_back_to_base(tmp_path: Path) -> None:
    toml = MINIMAL_TOML + '\n[usage_guard]\nresume = "yes"\n'
    result, resume = _run(tmp_path, "tool", alarm_line(), toml=toml)

    reason = _reason(result)
    assert "/lighthouse-handoff" in reason
    assert "CronCreate" not in reason
    assert not resume.exists() or list(resume.iterdir()) == []
    assert "usage-guard: config:" in result.stderr

"""The usage gate with `[usage_guard] resume` (#66): wording, marker and wake-up of an ordinary
session."""

import json
import re
from datetime import datetime, timezone
from pathlib import Path

from tests.pack._testkit import (
    MINIMAL_TOML,
    alarm_line,
    guard_env,
    install_core,
    run_guard,
    write_alarm,
)

TOML = MINIMAL_TOML + "\n[usage_guard]\nresume = true\n"
EVENTS = {"prompt": "UserPromptSubmit", "tool": "PostToolUse", "stop": "Stop"}


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


WAKE_CRON = json.loads(
    (Path(__file__).resolve().parent / "fixtures" / "usage_guard" / "wake_cron.json").read_text(
        encoding="utf-8"
    )
)


def _future_wake_case() -> tuple[str, str]:
    """A UTC case of `wake_cron.json` moved to next year, as (reset_at, cron): the cron names
    no year, and a case outside February keeps its cron in every year."""
    case = next(c for c in WAKE_CRON if c["tz"] == "UTC" and c["reset_at"][5:7] != "02")
    year = datetime.now(timezone.utc).year + 1
    return f"{year}{case['reset_at'][4:]}", case["expect"]


def test_tool_window_writes_marker(tmp_path: Path) -> None:
    at, cron = _future_wake_case()
    line = f"venster 92 14:30 {at}"
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
    assert f'cron "{cron}"' in reason
    assert "recurring false" in reason
    assert "CronList" in reason
    assert f"/lighthouse-write-handoff {resume / 's1.handoff.md'}" in reason
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
    assert "/lighthouse-write-handoff" in reason
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
    assert "/lighthouse-write-handoff" in reason
    assert "CronCreate" not in reason
    assert not resume.exists() or list(resume.iterdir()) == []
    assert "usage-gate: config:" in result.stderr

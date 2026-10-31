"""`pack/core/lib/usage_guard.py` (#66): who speaks for the usage-gate hook.

The lib reads the payload into a `Context`, builds the paths and keys, claims the
"once per window" key, and picks the speaker: the base, the subagent reason, or a
module (`resume`, `controller`). See the spec
`docs/developer/planning/specs/2026-10-25-usage-gate-design.md`, sections
*De basis-guard*, *De modules* and *Contract*.
"""

import dataclasses
import io
import json
import os
import time
from datetime import datetime, timezone

import pytest

from lighthouse_lib import usage_guard as ug
from tests.pack._testkit import MINIMAL_TOML

NOW = datetime(2026, 9, 6, 11, 58, tzinfo=timezone.utc)
ALARM = ug.Alarm("venster", 92, "14:30", "2026-09-06T12:30:00Z")
RUN = "0b9e2c4a-1f00-4c8e-9d7e-5a1b2c3d4e5f"
OFF = ug.Options(False, False)


@pytest.fixture(autouse=True)
def _env(monkeypatch, tmp_path):
    # A controller run executing this suite exports these; the lib reads the run id
    # from the env, so they must not leak in.
    for name in (
        "LIGHTHOUSE_CONTROLLER_RUN",
        "LIGHTHOUSE_CONTROLLER_STATE",
        "LIGHTHOUSE_USAGE_REFRESH",
        "LIGHTHOUSE_TOML",
    ):
        monkeypatch.delenv(name, raising=False)
    monkeypatch.setenv("XDG_STATE_HOME", str(tmp_path / "state"))
    monkeypatch.setenv("XDG_DATA_HOME", str(tmp_path / "data"))


def ctx(stand, payload, **over):
    text = payload if isinstance(payload, str) else json.dumps(payload)
    alarm = over.pop("alarm", ALARM)
    pause_until = over.pop("pause_until", "")
    pause_flag = over.pop("pause_flag", "")
    c = ug.build_context(stand, alarm, pause_until, pause_flag, text, NOW)
    return dataclasses.replace(c, **over) if over else c


def decide(c, tmp_path, options=OFF):
    return ug.decide(c, tmp_path / "hooks", lambda: options)


def done_keys():
    d = ug.state_dir()
    return sorted(p.name for p in d.glob("*.done")) if d.is_dir() else []


def write_module(tmp_path, name, body):
    d = tmp_path / "hooks" / "usage-gate"
    d.mkdir(parents=True, exist_ok=True)
    (d / f"{name}.py").write_text(body, encoding="utf-8")


FAILING = "def speak(ctx, stand):\n    raise RuntimeError('x')\n"


def test_paths_follow_xdg(tmp_path):
    assert ug.state_dir() == tmp_path / "state" / "lighthouse" / "usage-guard"
    assert ug.resume_dir() == tmp_path / "data" / "lighthouse" / "resume"


def test_alarm_key_forms():
    base = "venster-2026-09-06T12:30:00Z"
    d = ug.state_dir()
    assert ug.alarm_key(ctx("tool", {"session_id": "s1"}), "tool") == d / f"{base}.tool.s1.done"
    assert ug.alarm_key(ctx("tool", {}), "tool") == d / f"{base}.tool.done"
    c = ctx("tool", {"session_id": "s1"}, run_id="R")
    assert ug.alarm_key(c, "tool") == d / f"{base}.tool.s1.R.done"
    c = ctx("subagent", {"session_id": "s1", "agent_id": "a1"}, run_id="R")
    assert ug.alarm_key(c, "subagent") == d / f"{base}.subagent.s1.a1.done"


def test_pause_key():
    c = ctx("tool", {"session_id": "s1"}, pause_until="2026-10-03T21:00:00+02:00", run_id="R")
    assert ug.pause_key(c) == ug.state_dir() / "pause-2026-10-03T21:00:00+02:00.R.done"


def test_run_id_from_env(monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_CONTROLLER_RUN", RUN)
    assert ctx("tool", {}).run_id == RUN
    for bad in ("XYZ", f"{RUN}\n", "../x", ""):
        monkeypatch.setenv("LIGHTHOUSE_CONTROLLER_RUN", bad)
        assert ctx("tool", {}).run_id == ""


@pytest.mark.parametrize("bad", ["../x", "a/b", "s" * 129, "", 7])
def test_unsafe_ids_count_as_absent(bad):
    c = ctx("tool", {"session_id": bad, "agent_id": bad})
    assert c.session_id == ""
    assert c.agent_id == ""
    assert c.payload_ok is True


def test_safe_ids_are_kept():
    c = ctx("tool", {"session_id": "s" * 128, "agent_id": "a_1-B"})
    assert c.session_id == "s" * 128
    assert c.agent_id == "a_1-B"


def test_cwd_from_payload_else_hook(tmp_path):
    assert ctx("tool", {"cwd": "/repo"}).cwd == "/repo"
    assert ctx("tool", {}).cwd == os.getcwd()


def test_handoff_path():
    assert ug.handoff_path(ctx("tool", {"session_id": "s1"})) == ug.resume_dir() / "s1.handoff.md"
    assert ug.handoff_path(ctx("tool", {})) == (
        ug.resume_dir() / f"no-session-{int(NOW.timestamp())}.handoff.md"
    )


@pytest.mark.parametrize("text", ["not json", "[]"])
def test_unreadable_payload(tmp_path, text):
    c = ctx("stop", text)
    assert c.stop_hook_active is True
    assert c.payload_ok is False
    assert c.session_id == ""
    assert decide(c, tmp_path).output is None

    out = decide(ctx("tool", text), tmp_path).output
    assert out["decision"] == "block"
    assert done_keys() == ["venster-2026-09-06T12:30:00Z.tool.done"]


def test_prompt_warns_without_key(tmp_path):
    c = ctx("prompt", {"session_id": "s1"})
    d = decide(c, tmp_path)
    text = d.output["hookSpecificOutput"]["additionalContext"]
    assert d.output["hookSpecificOutput"]["hookEventName"] == "UserPromptSubmit"
    assert "92%" in text
    assert "14:30" in text
    assert f"/lighthouse-write-handoff {ug.handoff_path(c)}" in text
    assert "decision" not in d.output
    assert d.key_after is None
    assert done_keys() == []


def test_tool_blocks_once_per_window(tmp_path):
    c = ctx("tool", {"session_id": "s1"})
    first = decide(c, tmp_path).output
    assert first["decision"] == "block"
    assert "commit" in first["reason"]
    assert f"/lighthouse-write-handoff {ug.handoff_path(c)}" in first["reason"]
    assert decide(c, tmp_path) == ug.Decision(None, None)
    assert decide(ctx("tool", {"session_id": "s2"}), tmp_path).output["decision"] == "block"


def test_tool_with_agent_id_is_subagent(tmp_path):
    c = ctx("tool", {"session_id": "s1", "agent_id": "a1"})
    assert ug.effective_stand(c) == "subagent"
    reason = decide(c, tmp_path).output["reason"]
    assert reason == ug.subagent_speak(c)["reason"]
    assert "main thread" in reason
    assert "/lighthouse-write-handoff" not in reason
    assert "CronCreate" not in reason
    assert ug.alarm_key(c, "subagent").exists()
    assert not ug.alarm_key(c, "tool").exists()


def test_subagent_once_per_agent(tmp_path):
    a1 = ctx("subagent", {"session_id": "s1", "agent_id": "a1"})
    assert decide(a1, tmp_path).output["decision"] == "block"
    assert decide(a1, tmp_path).output is None
    a2 = ctx("subagent", {"session_id": "s1", "agent_id": "a2"})
    assert decide(a2, tmp_path).output["decision"] == "block"


def test_stop(tmp_path):
    assert decide(ctx("stop", {"session_id": "s1", "stop_hook_active": True}), tmp_path) == (
        ug.Decision(None, None)
    )
    c = ctx("stop", {"session_id": "s1"})
    d = decide(c, tmp_path)
    assert d.output["decision"] == "block"
    assert f"/lighthouse-write-handoff {ug.handoff_path(c)}" in d.output["reason"]
    assert d.key_after == ug.alarm_key(c, "stop")
    assert not d.key_after.exists()


def test_stop_once_per_window(tmp_path):
    decide(ctx("tool", {"session_id": "s1"}), tmp_path)
    c = ctx("stop", {"session_id": "s1"})
    d = decide(c, tmp_path)
    assert d.output["decision"] == "block"
    d.key_after.touch()  # what `main` does after printing
    assert decide(c, tmp_path) == ug.Decision(None, None)


def test_stop_prunes_old_keys(tmp_path):
    ug.state_dir().mkdir(parents=True)
    old, recent = ug.state_dir() / "old.done", ug.state_dir() / "recent.done"
    for path, days in ((old, 15), (recent, 13)):
        path.touch()
        t = time.time() - days * 86400
        os.utime(path, (t, t))
    decide(ctx("stop", {"session_id": "s1"}), tmp_path)
    assert not old.exists()
    assert recent.exists()


def test_failing_module_releases_key(tmp_path, capsys):
    write_module(tmp_path, "resume", FAILING)
    c = ctx("tool", {"session_id": "s1"})
    assert decide(c, tmp_path, ug.Options(True, False)).output is None
    assert not ug.alarm_key(c, "tool").exists()
    err = capsys.readouterr().err
    assert err.startswith("usage-gate: ")
    assert err.count("\n") == 1


def test_module_output_is_the_only_output(tmp_path):
    write_module(tmp_path, "resume", "def speak(ctx, stand):\n    return {'decision': 'block', 'reason': 'R'}\n")
    c = ctx("tool", {"session_id": "s1"})
    assert decide(c, tmp_path, ug.Options(True, False)).output == {"decision": "block", "reason": "R"}


def test_controller_module_speaks_in_a_run(tmp_path):
    write_module(tmp_path, "resume", "def speak(ctx, stand):\n    return {'from': 'resume'}\n")
    write_module(tmp_path, "controller", "def speak(ctx, stand):\n    return {'from': 'controller', 'stand': stand}\n")
    run = ctx("stop", {"session_id": "s1"}, run_id=RUN)
    both = ug.Options(True, True)
    assert decide(run, tmp_path, both).output == {"from": "controller", "stand": "stop"}
    plain = ctx("stop", {"session_id": "s2"})
    assert decide(plain, tmp_path, both).output == {"from": "resume"}
    other = ctx("stop", {"session_id": "s3"}, run_id=RUN)
    assert decide(other, tmp_path, ug.Options(True, False)).output == {"from": "resume"}


def test_module_may_define_a_dataclass(tmp_path):
    write_module(tmp_path, "resume", (
        "from __future__ import annotations\n"
        "from dataclasses import dataclass\n\n"
        "@dataclass(frozen=True)\n"
        "class Step:\n    text: str\n\n"
        "def speak(ctx, stand):\n    return {'decision': 'block', 'reason': Step('D').text}\n"
    ))
    c = ctx("tool", {"session_id": "s1"})
    assert decide(c, tmp_path, ug.Options(True, False)).output == {"decision": "block", "reason": "D"}


def test_subagent_never_loads_a_module(tmp_path):
    write_module(tmp_path, "resume", FAILING)
    write_module(tmp_path, "controller", FAILING)
    c = ctx("subagent", {"session_id": "s1", "agent_id": "a1"}, run_id=RUN)
    assert decide(c, tmp_path, ug.Options(True, True)).output == ug.subagent_speak(c)


def test_options_read_once(tmp_path):
    calls = []

    def options():
        calls.append(1)
        return ug.Options(False, False)

    ug.decide(ctx("tool", {"session_id": "s1"}, run_id=RUN), tmp_path / "hooks", options)
    assert len(calls) == 1


def test_unwritable_state_dir_is_silent(tmp_path, monkeypatch, capsys):
    blocker = tmp_path / "file"
    blocker.write_text("x")
    monkeypatch.setenv("XDG_STATE_HOME", str(blocker))
    for stand, payload in (("tool", {"session_id": "s1"}), ("subagent", {"agent_id": "a1"})):
        for _ in range(2):
            assert decide(ctx(stand, payload), tmp_path) == ug.Decision(None, None)
            err = capsys.readouterr().err
            assert err.startswith("usage-gate: ")
            assert err.count("\n") == 1


def test_unwritable_state_dir_silences_stop(tmp_path, monkeypatch, capsys):
    blocker = tmp_path / "file"
    blocker.write_text("x")
    monkeypatch.setenv("XDG_STATE_HOME", str(blocker))
    assert decide(ctx("stop", {"session_id": "s1"}), tmp_path) == ug.Decision(None, None)
    err = capsys.readouterr().err
    assert err.startswith("usage-gate: ")
    assert err.count("\n") == 1


@pytest.mark.parametrize(
    "body",
    ["def speak(ctx, stand):\n    return {'x': object()}\n",
     "def speak(ctx, stand):\n    return ['block']\n",
     "def speak(ctx, stand):\n    return None\n"],
)
def test_bad_module_output_releases_key(tmp_path, capsys, body):
    write_module(tmp_path, "resume", body)
    c = ctx("tool", {"session_id": "s1"})
    assert decide(c, tmp_path, ug.Options(True, False)) == ug.Decision(None, None)
    assert not ug.alarm_key(c, "tool").exists()
    assert capsys.readouterr().err.startswith("usage-gate: ")


@pytest.mark.parametrize("stand", ["prompt", "tool", "subagent", "stop"])
def test_no_alarm_no_pause(tmp_path, stand):
    c = ctx(stand, {"session_id": "s1", "agent_id": "a1"}, alarm=None, run_id=RUN)
    assert decide(c, tmp_path, ug.Options(True, True)) == ug.Decision(None, None)
    assert done_keys() == []


PAUSE_MODULE = (
    "def pause(ctx):\n    return {'decision': 'block', 'reason': 'P ' + ctx.pause_until}\n"
    "def speak(ctx, stand):\n    return {'decision': 'block', 'reason': 'A'}\n"
)
UNTIL = "2026-10-03T21:00:00+02:00"


def pause_ctx(tmp_path, payload=None, **over):
    flag = tmp_path / "ctl" / "PAUSE"
    if not flag.exists():
        flag.parent.mkdir(parents=True)
        flag.write_text(UNTIL + "\n")
    over.setdefault("alarm", None)
    return ctx("tool", payload or {"session_id": "s1"}, pause_until=UNTIL, pause_flag=str(flag), run_id=RUN, **over)


def test_pause_parks_once_per_flag(tmp_path):
    write_module(tmp_path, "controller", PAUSE_MODULE)
    c = pause_ctx(tmp_path)
    on = ug.Options(False, True)
    assert decide(c, tmp_path, on).output == {"decision": "block", "reason": f"P {UNTIL}"}
    assert ug.pause_key(c).exists()
    assert decide(c, tmp_path, on) == ug.Decision(None, None)
    later = ug.pause_key(c).stat().st_mtime + 10
    os.utime(c.pause_flag, (later, later))
    assert decide(c, tmp_path, on).output["reason"] == f"P {UNTIL}"


def test_pause_needs_option_tool_and_main_thread(tmp_path):
    write_module(tmp_path, "controller", PAUSE_MODULE)
    on = ug.Options(False, True)
    assert decide(pause_ctx(tmp_path), tmp_path, OFF) == ug.Decision(None, None)
    sub = pause_ctx(tmp_path, {"session_id": "s1", "agent_id": "a1"})
    assert decide(sub, tmp_path, on) == ug.Decision(None, None)
    for stand in ("prompt", "stop"):
        c = dataclasses.replace(pause_ctx(tmp_path), stand=stand)
        assert decide(c, tmp_path, on) == ug.Decision(None, None)
    assert done_keys() == []


def test_failing_pause_falls_through_to_alarm(tmp_path, capsys):
    write_module(tmp_path, "controller", "def pause(ctx):\n    raise RuntimeError('x')\n"
                 "def speak(ctx, stand):\n    return {'decision': 'block', 'reason': 'A'}\n")
    c = pause_ctx(tmp_path, alarm=ALARM)
    assert decide(c, tmp_path, ug.Options(False, True)).output == {"decision": "block", "reason": "A"}
    assert not ug.pause_key(c).exists()
    assert capsys.readouterr().err.startswith("usage-gate: ")


@pytest.mark.parametrize("ret", ["['x']", "None", "{'x': object()}"])
def test_bad_pause_output_falls_through_to_alarm(tmp_path, capsys, ret):
    write_module(tmp_path, "controller", f"def pause(ctx):\n    return {ret}\n"
                 "def speak(ctx, stand):\n    return {'decision': 'block', 'reason': 'A'}\n")
    c = pause_ctx(tmp_path, alarm=ALARM)
    assert decide(c, tmp_path, ug.Options(False, True)).output == {"decision": "block", "reason": "A"}
    assert not ug.pause_key(c).exists()
    assert capsys.readouterr().err.startswith("usage-gate: ")


def test_options_read(tmp_path, monkeypatch):
    toml = tmp_path / "lighthouse.toml"
    toml.write_text(MINIMAL_TOML + "\n[usage_guard]\nresume = true\n", encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(toml))
    assert ug.read_options() == ug.Options(True, False)


def test_options_config_error(tmp_path, monkeypatch, capsys):
    toml = tmp_path / "lighthouse.toml"
    toml.write_text(MINIMAL_TOML + '\n[usage_guard]\nresume = "yes"\n', encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(toml))
    assert ug.read_options() == ug.Options(False, False)
    assert "usage-gate: config:" in capsys.readouterr().err


def test_marker_and_write_marker(tmp_path):
    c = ctx("tool", {"session_id": "s1", "cwd": "/repo"})
    fields = ug.marker(c, session_id="s1", window="venster", reset_at=ALARM.reset_at, source="tool")
    assert fields == {
        "session_id": "s1",
        "cwd": "/repo",
        "window": "venster",
        "reset_at": "2026-09-06T12:30:00Z",
        "created_at": "2026-09-06T11:58:00Z",
        "source": "tool",
    }
    path = ug.write_marker(dict(fields, extra=True))
    assert path == ug.resume_dir() / "s1.json"
    assert json.loads(path.read_text()) == dict(fields, extra=True)
    assert [p.name for p in ug.resume_dir().iterdir()] == ["s1.json"]


def _main(monkeypatch, capsys, argv, payload):
    monkeypatch.setattr("sys.stdin", io.StringIO(payload))
    rc = ug.main(argv)
    out = capsys.readouterr()
    return rc, out.out, out.err


ALARM_ARGS = ["--alarm", "venster", "92", "14:30", "2099-09-06T12:30:00Z"]


def test_main_stop_prints_then_writes_key(tmp_path, monkeypatch, capsys):
    argv = ["stop", "--hooks", str(tmp_path / "hooks"), *ALARM_ARGS]
    rc, out, err = _main(monkeypatch, capsys, argv, json.dumps({"session_id": "s1"}))
    assert rc == 0
    assert json.loads(out)["decision"] == "block"
    assert out.count("\n") == 1
    assert done_keys() == ["venster-2099-09-06T12:30:00Z.stop.s1.done"]
    rc, out, err = _main(monkeypatch, capsys, argv, json.dumps({"session_id": "s1"}))
    assert (rc, out) == (0, "")


def test_main_reset_local_underscore_is_space(tmp_path, monkeypatch, capsys):
    argv = ["prompt", "--hooks", str(tmp_path / "hooks"),
            "--alarm", "week", "92", "Sep_6_14:30", "2099-09-06T12:30:00Z"]
    rc, out, _ = _main(monkeypatch, capsys, argv, "{}")
    assert "Sep 6 14:30" in json.loads(out)["hookSpecificOutput"]["additionalContext"]


@pytest.mark.parametrize(
    "argv",
    [
        ["tool", "--hooks", "/h", "--alarm", "venster", "x", "14:30", "2099-09-06T12:30:00Z"],
        ["nonsense", "--hooks", "/h"],
        ["tool"],
    ],
)
def test_main_never_fails(monkeypatch, capsys, argv):
    rc, out, err = _main(monkeypatch, capsys, argv, "{}")
    assert (rc, out) == (0, "")
    assert err.startswith("usage-gate: ")
    assert err.count("\n") == 1


def test_state_dir_keeps_old_name(tmp_path):
    # The state dir keeps its old name through the hook rename: a new name resets
    # every "once per window" key on every machine. Built from parts so a text
    # replacement of the hook name cannot touch this expectation.
    d = ug.state_dir()
    assert d.name == "usage" + "-guard"
    assert d.parent.name == "lighthouse"
    assert d.parent.parent == tmp_path / "state"

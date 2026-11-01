"""The `Notification` hook `permission-note.sh` (#115): a permission prompt in a
controller run is written to `<LIGHTHOUSE_STATE_DIR>/runs/<run>/permission.json`.
"""

import json
import os
import stat
import subprocess
from pathlib import Path

from tests.pack._testkit import MINIMAL_TOML, guard_env, install_core

RUN = "0b9e2c4a-1f00-4c8e-9d7e-5a1b2c3d4e5f"
MESSAGE = "Claude needs your permission to use Write"


def _entry(tool: str, tool_input: dict, stamp: str) -> dict:
    return {
        "type": "assistant",
        "timestamp": stamp,
        "message": {
            "role": "assistant",
            "content": [{"type": "tool_use", "id": "t1", "name": tool, "input": tool_input}],
        },
    }


def _write(path: Path, entries: list[dict]) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("".join(json.dumps(e) + "\n" for e in entries), encoding="utf-8")
    return path


def _run(
    tmp_path: Path,
    payload: dict,
    run: str | None = RUN,
    state: bool = True,
) -> tuple[subprocess.CompletedProcess, Path]:
    root = tmp_path / "root"
    install_core(root, toml=MINIMAL_TOML)
    state_dir = tmp_path / "state"
    env = guard_env(tmp_path)
    env.pop("LIGHTHOUSE_STATE_DIR", None)
    if run is not None:
        env["LIGHTHOUSE_CONTROLLER_RUN"] = run
    if state:
        env["LIGHTHOUSE_STATE_DIR"] = str(state_dir)
    result = subprocess.run(
        [str(root / ".claude/hooks/lighthouse/permission-note.sh")],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(root),
        env=env,
        check=False,
        timeout=30,
    )
    return result, state_dir


def _payload(transcript: Path, kind: str = "permission_prompt") -> dict:
    return {
        "hook_event_name": "Notification",
        "session_id": "s1",
        "transcript_path": str(transcript),
        "notification_type": kind,
        "message": MESSAGE,
    }


def _note(state_dir: Path) -> dict:
    return json.loads((state_dir / "runs" / RUN / "permission.json").read_text(encoding="utf-8"))


def test_no_controller_run_writes_nothing(tmp_path: Path) -> None:
    transcript = _write(tmp_path / "t.jsonl", [])
    result, state = _run(tmp_path, _payload(transcript), run=None)
    assert result.returncode == 0
    assert result.stdout == ""
    assert not (state / "runs").exists()


def test_run_id_not_uuid_shaped_writes_nothing(tmp_path: Path) -> None:
    transcript = _write(tmp_path / "t.jsonl", [])
    result, state = _run(tmp_path, _payload(transcript), run="../x")
    assert result.returncode == 0
    assert result.stdout == ""
    assert not (state / "runs").exists()
    assert not (tmp_path / "runs").exists()


def test_permission_prompt_writes_tool_and_path(tmp_path: Path) -> None:
    transcript = _write(
        tmp_path / "t.jsonl",
        [
            _entry("Read", {"file_path": "/tmp/old"}, "2026-11-01T10:00:00Z"),
            _entry("Write", {"file_path": "/tmp/x"}, "2026-11-01T10:00:05Z"),
        ],
    )
    result, state = _run(tmp_path, _payload(transcript))
    assert result.returncode == 0
    assert result.stdout == ""
    note = _note(state)
    assert note["version"] == 1
    assert note["tool"] == "Write"
    assert note["path"] == "/tmp/x"
    assert note["thread"] == ""
    assert note["message"] == MESSAGE
    assert note["at"].endswith("Z")
    path = state / "runs" / RUN / "permission.json"
    assert stat.S_IMODE(os.stat(path).st_mode) == 0o600
    assert [p.name for p in path.parent.iterdir()] == ["permission.json"]


def test_bash_path_is_first_80_chars_of_command(tmp_path: Path) -> None:
    command = "echo " + "x" * 200
    transcript = _write(
        tmp_path / "t.jsonl", [_entry("Bash", {"command": command}, "2026-11-01T10:00:00Z")]
    )
    result, state = _run(tmp_path, _payload(transcript))
    assert result.returncode == 0
    assert _note(state)["path"] == command[:80]


def test_subagent_tool_use_wins_when_newer(tmp_path: Path) -> None:
    transcript = _write(
        tmp_path / "t.jsonl",
        [_entry("Read", {"file_path": "/tmp/main"}, "2026-11-01T10:00:00Z")],
    )
    _write(
        tmp_path / "t" / "subagents" / "agent-a.jsonl",
        [_entry("Edit", {"file_path": "/tmp/sub"}, "2026-11-01T10:00:09Z")],
    )
    result, state = _run(tmp_path, _payload(transcript))
    assert result.returncode == 0
    note = _note(state)
    assert (note["tool"], note["path"], note["thread"]) == ("Edit", "/tmp/sub", "agent-a")


def test_main_wins_when_newer_than_subagent(tmp_path: Path) -> None:
    transcript = _write(
        tmp_path / "t.jsonl",
        [_entry("Write", {"file_path": "/tmp/main"}, "2026-11-01T10:00:20Z")],
    )
    _write(
        tmp_path / "t" / "subagents" / "agent-a.jsonl",
        [_entry("Edit", {"file_path": "/tmp/sub"}, "2026-11-01T10:00:09Z")],
    )
    result, state = _run(tmp_path, _payload(transcript))
    assert result.returncode == 0
    note = _note(state)
    assert (note["tool"], note["path"], note["thread"]) == ("Write", "/tmp/main", "")


def test_worker_permission_prompt_writes(tmp_path: Path) -> None:
    transcript = _write(
        tmp_path / "t.jsonl", [_entry("Write", {"file_path": "/tmp/x"}, "2026-11-01T10:00:00Z")]
    )
    result, state = _run(tmp_path, _payload(transcript, "worker_permission_prompt"))
    assert result.returncode == 0
    assert _note(state)["tool"] == "Write"


def test_other_notification_type_writes_nothing(tmp_path: Path) -> None:
    transcript = _write(
        tmp_path / "t.jsonl", [_entry("Write", {"file_path": "/tmp/x"}, "2026-11-01T10:00:00Z")]
    )
    result, state = _run(tmp_path, _payload(transcript, "idle_prompt"))
    assert result.returncode == 0
    assert not (state / "runs").exists()


def test_unreadable_transcript_falls_back_to_message(tmp_path: Path) -> None:
    result, state = _run(tmp_path, _payload(tmp_path / "missing.jsonl"))
    assert result.returncode == 0
    note = _note(state)
    assert (note["tool"], note["path"], note["thread"]) == ("Write", "", "")


def test_second_prompt_overwrites(tmp_path: Path) -> None:
    transcript = _write(
        tmp_path / "t.jsonl", [_entry("Write", {"file_path": "/tmp/a"}, "2026-11-01T10:00:00Z")]
    )
    _run(tmp_path, _payload(transcript))
    _write(transcript, [_entry("Edit", {"file_path": "/tmp/b"}, "2026-11-01T10:01:00Z")])
    _run(tmp_path, _payload(transcript))
    assert _note(tmp_path / "state")["path"] == "/tmp/b"


def test_no_state_dir_could_not_check(tmp_path: Path) -> None:
    transcript = _write(tmp_path / "t.jsonl", [])
    result, _ = _run(tmp_path, _payload(transcript), state=False)
    assert result.returncode == 0
    assert result.stdout == ""
    assert "COULD NOT CHECK" in result.stderr

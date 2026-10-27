"""`pack/core/lib/deadline.py`, as installed (#884): cutting off commands at an epoch deadline.

The pre-commit test gate (task 3) runs the pytest suite through this script so that the
gate itself ends before the PreToolUse hook timeout, instead of relying on the
fail-open behaviour of Claude Code on a timeout. These tests exercise the module
directly: no subagent, no real git commit needed.
"""

import os
import signal
import subprocess
import sys
import time

import pytest

from tests.pack._testkit import INSTALLED_LIB, load_hook_lib

dl = load_hook_lib("deadline")


def _gone_within(pid: int, seconds: float) -> bool:
    """Poll until `pid` is gone (ProcessLookupError) or time is up."""
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            return True
        time.sleep(0.05)
    return False


def _pid_within(pidfile, seconds: float) -> int | None:
    """Poll until `pidfile` holds a pid, or time is up."""
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        text = pidfile.read_text() if pidfile.exists() else ""
        if text.strip():
            return int(text)
        time.sleep(0.05)
    return None


def test_passes_exit_code_through():
    assert dl.run_until(time.time() + 30, ["sh", "-c", "exit 5"]) == 5


def test_expired_deadline_returns_124_and_kills_the_group(tmp_path):
    pidfile = tmp_path / "pid"
    start = time.monotonic()
    rc = dl.run_until(
        time.time() + 1, ["sh", "-c", f"sleep 30 & echo $! > {pidfile}; wait"]
    )
    assert rc == dl.TIMEOUT_EXIT and time.monotonic() - start < 10
    pid = int(pidfile.read_text())
    # small loop until the child is gone; after 5 s it is a failure
    assert _gone_within(pid, 5.0)


def test_sigterm_ignorer_is_killed_after_grace():
    start = time.monotonic()
    rc = dl.run_until(
        time.time() + 1, ["sh", "-c", "trap '' TERM; sleep 30"], grace=1.0
    )
    assert rc == dl.TIMEOUT_EXIT and time.monotonic() - start < 6


def test_past_deadline_does_not_start(tmp_path):
    marker = tmp_path / "ran"
    assert dl.run_until(time.time() - 1, ["touch", str(marker)]) == dl.TIMEOUT_EXIT
    assert not marker.exists()


def test_cli_usage_error_exits_2():
    r = subprocess.run(
        [sys.executable, str(INSTALLED_LIB / "deadline.py"), "nope"],
        capture_output=True,
        text=True,
        check=False,
    )
    assert r.returncode == 2 and "usage" in r.stderr.lower()
    assert "epoch-seconds" in r.stderr


def test_cli_runs_command_and_passes_exit_code(tmp_path):
    r = subprocess.run(
        [
            sys.executable,
            str(INSTALLED_LIB / "deadline.py"),
            str(time.time() + 30),
            "--",
            "sh",
            "-c",
            "exit 7",
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    assert r.returncode == 7


@pytest.mark.parametrize("argv", [[], ["123"], ["123", "--"]])
def test_cli_usage_error_variants_exit_2(argv):
    r = subprocess.run(
        [sys.executable, str(INSTALLED_LIB / "deadline.py"), *argv],
        capture_output=True,
        text=True,
        check=False,
    )
    assert r.returncode == 2 and "usage" in r.stderr.lower()


@pytest.mark.parametrize(
    "signum", [signal.SIGTERM, signal.SIGINT, signal.SIGHUP], ids=lambda s: s.name
)
def test_signal_to_deadline_kills_the_group(tmp_path, signum):
    """If the hook itself is killed, the suite must not keep running as an orphan.

    The child runs in its own session and so does not get the signal by itself.
    """
    pidfile = tmp_path / "pid"
    proc = subprocess.Popen(
        [
            sys.executable,
            str(INSTALLED_LIB / "deadline.py"),
            str(time.time() + 60),
            "--",
            "sh",
            "-c",
            f"sleep 30 & echo $! > {pidfile}; wait",
        ]
    )
    pid = None
    try:
        pid = _pid_within(pidfile, 5.0)
        assert pid is not None
        proc.send_signal(signum)
        assert proc.wait(timeout=15) == 128 + signum
        assert _gone_within(pid, 5.0)
    finally:
        if proc.poll() is None:
            proc.kill()
            proc.wait()
        if pid is not None:
            try:
                os.killpg(os.getpgid(pid), signal.SIGKILL)
            except ProcessLookupError:
                pass


def test_missing_command_returns_127():
    assert dl.run_until(time.time() + 30, ["lighthouse-does-not-exist-xyz"]) == 127


def test_cli_missing_command_exits_127_with_one_line():
    r = subprocess.run(
        [
            sys.executable,
            str(INSTALLED_LIB / "deadline.py"),
            str(time.time() + 30),
            "--",
            "lighthouse-does-not-exist-xyz",
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    assert r.returncode == 127
    assert "Traceback" not in r.stderr
    assert len(r.stderr.strip().splitlines()) == 1
    assert "lighthouse-does-not-exist-xyz" in r.stderr
    assert "command not found" in r.stderr


def test_timeout_survives_child_vanishing_before_the_kill(monkeypatch):
    """If the child is already gone just after the timeout, no ProcessLookupError crash.

    Simulated: every process-group lookup fails as if the child were already
    gone. With `start_new_session` the pgid is simply the pid; no lookup is needed.
    """

    def gone(_pid: int) -> int:
        raise ProcessLookupError

    monkeypatch.setattr(dl.os, "getpgid", gone)
    rc = dl.run_until(time.time() + 0.5, ["sleep", "30"], grace=1.0)
    assert rc == dl.TIMEOUT_EXIT


def test_capture_returns_stdout_and_stderr_together():
    rc, out = dl.run_until(
        time.time() + 30, ["sh", "-c", "echo a; echo b >&2; exit 3"], capture=True
    )
    assert rc == 3 and out.split() == [b"a", b"b"]


def test_capture_survives_more_than_a_pipe_buffer():
    rc, out = dl.run_until(
        time.time() + 30,
        [sys.executable, "-c", "import sys; sys.stdout.write('x'*200000)"],
        capture=True,
    )
    assert rc == 0 and len(out) == 200000


def test_stdin_bytes_and_cwd_reach_the_child(tmp_path):
    rc, out = dl.run_until(
        time.time() + 30,
        ["sh", "-c", "pwd; cat"],
        cwd=str(tmp_path),
        stdin_bytes=b"payload",
        capture=True,
    )
    assert rc == 0
    assert out.decode().split() == [str(tmp_path), "payload"]


def test_capture_on_timeout_returns_124_and_what_was_written():
    rc, out = dl.run_until(
        time.time() + 1, ["sh", "-c", "echo vroeg; sleep 30"], grace=1, capture=True
    )
    assert rc == dl.TIMEOUT_EXIT and b"vroeg" in out


def test_default_call_still_returns_a_bare_int():
    assert dl.run_until(time.time() + 30, ["true"]) == 0


def test_capture_on_missing_command_returns_not_found_and_empty_bytes():
    """As the docstring promises: the message goes only to stderr, not into the buffer."""
    rc, out = dl.run_until(time.time() + 30, ["lighthouse-does-not-exist-xyz"], capture=True)
    assert rc == dl.NOT_FOUND_EXIT and out == b""


def test_raise_on_signal_raises_interrupted_with_the_output():
    """With `raise_on_signal` a forwarded signal reports itself as an exception.

    The child sends SIGTERM to its parent (this process): the forwarder is
    already in place, so the signal aborts the wait instead of pytest.
    """
    with pytest.raises(dl.Interrupted) as caught:
        dl.run_until(
            time.time() + 30,
            ["sh", "-c", "echo vroeg; kill -TERM $PPID; sleep 30"],
            grace=1.0,
            capture=True,
            raise_on_signal=True,
        )
    assert caught.value.signum == signal.SIGTERM
    assert b"vroeg" in caught.value.output


@pytest.mark.parametrize("rc", [129, 130, 143])
def test_raise_on_signal_passes_a_child_exit_of_128_plus_n_through(rc):
    """A child that itself ends with 128+n is not a forwarded signal."""
    got, _out = dl.run_until(
        time.time() + 30,
        ["sh", "-c", f"exit {rc}"],
        capture=True,
        raise_on_signal=True,
    )
    assert got == rc


def test_run_until_env():
    """`env` is the child's whole environment; this process's own stays as it was."""
    assert "X" not in os.environ
    got = dl.run_until(
        time.time() + 30, ["sh", "-c", "echo $X"], env={**os.environ, "X": "y"}, capture=True
    )
    assert got == (0, b"y\n")
    assert "X" not in os.environ

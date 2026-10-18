"""`.claude/hooks/lib/deadline.py` (#884): commando's afkappen op een epoch-deadline.

De pre-commit test-gate (taak 3) draait de pytest-suite via dit script zodat de gate
zélf eindigt vóór de PreToolUse-hooktimeout, in plaats van te vertrouwen op het
fail-open gedrag van Claude Code bij een timeout. Deze tests toetsen de module
direct — geen subagent, geen echte git-commit nodig.
"""

import os
import signal
import subprocess
import sys
import time

import pytest

from tests.unit._hook_testkit import HOOKS, load_hook_lib

dl = load_hook_lib("deadline")


def _gone_within(pid: int, seconds: float) -> bool:
    """Poll tot `pid` weg is (ProcessLookupError) of de tijd om is."""
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            return True
        time.sleep(0.05)
    return False


def _pid_within(pidfile, seconds: float) -> int | None:
    """Poll tot `pidfile` een pid bevat, of de tijd om is."""
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
    # kleine lus tot het kind weg is; na 5 s is het een faal
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
        [sys.executable, str(HOOKS / "lib" / "deadline.py"), "nope"],
        capture_output=True,
        text=True,
        check=False,
    )
    assert r.returncode == 2 and "usage" in r.stderr.lower()


def test_cli_runs_command_and_passes_exit_code(tmp_path):
    r = subprocess.run(
        [
            sys.executable,
            str(HOOKS / "lib" / "deadline.py"),
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
        [sys.executable, str(HOOKS / "lib" / "deadline.py"), *argv],
        capture_output=True,
        text=True,
        check=False,
    )
    assert r.returncode == 2 and "usage" in r.stderr.lower()


@pytest.mark.parametrize(
    "signum", [signal.SIGTERM, signal.SIGINT, signal.SIGHUP], ids=lambda s: s.name
)
def test_signal_to_deadline_kills_the_group(tmp_path, signum):
    """Wordt de hook zelf gekild, dan mag de suite niet als wees doorlopen.

    Het kind draait in een eigen sessie en krijgt het signaal dus niet vanzelf.
    """
    pidfile = tmp_path / "pid"
    proc = subprocess.Popen(
        [
            sys.executable,
            str(HOOKS / "lib" / "deadline.py"),
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
    assert dl.run_until(time.time() + 30, ["paia-bestaat-niet-xyz"]) == 127


def test_cli_missing_command_exits_127_with_one_line():
    r = subprocess.run(
        [
            sys.executable,
            str(HOOKS / "lib" / "deadline.py"),
            str(time.time() + 30),
            "--",
            "paia-bestaat-niet-xyz",
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    assert r.returncode == 127
    assert "Traceback" not in r.stderr
    assert len(r.stderr.strip().splitlines()) == 1
    assert "paia-bestaat-niet-xyz" in r.stderr


def test_timeout_survives_child_vanishing_before_the_kill(monkeypatch):
    """Is het kind net na de timeout al weg, dan geen ProcessLookupError-crash.

    Nagebootst: elke procesgroep-opvraag faalt alsof het kind al weg is. De
    pgid is bij `start_new_session` gewoon de pid; opvragen is niet nodig.
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
    """Zoals de docstring belooft: de melding gaat alleen naar stderr, niet in de buffer."""
    rc, out = dl.run_until(time.time() + 30, ["paia-bestaat-niet-xyz"], capture=True)
    assert rc == dl.NOT_FOUND_EXIT and out == b""


def test_raise_on_signal_raises_interrupted_with_the_output():
    """Met `raise_on_signal` meldt een doorgestuurd signaal zich als uitzondering.

    Het kind stuurt SIGTERM naar zijn ouder (dit proces): de forwarder staat
    dan al, dus het signaal breekt het wachten af in plaats van pytest.
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
    """Een kind dat zelf met 128+n eindigt is geen doorgestuurd signaal."""
    got, _out = dl.run_until(
        time.time() + 30,
        ["sh", "-c", f"exit {rc}"],
        capture=True,
        raise_on_signal=True,
    )
    assert got == rc

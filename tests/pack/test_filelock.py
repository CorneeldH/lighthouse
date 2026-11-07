"""Tests for `pack/core/lib/filelock.py` and the Windows paths of the libs that use it (#90).

On POSIX the lock is a real `flock`: two `os.open` calls on one path are two open
file descriptions and clash, even within one process. The Windows path runs here
against a fake `msvcrt` that models the lock table per file and checks the
arguments `msvcrt.locking` takes; CI measures the real one with the
`windows-measure` workflow.
"""

import errno
import os
import signal
import sys
import types
from pathlib import Path

import pytest

from tests.pack._testkit import load_hook_lib

filelock = load_hook_lib("filelock")


def _open(path: Path) -> int:
    return os.open(path, os.O_RDWR | os.O_CREAT)


@pytest.fixture
def two_fds(tmp_path: Path):
    path = tmp_path / "x.lock"
    fds = (_open(path), _open(path))
    yield fds
    for fd in fds:
        os.close(fd)


def test_posix_lock_is_exclusive_until_unlocked(two_fds: tuple[int, int]) -> None:
    a, b = two_fds
    assert filelock.try_lock(a) is True
    assert filelock.try_lock(b) is False
    filelock.unlock(a)
    assert filelock.try_lock(b) is True


def test_posix_blocking_lock_holds_the_file(two_fds: tuple[int, int]) -> None:
    a, b = two_fds
    filelock.lock(a)
    assert filelock.try_lock(b) is False


def test_posix_lock_stays_held_past_exit() -> None:
    assert filelock.HELD_PAST_EXIT is True


class FakeMsvcrt(types.ModuleType):
    """`msvcrt.locking` as Windows does it: one byte from the file position, per file."""

    LK_UNLCK, LK_LOCK, LK_NLCK, LK_RLCK, LK_NBRLCK = 0, 1, 2, 3, 4
    LK_NBLCK = LK_NLCK

    def __init__(self) -> None:
        super().__init__("msvcrt")
        self.held: dict[int, int] = {}  # inode -> fd holding byte 0
        self.fail_errno: int | None = None

    def locking(self, fd: int, mode: int, nbytes: int) -> None:
        if nbytes != 1 or os.lseek(fd, 0, os.SEEK_CUR) != 0:
            raise AssertionError(f"locking({fd}, {mode}, {nbytes}) not on byte 0")
        if self.fail_errno is not None:
            raise OSError(self.fail_errno, os.strerror(self.fail_errno))
        inode = os.fstat(fd).st_ino
        if mode == self.LK_NBLCK:
            if inode in self.held:
                raise OSError(errno.EACCES, "Permission denied")
            self.held[inode] = fd
        elif mode == self.LK_UNLCK:
            if self.held.get(inode) != fd:
                raise OSError(errno.EACCES, "Permission denied")
            del self.held[inode]
        else:
            raise AssertionError(f"unexpected mode {mode}")


@pytest.fixture
def windows(monkeypatch: pytest.MonkeyPatch):
    """filelock loaded as on Windows: no fcntl, the fake msvcrt."""
    fake = FakeMsvcrt()
    monkeypatch.setitem(sys.modules, "fcntl", None)
    monkeypatch.setitem(sys.modules, "msvcrt", fake)
    return load_hook_lib("filelock"), fake


def test_windows_lock_is_exclusive_until_unlocked(windows, two_fds) -> None:
    lock, _ = windows
    a, b = two_fds
    assert lock.try_lock(a) is True
    assert lock.try_lock(b) is False
    lock.unlock(a)
    assert lock.try_lock(b) is True


def test_windows_blocking_lock_holds_the_file(windows, two_fds) -> None:
    lock, _ = windows
    a, b = two_fds
    lock.lock(a)
    assert lock.try_lock(b) is False


def test_windows_lock_keeps_the_file_position(windows, two_fds) -> None:
    lock, _ = windows
    a, _ = two_fds
    os.write(a, b"hello")
    assert lock.try_lock(a) is True
    assert os.lseek(a, 0, os.SEEK_CUR) == 5
    lock.unlock(a)
    assert os.lseek(a, 0, os.SEEK_CUR) == 5


@pytest.mark.parametrize("busy", [errno.EACCES, errno.EDEADLK])
def test_windows_busy_errnos_mean_taken(windows, two_fds, busy: int) -> None:
    lock, fake = windows
    fake.fail_errno = busy
    assert lock.try_lock(two_fds[0]) is False


def test_windows_other_errors_propagate(windows, two_fds) -> None:
    lock, fake = windows
    fake.fail_errno = errno.EBADF
    with pytest.raises(OSError) as info:
        lock.try_lock(two_fds[0])
    assert info.value.errno == errno.EBADF


def test_windows_lock_goes_with_the_process(windows) -> None:
    lock, _ = windows
    assert lock.HELD_PAST_EXIT is False


def test_gate_slot_refuses_where_the_lock_goes_with_the_process(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    gate_slot = load_hook_lib("gate_slot")
    monkeypatch.setattr(gate_slot.filelock, "HELD_PAST_EXIT", False)
    assert gate_slot.main(["--fd", "3", "--wait", "0"]) == 1
    captured = capsys.readouterr()
    assert captured.out == ""
    assert "gate slots need flock, which this platform lacks (Windows)" in captured.err


def test_deadline_imports_without_sighup_and_sigkill(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delattr(signal, "SIGHUP")
    monkeypatch.delattr(signal, "SIGKILL")
    deadline = load_hook_lib("deadline")
    assert deadline.FORWARDED == (signal.SIGTERM, signal.SIGINT)
    assert deadline.HARD_KILL == signal.SIGTERM


def test_deadline_kills_the_tree_with_taskkill_without_killpg(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    deadline = load_hook_lib("deadline")
    calls: list[list[str]] = []

    def fake_run(argv: list[str], **kwargs: object) -> None:
        assert kwargs.get("check") is False
        calls.append(argv)

    monkeypatch.delattr(os, "killpg")
    monkeypatch.setattr(deadline.subprocess, "run", fake_run)
    deadline._kill_group(4321, signal.SIGTERM)
    assert calls == [["taskkill", "/F", "/T", "/PID", "4321"]]

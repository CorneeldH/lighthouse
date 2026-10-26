"""Tests for `pack/core/lib/gate_slot.py`, as installed, the slot semaphore of the test gate (#1184).

A second `os.open` on the same path is its own open file description and so clashes
with a `flock` on the first, even within one process. That way the test keeps a slot
"taken" without a second process.
"""

import contextlib
import fcntl
import os
import subprocess
import sys
import threading
import time
from collections.abc import Iterator
from pathlib import Path

import pytest

from tests.pack._testkit import INSTALLED_LIB, load_hook_lib

gate_slot = load_hook_lib("gate_slot")
GATE_SLOT = INSTALLED_LIB / "gate_slot.py"


def _open(path: Path) -> int:
    return os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND)


@pytest.fixture
def slots(tmp_path: Path) -> Iterator[tuple[int, int]]:
    """Two lock files, opened the way the hook's bash opens them."""
    fds = (_open(tmp_path / "slot-1"), _open(tmp_path / "slot-2"))
    yield fds
    for fd in fds:
        with contextlib.suppress(OSError):
            os.close(fd)


def _hold(path: Path) -> int:
    """Take the slot at `path` through its own open file description."""
    fd = _open(path)
    fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    return fd


def test_acquire_takes_the_first_free_slot(slots: tuple[int, int]) -> None:
    assert gate_slot.acquire(list(slots), 0) == 1


def test_acquire_skips_a_held_slot(tmp_path: Path, slots: tuple[int, int]) -> None:
    holder = _hold(tmp_path / "slot-1")
    try:
        assert gate_slot.acquire(list(slots), 0) == 2
    finally:
        os.close(holder)


def test_acquire_gives_none_when_both_are_held(
    tmp_path: Path, slots: tuple[int, int]
) -> None:
    holders = [_hold(tmp_path / "slot-1"), _hold(tmp_path / "slot-2")]
    try:
        start = time.monotonic()
        assert gate_slot.acquire(list(slots), 0) is None
        assert time.monotonic() - start < 0.5
    finally:
        for fd in holders:
            os.close(fd)


def test_acquire_waits_for_a_slot_that_frees(
    tmp_path: Path, slots: tuple[int, int]
) -> None:
    holders = [_hold(tmp_path / "slot-1"), _hold(tmp_path / "slot-2")]
    timer = threading.Timer(0.3, os.close, args=(holders[1],))
    timer.start()
    try:
        assert gate_slot.acquire(list(slots), 2, interval=0.1) == 2
    finally:
        timer.join()
        os.close(holders[0])


def test_acquire_raises_on_a_bad_fd() -> None:
    with pytest.raises(OSError):
        gate_slot.acquire([987654], 0)


def _cli(fds: tuple[int, ...], wait: str) -> subprocess.CompletedProcess[str]:
    argv = [sys.executable, str(GATE_SLOT), "--wait", wait]
    for fd in fds:
        argv += ["--fd", str(fd)]
    return subprocess.run(
        argv, capture_output=True, text=True, pass_fds=fds, check=False
    )


def test_cli_prints_the_slot_and_the_lock_outlives_the_process(
    tmp_path: Path, slots: tuple[int, int]
) -> None:
    result = _cli(slots, "0")

    assert result.returncode == 0
    assert result.stdout == "1\n"
    # The child is gone; the flock belongs to the open file description of THIS process.
    probe = _open(tmp_path / "slot-1")
    try:
        with pytest.raises(BlockingIOError):
            fcntl.flock(probe, fcntl.LOCK_EX | fcntl.LOCK_NB)
    finally:
        os.close(probe)


def test_cli_exits_75_when_busy(tmp_path: Path, slots: tuple[int, int]) -> None:
    holders = [_hold(tmp_path / "slot-1"), _hold(tmp_path / "slot-2")]
    try:
        result = _cli(slots, "0")
    finally:
        for fd in holders:
            os.close(fd)

    assert result.returncode == gate_slot.SLOT_BUSY_EXIT == 75
    assert result.stdout == ""


def test_cli_exits_1_on_a_closed_fd() -> None:
    result = subprocess.run(
        [sys.executable, str(GATE_SLOT), "--fd", "987654", "--wait", "0"],
        capture_output=True,
        text=True,
        check=False,
    )

    assert result.returncode == 1
    assert "gate_slot.py:" in result.stderr

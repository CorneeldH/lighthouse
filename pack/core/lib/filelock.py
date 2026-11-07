"""An exclusive lock on an open fd: `flock` on POSIX, `msvcrt.locking` on Windows (#90).

The two differ in who holds the lock. A `flock` belongs to the open file
description, so it stays as long as any process keeps that fd open: gate_slot.py
relies on that (`HELD_PAST_EXIT`). A `msvcrt` lock belongs to the process and
goes when it exits; there only a caller that locks, works and unlocks within one
process (the ledger of bash_writes.py) gets what it expects.

On Windows the lock covers byte 0 of the file, whatever the file position is.
"""

# `X | None` in annotations: the hooks also run under python3 3.9 (macOS).
from __future__ import annotations

import errno
import os
import time

try:
    import fcntl
except ImportError:  # Windows: no fcntl, msvcrt instead
    fcntl = None
    import msvcrt

HELD_PAST_EXIT = fcntl is not None

# What msvcrt.locking raises for "another handle holds it": EACCES, or EDEADLOCK
# after its own retries.
_BUSY = (errno.EACCES, errno.EDEADLK)
_POLL = 0.05


def _msvcrt(fd: int, mode: int) -> None:
    # Restore the position: a caller that reads or writes through fd must not notice the lock.
    here = os.lseek(fd, 0, os.SEEK_CUR)
    os.lseek(fd, 0, os.SEEK_SET)
    try:
        msvcrt.locking(fd, mode, 1)
    finally:
        os.lseek(fd, here, os.SEEK_SET)


def try_lock(fd: int) -> bool:
    """Take the exclusive lock without waiting; False when another fd holds it.

    Every other `OSError` (a closed fd, say) propagates: then it was not tested
    whether the lock was free.
    """
    if fcntl is not None:
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return False
        return True
    try:
        _msvcrt(fd, msvcrt.LK_NBLCK)
    except OSError as err:
        if err.errno in _BUSY:
            return False
        raise
    return True


def lock(fd: int) -> None:
    """Take the exclusive lock; wait as long as it takes, like `flock(LOCK_EX)`."""
    if fcntl is not None:
        fcntl.flock(fd, fcntl.LOCK_EX)
        return
    while not try_lock(fd):
        time.sleep(_POLL)


def unlock(fd: int) -> None:
    if fcntl is not None:
        fcntl.flock(fd, fcntl.LOCK_UN)
        return
    _msvcrt(fd, msvcrt.LK_UNLCK)

"""Tests for `pack/core/lib/host_memory.py`, the host memory check of the test gate (#171).

A made-up meminfo in `tmp_path` stands in for `/proc/meminfo`; the CLI runs as
installed, the way the hook calls it.
"""

import subprocess
import sys
import threading
import time
from pathlib import Path

import pytest

from tests.pack._testkit import INSTALLED_LIB, load_hook_lib

host_memory = load_hook_lib("host_memory")
HOST_MEMORY = INSTALLED_LIB / "host_memory.py"

GIB_KB = 1024 * 1024


def _meminfo(path: Path, avail_kb: int, swap_free_kb: int, swap_total_kb: int = 24 * GIB_KB) -> Path:
    path.write_text(
        f"MemTotal:       {16 * GIB_KB} kB\n"
        f"MemFree:          123456 kB\n"
        f"MemAvailable:   {avail_kb} kB\n"
        f"SwapTotal:      {swap_total_kb} kB\n"
        f"SwapFree:       {swap_free_kb} kB\n"
    )
    return path


def _cli(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [sys.executable, str(HOST_MEMORY), *args], capture_output=True, text=True, timeout=30
    )


def test_shortfalls_both_low(tmp_path):
    info = host_memory.read_meminfo(_meminfo(tmp_path / "meminfo", 734003, 524288))
    assert host_memory.shortfalls(info, 4, 2) == (
        ["MemAvailable 0.7 GiB < 4 GiB", "SwapFree 0.5 GiB < 2 GiB"],
        [],
    )


def test_shortfalls_only_set_threshold_counts(tmp_path):
    info = host_memory.read_meminfo(_meminfo(tmp_path / "meminfo", 734003, 524288))
    assert host_memory.shortfalls(info, 4, None) == (["MemAvailable 0.7 GiB < 4 GiB"], [])


def test_shortfalls_enough_is_empty(tmp_path):
    info = host_memory.read_meminfo(_meminfo(tmp_path / "meminfo", 8 * GIB_KB, 4 * GIB_KB))
    assert host_memory.shortfalls(info, 4, 2) == ([], [])


def test_no_swap_skips_swap_check(tmp_path):
    info = host_memory.read_meminfo(_meminfo(tmp_path / "meminfo", 8 * GIB_KB, 0, swap_total_kb=0))
    assert host_memory.shortfalls(info, None, 2) == (
        [],
        ["no swap on this host, SwapFree not checked"],
    )


def test_read_meminfo_missing_memavailable(tmp_path):
    path = tmp_path / "meminfo"
    path.write_text("MemTotal:       16000000 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n")
    with pytest.raises(ValueError):
        host_memory.read_meminfo(path)
    with pytest.raises(OSError):
        host_memory.read_meminfo(tmp_path / "absent")


def test_cli_ok(tmp_path):
    path = _meminfo(tmp_path / "meminfo", 8 * GIB_KB, 4 * GIB_KB)
    result = _cli("--meminfo", str(path), "--min-available-gib", "4", "--min-swap-free-gib", "2", "--wait", "0")
    assert (result.returncode, result.stdout) == (0, "")


def test_cli_prints_notes(tmp_path):
    path = _meminfo(tmp_path / "meminfo", 8 * GIB_KB, 0, swap_total_kb=0)
    result = _cli("--meminfo", str(path), "--min-swap-free-gib", "2", "--wait", "0")
    assert (result.returncode, result.stdout) == (0, "no swap on this host, SwapFree not checked\n")


def test_cli_times_out(tmp_path):
    path = _meminfo(tmp_path / "meminfo", 734003, 4 * GIB_KB)
    result = _cli("--meminfo", str(path), "--min-available-gib", "4", "--wait", "0")
    assert (result.returncode, result.stdout) == (75, "MemAvailable 0.7 GiB < 4 GiB\n")


def test_cli_joins_shortfalls(tmp_path):
    path = _meminfo(tmp_path / "meminfo", 734003, 524288)
    result = _cli("--meminfo", str(path), "--min-available-gib", "4", "--min-swap-free-gib", "2", "--wait", "0")
    assert (result.returncode, result.stdout) == (
        75,
        "MemAvailable 0.7 GiB < 4 GiB; SwapFree 0.5 GiB < 2 GiB\n",
    )


def test_cli_waits_until_enough(tmp_path):
    path = _meminfo(tmp_path / "meminfo", 734003, 4 * GIB_KB)
    timer = threading.Timer(0.5, _meminfo, (path, 8 * GIB_KB, 4 * GIB_KB))
    timer.start()
    try:
        start = time.monotonic()
        result = _cli("--meminfo", str(path), "--min-available-gib", "4", "--wait", "5", "--interval", "0.1")
        elapsed = time.monotonic() - start
    finally:
        timer.cancel()
    assert result.returncode == 0, result.stdout + result.stderr
    assert 0.4 < elapsed < 3


def test_cli_wait_does_not_overshoot(tmp_path):
    # The last sleep stops at start + wait: a gate whose budget is the rest of its
    # deadline must not hand deadline.py an expired one.
    path = _meminfo(tmp_path / "meminfo", 734003, 4 * GIB_KB)
    start = time.monotonic()
    result = _cli("--meminfo", str(path), "--min-available-gib", "4", "--wait", "0.3", "--interval", "5")
    elapsed = time.monotonic() - start
    assert result.returncode == 75
    assert elapsed < 2


def test_cli_unreadable(tmp_path):
    path = tmp_path / "absent"
    result = _cli("--meminfo", str(path), "--min-available-gib", "4", "--wait", "0")
    assert result.returncode == 2
    assert str(path) in result.stderr

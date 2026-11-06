"""Wait until the host has enough free memory for one test gate run (#171).

The test gate takes a slot first and then calls this script, before it starts the
suite. A slot alone does not protect the host: a slot says how many gates run, not
how much memory the rest of the host leaves them. This script reads
`/proc/meminfo` and checks `MemAvailable` and `SwapFree` against the thresholds
`[host] gate_min_mem_available_gb` and `gate_min_swap_free_gb` in `host.toml`.
Each threshold checks only its own value; one not given is not checked.

It measures at least once, also with `--wait 0`, and then every `interval`
seconds until start + `wait`. Exit 0: the thresholds are met; every note goes to
stdout, one per line. Exit `BELOW_EXIT`: still short after `wait`; stdout holds
one line with the shortfalls joined by `; `. Exit 2: not measurable (the file
cannot be read, or lacks `MemAvailable:`); stderr says why. The gate never treats
exit 2 as a pass.

A host without swap (`SwapTotal: 0 kB`) skips the swap threshold with a note:
checking it literally would block every commit there.
"""

# `X | None` in annotations: the hooks also run under python3 3.9 (macOS).
from __future__ import annotations

import argparse
import sys
import time
from pathlib import Path

BELOW_EXIT = 75
KIB_PER_GIB = 1024 * 1024
NO_SWAP_NOTE = "no swap on this host, SwapFree not checked"


def read_meminfo(path: Path) -> dict[str, int]:
    """The fields of a meminfo file in kB; `OSError` or `ValueError` when it is not usable."""
    info: dict[str, int] = {}
    for line in path.read_text().splitlines():
        name, sep, rest = line.partition(":")
        fields = rest.split()
        if not sep or not fields:
            continue
        try:
            info[name.strip()] = int(fields[0])
        except ValueError:
            continue
    if "MemAvailable" not in info:
        raise ValueError(f"{path}: no MemAvailable: line")
    return info


def _shortfall(name: str, value_kb: int, minimum_gib: float) -> str | None:
    value_gib = value_kb / KIB_PER_GIB
    if value_gib >= minimum_gib:
        return None
    return f"{name} {value_gib:.1f} GiB < {minimum_gib:g} GiB"


def shortfalls(
    info: dict[str, int], min_available_gib: float | None, min_swap_free_gib: float | None
) -> tuple[list[str], list[str]]:
    """(shortfalls, notes) of one measurement against the thresholds that are set."""
    found: list[str] = []
    notes: list[str] = []
    if min_available_gib is not None:
        line = _shortfall("MemAvailable", info["MemAvailable"], min_available_gib)
        if line is not None:
            found.append(line)
    if min_swap_free_gib is not None:
        if info.get("SwapTotal", 0) == 0:
            notes.append(NO_SWAP_NOTE)
        else:
            line = _shortfall("SwapFree", info.get("SwapFree", 0), min_swap_free_gib)
            if line is not None:
                found.append(line)
    return found, notes


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="host_memory.py")
    parser.add_argument("--meminfo", type=Path, required=True)
    parser.add_argument("--min-available-gib", type=float)
    parser.add_argument("--min-swap-free-gib", type=float)
    parser.add_argument("--wait", type=float, required=True)
    parser.add_argument("--interval", type=float, default=1.0)
    args = parser.parse_args(argv)
    deadline = time.monotonic() + args.wait
    while True:
        try:
            info = read_meminfo(args.meminfo)
        except (OSError, ValueError) as err:
            sys.stderr.write(f"host_memory.py: {args.meminfo}: {err}\n")
            return 2
        found, notes = shortfalls(info, args.min_available_gib, args.min_swap_free_gib)
        if not found:
            for note in notes:
                sys.stdout.write(f"{note}\n")
            return 0
        if time.monotonic() >= deadline:
            sys.stdout.write("; ".join(found) + "\n")
            return BELOW_EXIT
        time.sleep(args.interval)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

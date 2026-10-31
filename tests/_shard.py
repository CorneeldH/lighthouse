"""Split the suite into parts for CI: `LIGHTHOUSE_PYTEST_SHARD=<i>/<n>` runs part i of n.

A test belongs to part `zlib.crc32(nodeid) % n + 1`. Not `hash()`: that differs per process, so
the xdist workers and the CI jobs would each split differently.
"""

import re
import zlib

ENV = "LIGHTHOUSE_PYTEST_SHARD"


def parse_shard(value: str | None) -> tuple[int, int] | None:
    """`(index, total)` from `<i>/<n>`, None when unset; ValueError on anything else."""
    if value is None:
        return None
    match = re.fullmatch(r"(\d+)/(\d+)", value)
    if match:
        index, total = int(match.group(1)), int(match.group(2))
        if 1 <= index <= total:
            return index, total
    raise ValueError(f"{ENV}={value!r}: expected <i>/<n> with 1 <= i <= n, e.g. 2/3")


def in_shard(nodeid: str, index: int, total: int) -> bool:
    return zlib.crc32(nodeid.encode()) % total + 1 == index

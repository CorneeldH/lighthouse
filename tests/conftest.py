"""Run one part of the suite when `LIGHTHOUSE_PYTEST_SHARD` is set (CI splits pytest, #151).

Without the variable every test runs: locally and in the test gate nothing changes.
"""

import os

import pytest

from tests._shard import ENV, in_shard, parse_shard

SHARD = pytest.StashKey[tuple[int, int] | None]()


def pytest_configure(config):
    # Checked here, not during collection: pytest_configure runs in the main process before the
    # xdist workers start, so a bad value gives one clean error (exit 4), not crashing workers.
    try:
        config.stash[SHARD] = parse_shard(os.environ.get(ENV))
    except ValueError as err:
        raise pytest.UsageError(str(err)) from err


def pytest_collection_modifyitems(config, items):
    shard = config.stash[SHARD]
    if shard is None:
        return
    kept = [item for item in items if in_shard(item.nodeid, *shard)]
    dropped = [item for item in items if not in_shard(item.nodeid, *shard)]
    items[:] = kept
    if dropped:
        config.hook.pytest_deselected(items=dropped)

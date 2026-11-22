"""Run one part of the suite when `LIGHTHOUSE_PYTEST_SHARD` is set (CI splits pytest, #151).

Without the variable every test runs: locally and in the test gate nothing changes.
"""

import os
from pathlib import Path

import pytest

from tests._network import network_error, offline
from tests._shard import ENV, in_shard, parse_shard
from tests.pack.python._testkit import lighthouse_dir

SHARD = pytest.StashKey[tuple[int, int] | None]()


def pytest_configure(config):
    # Checked here, not during collection: pytest_configure runs in the main process before the
    # xdist workers start, so a bad value gives one clean error (exit 4), not crashing workers.
    try:
        config.stash[SHARD] = parse_shard(os.environ.get(ENV))
    except ValueError as err:
        raise pytest.UsageError(str(err)) from err


def pytest_report_header(config):
    # Under xdist the closing line lacks `N deselected`; this hook runs in the main process, so
    # the header names the part there too.
    shard = config.stash[SHARD]
    if shard is None:
        return []
    return [f"{ENV}={shard[0]}/{shard[1]}"]


def pytest_collection_modifyitems(config, items):
    shard = config.stash[SHARD]
    if shard is None:
        return
    kept = [item for item in items if in_shard(item.nodeid, *shard)]
    dropped = [item for item in items if not in_shard(item.nodeid, *shard)]
    items[:] = kept
    if dropped:
        config.hook.pytest_deselected(items=dropped)


@pytest.hookimpl(wrapper=True)
def pytest_runtest_makereport(item, call):
    # A failure whose explanation names a network error of a tool (curl, uv, go) while the
    # runner is offline is a skip with that line as the reason: lighthouse did nothing wrong
    # (#338). Every other failure stays red; tests/_network.py says which lines count.
    report = yield
    exempt = item.get_closest_marker("network_stays_red") is not None
    reason = network_error(report.longreprtext) if report.failed and not exempt else None
    if reason and offline():
        report.outcome = "skipped"
        report.longrepr = (str(item.path), item.location[1], f"Skipped: network: {reason}")
    return report


@pytest.fixture(scope="session")
def _lighthouse_build_dir() -> Path:
    """The binary is built once per process by `lighthouse_dir`; the fixture shares that build."""
    return lighthouse_dir()


@pytest.fixture
def lighthouse_bin(_lighthouse_build_dir, monkeypatch) -> Path:
    """The binary of this checkout, with its directory first on the PATH."""
    monkeypatch.setenv("PATH", f"{_lighthouse_build_dir}{os.pathsep}{os.environ.get('PATH', '')}")
    return _lighthouse_build_dir / "lighthouse"

"""Run one part of the suite when `LIGHTHOUSE_PYTEST_SHARD` is set (CI splits pytest, #151).

Without the variable every test runs: locally and in the test gate nothing changes.
"""

import os
import subprocess
from pathlib import Path

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


REPO = Path(__file__).resolve().parents[1]


@pytest.fixture(scope="session")
def _lighthouse_build_dir(tmp_path_factory) -> Path:
    """`go build` the binary once per session (once per xdist worker)."""
    out = tmp_path_factory.mktemp("lighthouse-bin")
    subprocess.run(
        ["go", "build", "-o", str(out / "lighthouse"), "./cmd/lighthouse"],
        cwd=REPO,
        check=True,
    )
    return out


@pytest.fixture
def lighthouse_bin(_lighthouse_build_dir, monkeypatch) -> Path:
    """The binary of this checkout, with its directory first on the PATH."""
    monkeypatch.setenv("PATH", f"{_lighthouse_build_dir}{os.pathsep}{os.environ.get('PATH', '')}")
    return _lighthouse_build_dir / "lighthouse"

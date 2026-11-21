"""The tests of `pack/core` run the hooks from an installed copy (see `_testkit`)."""

import os
import sys

import pytest

from tests.pack._network import network_error, offline
from tests.pack.python._testkit import INSTALL_ROOT

os.environ["PYTHONDONTWRITEBYTECODE"] = "1"
sys.dont_write_bytecode = True


@pytest.fixture(autouse=True)
def _installed_core(monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(INSTALL_ROOT))


@pytest.hookimpl(wrapper=True)
def pytest_runtest_makereport(item, call):
    # A failure whose explanation names a network error of a tool (curl, uv, go) while the
    # runner is offline is a skip with that line as the reason: lighthouse did nothing wrong
    # (#338). Every other failure stays red; tests/pack/_network.py says which lines count.
    report = yield
    exempt = item.get_closest_marker("network_stays_red") is not None
    reason = network_error(report.longreprtext) if report.failed and not exempt else None
    if reason and offline():
        report.outcome = "skipped"
        report.longrepr = (str(item.path), item.location[1], f"Skipped: network: {reason}")
    return report

"""The tests of `pack/python` run its hooks and scripts from an installation of core
plus python (`install_pack`)."""

import atexit
import os
import shutil
import sys
import tempfile
from pathlib import Path

import pytest

# The scripts of pack/python are imported by the tests: leave no __pycache__ under pack/.
os.environ["PYTHONDONTWRITEBYTECODE"] = "1"
sys.dont_write_bytecode = True

from tests.pack.python._network import network_error, offline
from tests.pack.python._testkit import install_pack

PY_ROOT = Path(tempfile.mkdtemp(prefix="lighthouse-python-"))
atexit.register(shutil.rmtree, PY_ROOT, True)
PY_HOOKS = install_pack("python", PY_ROOT)
PY_SCRIPTS = PY_ROOT / ".lighthouse" / "scripts"


@pytest.fixture(autouse=True)
def _installed_python(monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(PY_ROOT))


# A repository with a board (the source repository's `other_identity`, tests/unit/
# conftest.py:275-311): the tests of prcov_issues and the nightly point
# LIGHTHOUSE_TOML at it.
BOARD_TOML = """\
[project]
language = "en"

[forge]
kind = "github"
repo = "Other/repo"

[forge.board]
project_number = 7
project_id = "PVT_other"
status_field_id = "FIELD_other"

[forge.board.status_options]
"To do" = "opt-todo"
"In progress" = "opt-progress"
"Done" = "opt-done"
"On hold" = "opt-hold"
"Controller" = "opt-controller"
"""


@pytest.fixture
def lighthouse_toml(tmp_path, monkeypatch):
    """Write a `lighthouse.toml` (default: `BOARD_TOML`) and point LIGHTHOUSE_TOML at
    it; call the returned function again for another text."""

    def write(text: str = BOARD_TOML) -> Path:
        path = tmp_path / "lighthouse-test.toml"
        path.write_text(text, encoding="utf-8")
        monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))
        return path

    write()
    return write


@pytest.hookimpl(wrapper=True)
def pytest_runtest_makereport(item, call):
    # A failure whose explanation names a network error of a tool (curl, uv, go) while the
    # runner is offline is a skip with that line as the reason: lighthouse did nothing wrong
    # (#338). Every other failure stays red; tests/pack/python/_network.py says which lines count.
    report = yield
    exempt = item.get_closest_marker("network_stays_red") is not None
    reason = network_error(report.longreprtext) if report.failed and not exempt else None
    if reason and offline():
        report.outcome = "skipped"
        report.longrepr = (str(item.path), item.location[1], f"Skipped: network: {reason}")
    return report

"""The tests of `pack/python` run its hooks and scripts from an installation of core
plus python (`install_pack`)."""

import atexit
import shutil
import tempfile
from pathlib import Path

import pytest

from tests.pack._testkit import install_pack

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

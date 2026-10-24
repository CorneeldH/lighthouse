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

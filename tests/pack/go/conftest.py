"""The tests of `pack/go` run its hooks and scripts from an installation of core
plus go (`install_pack`)."""

import atexit
import shutil
import tempfile
from pathlib import Path

import pytest

from tests.pack._testkit import install_pack

GO_ROOT = Path(tempfile.mkdtemp(prefix="lighthouse-go-"))
atexit.register(shutil.rmtree, GO_ROOT, True)
GO_HOOKS = install_pack("go", GO_ROOT)


@pytest.fixture(autouse=True)
def _installed_go(monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(GO_ROOT))

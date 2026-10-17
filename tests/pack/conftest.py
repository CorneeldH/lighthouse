"""The tests of `pack/core` run the hooks from an installed copy (see `_testkit`)."""

import os
import sys

import pytest

from tests.pack._testkit import IMPORT_ROOT, INSTALL_ROOT

os.environ["PYTHONDONTWRITEBYTECODE"] = "1"
sys.dont_write_bytecode = True
sys.path.insert(0, str(IMPORT_ROOT))


@pytest.fixture(autouse=True)
def _installed_core(monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(INSTALL_ROOT))

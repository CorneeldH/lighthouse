"""The tests of `pack/core` run the hooks from an installed copy (see `_testkit`)."""

import os

import pytest

from tests.pack._testkit import INSTALL_ROOT

os.environ["PYTHONDONTWRITEBYTECODE"] = "1"


@pytest.fixture(autouse=True)
def _installed_core(monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(INSTALL_ROOT))

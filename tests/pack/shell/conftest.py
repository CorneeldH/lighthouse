"""The tests of `pack/shell` run its hook from an installation of core plus shell."""

import atexit
import shutil
import subprocess
import tempfile
from pathlib import Path

import pytest

from tests.pack._testkit import install_pack

SHELL_ROOT = Path(tempfile.mkdtemp(prefix="lighthouse-shell-"))
atexit.register(shutil.rmtree, SHELL_ROOT, True)
SHELL_HOOKS = install_pack("shell", SHELL_ROOT)
SHELL_SCRIPTS = SHELL_ROOT / ".lighthouse" / "scripts"
SHELLCHECK_LIB = SHELL_SCRIPTS / "lib" / "shellcheck-bin.sh"
# This checkout: the pinned shellcheck lands once in its `lighthouse-tools` cache.
CHECKOUT = Path(__file__).resolve().parents[3]


@pytest.fixture(autouse=True)
def _installed_core(monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(SHELL_ROOT))
    monkeypatch.delenv("LIGHTHOUSE_TOML", raising=False)
    monkeypatch.delenv("SHELLCHECK_BIN", raising=False)


@pytest.fixture(scope="session")
def pinned_shellcheck() -> str:
    """The pinned shellcheck, resolved once from this checkout (cache, PATH or
    download). No pin is a failure, not a skip: the gate tests would say nothing."""
    result = subprocess.run(
        ["bash", "-c", f'. "{SHELLCHECK_LIB}"\nresolve_shellcheck\n'],
        capture_output=True,
        text=True,
        cwd=str(CHECKOUT),
        check=False,
    )
    assert result.returncode == 0 and result.stderr == "", result.stderr
    return result.stdout.strip()

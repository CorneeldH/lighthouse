"""`pack/core/lib/no_verify.py`, as installed: the lib runs as a file on its path.

The cases for the flag itself run against the commit-tests hook (`test_commit_tests_no_verify.py`).
"""

import subprocess
import sys

import pytest

from tests.pack._testkit import INSTALLED_LIB


@pytest.mark.parametrize(
    "command,rc",
    [
        ("git commit -n -m x", 2),
        ("git commit --no-verify -m x", 2),
        ("git commit -m x", 0),
        ("git log -n 1", 0),
    ],
)
def test_no_verify_runs_as_file(tmp_path, command, rc):
    result = subprocess.run(
        [sys.executable, str(INSTALLED_LIB / "no_verify.py")],
        input=command,
        capture_output=True,
        text=True,
        cwd=tmp_path,
        check=False,
    )
    assert result.returncode == rc, result.stderr
    assert "Traceback" not in result.stderr


def test_no_verify_imports_as_package():
    """`lighthouse_lib.no_verify` imports its parser from the package, not as a sibling."""
    import importlib

    module = importlib.import_module("lighthouse_lib.no_verify")
    assert module.command_has_no_verify("git commit -n -m x")

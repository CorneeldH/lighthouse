"""The failure paths of ci/no-python.sh (#285): a registration without a fixture, and a `python3`
in the initialised repo. The green path runs in CI, in the `no-python` job."""

import os
import shutil
import subprocess
from pathlib import Path

from tests.pack._testkit import REPO, lighthouse_dir

SCRIPT = REPO / "ci" / "no-python.sh"
FIXTURES = REPO / "tests" / "pack" / "fixtures" / "hooks"


def _run(work: Path, **env: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["bash", str(SCRIPT), str(lighthouse_dir() / "lighthouse"), str(work)],
        capture_output=True, text=True, check=False, env={**os.environ, **env},
    )


def test_missing_fixture_fails(tmp_path):
    fixtures = tmp_path / "fixtures"
    shutil.copytree(FIXTURES, fixtures)
    (fixtures / "Stop" / "go-lint-gate.json").unlink()
    work = tmp_path / "work"
    work.mkdir()

    result = _run(work, NO_PYTHON_FIXTURES=str(fixtures))

    assert result.returncode == 1, result.stderr
    assert f"no fixture {fixtures}/Stop/go-lint-gate.json" in result.stderr


def test_python3_hit_fails(tmp_path):
    extra = tmp_path / "extra.txt"
    extra.write_text("one\nexec python3 -c pass\n")
    work = tmp_path / "work"
    work.mkdir()

    result = _run(work, NO_PYTHON_EXTRA=str(extra))

    assert result.returncode == 1, result.stderr
    assert f"python3 in {work}/repo/extra.txt:2" in result.stderr

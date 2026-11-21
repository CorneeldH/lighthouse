"""One canary per blocking reason of the python pack.

Ruff is a ceiling: the hook blocks on `BLOCKING_RULES` and on nothing else, so the list in
`ruff-format-check.sh` is the contract. Every code on it gets a minimal violation under
`testdata/ruff-format-check/<code>.py` that the full hook must block, with that code and no other
code of the list in the message. `ty-gate.sh` declares its reasons the same way, and each one gets
a fixture under `testdata/ty-gate/<reason>.py`.

The fixtures are copied to a neutral name (`canary.py`) in a temporary directory, so the code
cannot match on the path.
"""

import json
import re
import shutil
import subprocess
from pathlib import Path

import pytest

from tests.pack.python.conftest import PY_HOOKS
from tests.pack.python.test_ty_gate import _fake_repo, _run as _run_ty

pytestmark = pytest.mark.usefixtures("lighthouse_bin")

TESTDATA = Path(__file__).resolve().parent / "testdata"
BASELINE = (
    Path(__file__).resolve().parents[3] / "pack/python/scripts/quality/baseline.py"
)


def _declared(hook: str) -> list[str]:
    """The codes in the one `BLOCKING_RULES="..."` line of a hook."""
    lines = re.findall(
        r'^BLOCKING_RULES="([^"]*)"$', (PY_HOOKS / hook).read_text(), re.MULTILINE
    )
    assert len(lines) == 1, f"{hook}: expected one BLOCKING_RULES line, got {lines}"
    return lines[0].split(",")


RUFF_RULES = _declared("ruff-format-check.sh")
TY_REASONS = _declared("ty-gate.sh")


def _fixtures(directory: str) -> set[str]:
    return {p.stem for p in (TESTDATA / directory).glob("*.py")}


def test_ruff_rules_have_a_file_each():
    assert _fixtures("ruff-format-check") == set(RUFF_RULES)


def test_ty_reasons_have_a_file_each():
    assert _fixtures("ty-gate") == set(TY_REASONS)


@pytest.mark.parametrize("code", RUFF_RULES)
def test_every_ruff_rule_blocks(code, tmp_path):
    target = tmp_path / "canary.py"
    shutil.copy(TESTDATA / "ruff-format-check" / f"{code}.py", target)

    result = subprocess.run(
        [str(PY_HOOKS / "ruff-format-check.sh")],
        input=json.dumps({"tool_input": {"file_path": str(target)}}),
        capture_output=True,
        text=True,
        cwd=str(tmp_path),
        check=False,
    )

    assert result.returncode == 2, result.stdout + result.stderr
    assert f": {code} " in result.stderr, result.stderr
    others = [c for c in RUFF_RULES if c != code and f": {c} " in result.stderr]
    assert others == [], f"the fixture of {code} also triggers {others}"


@pytest.mark.parametrize("reason", TY_REASONS)
def test_every_ty_reason_blocks(reason, tmp_path):
    root = _fake_repo(tmp_path)
    shutil.copy(TESTDATA / "ty-gate" / f"{reason}.py", root / "src" / "canary.py")

    result = _run_ty(root)

    assert result.returncode == 2, result.stdout + result.stderr
    first = result.stderr.splitlines()[0]
    assert first.startswith(f"[ty-gate/{reason}] "), result.stderr
    others = [r for r in TY_REASONS if r != reason and f"[ty-gate/{r}]" in result.stderr]
    assert others == [], f"the fixture of {reason} also triggers {others}"


def test_baseline_py_lists_the_same_ruff_rules():
    block = re.search(r"ZERO_RULES[^=]*=\s*\((.*?)\)", BASELINE.read_text(), re.DOTALL)
    assert block is not None
    assert re.findall(r'"([A-Z]+\d+)"', block.group(1)) == RUFF_RULES

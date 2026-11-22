"""The package files of `pack/python` that `init` reads: detection, the fragment and the
instruction block (plan M0 (c), Task 8)."""

import tomllib
from pathlib import Path

PY = Path(__file__).resolve().parents[3] / "pack" / "python"


def test_detect_on_pyproject():
    assert tomllib.loads((PY / "detect.toml").read_text())["files"] == ["pyproject.toml"]


def test_fragment_is_the_default_test_gate():
    fragment = tomllib.loads((PY / "lighthouse.toml.fragment").read_text())
    assert fragment == {"test_gate": {"command": "uv run pytest -q", "unchecked_exit_codes": [3, 4, 5]}}


def test_instruction_block_has_its_header_and_the_imports_check():
    text = (PY / "instructions" / "AGENTS.md").read_text()
    assert text.startswith("## Lighthouse: python\n")
    assert "lighthouse imports check" in text

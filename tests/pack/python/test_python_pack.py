"""The package files of `pack/python` that `init` reads: detection, the fragment and the
instruction block (plan M0 (c), Task 8)."""

import re
import tomllib
from pathlib import Path

PY = Path(__file__).resolve().parents[3] / "pack" / "python"


def test_detect_on_pyproject():
    assert tomllib.loads((PY / "detect.toml").read_text())["files"] == ["pyproject.toml"]


def test_fragment_is_the_default_test_gate():
    fragment = tomllib.loads((PY / "lighthouse.toml.fragment").read_text())
    assert fragment == {"test_gate": {"command": "uv run pytest -q", "unchecked_exit_codes": [3, 4, 5]}}


def test_instruction_block_is_short_and_names_skill_gates_and_ratchet():
    text = (PY / "instructions" / "AGENTS.md").read_text()
    assert text.startswith("## Lighthouse: python\n")
    assert len(re.findall(r"\S+", text)) <= 80
    for needle in ("/lighthouse-python-standard", "ruff-format-check", "ty-gate", ".lighthouse/scripts/quality/ratchet.py"):
        assert needle in text, needle


def test_the_ratchet_line_applies_only_with_a_baseline():
    """A fresh `init` has no `quality-baseline.toml`; the block must not send every
    new repository into a command that stops on it."""
    text = (PY / "instructions" / "AGENTS.md").read_text()
    ratchet_line = next(line for line in text.splitlines() if "ratchet.py" in line)
    assert "quality-baseline.toml" in ratchet_line

"""The package files of `pack/go` that `init` reads: detection and the instruction block."""

import tomllib
from pathlib import Path

GO = Path(__file__).resolve().parents[3] / "pack" / "go"


def test_detect_on_go_mod():
    assert tomllib.loads((GO / "detect.toml").read_text())["files"] == ["go.mod"]


def test_no_fragment():
    assert not (GO / "lighthouse.toml.fragment").exists()


def test_instruction_block_has_its_header_and_the_imports_check():
    text = (GO / "instructions" / "AGENTS.md").read_text()
    assert text.startswith("## Lighthouse: go\n")
    assert "lighthouse imports check" in text
    assert "go vet" not in text

"""The package files of `pack/node` that `init` reads: detection, and no hooks or instructions."""

import tomllib
from pathlib import Path

NODE = Path(__file__).resolve().parents[3] / "pack" / "node"


def test_detect_on_package_json():
    assert tomllib.loads((NODE / "detect.toml").read_text())["files"] == ["**/package.json"]


def test_no_fragment_hooks_or_instructions():
    for name in ("lighthouse.toml.fragment", "hooks", "instructions"):
        assert not (NODE / name).exists(), name

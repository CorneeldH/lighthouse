"""The package files of `pack/node` that `init` reads: detection, the imports-gate hook and its instruction line."""

import json
import tomllib
from pathlib import Path

NODE = Path(__file__).resolve().parents[3] / "pack" / "node"


def test_detect_on_package_json():
    assert tomllib.loads((NODE / "detect.toml").read_text())["files"] == ["**/package.json"]


def test_no_fragment():
    assert not (NODE / "lighthouse.toml.fragment").exists()


def test_imports_gate_hook_and_instructions():
    hooks = json.loads((NODE / "hooks" / "hooks.json").read_text())["hooks"]
    assert list(hooks) == ["Stop"]
    (entry,) = hooks["Stop"]
    (hook,) = entry["hooks"]
    assert hook["command"] == "${CLAUDE_PLUGIN_ROOT}/hooks/node-imports-gate.sh"
    assert hook["timeout"] == 120
    assert (NODE / "hooks" / "node-imports-gate.sh").exists()
    assert "lighthouse imports check" in (NODE / "instructions" / "AGENTS.md").read_text()


def test_requires_npx_for_imports_check():
    assert tomllib.loads((NODE / "requires.toml").read_text())["npx"] == ["lighthouse imports check"]

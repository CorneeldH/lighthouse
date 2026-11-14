"""The language packs go, node, shell and expo need no Python: their requires.toml names neither
python3 nor tomllib (#284, spec zonder Python §5)."""

import tomllib
from pathlib import Path

import pytest

PACK = Path(__file__).resolve().parents[2] / "pack"


@pytest.mark.parametrize("pack", ["go", "node", "shell", "expo"])
def test_language_pack_requires_no_python(pack):
    requires = tomllib.loads((PACK / pack / "requires.toml").read_text(encoding="utf-8"))
    assert "python3" not in requires, f"{pack}: python3 = {requires.get('python3')}"
    assert "tomllib" not in requires, f"{pack}: tomllib = {requires.get('tomllib')}"

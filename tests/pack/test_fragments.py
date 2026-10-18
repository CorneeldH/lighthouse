"""Every `pack/<pakket>/lighthouse.toml.fragment` is config the config reader accepts
(plan M0 (c), task 4): `init` copies its keys into `lighthouse.toml` as they are, so a
wrong type or an unknown key would break every hook that reads the config."""

from pathlib import Path

import pytest

from lighthouse_lib import config as cfgmod

PACK = Path(__file__).resolve().parents[2] / "pack"
FRAGMENTS = sorted(PACK.glob("*/lighthouse.toml.fragment"))
BASE = '[project]\nlanguage = "en"\n\n'


def _keys(text: str) -> list[str]:
    keys, table = [], ""
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("["):
            table = line.strip("[]")
        else:
            keys.append(table + "." + line.split("=", 1)[0].strip())
    return keys


def _check(text: str, tmp_path: Path) -> None:
    path = tmp_path / "lighthouse.toml"
    path.write_text(BASE + text)
    cfgmod.load(path)
    unknown = [k for k in _keys(text) if not cfgmod.is_known_key(k)]
    assert not unknown, f"unknown keys: {unknown}"


@pytest.mark.parametrize(
    "fragment",
    FRAGMENTS or [pytest.param(None, marks=pytest.mark.skip(reason="no package has a fragment yet"))],
    ids=lambda p: p.parent.name if p else "none",
)
def test_fragment_is_valid_config(fragment, tmp_path):
    _check(fragment.read_text(), tmp_path)


def test_check_catches_a_wrong_type(tmp_path):
    with pytest.raises(cfgmod.ConfigError):
        _check('[test_gate]\nunchecked_exit_codes = ["3"]\n', tmp_path)


def test_check_catches_an_unknown_key(tmp_path):
    with pytest.raises(AssertionError, match="test_gate.nope"):
        _check('[test_gate]\nnope = 1\n', tmp_path)


def test_check_accepts_the_python_default(tmp_path):
    _check('[test_gate]\ncommand = "uv run pytest -q"\nunchecked_exit_codes = [3, 4, 5]\n', tmp_path)

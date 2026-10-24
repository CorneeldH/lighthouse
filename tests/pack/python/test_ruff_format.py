"""De drie kwaliteitshooks: blokkeren ze wat ze moeten blokkeren, en laten ze de rest door?

De hooks zijn bash en lezen hun input als JSON op stdin. Deze tests draaien ze
als subprocess met een echte tijdelijke file, zodat het contract (exit code +
stderr) getest wordt en niet een herimplementatie ervan.
"""

import json
import os
import re
import shutil
import subprocess
import sys
import tomllib
from pathlib import Path

import pytest

HOOKS = Path(__file__).resolve().parents[2] / ".claude" / "hooks"
RUFF_HOOK = HOOKS / "ruff-format.sh"
TY_HOOK = HOOKS / "ty-check.sh"
PYPROJECT = Path(__file__).resolve().parents[2] / "pyproject.toml"


def _run(hook: Path, payload: dict, cwd: Path) -> subprocess.CompletedProcess:
    return subprocess.run(
        [str(hook)],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(cwd),
        check=False,
    )


def _run_offline(hook: Path, payload: dict, cwd: Path) -> subprocess.CompletedProcess:
    """Zoals `_run`, maar met een lege uv-cache en netwerk uit — simuleert een
    gebroken omgeving (issue #591 finding 1: hooks moeten dan fail-open zijn).
    """
    empty_cache = cwd / "empty-uv-cache"
    empty_cache.mkdir(exist_ok=True)
    env = dict(os.environ, UV_OFFLINE="1", UV_CACHE_DIR=str(empty_cache))
    return subprocess.run(
        [str(hook)],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(cwd),
        env=env,
        check=False,
    )


def test_ruff_hook_passes_clean_file(tmp_path):
    target = tmp_path / "clean.py"
    target.write_text('"""Docstring."""\n\n\ndef f() -> int:\n    return 1\n')
    result = _run(RUFF_HOOK, {"tool_input": {"file_path": str(target)}}, tmp_path)
    assert result.returncode == 0, result.stderr


def test_ruff_hook_blocks_bare_except(tmp_path):
    target = tmp_path / "bare.py"
    target.write_text("def f():\n    try:\n        pass\n    except:\n        pass\n")
    result = _run(RUFF_HOOK, {"tool_input": {"file_path": str(target)}}, tmp_path)
    assert result.returncode == 2, result.stdout
    assert "E722" in result.stderr


def test_ruff_hook_blocks_blanket_type_ignore(tmp_path):
    target = tmp_path / "blanket.py"
    target.write_text("x: int = 1  # type: ignore\n")
    result = _run(RUFF_HOOK, {"tool_input": {"file_path": str(target)}}, tmp_path)
    assert result.returncode == 2, result.stdout
    assert "PGH003" in result.stderr


def test_ruff_hook_ignores_non_python(tmp_path):
    target = tmp_path / "notes.md"
    target.write_text("# hi\n")
    result = _run(RUFF_HOOK, {"tool_input": {"file_path": str(target)}}, tmp_path)
    assert result.returncode == 0


def test_ruff_hook_does_not_block_on_unselected_rule(tmp_path):
    """DTZ005 staat op 273+ hits in de repo; blokkeren erop legt elke edit stil."""
    target = tmp_path / "dtz.py"
    target.write_text("from datetime import datetime\n\nx = datetime.now()\n")
    result = _run(RUFF_HOOK, {"tool_input": {"file_path": str(target)}}, tmp_path)
    assert result.returncode == 0, result.stderr


def test_ruff_hook_fails_open_when_tool_unresolvable(tmp_path):
    """Offline + lege cache mag niet als 'de code is fout' overkomen — fail-open."""
    target = tmp_path / "clean.py"
    target.write_text('"""Docstring."""\n\n\ndef f() -> int:\n    return 1\n')
    result = _run_offline(
        RUFF_HOOK, {"tool_input": {"file_path": str(target)}}, tmp_path
    )
    assert result.returncode == 0, result.stderr
    assert "Lint-gate" not in result.stderr


def _hook_blocking_rules() -> set[str]:
    match = re.search(r'^BLOCKING_RULES="([^"]+)"', RUFF_HOOK.read_text(), re.MULTILINE)
    assert match, "BLOCKING_RULES niet gevonden in ruff-format.sh"
    return set(match.group(1).split(","))


def test_blocking_rules_are_selected_in_pyproject():
    """De hook mag alleen blokkeren op regels die de config ook echt selecteert.

    Deelverzameling, geen gelijkheid: #562 breidt `extend-select` uit met zeven
    TELLENDE regels (163 bestaande hits). Die horen wel in de lint-selectie en
    niet in de blokkerende set — een kale `ruff check` in de hook zou daarmee
    elke edit in een legacy-bestand stilleggen.
    """
    data = tomllib.loads(PYPROJECT.read_text())
    selected = set(data["tool"]["ruff"]["lint"]["extend-select"])
    assert _hook_blocking_rules() <= selected


def test_pgh_rules_are_selected():
    """PGH003/PGH004 zitten NIET in ruff's defaults (gemeten met ruff 0.16.4).

    Zonder deze twee in extend-select bewaakt alleen de hook de kale-suppressie-
    route; een handmatige `uv run ruff check` en de IDE zien hem niet.
    """
    data = tomllib.loads(PYPROJECT.read_text())
    selected = set(data["tool"]["ruff"]["lint"]["extend-select"])
    assert {"PGH003", "PGH004"} <= selected


def test_ty_pin_matches_hook():
    """De ty-pin staat op twee plekken; ze mogen niet uit elkaar lopen."""
    data = tomllib.loads(PYPROJECT.read_text())
    pins = [d for d in data["dependency-groups"]["dev"] if d.startswith("ty==")]
    assert len(pins) == 1, pins
    pyproject_version = pins[0].split("==", 1)[1]

    match = re.search(r'^TY_VERSION="([^"]+)"', TY_HOOK.read_text(), re.MULTILINE)
    assert match, "TY_VERSION niet gevonden in ty-check.sh"
    assert match.group(1) == pyproject_version


RUFF_VERSION_FILE = Path(__file__).resolve().parents[2] / ".ruff-version"


def test_the_ruff_pin_has_exactly_one_source():
    """De ruff-versie staat in .ruff-version en nergens anders hardcoded.

    Twee plekken betekent drift, en drift in een teller betekent een rode CI op
    een PR die niets veranderde. De hook leest het bestand; er staat geen
    versienummer in het script zelf.
    """
    pin = RUFF_VERSION_FILE.read_text(encoding="utf-8").strip()
    assert re.fullmatch(r"\d+\.\d+\.\d+", pin), pin

    hook = RUFF_HOOK.read_text()
    assert ".ruff-version" in hook, "de hook leest de pin niet uit .ruff-version"
    assert f"ruff@{pin}" not in hook, (
        "de versie staat hardcoded in ruff-format.sh naast .ruff-version — dat is "
        "precies de drift die één bron moest voorkomen"
    )

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




def test_ty_hook_exits_zero_when_stop_hook_already_active():
    """Zonder deze guard blokkeert de hook zichzelf in een lus."""
    result = subprocess.run(
        [str(TY_HOOK)],
        input=json.dumps({"stop_hook_active": True, "cwd": str(HOOKS.parent.parent)}),
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0, result.stderr


def test_ty_hook_is_green_on_this_repo():
    """De hele reden van Task 8: src/ staat op nul, dus de Stop-hook is stil."""
    root = HOOKS.parent.parent
    result = subprocess.run(
        [str(TY_HOOK)],
        input=json.dumps({"stop_hook_active": False, "cwd": str(root)}),
        capture_output=True,
        text=True,
        cwd=str(root),
        check=False,
    )
    assert result.returncode == 0, result.stderr


def test_ty_hook_fails_open_when_tool_unresolvable(tmp_path):
    """Offline + lege cache mag niet als 'de code is fout' overkomen — fail-open."""
    root = HOOKS.parent.parent
    empty_cache = tmp_path / "empty-uv-cache"
    empty_cache.mkdir()
    env = dict(os.environ, UV_OFFLINE="1", UV_CACHE_DIR=str(empty_cache))
    result = subprocess.run(
        [str(TY_HOOK)],
        input=json.dumps({"stop_hook_active": False, "cwd": str(root)}),
        capture_output=True,
        text=True,
        cwd=str(root),
        env=env,
        check=False,
    )
    assert result.returncode == 0, result.stderr
    assert "Type-gate" not in result.stderr


def _fake_repo(tmp_path: Path) -> Path:
    """Een minimale repo waar de ty-hook op wil draaien: git-root, src/ en .venv.

    De venv gebruikt bewust de interpreter van de testrun (`sys.executable`), zodat
    `uv venv` nooit een Python hoeft te downloaden — unit-tests mogen het net niet op.
    """
    (tmp_path / "src").mkdir()
    subprocess.run(["git", "init", "-q"], cwd=tmp_path, check=True)
    subprocess.run(
        ["uv", "venv", "--python", sys.executable, str(tmp_path / ".venv")],
        cwd=tmp_path,
        capture_output=True,
        check=True,
    )
    return tmp_path


def _run_ty_hook(root: Path) -> subprocess.CompletedProcess:
    return subprocess.run(
        [str(TY_HOOK)],
        input=json.dumps({"stop_hook_active": False, "cwd": str(root)}),
        capture_output=True,
        text=True,
        cwd=str(root),
        check=False,
    )


def test_ty_hook_blames_the_venv_when_only_imports_are_unresolved(tmp_path):
    """Een venv die achterloopt op uv.lock is geen codefout.

    Gemeten op 2026-08-23: met `markdownify` gedeïnstalleerd meldde ty één
    `unresolved-import` in src/capture_api/html_markdown.py, terwijl het pakket
    gewoon in [project].dependencies én uv.lock staat. De hook zei toen "Los ze
    op in de code" — precies het verkeerde spoor.
    """
    root = _fake_repo(tmp_path)
    (root / "src" / "only_import.py").write_text(
        "import definitely_not_a_real_package_xyz\n\n"
        "print(definitely_not_a_real_package_xyz)\n"
    )

    result = _run_ty_hook(root)

    assert result.returncode == 2, result.stdout
    assert "uv sync" in result.stderr
    assert "pyproject.toml" in result.stderr
    assert "Los ze op in de code" not in result.stderr
    # De diagnostics zelf blijven zichtbaar, anders is de melding niet na te lopen.
    assert "unresolved-import" in result.stderr


def test_ty_hook_blocks_on_code_when_diagnostics_are_mixed(tmp_path):
    """Een echte typefout naast een onvindbare import blijft de volle blokkade.

    De tegenkracht: `unresolved-import` is óók het signaal dat iemand een import
    toevoegde zonder de dependency te declareren. Alleen een set die *uitsluitend*
    uit importfouten bestaat mag de venv-tekst krijgen.
    """
    root = _fake_repo(tmp_path)
    (root / "src" / "only_import.py").write_text(
        "import definitely_not_a_real_package_xyz\n\n"
        "print(definitely_not_a_real_package_xyz)\n"
    )
    (root / "src" / "real_error.py").write_text('x: int = "not an int"\n')

    result = _run_ty_hook(root)

    assert result.returncode == 2, result.stdout
    assert "Los ze op in de code" in result.stderr
    assert "uv sync" not in result.stderr
    assert "invalid-assignment" in result.stderr


def test_ty_hook_stays_silent_on_a_clean_fake_repo(tmp_path):
    """Controle op de fixture zelf: zonder fouten zegt de hook niets."""
    root = _fake_repo(tmp_path)
    (root / "src" / "fine.py").write_text('"""Docstring."""\n\n\nX: int = 1\n')

    result = _run_ty_hook(root)

    assert result.returncode == 0, result.stderr
def test_every_hook_script_is_registered_in_settings():
    """De pairing-regel op zijn smalst: een hook die nergens geregistreerd staat,
    draait niet en meldt dus niets — precies de stille nul uit #650.

    `statusline.sh` (#669) is uitgezonderd: het is geen PreToolUse/PostToolUse/Stop-
    hook maar een statusLine-renderer, en die wordt niet onder `hooks` in dít
    repo-bestand geregistreerd maar als `statusLine` in het gebruikersbestand
    `~/.claude/settings.json` (Task 8 van
    docs/developer/planning/plans/2026-08-23-usage-window-fase1.md). Deze test kan
    dat gebruikersbestand niet lezen, dus zou voor `statusline.sh` per ontwerp nooit
    kunnen slagen.
    """
    settings = json.loads((REPO / ".claude" / "settings.json").read_text())
    registered = json.dumps(settings["hooks"])
    exempt = {"statusline.sh"}
    for script in sorted(HOOKS.glob("*.sh")):
        if script.name in exempt:
            continue
        assert script.name in registered, f"{script.name} staat in geen enkele hook"

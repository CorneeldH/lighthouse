"""The Stop hook `ty-check.sh` of `pack/python`: it runs `ty` on the directories in
`python.source_dirs`, blocks once per turn on diagnostics, and says so — without
blocking — when it could not check (ontwerpkeuze 13).

From the source repository's `test_quality_hooks.py:166-292` and 741-759. Left out
there: `test_ty_pin_matches_hook` (the source repository's `pyproject.toml`) and
`test_ty_hook_is_green_on_this_repo` (its own tree). The fail-open test is turned
around: an unresolvable ty is a visible COULD NOT CHECK now.
"""

import json
import os
import re
import subprocess
import sys
from pathlib import Path

import pytest

from tests.pack._testkit import MINIMAL_TOML, install_pack, tool_bin

PACK = Path(__file__).resolve().parents[3] / "pack" / "python"
UNCHECKED = "ty-check: COULD NOT CHECK — "
NO_DIR = (
    "ty-check: not checked — no directory from python.source_dirs exists "
    "(set it in lighthouse.toml; [] turns the check off)"
)


def _fake_repo(tmp_path: Path, python: str = "", src: bool = True, venv: bool = True) -> Path:
    """A repository with core and python installed, `src/` and a `.venv`.

    The venv uses the interpreter of the test run (`sys.executable`), so `uv venv`
    never downloads a Python.
    """
    root = tmp_path / "repo"
    root.mkdir()
    subprocess.run(["git", "init", "-q"], cwd=root, check=True)
    toml = MINIMAL_TOML + (f"\n[python]\n{python}\n" if python else "")
    install_pack("python", root, toml)
    if src:
        (root / "src").mkdir()
    if venv:
        subprocess.run(
            ["uv", "venv", "--python", sys.executable, str(root / ".venv")],
            cwd=root,
            capture_output=True,
            check=True,
        )
    return root


def _run(
    root: Path, payload: dict | str | None = None, env: dict | None = None
) -> subprocess.CompletedProcess:
    if payload is None:
        payload = {"stop_hook_active": False, "cwd": str(root)}
    full_env = dict(os.environ, LIGHTHOUSE_REPO_ROOT=str(root), CLAUDE_PROJECT_DIR=str(root))
    full_env.update(env or {})
    return subprocess.run(
        [str(root / ".claude/hooks/lighthouse/ty-check.sh")],
        input=payload if isinstance(payload, str) else json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(root),
        env=full_env,
        check=False,
    )


def _message(result: subprocess.CompletedProcess) -> str:
    assert result.returncode == 0, result.stderr
    return json.loads(result.stdout)["systemMessage"]


ONLY_IMPORT = (
    "import definitely_not_a_real_package_xyz\n\nprint(definitely_not_a_real_package_xyz)\n"
)


def test_ty_hook_exits_zero_when_stop_hook_already_active(tmp_path):
    """Without this guard the hook blocks itself in a loop."""
    root = _fake_repo(tmp_path)
    (root / "src" / "bad.py").write_text('x: int = "not an int"\n')
    result = _run(root, {"stop_hook_active": True, "cwd": str(root)})
    assert (result.returncode, result.stdout, result.stderr) == (0, "", "")


def test_ty_hook_blames_the_venv_when_only_imports_are_unresolved(tmp_path):
    """A venv behind on uv.lock is not a code error."""
    root = _fake_repo(tmp_path)
    (root / "src" / "only_import.py").write_text(ONLY_IMPORT)
    result = _run(root)
    assert result.returncode == 2, result.stdout
    assert result.stderr.startswith("ty-check: "), result.stderr
    assert "uv sync" in result.stderr
    assert "pyproject.toml" in result.stderr
    assert "Fix them in the code" not in result.stderr
    # The diagnostics stay visible, or the message cannot be traced.
    assert "unresolved-import" in result.stderr


def test_ty_hook_blocks_on_code_when_diagnostics_are_mixed(tmp_path):
    """A real type error next to an unresolvable import stays the full block."""
    root = _fake_repo(tmp_path)
    (root / "src" / "only_import.py").write_text(ONLY_IMPORT)
    (root / "src" / "real_error.py").write_text('x: int = "not an int"\n')
    result = _run(root)
    assert result.returncode == 2, result.stdout
    assert result.stderr.startswith("ty-check: "), result.stderr
    assert "Fix them in the code" in result.stderr
    assert "uv sync" not in result.stderr
    assert "invalid-assignment" in result.stderr


def test_ty_hook_stays_silent_on_a_clean_fake_repo(tmp_path):
    root = _fake_repo(tmp_path)
    (root / "src" / "fine.py").write_text('"""Docstring."""\n\n\nX: int = 1\n')
    result = _run(root)
    assert (result.returncode, result.stdout, result.stderr) == (0, "", ""), result.stderr


def test_ty_reads_source_dirs(tmp_path):
    root = _fake_repo(tmp_path, 'source_dirs = ["lib"]', src=False)
    (root / "lib").mkdir()
    (root / "lib" / "real_error.py").write_text('x: int = "not an int"\n')
    result = _run(root)
    assert result.returncode == 2, result.stdout
    assert "invalid-assignment" in result.stderr
    assert "lib" in result.stderr.splitlines()[0]


def test_ty_skips_a_dir_outside_source_dirs(tmp_path):
    root = _fake_repo(tmp_path, 'source_dirs = ["lib"]')
    (root / "lib").mkdir()
    (root / "src" / "real_error.py").write_text('x: int = "not an int"\n')
    result = _run(root)
    assert (result.returncode, result.stdout, result.stderr) == (0, "", ""), result.stderr


def test_ty_no_source_dir_is_loud(tmp_path):
    root = _fake_repo(tmp_path, src=False)
    assert _message(_run(root)) == NO_DIR


def test_ty_empty_source_dirs_is_silent(tmp_path):
    root = _fake_repo(tmp_path, "source_dirs = []", src=False, venv=False)
    result = _run(root)
    assert (result.returncode, result.stdout, result.stderr) == (0, "", "")


def test_ty_unchecked_when_tool_unresolvable(tmp_path):
    """Offline with an empty cache: a message, not a block, and not silence."""
    root = _fake_repo(tmp_path)
    (root / "src" / "fine.py").write_text("X: int = 1\n")
    empty_cache = tmp_path / "empty-uv-cache"
    empty_cache.mkdir()
    result = _run(root, env={"UV_OFFLINE": "1", "UV_CACHE_DIR": str(empty_cache)})
    assert _message(result).startswith(UNCHECKED + "ty@0.0.72 does not start")


def test_ty_unchecked_without_venv(tmp_path):
    root = _fake_repo(tmp_path, venv=False)
    other = tmp_path / "elsewhere"
    other.mkdir()
    message = _message(_run(root, env={"CLAUDE_PROJECT_DIR": str(other)}))
    assert message.startswith(UNCHECKED + "no .venv"), message


@pytest.mark.parametrize("tool", ["jq", "uvx"])
def test_ty_unchecked_without_tool(tmp_path, tool):
    root = _fake_repo(tmp_path)
    bin_dir = tool_bin(
        tmp_path / "bin", ("bash", "jq", "git", "uvx", "python3", "cat", "sort", "sed"), drop=tool
    )
    message = _message(_run(root, env={"PATH": str(bin_dir)}))
    assert message.startswith(UNCHECKED + f"{tool} is missing"), message


def test_ty_unchecked_on_invalid_json(tmp_path):
    root = _fake_repo(tmp_path)
    assert _message(_run(root, "{not json")).startswith(UNCHECKED + "the payload is not valid JSON")


def test_ty_unchecked_on_broken_config(tmp_path):
    root = _fake_repo(tmp_path, 'source_dirs = "src"')
    assert _message(_run(root)).startswith(UNCHECKED + "the configuration cannot be read")


def test_ty_unchecked_outside_a_repository(tmp_path):
    root = _fake_repo(tmp_path)
    outside = tmp_path / "outside"
    outside.mkdir()
    payload = {"stop_hook_active": False, "cwd": str(outside)}
    result = _run(root, payload, env={"CLAUDE_PROJECT_DIR": str(outside)})
    assert _message(result).startswith(UNCHECKED + "not inside a git repository")


def test_skill_uses_the_hook_pin():
    pin = re.search(r'^TY_VERSION="([^"]+)"', (PACK / "hooks/ty-check.sh").read_text(), re.M)
    assert pin, "TY_VERSION not found in ty-check.sh"
    for variant in ("claude", "agents"):
        text = (PACK / f"skills/python-writing/{variant}/SKILL.md").read_text()
        assert re.findall(r"ty@([\d.]+)", text) and set(re.findall(r"ty@([\d.]+)", text)) == {
            pin.group(1)
        }, variant


def test_registered_at_stop():
    registered = json.loads((PACK / "hooks/hooks.json").read_text())
    entries = [
        (block.get("matcher"), h["command"], h["timeout"])
        for block in registered["hooks"]["Stop"]
        for h in block["hooks"]
    ]
    assert entries == [("", "${CLAUDE_PLUGIN_ROOT}/hooks/ty-check.sh", 60)]


def test_every_hook_script_is_registered():
    """A hook registered nowhere does not run and so reports nothing."""
    registered = (PACK / "hooks/hooks.json").read_text()
    for script in sorted((PACK / "hooks").glob("*.sh")):
        assert f"/hooks/{script.name}" in registered, f"{script.name} is in no hook"


def test_ty_unchecked_on_a_broken_tool_config(tmp_path):
    """ty exit 2 (an invalid `[tool.ty]`) is no type error: a message, not a block."""
    root = _fake_repo(tmp_path)
    (root / "src" / "fine.py").write_text("X: int = 1\n")
    (root / "pyproject.toml").write_text('[project]\nname = "x"\n\n[tool.ty.rules]\nnot-a-rule = 3\n')
    message = _message(_run(root))
    assert message.startswith("ty-check: COULD NOT CHECK — ty exited 2"), message

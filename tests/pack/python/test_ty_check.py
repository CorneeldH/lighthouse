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
from datetime import timedelta
from pathlib import Path

import pytest

from tests.pack._testkit import (
    CORE,
    MINIMAL_TOML,
    alarm_line,
    git,
    install_pack,
    prompt_entry,
    result_entry,
    stop_payload,
    tool_bin,
    tool_entry,
    write_alarm,
    write_transcript,
)

PACK = Path(__file__).resolve().parents[3] / "pack" / "python"
UNCHECKED = "ty-check: COULD NOT CHECK — "
UNDETERMINED = UNCHECKED + "the checkout of this turn cannot be determined: "

# The real uv cache, fixed once at import: `_run` moves XDG_CACHE_HOME, which uv
# would follow, and ty would be downloaded again for every test.
UV_CACHE = os.environ.get("UV_CACHE_DIR") or subprocess.run(
    ["uv", "cache", "dir"], capture_output=True, text=True, check=True
).stdout.strip()


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
    root: Path,
    payload: dict | str | None = None,
    env: dict | None = None,
    hook: Path | None = None,
) -> subprocess.CompletedProcess:
    if payload is None:
        # A turn without tool calls: the hook falls back to the payload `cwd`.
        transcript = write_transcript(root.parent / "transcript.jsonl", [prompt_entry(root)])
        payload = stop_payload(root, transcript)
    full_env = dict(
        os.environ,
        LIGHTHOUSE_REPO_ROOT=str(root),
        CLAUDE_PROJECT_DIR=str(root),
        # Never the real alarm of the machine running the tests.
        XDG_CACHE_HOME=str(root.parent / "xdg-cache"),
        UV_CACHE_DIR=UV_CACHE,
    )
    full_env.update(env or {})
    return subprocess.run(
        [str(hook or root / ".claude/hooks/lighthouse/ty-check.sh")],
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
    assert _message(_run(root)) == (
        "ty-check: not checked — no directory from python.source_dirs exists in "
        f"{os.path.realpath(root)} (set it in lighthouse.toml; [] turns the check off)"
    )


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
        tmp_path / "bin",
        ("bash", "jq", "git", "uvx", "python3", "cat", "sort", "sed", "mktemp", "rm"),
        drop=tool,
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
    transcript = write_transcript(tmp_path / "transcript.jsonl", [prompt_entry(outside)])
    result = _run(root, stop_payload(outside, transcript), env={"CLAUDE_PROJECT_DIR": str(outside)})
    assert _message(result).startswith(UNDETERMINED + "not inside a git repository")


def test_ty_unchecked_without_a_transcript(tmp_path):
    root = _fake_repo(tmp_path)
    (root / "src" / "bad.py").write_text('x: int = "not an int"\n')
    message = _message(_run(root, {"stop_hook_active": False, "cwd": str(root)}))
    assert message.startswith(UNDETERMINED + "the payload has no transcript_path"), message


def _worktree(tmp_path: Path) -> tuple[Path, Path]:
    """A main checkout with a clean, committed `src/`, and a worktree of it."""
    root = _fake_repo(tmp_path)
    (root / "src" / "fine.py").write_text('"""Docstring."""\n\n\nX: int = 1\n')
    git(root, "add", "-A")
    git(root, "commit", "-qm", "init")
    wt = root / ".claude" / "worktrees" / "wt"
    git(root, "worktree", "add", "-q", str(wt), "-b", "wt")
    (wt / "src" / "bad.py").write_text('x: int = "not an int"\n')
    return root, wt


def _edit_turn(tmp_path: Path, root: Path, edited: Path) -> dict:
    """The payload of a turn that edited `edited`, from a session whose cwd is `root`."""
    transcript = write_transcript(
        tmp_path / "transcript.jsonl",
        [prompt_entry(root), tool_entry("Edit", {"file_path": str(edited)}, root), result_entry(root)],
    )
    return stop_payload(root, transcript)


def test_ty_blocks_on_the_edited_worktree_with_the_cwd_on_main(tmp_path):
    root, wt = _worktree(tmp_path)
    result = _run(root, _edit_turn(tmp_path, root, wt / "src" / "bad.py"))
    assert result.returncode == 2, result.stdout
    assert result.stderr.splitlines()[0].startswith(
        f"ty-check: ty finds errors in src of {os.path.realpath(wt)}."
    ), result.stderr
    assert "invalid-assignment" in result.stderr


def test_ty_passes_when_the_turn_edited_only_the_clean_checkout(tmp_path):
    root, _wt = _worktree(tmp_path)
    result = _run(root, _edit_turn(tmp_path, root, root / "src" / "fine.py"))
    assert (result.returncode, result.stdout, result.stderr) == (0, "", ""), result.stderr


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


def _alarm_repo(tmp_path: Path, line: str | None = None) -> tuple[Path, dict]:
    """A repository with a type error, and the env that points `_run` at `line` as the alarm."""
    root = _fake_repo(tmp_path)
    (root / "src" / "bad.py").write_text('x: int = "not an int"\n')
    env = {"XDG_CACHE_HOME": str(tmp_path / "xdg-cache")}
    if line is not None:
        write_alarm(env, line)
    return root, env


def test_valid_usage_alarm_skips_the_check(tmp_path):
    root, env = _alarm_repo(tmp_path, alarm_line(local="14_30"))
    result = _run(root, env=env)
    assert _message(result) == (
        "ty-check: not checked — the usage alarm is up until 14 30; "
        "the usage guard wraps up this session."
    )
    assert result.stderr == ""


def test_no_usage_alarm_still_blocks(tmp_path):
    root, env = _alarm_repo(tmp_path)
    result = _run(root, env=env)
    assert result.returncode == 2, result.stdout
    assert "invalid-assignment" in result.stderr


def test_expired_usage_alarm_still_blocks(tmp_path):
    root, env = _alarm_repo(tmp_path, alarm_line(delta=timedelta(hours=-1)))
    assert _run(root, env=env).returncode == 2


@pytest.mark.parametrize("kind", ["three_fields", "symlink"])
def test_unreadable_usage_alarm_still_blocks(tmp_path, kind):
    root, env = _alarm_repo(tmp_path)
    if kind == "three_fields":
        write_alarm(env, "venster 92 14:30")
    else:
        target = tmp_path / "real.alarm"
        target.write_text(alarm_line() + "\n")
        link = Path(env["XDG_CACHE_HOME"]) / "lighthouse" / "usage-window.alarm"
        link.parent.mkdir(parents=True)
        link.symlink_to(target)
    assert _run(root, env=env).returncode == 2


def test_without_core_a_valid_alarm_still_blocks(tmp_path):
    root, env = _alarm_repo(tmp_path, alarm_line())
    result = _run(root, env=env, hook=CORE.parent / "python" / "hooks" / "ty-check.sh")
    assert result.returncode == 2, result.stdout
    assert "invalid-assignment" in result.stderr


def test_stop_hook_active_with_alarm_is_silent(tmp_path):
    root, env = _alarm_repo(tmp_path, alarm_line())
    result = _run(root, {"stop_hook_active": True, "cwd": str(root)}, env=env)
    assert (result.returncode, result.stdout, result.stderr) == (0, "", "")

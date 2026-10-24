"""The gate `ruff-format.sh` of `pack/python`: does it block what it must block, let
the rest through, and say so when it could not check?

The hook is bash and reads its input as JSON on stdin. These tests run it as a
subprocess with a real temporary file, so the contract (exit code + stderr) is tested
and not a reimplementation of it.

From the source repository's `test_quality_hooks.py:54-163`. Left out there:
`test_blocking_rules_are_selected_in_pyproject` and `test_pgh_rules_are_selected`
(they read the source repository's own `pyproject.toml`). The fail-open test is turned
around: an unresolvable ruff is COULD NOT CHECK now (ontwerpkeuze 13).
"""

import json
import os
import re
import subprocess
from pathlib import Path

from lighthouse_lib import bash_writes
from tests.pack._testkit import install_pack, tool_bin
from tests.pack.python.conftest import PY_HOOKS

RUFF_HOOK = PY_HOOKS / "ruff-format.sh"
SKILL = (
    Path(__file__).resolve().parents[3]
    / "pack/python/skills/python-writing/claude/SKILL.md"
)
CLEAN_PY = '"""Docstring."""\n\n\ndef f() -> int:\n    return 1\n'
BARE_EXCEPT_PY = "def f():\n    try:\n        pass\n    except:\n        pass\n"
UNCHECKED = "ruff-format: COULD NOT CHECK — "


def _run(
    payload: dict | str, cwd: Path, env: dict | None = None
) -> subprocess.CompletedProcess:
    return subprocess.run(
        [str(RUFF_HOOK)],
        input=payload if isinstance(payload, str) else json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(cwd),
        env=dict(os.environ, **(env or {})),
        check=False,
    )


def _file(tmp_path: Path, name: str, text: str) -> dict:
    target = tmp_path / name
    target.write_text(text)
    return {"tool_input": {"file_path": str(target)}}


def _offline(tmp_path: Path) -> dict:
    """An empty uv cache and no network: a broken environment."""
    empty_cache = tmp_path / "empty-uv-cache"
    empty_cache.mkdir(exist_ok=True)
    return {"UV_OFFLINE": "1", "UV_CACHE_DIR": str(empty_cache)}


def test_ruff_hook_passes_clean_file(tmp_path):
    result = _run(_file(tmp_path, "clean.py", CLEAN_PY), tmp_path)
    assert result.returncode == 0, result.stderr


def test_ruff_hook_blocks_bare_except(tmp_path):
    result = _run(_file(tmp_path, "bare.py", BARE_EXCEPT_PY), tmp_path)
    assert result.returncode == 2, result.stdout
    assert "E722" in result.stderr


def test_ruff_hook_blocks_blanket_type_ignore(tmp_path):
    result = _run(_file(tmp_path, "blanket.py", "x: int = 1  # type: ignore\n"), tmp_path)
    assert result.returncode == 2, result.stdout
    assert "PGH003" in result.stderr


def test_ruff_hook_ignores_non_python(tmp_path):
    result = _run(_file(tmp_path, "notes.md", "# hi\n"), tmp_path)
    assert result.returncode == 0


def test_ruff_hook_ignores_missing_file_and_empty_path(tmp_path):
    assert _run({"tool_input": {"file_path": str(tmp_path / "gone.py")}}, tmp_path).returncode == 0
    assert _run({"tool_input": {}}, tmp_path).returncode == 0


def test_ruff_hook_does_not_block_on_unselected_rule(tmp_path):
    """A rule outside the blocking set never blocks: legacy hits must not stop an edit."""
    payload = _file(tmp_path, "dtz.py", "from datetime import datetime\n\nx = datetime.now()\n")
    result = _run(payload, tmp_path)
    assert result.returncode == 0, result.stderr


def test_ruff_finding_first_line(tmp_path):
    """A finding starts with `ruff-format:`, is a `finding` for `bash_writes.classify`,
    and pass 1 (format, --fix) puts nothing on stdout."""
    result = _run(_file(tmp_path, "bad.py", "import os,sys\n" + BARE_EXCEPT_PY), tmp_path)
    assert result.returncode == 2, result.stdout
    assert result.stdout == ""
    assert result.stderr.startswith("ruff-format: "), result.stderr
    assert "COULD NOT CHECK" not in result.stderr.splitlines()[0]
    assert bash_writes.classify(2, result.stderr.encode()) == "finding"


def test_ruff_unchecked_when_tool_unresolvable(tmp_path):
    """Offline with an empty cache: nothing was checked, and that is not a green light."""
    result = _run(_file(tmp_path, "clean.py", CLEAN_PY), tmp_path, _offline(tmp_path))
    assert result.returncode == 2, result.stdout
    assert result.stderr.startswith(UNCHECKED), result.stderr
    assert bash_writes.classify(2, result.stderr.encode()) == "unchecked"


def test_ruff_unchecked_without_tool(tmp_path):
    """No `uvx` and no `ruff` on the PATH."""
    bin_dir = tool_bin(tmp_path / "bin", ("bash", "jq", "git", "tr", "cat"))
    result = _run(_file(tmp_path, "clean.py", CLEAN_PY), tmp_path, {"PATH": str(bin_dir)})
    assert result.returncode == 2, result.stdout
    assert result.stderr.startswith(UNCHECKED + "neither uvx nor ruff"), result.stderr


def test_ruff_unchecked_without_jq(tmp_path):
    bin_dir = tool_bin(tmp_path / "bin", ("bash", "jq", "git", "tr", "cat", "uvx"), drop="jq")
    result = _run(_file(tmp_path, "clean.py", CLEAN_PY), tmp_path, {"PATH": str(bin_dir)})
    assert result.returncode == 2, result.stdout
    assert result.stderr.startswith(UNCHECKED + "jq is missing"), result.stderr


def test_ruff_unchecked_on_invalid_json(tmp_path):
    result = _run("{not json", tmp_path)
    assert result.returncode == 2, result.stdout
    assert result.stderr.startswith(UNCHECKED + "the payload is not valid JSON"), result.stderr


def test_ruff_unchecked_names_a_pin_that_does_not_resolve(tmp_path):
    """A `.ruff-version` uvx cannot fetch: the probe fails, and the message names the pin."""
    (tmp_path / ".ruff-version").write_text("0.0.0\n")
    env = dict(_offline(tmp_path), LIGHTHOUSE_REPO_ROOT=str(tmp_path))
    result = _run(_file(tmp_path, "clean.py", CLEAN_PY), tmp_path, env)
    assert result.returncode == 2, result.stdout
    assert result.stderr.startswith(UNCHECKED), result.stderr
    assert "ruff@0.0.0" in result.stderr


def test_the_ruff_pin_has_exactly_one_source():
    """The ruff version is in `.ruff-version` and hardcoded nowhere: the hook reads the
    file and has no version number in the script itself."""
    hook = RUFF_HOOK.read_text()
    assert ".ruff-version" in hook, "the hook does not read the pin from .ruff-version"
    assert not re.search(r"ruff@\d", hook), "a ruff version hardcoded in ruff-format.sh"


def _hook_blocking_rules() -> str:
    match = re.search(r'^BLOCKING_RULES="([^"]+)"', RUFF_HOOK.read_text(), re.MULTILINE)
    assert match, "BLOCKING_RULES not found in ruff-format.sh"
    return match.group(1)


def test_skill_blocks_on_the_same_rules():
    """The KPI row of `python-writing` measures the same axis as the gate."""
    assert f"--select {_hook_blocking_rules()} " in SKILL.read_text()


def test_registered_after_edit_and_write():
    registered = json.loads(
        (Path(__file__).resolve().parents[3] / "pack/python/hooks/hooks.json").read_text()
    )
    entries = [
        h
        for block in registered["hooks"]["PostToolUse"]
        if block.get("matcher") == "Edit|Write"
        for h in block["hooks"]
    ]
    assert [(h["command"], h["timeout"]) for h in entries] == [
        ("${CLAUDE_PLUGIN_ROOT}/hooks/ruff-format.sh", 15)
    ]


def test_bash_write_through_gate(tmp_path):
    """With core and python installed, a Bash write of a `.py` with `except:` is a
    finding via `bash-write-gates post`, not `no python gate installed`."""
    from tests.pack.test_bash_write_gates import _Session, context, heredoc

    s = _Session(tmp_path, language_gates=False)
    install_pack("python", s.root)
    out = s.call(heredoc("bad.py", BARE_EXCEPT_PY))
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "== bad.py (ruff-format.sh) ==" in out.stderr
    assert "E722" in out.stderr
    assert "no python gate installed" not in out.stderr + context(out)


def test_ruff_unchecked_on_a_broken_config(tmp_path):
    """ruff exit 2 (a broken `[tool.ruff]`) is no finding: the code was not checked."""
    subprocess.run(["git", "init", "-q"], cwd=tmp_path, check=True)
    (tmp_path / "pyproject.toml").write_text('[tool.ruff]\nline-length = "x"\n')
    result = _run(_file(tmp_path, "clean.py", CLEAN_PY), tmp_path, {"LIGHTHOUSE_REPO_ROOT": str(tmp_path)})
    assert result.returncode == 2, result.stdout
    assert result.stderr.startswith(UNCHECKED + "ruff exited 2"), result.stderr
    assert "fix this in the code" not in result.stderr
    assert bash_writes.classify(2, result.stderr.encode()) == "unchecked"


def test_ruff_unchecked_on_an_empty_payload(tmp_path):
    result = _run("", tmp_path)
    assert result.returncode == 2, result.stdout
    assert result.stderr.startswith(UNCHECKED + "the payload is empty"), result.stderr

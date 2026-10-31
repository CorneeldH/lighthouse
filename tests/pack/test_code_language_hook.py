"""pack/core code-language-check.sh, as installed (#26): blocks a non-English comment,
docstring or name in a written .py/.sh/.bash/.go file, and says so when it could not look.

The hook is bash and reads its payload as JSON on stdin. These tests run it as a
subprocess against real temporary git repos, so the contract (exit code + stderr)
is tested and not a reimplementation of it.
"""

import json
import os
import subprocess
import time
from pathlib import Path

import pytest

from tests.pack._fake_llm import FakeEndpoint
from tests.pack._testkit import GIT_ENV, INSTALLED_HOOKS, MINIMAL_TOML, git

HOOK = INSTALLED_HOOKS / "code-language-check.sh"
ON_TOML = MINIMAL_TOML.replace('language = "en"\n', 'language = "en"\ncode_language = "en"\n')
DUTCH = "# lees het bestand en geef de regels terug\nx = 1\n"
COULD_NOT = "code-language-check: COULD NOT CHECK — "


def _repo(path: Path, toml: str | None = ON_TOML) -> Path:
    path.mkdir(parents=True, exist_ok=True)
    git(path, "init", "-q", "-b", "main")
    if toml is not None:
        (path / "lighthouse.toml").write_text(toml, encoding="utf-8")
    return path


def _write(root: Path, rel: str, text: str = DUTCH) -> Path:
    path = root / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")
    return path


def _run(
    file_path: str, cwd: Path, raw: str | None = None, env: dict[str, str] | None = None
) -> subprocess.CompletedProcess:
    payload = raw if raw is not None else json.dumps(
        {"tool_name": "Edit", "cwd": str(cwd), "tool_input": {"file_path": file_path}}
    )
    return subprocess.run(
        [str(HOOK)],
        input=payload,
        capture_output=True,
        text=True,
        cwd=str(cwd),
        env=dict(os.environ, **GIT_ENV, **(env or {})),
        check=False,
    )


def test_hook_off_without_key(tmp_path):
    root = _repo(tmp_path / "r", MINIMAL_TOML)
    result = _run(str(_write(root, "a.py")), root)
    assert result.returncode == 0
    assert result.stderr == ""


def test_hook_blocks_dutch_comment(tmp_path):
    root = _repo(tmp_path / "r")
    file = _write(root, "a.py")
    result = _run(str(file), root)
    assert result.returncode == 2
    assert f"{file}:1: comment:" in result.stderr


def test_hook_relative_path_from_cwd(tmp_path):
    root = _repo(tmp_path / "r")
    _write(root, "sub/a.py")
    result = _run("sub/a.py", root)
    assert result.returncode == 2
    assert "comment:" in result.stderr


def test_hook_other_repo_without_toml(tmp_path):
    root = _repo(tmp_path / "r")
    other = _repo(tmp_path / "other", None)
    result = _run(str(_write(other, "a.py")), root)
    assert result.returncode == 0
    assert result.stderr == ""


def test_hook_outside_git(tmp_path):
    plain = tmp_path / "plain"
    result = _run(str(_write(plain, "a.py")), plain)
    assert result.returncode == 0


def test_hook_cannot_check_bad_json(tmp_path):
    result = _run("", tmp_path, raw="not json")
    assert result.returncode == 2
    assert result.stderr.startswith(COULD_NOT)


def test_hook_cannot_check_config_error(tmp_path):
    root = _repo(tmp_path / "r", ON_TOML.replace('code_language = "en"', 'code_language = "nl"'))
    result = _run(str(_write(root, "a.py")), root)
    assert result.returncode == 2
    assert result.stderr.startswith(COULD_NOT)


def test_hook_cannot_check_syntax_error(tmp_path):
    root = _repo(tmp_path / "r")
    result = _run(str(_write(root, "a.py", "def f(:\n")), root)
    assert result.returncode == 2
    assert result.stderr.startswith(COULD_NOT)
    assert "Traceback" not in result.stderr


def test_hook_missing_file(tmp_path):
    root = _repo(tmp_path / "r")
    result = _run(str(root / "gone.py"), root)
    assert result.returncode == 0


def test_hook_other_extension_skips(tmp_path):
    root = _repo(tmp_path / "r")
    result = _run(str(_write(root, "a.txt")), root)
    assert result.returncode == 0


def test_hook_exempt_skips(tmp_path):
    root = _repo(
        tmp_path / "r",
        ON_TOML.replace(
            'code_language = "en"\n', 'code_language = "en"\ncode_language_exempt = ["tests/fixtures/*"]\n'
        ),
    )
    _write(root, "tests/fixtures/a.py")
    result = _run("tests/fixtures/a.py", root)
    assert result.returncode == 0


def test_hook_manifest_skips(tmp_path):
    root = _repo(tmp_path / "r")
    _write(root, ".lighthouse/manifest.toml", '[[unit]]\npath = "managed/a.py"\n')
    result = _run(str(_write(root, "managed/a.py")), root)
    assert result.returncode == 0


def test_hook_marker_skips(tmp_path):
    root = _repo(tmp_path / "r")
    text = "# lees het bestand en geef de regels terug  lighthouse: allow-language\nx = 1\n"
    result = _run(str(_write(root, "a.py", text)), root)
    assert result.returncode == 0


def test_hook_cannot_check_without_tomllib(tmp_path):
    shadow = tmp_path / "shadow"
    shadow.mkdir()
    (shadow / "tomllib.py").write_text("raise ImportError('no tomllib')\n", encoding="utf-8")
    root = _repo(tmp_path / "r")
    result = _run(str(_write(root, "a.py")), root, env={"PYTHONPATH": str(shadow)})
    assert result.returncode == 2
    assert result.stderr.splitlines()[0].startswith(COULD_NOT)
    assert "python3 >= 3.11" in result.stderr.splitlines()[0]


def test_hook_broken_import_is_never_an_empty_block(tmp_path):
    shadow = tmp_path / "shadow"
    shadow.mkdir()
    (shadow / "fnmatch.py").write_text("raise RuntimeError('boom at import')\n", encoding="utf-8")
    root = _repo(tmp_path / "r")
    result = _run(str(_write(root, "a.py")), root, env={"PYTHONPATH": str(shadow)})
    assert result.returncode == 2
    assert result.stderr.splitlines()[0].startswith(COULD_NOT)
    assert "exit 1" in result.stderr
    assert "boom at import" in result.stderr


SCOPED_TOML = ON_TOML.replace(
    'code_language = "en"\n', 'code_language = "en"\ncode_language_paths = ["pack/**"]\n'
)


def test_hook_markdown_in_scope(tmp_path):
    root = _repo(tmp_path / "r", SCOPED_TOML)
    file = _write(root, "pack/a.md", "Dit is niet goed en het bestand is niet gevonden.\n")
    result = _run(str(file), root)
    assert result.returncode == 2
    assert f"{file}:1: prose:" in result.stderr


def test_hook_toml_out_of_scope(tmp_path):
    root = _repo(tmp_path / "r", SCOPED_TOML)
    result = _run(str(_write(root, "docs/notulen.toml", "x = 1\n")), root)
    assert result.returncode == 0
    assert result.stderr == ""


# --- layer 2 (#37): the notice, the deadline, and the Bash path ---

LAYER1_LINE = "lees_bestand = 1\n"
DOUBT_LINE = "legacy_processed_lijst_weg = 2\n"


def _table(url: str) -> str:
    return f'\n[code_language_llm]\nurl = "{url}"\nmodels = ["m1"]\nkey_env = "LLM_TEST_KEY"\n'


def _session_toml(tmp_path: Path, table: str = "") -> Path:
    """The session repo's `lighthouse.toml`, which the hook reads through `LIGHTHOUSE_TOML`."""
    path = tmp_path / "session.toml"
    path.write_text(MINIMAL_TOML + table, encoding="utf-8")
    return path


def _llm_repo(tmp_path: Path, url: str) -> Path:
    """A repo with `code_language` on; the chain lives in the session's `lighthouse.toml`."""
    _session_toml(tmp_path, _table(url))
    return _repo(tmp_path / "r")


def _llm_env(tmp_path: Path, **extra: str) -> dict[str, str]:
    xdg = tmp_path / "xdg"
    xdg.mkdir(exist_ok=True)
    return {
        "XDG_RUNTIME_DIR": str(xdg),
        "LLM_TEST_KEY": "sekrit",
        "LIGHTHOUSE_TOML": str(tmp_path / "session.toml"),
        **extra,
    }


def test_hook_foreign_chain_is_never_used(tmp_path):
    """A repo the agent writes into cannot send the session's key anywhere (R7)."""
    with FakeEndpoint() as foreign, FakeEndpoint() as own:
        root = _repo(tmp_path / "r", ON_TOML + _table(foreign.url))
        file = _write(root, "a.py", DOUBT_LINE)
        _session_toml(tmp_path)
        result = _run(str(file), root, env=_llm_env(tmp_path))
        assert (result.returncode, result.stdout, result.stderr) == (0, "", "")
        assert foreign.requests == []
        _session_toml(tmp_path, _table(own.url))
        result = _run(str(file), root, env=_llm_env(tmp_path))
    assert foreign.requests == []
    assert [h["Authorization"] for h in own.headers] == ["Bearer sekrit"]
    assert result.returncode == 2
    assert f"{file}:1: name: legacy_processed_lijst_weg [layer 2 (m1)]" in result.stderr


def _notice_json(stdout: str) -> tuple[str, str]:
    data = json.loads(stdout)
    specific = data["hookSpecificOutput"]
    assert specific["hookEventName"] == "PostToolUse"
    return data["systemMessage"], specific["additionalContext"]


def test_hook_notice_without_findings(tmp_path):
    with FakeEndpoint(default="http500") as fake:
        root = _llm_repo(tmp_path, fake.url)
        result = _run(str(_write(root, "a.py", DOUBT_LINE)), root, env=_llm_env(tmp_path))
    assert result.returncode == 0, result.stderr
    assert result.stderr == ""
    system, context = _notice_json(result.stdout)
    assert "legacy_processed_lijst_weg" in system
    assert "legacy_processed_lijst_weg" in context
    assert "layer 2 did not answer (m1: HTTP 500)" in system


def test_hook_notice_with_findings(tmp_path):
    with FakeEndpoint(default="http500") as fake:
        root = _llm_repo(tmp_path, fake.url)
        file = _write(root, "a.py", LAYER1_LINE + DOUBT_LINE)
        result = _run(str(file), root, env=_llm_env(tmp_path))
    assert result.returncode == 2
    assert f"{file}:1: name: lees_bestand [layer 1]" in result.stderr
    assert "layer 2 did not answer" in result.stderr
    assert f"{file}:2: name: legacy_processed_lijst_weg" in result.stderr


@pytest.mark.parametrize("mode", ["silent", "drip", "http://lighthouse-test.invalid/v1"])
def test_hook_ends_within_deadline(tmp_path, mode):
    with FakeEndpoint(default=mode if not mode.startswith("http") else "answer") as fake:
        root = _llm_repo(tmp_path, mode if mode.startswith("http") else fake.url)
        file = _write(root, "a.py", DOUBT_LINE)
        start = time.monotonic()
        result = _run(str(file), root, env=_llm_env(tmp_path))
        elapsed = time.monotonic() - start
    assert elapsed < 10, elapsed
    assert result.returncode == 0, result.stderr
    system, _context = _notice_json(result.stdout)
    assert "layer 2 did not answer (m1: " in system


def test_hook_under_gates(tmp_path):
    with FakeEndpoint() as fake:
        root = _llm_repo(tmp_path, fake.url)
        env = _llm_env(tmp_path, LIGHTHOUSE_UNDER_BASH_WRITE_CHECK="1")
        result = _run(str(_write(root, "a.py", DOUBT_LINE)), root, env=env)
    assert fake.requests == []
    assert result.returncode == 0, result.stderr
    assert result.stdout.startswith("code-language-check: NOTICE — ")
    assert "legacy_processed_lijst_weg" in result.stdout

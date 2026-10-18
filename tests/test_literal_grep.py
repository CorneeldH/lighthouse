"""ci/literal-grep.sh: no repo name, label, board ID, host name or host IP in pack/."""

import subprocess
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[1]
SCRIPT = REPO / "ci" / "literal-grep.sh"


def _grep(target: Path, *, env: dict | None = None) -> subprocess.CompletedProcess:
    return subprocess.run(
        [str(SCRIPT), str(target)],
        capture_output=True,
        text=True,
        cwd=REPO,
        env=env,
        check=False,
    )


def test_clean_dir_passes(tmp_path):
    (tmp_path / "a.sh").write_text("echo hello\n")
    assert _grep(tmp_path).returncode == 0


@pytest.mark.parametrize(
    "hit", ["PAIA_ROOT", "Beelink", "/home/x", "4-lane:stalled", "PVT_kwHOAGmU3s4BfI-i"]
)
def test_hit_fails_case_insensitively(tmp_path, hit):
    (tmp_path / "a.md").write_text(f"x {hit} y\n")
    result = _grep(tmp_path)
    assert result.returncode == 1
    assert "a.md:1:" in result.stdout


def test_comment_and_blank_lines_in_denylist_match_nothing(tmp_path):
    """A blank line as a pattern would match every line."""
    (tmp_path / "a.txt").write_text("plain text\n")
    assert _grep(tmp_path).returncode == 0


def test_pack_is_clean():
    result = _grep(REPO / "pack")
    assert result.returncode == 0, result.stdout


def test_empty_denylist_is_rc_2(tmp_path):
    """A deny list that lost its patterns must not turn the check green."""
    empty = tmp_path / "deny.txt"
    empty.write_text("# only a comment\n\n")
    (tmp_path / "a.txt").write_text("PAIA\n")
    import os

    result = _grep(tmp_path, env=dict(os.environ, LITERAL_DENYLIST=str(empty)))
    assert result.returncode == 2


def test_python_caches_are_skipped(tmp_path):
    cache = tmp_path / "lib" / "__pycache__"
    cache.mkdir(parents=True)
    (cache / "x.cpython-314.pyc").write_bytes(b"paia")
    (tmp_path / "lib" / "y.pyc").write_bytes(b"paia")
    assert _grep(tmp_path).returncode == 0


def _allow(tmp_path, *lines: str) -> dict:
    import os

    allow = tmp_path / "allow.txt"
    allow.write_text("# exceptions\n\n" + "".join(f"{line}\n" for line in lines))
    return dict(os.environ, LITERAL_ALLOWLIST=str(allow))


def test_allowlisted_line_passes(tmp_path):
    pack = tmp_path / "pack"
    pack.mkdir()
    (pack / "a.py").write_text('MARKERS = ("<!-- paia:",)\n')
    env = _allow(tmp_path, f'{pack}/a.py:MARKERS = ("<!-- paia:",)')
    result = _grep(pack, env=env)
    assert result.returncode == 0, result.stdout


def test_same_text_in_other_file_fails(tmp_path):
    pack = tmp_path / "pack"
    pack.mkdir()
    (pack / "a.py").write_text('MARKERS = ("<!-- paia:",)\n')
    (pack / "b.py").write_text('MARKERS = ("<!-- paia:",)\n')
    env = _allow(tmp_path, f'{pack}/a.py:MARKERS = ("<!-- paia:",)')
    result = _grep(pack, env=env)
    assert result.returncode == 1
    assert f"{pack}/b.py:1:" in result.stdout
    assert f"{pack}/a.py" not in result.stdout


def test_other_line_in_allowlisted_file_fails(tmp_path):
    pack = tmp_path / "pack"
    pack.mkdir()
    (pack / "a.py").write_text('MARKERS = ("<!-- paia:",)\nPAIA_ROOT = 1\n')
    env = _allow(tmp_path, f'{pack}/a.py:MARKERS = ("<!-- paia:",)')
    result = _grep(pack, env=env)
    assert result.returncode == 1
    assert f"{pack}/a.py:2:" in result.stdout
    assert f"{pack}/a.py:1:" not in result.stdout


def test_sources_toml_is_skipped(tmp_path):
    pack = tmp_path / "pack"
    (pack / "core").mkdir(parents=True)
    line = 'source_path = ".claude/skills/paia-start-issue/SKILL.md"\n'
    (pack / "SOURCES.toml").write_text(line)
    assert _grep(pack).returncode == 0
    (pack / "core" / "x.md").write_text(line)
    result = _grep(pack)
    assert result.returncode == 1
    assert "SOURCES.toml" not in result.stdout


def test_unreadable_allowlist_is_rc_2(tmp_path):
    import os

    (tmp_path / "a.txt").write_text("clean\n")
    env = dict(os.environ, LITERAL_ALLOWLIST=str(tmp_path / "missing.txt"))
    assert _grep(tmp_path, env=env).returncode == 2

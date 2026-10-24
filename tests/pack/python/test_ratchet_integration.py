"""Integration test of the quality ratchet: the real ruff and complexipy on a fixture
repository, through the installed `.lighthouse/scripts/quality/ratchet.py ruff`.

The ratchet part of the source repository's `tests/integration/test_quality_ratchet.sh`
(sections 1-5c), as pytest. Left out there: the provenance table and the pre-push cases
(sections 6-8); they test the source repository's own `pre-push-quality.sh`.

What is pinned, and why:
  - Green on a no-op. A gate that is red on its own repository is switched off within a
    week.
  - Red on an inserted `except Exception: pass` (S110 rises) and on a bare `except:`
    (E722, a zero rule).
  - Red also with a `# noqa` for exactly that rule: --ignore-noqa is a property, not a
    claim.
  - Red on a function with cognitive 16-25: the counter `cognitive_over_15` rises, and a
    run leaves no complexipy litter.
  - Red on a function above cyclomatic 20 or cognitive 25, also with `# noqa: C901` or
    `# noqa: complexipy`, a complexipy exclude, or a higher ruff `max-complexity`.
  - Determinism: two runs on the same tree give the same number.
"""

import os
import shutil
import subprocess
from pathlib import Path

import pytest

from tests.pack._testkit import GIT_ENV, git, install_pack

if shutil.which("uv") is None:
    if os.environ.get("CI"):
        pytest.fail("uv not on PATH; CI must install it before running this test", pytrace=False)
    pytest.skip("uv not on PATH (local run); the ratchet needs uvx", allow_module_level=True)

PYPROJECT = """\
[project]
name = "ratchet-fixture"
version = "0"
requires-python = ">=3.11"

[tool.ruff.lint]
extend-select = ["E722", "B006", "F403", "F811", "B023", "PT012", "PGH003", "PGH004",
                 "C901", "TRY400", "F401", "F841", "S110", "S112", "PT011"]

[tool.ruff.lint.flake8-pytest-style]
raises-require-match-for = ["Exception", "BaseException"]
"""

BASELINE = """\
[ruff]
C901   = 0
TRY400 = 0
F401   = 0
F841   = 0
S110   = 0
PT011  = 0
S112   = 0

[complexipy]
cognitive_over_15 = 0

[coverage]
min_diff_pct = 70
min_total_pct = 0
"""

APP = '''"""A clean module: none of the fifteen rules hits it."""


def add(a: int, b: int) -> int:
    return a + b
'''

TEST_APP = """from src.app import add


def test_add() -> None:
    assert add(1, 2) == 3
"""

NESTED_21 = """

def nested(a: int, b: int, c: int, d: int, e: int, f: int) -> int:
    if a:
        if b:
            if c:
                if d:
                    if e:
                        if f:
                            return 1
    return 0
"""

NESTED_28 = """

def nested(a: int, b: int, c: int, d: int, e: int, f: int, g: int) -> int:{noqa}
    if a:
        if b:
            if c:
                if d:
                    if e:
                        if f:
                            if g:
                                return 1
    return 0
"""


def _chain(noqa: str = "") -> str:
    """19 elif branches plus the if: ruff 0.16.4 counts cyclomatic 21."""
    lines = [f"\n\ndef chain(x: int) -> int:{noqa}", "    if x == 0:", "        return 0"]
    for i in range(1, 20):
        lines += [f"    elif x == {i}:", f"        return {i}"]
    lines.append("    return -1\n")
    return "\n".join(lines)


def _fixture(tmp_path: Path) -> Path:
    """A miniature repository with core and python installed, the pins, a baseline at
    the level of the fixture, `src/` and `tests/`: an INSERTED violation is the only rise."""
    root = tmp_path / "fixture"
    install_pack("python", root)
    (root / ".ruff-version").write_text("0.16.4\n")
    (root / ".complexipy-version").write_text("8.0.1\n")
    (root / "pyproject.toml").write_text(PYPROJECT)
    (root / "quality-baseline.toml").write_text(BASELINE)
    (root / "src").mkdir()
    (root / "tests").mkdir()
    (root / "src" / "app.py").write_text(APP)
    (root / "tests" / "test_app.py").write_text(TEST_APP)
    git(root, "init", "-q", "-b", "main")
    git(root, "add", "-A")
    git(root, "commit", "-qm", "clean fixture")
    return root


def _ratchet(root: Path) -> subprocess.CompletedProcess:
    result = subprocess.run(
        ["python3", ".lighthouse/scripts/quality/ratchet.py", "ruff"],
        cwd=root,
        capture_output=True,
        text=True,
        env=dict(os.environ, **GIT_ENV, LIGHTHOUSE_REPO_ROOT=str(root)),
        check=False,
    )
    # A run writes its complexipy output and cache to a temporary directory.
    litter = subprocess.run(
        ["git", "-C", str(root), "status", "--porcelain", "--ignored"],
        capture_output=True,
        text=True,
        check=True,
    ).stdout
    assert "complexipy" not in litter, litter
    return result


def _append(root: Path, text: str) -> None:
    with (root / "src" / "app.py").open("a") as f:
        f.write(text)


@pytest.fixture
def fixture(tmp_path) -> Path:
    return _fixture(tmp_path)


def test_a_clean_tree_is_green_and_deterministic(fixture):
    first = _ratchet(fixture)
    assert first.returncode == 0, first.stdout + first.stderr
    assert "complexipy ok (0 functions cognitive > 15)" in first.stdout
    assert not (fixture / "complexipy-results.json").exists()
    assert not (fixture / ".complexipy_cache").exists()
    second = _ratchet(fixture)
    assert (first.stdout, first.stderr) == (second.stdout, second.stderr)


def test_a_new_try_except_pass_is_red(fixture):
    _append(fixture, "\n\ndef swallow() -> None:\n    try:\n        add(1, 2)\n    except Exception:\n        pass\n")
    result = _ratchet(fixture)
    assert result.returncode != 0
    assert "S110: 0 -> 1" in result.stderr


def test_a_bare_except_is_a_zero_rule_hit(fixture):
    # `except:\n    raise` does not count as E722 in ruff 0.16.4; `pass` in the body does.
    _append(fixture, "\n\ndef bare() -> None:\n    try:\n        add(1, 2)\n    except:\n        pass\n")
    result = _ratchet(fixture)
    assert result.returncode != 0
    assert "E722" in result.stderr
    assert "no bump" in result.stderr


def test_a_noqa_does_not_lower_the_counter(fixture):
    _append(
        fixture,
        "\n\ndef swallow() -> None:\n    try:\n        add(1, 2)\n    except Exception:  # noqa: S110\n        pass\n",
    )
    result = _ratchet(fixture)
    assert result.returncode != 0
    assert "S110: 0 -> 1" in result.stderr


def test_cognitive_21_raises_the_counter(fixture):
    _append(fixture, NESTED_21)
    result = _ratchet(fixture)
    assert result.returncode != 0
    assert "cognitive_over_15: 0 -> 1" in result.stderr
    assert "src/app.py::nested" in result.stderr


def test_cyclomatic_21_with_noqa_is_red(fixture):
    _append(fixture, _chain("  # noqa: C901"))
    result = _ratchet(fixture)
    assert result.returncode != 0
    assert "cyclomatic 21 > 20" in result.stderr
    assert "no bump" in result.stderr


def test_a_higher_mccabe_threshold_keeps_cyclomatic_21_red(fixture):
    with (fixture / "pyproject.toml").open("a") as f:
        f.write("\n[tool.ruff.lint.mccabe]\nmax-complexity = 25\n")
    baseline = (fixture / "quality-baseline.toml").read_text()
    baseline = baseline.replace("C901   = 0", "C901   = 1").replace(
        "cognitive_over_15 = 0", "cognitive_over_15 = 1"
    )
    (fixture / "quality-baseline.toml").write_text(baseline)
    git(fixture, "commit", "-qam", "mccabe threshold 25, counters at 1")
    _append(fixture, _chain())
    result = _ratchet(fixture)
    assert result.returncode != 0
    assert "cyclomatic 21 > 20" in result.stderr


def test_cognitive_28_with_noqa_is_red(fixture):
    _append(fixture, NESTED_28.format(noqa="  # noqa: complexipy"))
    result = _ratchet(fixture)
    assert result.returncode != 0
    assert "cognitive 28 > 25" in result.stderr
    assert "src/app.py::nested" in result.stderr


def test_a_complexipy_exclude_keeps_cognitive_28_red(fixture):
    with (fixture / "pyproject.toml").open("a") as f:
        f.write('\n[tool.complexipy]\nexclude = ["app.py", "src/app.py"]\n')
    (fixture / "complexipy.toml").write_text('exclude = ["app.py", "src/app.py"]\n')
    shutil.copyfile(fixture / "complexipy.toml", fixture / ".complexipy.toml")
    git(fixture, "add", "-A")
    git(fixture, "commit", "-qm", "complexipy exclude")
    _append(fixture, NESTED_28.format(noqa=""))
    result = _ratchet(fixture)
    assert result.returncode != 0
    assert "cognitive 28 > 25" in result.stderr


def test_lint_dirs_from_config(fixture):
    """A violation outside python.lint_dirs is not counted; inside it is."""
    (fixture / "lib").mkdir()
    (fixture / "lib" / "m.py").write_text(
        "def swallow() -> None:\n    try:\n        pass\n    except Exception:\n        pass\n"
    )
    assert _ratchet(fixture).returncode == 0
    with (fixture / "lighthouse.toml").open("a") as f:
        f.write('\n[python]\nlint_dirs = ["src", "tests", "lib"]\n')
    result = _ratchet(fixture)
    assert result.returncode != 0
    assert "S110: 0 -> 1" in result.stderr

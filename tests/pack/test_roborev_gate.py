"""Tests for `.lighthouse/scripts/roborev_gate.py gate` (#119): threshold, Codex budget and
the per-branch state files of the local roborev review.

Each test builds a temporary git repo with `refs/remotes/origin/main`, a `lighthouse.toml`, a
`.roborev.toml` and fake `roborev`/`codex` binaries on `PATH`. The budget is a lambda: the real
`codex_usage.py` never runs here.
"""

from __future__ import annotations

import importlib.util
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

import pytest

from tests.pack._testkit import GIT_ENV, INSTALL_ROOT

SCRIPT = INSTALL_ROOT / ".lighthouse" / "scripts" / "roborev_gate.py"
_SPEC = importlib.util.spec_from_file_location("roborev_gate", SCRIPT)
assert _SPEC and _SPEC.loader
rg = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = rg
_SPEC.loader.exec_module(rg)

CODEX_OK = (0, '{"decision":"codex","reason":"ok"}')


def _git(repo: Path, *args: str) -> str:
    return subprocess.run(
        ["git", "-C", str(repo), *args],
        capture_output=True,
        text=True,
        check=True,
        env={**os.environ, **GIT_ENV},
    ).stdout.strip()


def _commit_lines(repo: Path, count: int, name: str = "x.go") -> None:
    (repo / name).write_text("".join(f"line {i}\n" for i in range(count)))
    _git(repo, "add", name)
    _git(repo, "commit", "-m", f"add {name}")


@pytest.fixture
def repo(tmp_path, monkeypatch) -> Path:
    """Repo on branch `feat` with 100 added code lines on top of origin/main."""
    root = tmp_path / "repo"
    root.mkdir()
    _git(root, "init", "-b", "main")
    (root / "lighthouse.toml").write_text('[project]\nlanguage = "nl"\n[review]\nroborev = "local"\n')
    (root / ".roborev.toml").write_text("")
    _git(root, "add", ".")
    _git(root, "commit", "-m", "base")
    _git(root, "update-ref", "refs/remotes/origin/main", "HEAD")
    _git(root, "checkout", "-b", "feat")
    _commit_lines(root, 100)
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    for tool in ("roborev", "codex"):
        (bin_dir / tool).write_text("#!/bin/sh\n")
        (bin_dir / tool).chmod(0o755)
    monkeypatch.setenv("PATH", f"{bin_dir}:/usr/bin:/bin")
    monkeypatch.delenv("LIGHTHOUSE_TOML", raising=False)
    return root


def _must_not_run() -> tuple[int, str]:
    raise AssertionError("budget must not be consulted")


def test_count_code_lines_follows_the_paia_size_rule():
    numstat = (
        "10\t2\tinternal/x.go\n5\t5\tdocs/a.go\n-\t-\tlogo.png\n7\t0\tREADME.md\n"
        "3\t1\t.github/workflows/ci.yml\n4\t0\tpack/x.toml\n"
    )
    assert rg.count_code_lines(numstat) == 16


def test_gate_is_off_without_the_setting(repo):
    (repo / "lighthouse.toml").write_text('[project]\nlanguage = "nl"\n')
    assert rg.gate(repo, _must_not_run) == {"verdict": "off"}


def test_gate_skips_without_roborev_toml(repo):
    (repo / ".roborev.toml").unlink()
    out = rg.gate(repo, _must_not_run)
    assert (out["verdict"], out["reason"]) == ("skip", "geen .roborev.toml")


def test_gate_skips_without_roborev_on_path(repo, monkeypatch, tmp_path):
    only_git = tmp_path / "only-git"
    only_git.mkdir()
    (only_git / "git").symlink_to(shutil.which("git"))
    monkeypatch.setenv("PATH", str(only_git))
    out = rg.gate(repo, _must_not_run)
    assert (out["verdict"], out["reason"]) == ("skip", "geen roborev of codex op PATH")


def test_gate_skips_below_the_threshold(repo):
    _git(repo, "reset", "--hard", "origin/main")
    _commit_lines(repo, 99)
    out = rg.gate(repo, _must_not_run)
    assert (out["verdict"], out["reason"]) == ("skip", "99 coderegels (drempel 100)")


def test_gate_runs_at_the_threshold(repo):
    out = rg.gate(repo, lambda: CODEX_OK)
    base = _git(repo, "rev-parse", "origin/main")
    head = _git(repo, "rev-parse", "HEAD")
    assert out["verdict"] == "run"
    assert out["range"] == f"{base}..{head}"
    assert out["lines"] == 100


@pytest.mark.parametrize(
    "budget,reason",
    [
        ((6, '{"decision":"fallback","reason":"5h 93%"}'), "Codex-budget: fallback - 5h 93%"),
        (
            (0, '{"decision":"not_measurable","reason":"not measurable"}'),
            "Codex-budget: not_measurable - not measurable",
        ),
        ((1, "Traceback"), "Codex-budget: onleesbaar - exit 1"),
        ((0, "not json"), "Codex-budget: onleesbaar - exit 0"),
    ],
)
def test_gate_skips_on_the_budget(repo, budget, reason):
    out = rg.gate(repo, lambda: budget)
    assert (out["verdict"], out["reason"]) == ("skip", reason)


def test_gate_records_its_skip_and_repeats_it(repo):
    first = rg.gate(repo, lambda: (6, '{"decision":"fallback","reason":"5h 93%"}'))
    written = json.loads(Path(first["state"]).read_text())
    base = _git(repo, "rev-parse", "origin/main")
    head = _git(repo, "rev-parse", "HEAD")
    assert written == {
        "range": f"{base}..{head}",
        "uitkomst": "skip",
        "reden": "Codex-budget: fallback - 5h 93%",
    }
    again = rg.gate(repo, lambda: CODEX_OK)
    assert (again["verdict"], again["reason"]) == ("skip", "Codex-budget: fallback - 5h 93%")


def test_gate_reads_a_done_record(repo):
    state = rg.state_dir(repo) / "feat.json"
    state.parent.mkdir(parents=True)
    state.write_text(json.dumps({"range": "a..b", "uitkomst": "done", "sha": "abc", "bevindingen": []}))
    out = rg.gate(repo, _must_not_run)
    assert (out["verdict"], out["sha"]) == ("done", "abc")


def test_gate_asks_to_classify_raw_output_without_a_record(repo):
    raw = rg.state_dir(repo) / "feat.md"
    raw.parent.mkdir(parents=True)
    raw.write_text("findings")
    out = rg.gate(repo, _must_not_run)
    assert out["verdict"] == "classify"
    assert out["raw"] == str(raw)


def test_state_files_of_a_slashed_branch(repo):
    _git(repo, "checkout", "-b", "fix/x")
    out = rg.gate(repo, lambda: CODEX_OK)
    assert out["state"] == str(rg.state_dir(repo) / "fix-x.json")
    assert out["raw"] == str(rg.state_dir(repo) / "fix-x.md")


def test_every_verdict_names_state_and_raw(repo):
    sd = rg.state_dir(repo)
    run = rg.gate(repo, lambda: CODEX_OK)
    skip = rg.gate(repo, lambda: (6, '{"decision":"fallback","reason":"r"}'))
    (sd / "feat.json").unlink()
    (sd / "feat.md").parent.mkdir(parents=True, exist_ok=True)
    (sd / "feat.md").write_text("x")
    classify = rg.gate(repo, _must_not_run)
    (sd / "feat.json").write_text(json.dumps({"range": "a..b", "uitkomst": "done", "sha": "s"}))
    done = rg.gate(repo, _must_not_run)
    for out in (run, skip, classify, done):
        assert out["state"] == str(sd / "feat.json")
        assert out["raw"] == str(sd / "feat.md")
        assert Path(out["state"]).is_absolute()


def test_state_dir_is_per_worktree(repo, tmp_path):
    other = tmp_path / "wt"
    _git(repo, "worktree", "add", str(other), "-b", "other")
    assert rg.state_dir(other) != rg.state_dir(repo)
    assert rg.state_dir(other).is_absolute()
    assert rg.state_dir(repo) == repo / ".git" / "lighthouse" / "roborev"


def test_cli_prints_the_verdict_as_json(repo):
    result = subprocess.run(
        [sys.executable, str(SCRIPT), "gate"],
        cwd=repo,
        capture_output=True,
        text=True,
        env={"PATH": os.environ["PATH"], **GIT_ENV},
    )
    assert result.returncode == 0
    assert json.loads(result.stdout)["verdict"] in {"run", "skip"}


def test_cli_cannot_check_outside_a_repo(tmp_path):
    result = subprocess.run(
        [sys.executable, str(SCRIPT), "gate"],
        cwd=tmp_path,
        capture_output=True,
        text=True,
        env={"PATH": os.environ["PATH"], "GIT_CEILING_DIRECTORIES": str(tmp_path.parent)},
    )
    assert result.returncode == 3
    assert result.stderr.startswith("COULD NOT CHECK:")

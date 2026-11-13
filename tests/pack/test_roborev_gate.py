"""Tests for `.lighthouse/scripts/roborev_gate.py` (#119): threshold, Codex budget, the per-branch
state files, classify, record and body-line of the local roborev review.

The cases are shared with the Go port (`internal/cli/roborev_fixture_test.go`): one JSON file per
case in `tests/pack/fixtures/roborev_gate/`. Each case builds a temporary git repo with
`refs/remotes/origin/main` (branch `feat`, 100 added code lines), a `lighthouse.toml`, a
`.roborev.toml` and fake `roborev`/`codex` binaries on `PATH`, then runs its steps. The budget of
a `gate` step is given by the case: the real `codex_usage.py` never runs here.
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

FIXTURES = Path(__file__).parent / "fixtures" / "roborev"
CASES = Path(__file__).parent / "fixtures" / "roborev_gate"


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


class Run:
    """One case: the repo, the variables of the placeholders, and the steps."""

    def __init__(self, tmp_path: Path, monkeypatch, case: dict):
        self.tmp = tmp_path / "tmp"
        self.tmp.mkdir()
        self.repo = tmp_path / "repo"
        self.vars: dict[str, str] = {"tmp": str(self.tmp), "fixtures": str(FIXTURES)}
        self.monkeypatch = monkeypatch
        self.has_repo = case.get("repo", True)
        if not self.has_repo:
            self.repo.mkdir()
            self.vars["repo"] = str(self.repo)
            return
        self.repo.mkdir()
        _git(self.repo, "init", "-b", "main")
        (self.repo / "lighthouse.toml").write_text('[project]\nlanguage = "nl"\n[review]\nroborev = "local"\n')
        (self.repo / ".roborev.toml").write_text("")
        _git(self.repo, "add", ".")
        _git(self.repo, "commit", "-m", "base")
        _git(self.repo, "update-ref", "refs/remotes/origin/main", "HEAD")
        _git(self.repo, "checkout", "-b", "feat")
        _commit_lines(self.repo, 100)
        bin_dir = tmp_path / "bin"
        bin_dir.mkdir()
        for tool in ("roborev", "codex"):
            (bin_dir / tool).write_text("#!/bin/sh\n")
            (bin_dir / tool).chmod(0o755)
        monkeypatch.setenv("PATH", f"{bin_dir}:/usr/bin:/bin")
        monkeypatch.delenv("LIGHTHOUSE_TOML", raising=False)
        self.vars.update(repo=str(self.repo), base=_git(self.repo, "rev-parse", "origin/main"))

    def sub(self, value):
        if isinstance(value, str):
            self.vars["head"] = _git(self.repo, "rev-parse", "HEAD") if self.has_repo else ""
            if self.has_repo:
                self.vars["state"] = str(rg.state_dir(self.repo))
            for key in sorted(self.vars, key=len, reverse=True):
                value = value.replace(f"${key}", self.vars[key])
            return value
        if isinstance(value, list):
            return [self.sub(v) for v in value]
        if isinstance(value, dict):
            return {k: self.sub(v) for k, v in value.items()}
        return value

    def setup(self, actions: list[dict]) -> None:
        for act in actions:
            act = self.sub(act)
            do = act["do"]
            if do == "toml":
                (self.repo / "lighthouse.toml").write_text(act["text"])
            elif do == "remove":
                (self.repo / act["path"]).unlink()
            elif do == "path":
                only_git = self.tmp / "only-git"
                only_git.mkdir()
                (only_git / "git").symlink_to(shutil.which("git"))
                self.monkeypatch.setenv("PATH", str(only_git))
            elif do == "git":
                _git(self.repo, *act["args"])
            elif do == "commit_lines":
                _commit_lines(self.repo, act["count"], act["name"])
            elif do == "commit_files":
                for name, spec in act["files"].items():
                    (self.repo / name).parent.mkdir(parents=True, exist_ok=True)
                    (self.repo / name).write_text("x\n" * spec["lines"])
                _git(self.repo, "add", ".")
                _git(self.repo, "commit", "-m", act["message"])
            elif do == "fix_commit":
                _commit_lines(self.repo, 1, act["name"])
                _git(self.repo, "commit", "--amend", "-m", act["subject"])
                self.vars[act["as"]] = _git(self.repo, "rev-parse", "HEAD")
            elif do == "raw":
                text = act["text"] if "text" in act else (FIXTURES / act["fixture"]).read_text()
                path = rg.state_dir(self.repo) / f"{act['name']}.md"
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(text)
            elif do == "state":
                rg.write_state(rg.state_dir(self.repo) / f"{act['name']}.json", act["json"])
            elif do == "remove_state":
                (rg.state_dir(self.repo) / f"{act['name']}.json").unlink()
            elif do == "file":
                path = Path(act["path"])
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(act["text"] if "text" in act else json.dumps(act["json"]))
            elif do == "worktree":
                wt = self.tmp / act["name"]
                _git(self.repo, "worktree", "add", str(wt), "-b", act["branch"])
                self.vars["wt"] = str(wt)
            else:
                raise AssertionError(f"unknown setup action {do}")

    def check_state_file(self, spec: dict) -> None:
        path = Path(self.sub(spec["path"])) if "path" in spec else rg.state_dir(self.repo) / f"{spec['name']}.json"
        data = json.loads(path.read_text())
        if "equals" in spec:
            assert data == self.sub(spec["equals"])
        for key, want in self.sub(spec.get("subset", {})).items():
            assert data[key] == want

    def check_absent(self, step: dict) -> None:
        for name in step.get("absent", []):
            assert not (rg.state_dir(self.repo) / f"{name}.json").exists()
        for path in self.sub(step.get("absent_path", [])):
            assert not Path(path).exists()


def _rows(rows) -> list[dict]:
    return [
        {"path": p, "line": ln, "severity": s, "uitkomst": o, "commit": c, "reden": r}
        for p, ln, s, o, c, r in rows
    ]


def _budget(spec):
    if spec == "forbidden":

        def forbidden() -> tuple[int, str]:
            raise AssertionError("budget must not be consulted")

        return forbidden
    return lambda: tuple(spec)


def _expect_subset(got: dict, want: dict) -> None:
    for key, value in want.items():
        assert got[key] == value, key


def _step(run: Run, step: dict) -> None:
    op = step["op"]
    root = Path(run.sub(step["root"])) if "root" in step else run.repo
    if op == "prepare":
        run.setup(step["setup"])
    elif op == "count_lines":
        assert rg.count_code_lines(step["numstat"]) == step["expect"]
    elif op == "gate":
        out = rg.gate(root, _budget(step["budget"]))
        if "expect_exact" in step:
            assert out == step["expect_exact"]
        _expect_subset(out, run.sub(step.get("expect", {})))
        for key in step.get("absolute", []):
            assert Path(out[key]).is_absolute()
        if step.get("raw_writable"):
            with open(out["raw"], "w") as handle:
                handle.write("review")
        for key, var in step.get("save", {}).items():
            run.vars[var] = out[key]
        if "state_file" in step:
            run.check_state_file(step["state_file"])
    elif op == "classify":
        text = step["text"] if "text" in step else (FIXTURES / step["fixture"]).read_text()
        if "replace" in step:
            text = text.replace(*step["replace"])
        got = rg.classify(text)
        if "expect_exact" in step:
            assert got == step["expect_exact"]
        _expect_subset(got, step.get("expect", {}))
        if "triples" in step:
            assert [[f["path"], f["line"], f["severity"]] for f in got["findings"]] == step["triples"]
        if "text_prefix" in step:
            index, prefix = step["text_prefix"]
            assert got["findings"][index]["text"].startswith(prefix)
    elif op == "fixed":
        got = rg.fixed_findings(root)
        assert [[f["path"], f["line"], f["severity"], f["commit"]] for f in got] == run.sub(step["expect"])
    elif op == "body_line":
        assert rg.body_line(root) == step["expect"]
    elif op == "record":
        kwargs = {
            "skip": step.get("skip"),
            "sha": step.get("sha"),
            "decisions": _rows(step["decisions"]) if "decisions" in step else None,
            "out": Path(run.sub(step["out"])) if "out" in step else None,
            "raw": Path(run.sub(step["raw"])) if "raw" in step else None,
            "range_": run.sub(step["range"]) if "range" in step else None,
        }
        if "error" in step:
            with pytest.raises(ValueError, match=step["error"].replace(".", r"\.") or None):
                rg.record(root, **kwargs)
        else:
            written = rg.record(root, **kwargs)
            if "returns" in step:
                assert str(written) == run.sub(step["returns"])
        run.check_absent(step)
        if "state_file" in step:
            run.check_state_file(step["state_file"])
    elif op == "cli":
        _cli(run, step)
    else:
        raise AssertionError(f"unknown op {op}")


def _cli(run: Run, step: dict) -> None:
    cwd = run.tmp if step.get("cwd") == "tmp" else run.repo
    env = {"PATH": os.environ["PATH"], **GIT_ENV}
    if step.get("cwd") == "tmp":
        env = {"PATH": os.environ["PATH"], "GIT_CEILING_DIRECTORIES": str(run.tmp.parent)}
    result = subprocess.run(
        [sys.executable, str(SCRIPT), *run.sub(step["args"])],
        cwd=cwd, capture_output=True, text=True, env=env,
    )
    assert result.returncode == step["exit"], result.stderr
    if "stdout_json" in step:
        got, want = json.loads(result.stdout), run.sub(step["stdout_json"])
        if step.get("subset"):
            _expect_subset(got, want)
        else:
            assert got == want
    if "stdout" in step:
        assert result.stdout.strip() == step["stdout"]
    if "stderr_starts" in step:
        assert result.stderr.startswith(step["stderr_starts"])
    if step.get("stderr_nonempty"):
        assert result.stderr.strip()
    run.check_absent(step)
    if "state_file" in step:
        run.check_state_file(step["state_file"])


@pytest.mark.parametrize("path", sorted(CASES.glob("*.json")), ids=lambda p: p.stem)
def test_case(path, tmp_path, monkeypatch):
    case = json.loads(path.read_text())
    run = Run(tmp_path, monkeypatch, case)
    run.setup(case["setup"])
    for step in case["steps"]:
        _step(run, step)

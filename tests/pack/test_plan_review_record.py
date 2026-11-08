"""Tests for scripts/plan_review_record.py — the measurement record of the plan review (#972).

Spec: docs/developer/planning/specs/2026-09-25-plan-adversarial-review-design.md,
section 5. Tested: the attempt directory, the content of record.json, the host check
and a write error (simulated): only the staging directory stays, a loud message, exit 4.

The cases with data live as JSON in `fixtures/plan_review_record/`, one file per case, and
the Go tests (`internal/cli/planreviewrecord_test.go`, `internal/review`) read the same files.
A scenario is a list of steps (`run`, `round`, `file`, `commit`, ...) against a throwaway
repo; see `_world.json` for that repo. `${name}` in a string is a variable of the scenario:
`repo`, `tmp`, `tmpdir`, `data`, `home`, `root` (bare world), `PLAN`, `SPEC`, `base_sha`
(HEAD before the steps), `head` (HEAD now), `stdout` (of the last run), and per bound
attempt `a` (its path) and `a.name`. The cases that patch the Python module itself (host
check, a failing rename) stay inline here; Go has its own tests for them.

In lighthouse: the script runs from the installed layout (`install_core`), the
world sets `LIGHTHOUSE_REPO_ROOT`, `LIGHTHOUSE_DATA` and a `lighthouse.toml` with
`language = "nl"` (so the texts below stay those of paia).
"""

from __future__ import annotations

import importlib.util
import json
import pathlib
import re
import subprocess

import pytest

from lighthouse_lib.config import Host
from tests.pack._testkit import INSTALL_ROOT

SCRIPT = INSTALL_ROOT / ".lighthouse" / "scripts" / "plan_review_record.py"
HEADINGS = INSTALL_ROOT / ".lighthouse" / "headings.toml"
FIXTURES = pathlib.Path(__file__).resolve().parent / "fixtures" / "plan_review_record"
WORLD = json.loads((FIXTURES / "_world.json").read_text())
CASES = sorted(p for p in FIXTURES.glob("*.json") if not p.name.startswith("_"))
_SPEC = importlib.util.spec_from_file_location("plan_review_record", SCRIPT)
assert _SPEC and _SPEC.loader
rec = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(rec)
# The scenarios stub `host_refusal`; the host tests below put the real one back.
REAL_HOST_REFUSAL = rec.host_refusal

PLAN = WORLD["plan_path"]
SPEC = WORLD["spec_path"]


def _git(repo: pathlib.Path, *args: str) -> str:
    return subprocess.run(
        ["git", "-C", str(repo), "-c", "user.name=t", "-c", "user.email=t@t", *args],
        capture_output=True,
        text=True,
        check=True,
    ).stdout.strip()


@pytest.fixture(autouse=True)
def config(tmp_path, monkeypatch) -> pathlib.Path:
    """A `lighthouse.toml` with `language = "nl"` outside the repo, and the headings table."""
    toml = tmp_path / "lighthouse.toml"
    toml.write_text(f'[project]\nlanguage = "{WORLD["language"]}"\n')
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(toml))
    monkeypatch.setenv("LIGHTHOUSE_HEADINGS", str(HEADINGS))
    return toml


class Scenario:
    """One fixture run: the world, the variables and the steps."""

    def __init__(self, case, tmp_path, monkeypatch, capsys):
        self.case, self.tmp, self.mp, self.capsys = case, tmp_path, monkeypatch, capsys
        self.vars = {
            "PLAN": PLAN,
            "SPEC": SPEC,
            "tmp": str(tmp_path),
            "tmpdir": str(tmp_path / "tmp"),
            "data": str(tmp_path / "data"),
            "home": str(tmp_path / "home"),
            "repo": str(tmp_path / "repo"),
            "root": str(tmp_path / "root"),
            "stdout": "",
        }
        self.snapshots: dict[str, bytes] = {}
        self.saved_stdout: dict[str, str] = {}
        self.base_sha = ""
        self._build(case)

    def _build(self, case):
        mp, tmp_path = self.mp, self.tmp
        codex_home = tmp_path / "ch"
        codex_home.mkdir()
        (codex_home / "config.toml").write_text(WORLD["codex_config"])
        mp.setenv("HOME", self.vars["home"])
        mp.setenv("TMPDIR", self.vars["tmpdir"])
        mp.setenv("LIGHTHOUSE_DATA", self.vars["data"])
        mp.setenv("CODEX_SPECREVIEW_HOME", str(codex_home))
        mp.setattr(rec, "codex_version", lambda: WORLD["codex_version"])
        if case.get("world") == "bare":
            root = pathlib.Path(self.vars["root"])
            root.mkdir()
            mp.setenv("LIGHTHOUSE_REPO_ROOT", str(root))
        else:
            repo = pathlib.Path(self.vars["repo"])
            for rel, text in WORLD["repo_files"].items():
                (repo / rel).parent.mkdir(parents=True, exist_ok=True)
                (repo / rel).write_text(text)
            _git(repo, "init", "-q")
            _git(repo, "add", "-A")
            _git(repo, "commit", "-qm", "init")
            if case.get("world_commit"):
                self._commit(case["world_commit"], "world")
            self.base_sha = _git(repo, "rev-parse", "HEAD")
            mp.chdir(repo)
            mp.setenv("LIGHTHOUSE_REPO_ROOT", str(repo))
        host = case.get("host", "ok")
        if host == "ok":
            mp.setattr(rec, "host_refusal", lambda kind: None)
        elif host == "refuse":
            mp.setattr(
                rec,
                "host_refusal",
                lambda kind: WORLD["refusal"].replace("${KIND}", kind.upper()),
            )
        else:  # unknown: the real refusal, with a host.toml that is not there
            mp.setenv("LIGHTHOUSE_HOST_TOML", str(tmp_path / "no-host.toml"))
            mp.setattr(rec, "host_refusal", REAL_HOST_REFUSAL)

    def _commit(self, files, message):
        repo = pathlib.Path(self.vars["repo"])
        for rel, text in files.items():
            (repo / self.sub(rel)).parent.mkdir(parents=True, exist_ok=True)
            (repo / self.sub(rel)).write_text(text)
        _git(repo, "add", "-A")
        _git(repo, "commit", "-qm", message)

    def sub(self, value, escape=None):
        if isinstance(value, dict):
            return {self.sub(k): self.sub(v, escape) for k, v in value.items()}
        if isinstance(value, list):
            return [self.sub(v, escape) for v in value]
        if not isinstance(value, str):
            return value

        def one(m):
            text = self.lookup(m.group(1))
            return escape(text) if escape else text

        return re.sub(r"\$\{([A-Za-z_.]+)\}", one, value)

    def lookup(self, name):
        if name == "head":
            return _git(pathlib.Path(self.vars["repo"]), "rev-parse", "HEAD")
        if name == "base_sha":
            return self.base_sha
        if name.endswith(".name"):
            return pathlib.Path(self.vars[name[:-5]]).name
        return self.vars[name]

    def path(self, value) -> pathlib.Path:
        return pathlib.Path(self.sub(value))

    # --- steps
    def run(self, step):
        args = self.sub(step["args"])
        try:
            code = rec.main(args)
        except SystemExit as exc:
            code = exc.code
        captured = self.capsys.readouterr()
        self.vars["stdout"] = captured.out
        exp = step.get("expect", {})
        assert code == exp.get("code", 0), captured.err
        out, err = captured.out, captured.err
        if "stdout_equals" in exp:
            assert out == self.sub(exp["stdout_equals"])
        if "stdout_starts_with" in exp:
            assert out.startswith(self.sub(exp["stdout_starts_with"]))
        for text in exp.get("stdout_contains", []):
            assert self.sub(text) in out
        for text in exp.get("stdout_not_contains", []):
            assert self.sub(text) not in out
        if "stdout_matches" in exp:
            assert re.search(self.sub(exp["stdout_matches"], re.escape), out)
        if "stdout_not_matches" in exp:
            assert not re.search(exp["stdout_not_matches"], out)
        if "stdout_equals_saved" in exp:
            assert out == self.saved_stdout[exp["stdout_equals_saved"]]
        if "stdout_differs_from_saved" in exp:
            assert out != self.saved_stdout[exp["stdout_differs_from_saved"]]
        for text in exp.get("stderr_contains", []):
            assert self.sub(text) in err
        for text in exp.get("stderr_not_contains", []):
            assert text not in err
        if "stderr_starts_with" in exp:
            assert err.startswith(exp["stderr_starts_with"])
        name = step.get("save_stdout") or exp.get("save_stdout")
        if name:
            self.saved_stdout[name] = out
        if "bind" in step:
            self.vars[step["bind"]] = out.strip()
        if "record" in exp or "record_absent" in exp or "dir_files" in exp:
            record_dir = pathlib.Path(out.strip())
            if "dir_files" in exp:
                assert sorted(p.name for p in record_dir.iterdir()) == exp["dir_files"]
            record = json.loads((record_dir / "record.json").read_text())
            for key, want in self.sub(exp.get("record", {})).items():
                assert record[key] == want, key
            for key in exp.get("record_absent", []):
                assert key not in record

    def round(self, step):
        findings = [
            {
                "severity": s,
                "cat": "code",
                "plan_section": "Taak 1",
                "problem": "p",
                "evidence": "e",
                "proposed_fix": "f",
            }
            for s in step["severities"]
        ]
        self.round_raw({**step, "findings": findings})

    def round_raw(self, step):
        doc = {"review_markdown": "x", "status": "Majors only", "findings": step["findings"]}
        if "verified" in step:
            doc["verified"] = step["verified"]
        attempt = pathlib.Path(self.vars[step["attempt"]])
        (attempt / f"round-{step['n']}.json").write_text(json.dumps(doc))

    def file(self, step):
        path = self.path(step["path"])
        path.parent.mkdir(parents=True, exist_ok=True)
        if "json" in step:
            path.write_text(json.dumps(self.sub(step["json"])))
        elif "bytes_hex" in step:
            path.write_bytes(bytes.fromhex(step["bytes_hex"]))
        else:
            path.write_text(self.sub(step["text"]))

    def edit_json(self, step):
        path = self.path(step["path"])
        doc = json.loads(path.read_text())
        for key in step["delete"]:
            del doc[key]
        path.write_text(json.dumps(doc))

    def commit(self, step):
        self._commit(step["files"], step["message"])

    def snapshot(self, step):
        self.snapshots[step["as"]] = self.path(step["path"]).read_bytes()

    def unset_env(self, step):
        self.mp.delenv(step["name"], raising=False)

    def expect_file(self, step):
        path = self.path(step["path"])
        if "exists" in step:
            assert path.exists() == step["exists"], path
        if "text" in step:
            assert path.read_text() == self.sub(step["text"])
        if "json" in step:
            assert json.loads(path.read_text()) == self.sub(step["json"])
        if "json_equals_file" in step:
            other = self.path(step["json_equals_file"])
            assert json.loads(path.read_text()) == json.loads(other.read_text())
        if "same_as" in step:
            assert path.read_bytes() == self.snapshots[step["same_as"]]
        for text, count in step.get("counts", {}).items():
            assert path.read_text().count(text) == count


def _scenario(path: pathlib.Path, tmp_path, monkeypatch, capsys) -> None:
    case = json.loads(path.read_text())
    scenario = Scenario(case, tmp_path, monkeypatch, capsys)
    for step in case["steps"]:
        getattr(scenario, step["op"])(step)


def _open_section_case(case) -> None:
    span = rec.open_section(case["text"])
    assert (list(span) if span is not None else None) == case["span"], case["text"]


def _render_open_case(case, config) -> None:
    config.write_text(f'[project]\nlanguage = "{case["language"]}"\n')
    block = rec.render_open(case["round"], [case["point"]])
    assert block.startswith(case["starts_with"])
    for text in case["contains"]:
        assert text in block
    # `open_section` finds the block again in every language.
    assert rec.open_section(f"# Plan\n\n{block}") is not None


@pytest.mark.parametrize("path", CASES, ids=[p.stem for p in CASES])
def test_fixture_case(path, tmp_path, monkeypatch, capsys, config):
    kind = json.loads(path.read_text()).get("kind", "scenario")
    if kind == "open_section":
        for case in json.loads(path.read_text())["cases"]:
            _open_section_case(case)
    elif kind == "render_open":
        _render_open_case(json.loads(path.read_text()), config)
    else:
        _scenario(path, tmp_path, monkeypatch, capsys)


def test_host_refusal_unknown_host(monkeypatch):
    def unknown(path=None):
        raise rec.HostError("host onbekend: geen host.toml (/x)")

    monkeypatch.setattr(rec, "recognize", unknown)
    text = rec.host_refusal("plan")
    assert text is not None
    assert "PLAN-REVIEW NOT RECORDED" in text
    assert "host onbekend: geen host.toml (/x)" in text
    assert "niet op de Beelink" not in text


def test_host_refusal_other_host(monkeypatch):
    monkeypatch.setattr(rec, "recognize", lambda path=None: None)
    text = rec.host_refusal("spec")
    assert text is not None
    assert "not on the host from host.toml" in text
    assert "falls outside the measurement" in text


def test_host_refusal_recognized(monkeypatch):
    monkeypatch.setattr(
        rec,
        "recognize",
        lambda path=None: Host("beelink", "100.76.126.126"),
    )
    assert rec.host_refusal("plan") is None


def test_recognize_without_tailscale_ip(tmp_path, monkeypatch):
    path = tmp_path / "host.toml"
    path.write_text('[host]\nname = "server"\n', encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_HOST_TOML", str(path))
    monkeypatch.setattr(rec, "is_local", lambda ip: pytest.fail("is_local called"))
    assert rec.recognize() is None


def test_host_refusal_without_tailscale_ip(tmp_path, monkeypatch):
    path = tmp_path / "host.toml"
    path.write_text('[host]\nname = "server"\n', encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_HOST_TOML", str(path))
    text = REAL_HOST_REFUSAL("spec")
    assert text is not None
    assert "not on the host from host.toml" in text


def test_write_failure_leaves_only_staging(tmp_path, capsys, monkeypatch):
    """Only the staging directory stays when the final rename fails (patches the module)."""
    run = Scenario({}, tmp_path, monkeypatch, capsys)
    run.run({"args": ["start", "${PLAN}"], "bind": "a"})
    run.round({"attempt": "a", "n": 1, "severities": []})

    def boom(src, dst):
        raise OSError("schijf vol")

    monkeypatch.setattr(rec.os, "rename", boom)
    run.run(
        {
            "args": ["write", "${a}", "--outcome", "converged", "--review-mode", "codex", "--spec", "${SPEC}"],
            "expect": {"code": 4, "stderr_contains": ["PLAN-REVIEW-RECORD FAILED"]},
        }
    )
    chain = tmp_path / "data" / "plan-review" / "2026-09-25-voorbeeld"
    assert sorted(p.name for p in chain.iterdir()) == [f".{pathlib.Path(run.vars['a']).name}.tmp"]


def test_skill_dirs_are_lighthouse():
    assert rec.DEFAULT_SKILL_DIRS == {
        "plan": ".claude/skills/lighthouse-review-plan",
        "spec": ".claude/skills/lighthouse-review-spec",
    }

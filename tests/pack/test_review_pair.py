"""Tests for scripts/review_pair.py — the paired round-1 shadow run (#988).

Plan: docs/developer/planning/plans/2026-09-27-codex-fallback-meting.md, Task 3–4.
The cases with data live as JSON in `fixtures/review_pair/`, one file per case, and are
shared with the Go tests in `internal/review/pair_test.go`; see `_run_step` for the format.
Inline stay the cases that patch this module itself (`_tree_state` against a real repo,
`_merged_round` failing, `_cell`) and the ones about the CLI. No network: `_coin` and
`_tree_state` are patched. The `status` switch is gone since the decision of 2026-10-04 (#988).

In lighthouse: the script runs from the installed layout (`install_core`);
`tally` and its tests stay in paia. The findings schemas come from `fixtures/`
(verbatim from paia) until the review skills are in the pack.
"""

from __future__ import annotations

import importlib.util
import json
import pathlib
import shutil
import subprocess

import pytest

from tests.pack._testkit import INSTALL_ROOT

SCRIPTS = INSTALL_ROOT / ".lighthouse" / "scripts"
FIXTURES = pathlib.Path(__file__).resolve().parent / "fixtures"
CASES = sorted((FIXTURES / "review_pair").glob("*.json"))


def _load(name: str) -> object:
    spec = importlib.util.spec_from_file_location(name, SCRIPTS / f"{name}.py")
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


rp = _load("review_pair")
rec = _load("plan_review_record")

PLAN_SCHEMA = FIXTURES / "plan-findings.schema.json"
SPEC_SCHEMA = FIXTURES / "spec-findings.schema.json"


@pytest.fixture(autouse=True)
def root(tmp_path, monkeypatch) -> pathlib.Path:
    """A repo root with the findings schemas where `DEFAULT_SKILL_DIRS` looks for them."""
    root = tmp_path / "root"
    for kind, schema in (("plan", PLAN_SCHEMA), ("spec", SPEC_SCHEMA)):
        skill = root / rec.DEFAULT_SKILL_DIRS[kind]
        skill.mkdir(parents=True)
        shutil.copyfile(schema, skill / "findings.schema.json")
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(root))
    return root


def _write_value(path: pathlib.Path, value: object) -> None:
    """A step file: None removes it, {"text": ...} is verbatim, anything else is JSON."""
    if value is None:
        path.unlink(missing_ok=True)
    elif isinstance(value, dict) and set(value) == {"text"}:
        path.write_text(value["text"])
    else:
        path.write_text(json.dumps(value))


def _unescaped_pipes(line: str) -> int:
    count, escaped = 0, False
    for ch in line:
        if escaped:
            escaped = False
        elif ch == "\\":
            escaped = True
        elif ch == "|":
            count += 1
    return count


def _table_row(table: str, blind_id: str) -> str:
    return next(
        line for line in table.splitlines() if line.startswith(f"| {blind_id} |")
    )


def _check_step(step: dict, attempt: pathlib.Path, out: str, err: str) -> None:
    """The expectations of one step: keys are optional and all literal."""
    for name, value in step.get("files", {}).items():
        text = (attempt / name).read_text()
        if isinstance(value, dict) and set(value) == {"text"}:
            assert text == value["text"], name
        else:
            assert json.loads(text) == value, name
    for name in step.get("absent", []):
        assert not (attempt / name).exists(), name
    for name in step.get("present", []):
        assert (attempt / name).exists(), name
    if "stdout" in step:
        assert out == step["stdout"]
    if "stdout_starts" in step:
        assert out.startswith(step["stdout_starts"])
    for needle in step.get("stdout_contains", []):
        assert needle in out, needle
    for needle in step.get("stdout_lacks_ci", []):
        assert needle not in out.lower(), needle
    for line in step.get("stdout_lines", []):
        assert line in out.splitlines(), line
    if "stdout_equals_file" in step:
        assert (attempt / step["stdout_equals_file"]).read_text() == out
    for needle in step.get("stderr_contains", []):
        assert needle in err, needle
    for needle in step.get("stderr_lacks", []):
        assert needle not in err, needle
    if "blind_ids" in step:
        ids = [line.split("|")[1].strip() for line in out.splitlines()[2:]]
        assert ids == step["blind_ids"]
    for blind_id, needle in step.get("row_contains", {}).items():
        assert needle in _table_row(out, blind_id)
    for blind_id, (index, value) in step.get("cell_of", {}).items():
        assert _table_row(out, blind_id).split("|")[index].strip() == value
    if "unescaped_pipes" in step:
        for line in out.splitlines():
            assert _unescaped_pipes(line) == step["unescaped_pipes"], line
    if "merged_findings" in step:
        merged = json.loads((attempt / "round-1.json").read_text())
        got = [[f["problem"], f["severity"]] for f in merged["findings"]]
        assert got == step["merged_findings"]


def _run_case(case: dict, tmp_path: pathlib.Path, monkeypatch, capsys) -> pathlib.Path:
    """One fixture: the attempt, then steps `blind`/`merge` with their expectations.

    A step may set `write` (files before the run: None removes, {"text": ..} is verbatim),
    `coin` (False makes fallback A) and `tree` ({"head", "clean"}); the last two stay.
    """
    state = {"coin": case["coin"], "tree": case["tree"]}
    monkeypatch.setattr(rp, "_coin", lambda: state["coin"])
    monkeypatch.setattr(
        rp, "_tree_state", lambda *_: (state["tree"]["head"], state["tree"]["clean"])
    )
    attempt = tmp_path / "attempt"
    attempt.mkdir()
    (attempt / "attempt.json").write_text(
        json.dumps({"kind": case["kind"], "reviewed_sha": "abc123"})
    )
    for name, value in case["attempt"].items():
        _write_value(attempt / name, value)
    for step in case["steps"]:
        state["coin"] = step.get("coin", state["coin"])
        state["tree"] = step.get("tree", state["tree"])
        for name, value in step.get("write", {}).items():
            _write_value(attempt / name, value)
        assert rp.main([step["cmd"], str(attempt)]) == step["exit"]
        captured = capsys.readouterr()
        _check_step(step, attempt, captured.out, captured.err)
    return attempt


@pytest.mark.parametrize("path", CASES, ids=lambda p: p.stem)
def test_fixture_case(path, tmp_path, monkeypatch, capsys):
    _run_case(json.loads(path.read_text()), tmp_path, monkeypatch, capsys)


def test_merged_round_is_schema_valid(tmp_path, monkeypatch, capsys):
    jsonschema = pytest.importorskip("jsonschema")
    case = json.loads((FIXTURES / "review_pair" / "07-merge-folds-a-duplicate-into-one-finding.json").read_text())
    attempt = _run_case(case, tmp_path, monkeypatch, capsys)
    jsonschema.validate(
        json.loads((attempt / "round-1.json").read_text()),
        json.loads(PLAN_SCHEMA.read_text()),
    )


def test_tree_state_sees_untracked_files(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir()

    def git(*args: str) -> str:
        return subprocess.run(
            [
                "git",
                "-C",
                str(repo),
                "-c",
                "user.name=t",
                "-c",
                "user.email=t@t",
                "-c",
                "commit.gpgsign=false",
                *args,
            ],
            capture_output=True,
            text=True,
            check=True,
        ).stdout.strip()

    git("init", "-q")
    (repo / "a.txt").write_text("a\n")
    git("add", "a.txt")
    git("commit", "-qm", "init")
    sha = git("rev-parse", "HEAD")
    assert rp._tree_state(repo) == (sha, True)
    (repo / "nieuw.txt").write_text("ongevolgd\n")
    assert rp._tree_state(repo) == (sha, False)


def test_cell_escapes_backslash_pipe_and_line_breaks():
    assert rp._cell("x\\|y\r\nz") == "x\\\\\\|y z"


def test_merge_writes_no_sidecar_when_building_the_round_fails(
    tmp_path, monkeypatch, capsys
):
    case = json.loads((FIXTURES / "review_pair" / "07-merge-folds-a-duplicate-into-one-finding.json").read_text())
    case["steps"] = case["steps"][:1]
    attempt = _run_case(case, tmp_path, monkeypatch, capsys)
    (attempt / "round-1.blind-decisions.json").write_text(json.dumps(
        [{"finding_id": "A-f1", "also": "B-f1", "decision": "accept", "reason": "r"},
         {"finding_id": "A-f2", "decision": "minor_no", "reason": "r"},
         {"finding_id": "B-f2", "decision": "accept", "reason": "r"}]))

    def boom(*_args: object) -> dict:
        raise TypeError("kapot")

    monkeypatch.setattr(rp, "_merged_round", boom)
    assert rp.main(["merge", str(attempt)]) == 4
    assert "SHADOW-PAIR FAILED: kapot" in capsys.readouterr().err
    for name in ("round-1.json", "round-1.decisions.json", "round-1.sources.json"):
        assert not (attempt / name).exists()


def test_usage_error_exits_2():
    with pytest.raises(SystemExit) as exc:
        rp.main(["onbekend"])
    assert exc.value.code == 2


def test_outside_a_repo_is_exit_4_not_a_traceback(tmp_path, monkeypatch, capsys):
    """Without a repo root the run fails as SHADOW-PAIR FAILED (exit 4), not with a traceback."""
    case = json.loads((FIXTURES / "review_pair" / "01-blind-labels-without-revealing-the-reviewer.json").read_text())
    case["steps"] = []
    attempt = _run_case(case, tmp_path, monkeypatch, capsys)
    monkeypatch.delenv("LIGHTHOUSE_REPO_ROOT")
    monkeypatch.chdir(tmp_path)
    assert rp.main(["blind", str(attempt)]) == 4
    assert capsys.readouterr().err.startswith("SHADOW-PAIR FAILED: ")

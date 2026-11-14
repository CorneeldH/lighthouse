"""Run `fixtures/worktree_audit/{judge,rows,parse,render}.json` against `worktree_audit.py`.

The proof that the fixtures describe the Python script; the Go twin is `internal/worktreeaudit`.
A row is a JSON object with the field names of `Row`; a missing field has the default of `Row`.
`alive` is `never`, `always` or `unmeasurable`. `config` (judge, render) is the text of
`lighthouse.toml`; without it the protect list is `entire/*` plus `.port-registry`. Everything
runs with `TZ=UTC`.
"""

from __future__ import annotations

import dataclasses
import importlib.util
import json
import sys
import time
from pathlib import Path

import pytest

from tests.pack._testkit import INSTALL_ROOT

SCRIPT = INSTALL_ROOT / ".lighthouse" / "scripts" / "worktree_audit.py"
_SPEC = importlib.util.spec_from_file_location("worktree_audit_cases", SCRIPT)
assert _SPEC and _SPEC.loader
audit = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = audit  # @dataclass resolves its string annotations through sys.modules
_SPEC.loader.exec_module(audit)

FIXTURES = Path(__file__).parent / "fixtures" / "worktree_audit"
PAIA = '[worktree_audit]\nprotected_refs = ["entire/*"]\nprotected_paths = [".port-registry"]\n'
ALIVE = {"never": lambda _pid: False, "always": lambda _pid: True, "unmeasurable": lambda _pid: None}
TUPLES = ("unique_files", "dir_entries")
PROBE_FIELDS = ("dirty", "unpushed", "unique_files", "own_commits", "tip_committed_at", "root_commit")


def _cases(name: str) -> list:
    cases = json.loads((FIXTURES / f"{name}.json").read_text(encoding="utf-8"))
    return [pytest.param(c, id=c["name"]) for c in cases]


@pytest.fixture(autouse=True)
def _utc(monkeypatch):
    monkeypatch.setenv("TZ", "UTC")
    time.tzset()
    yield
    monkeypatch.undo()
    time.tzset()


def _config(tmp_path, monkeypatch, text: str | None) -> None:
    path = tmp_path / "lighthouse.toml"
    path.write_text(PAIA if text is None else text, encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))
    for name in ("DEFAULT_BRANCH", "PROTECTED_REFS", "PROTECTED_PATHS"):
        monkeypatch.setattr(audit, name, getattr(audit, name))
    audit.apply_config()


def _row(raw: dict):
    return audit.Row(**{k: tuple(v) if k in TUPLES and v is not None else v for k, v in raw.items()})


def _as_dict(row) -> dict:
    out = dataclasses.asdict(row)
    for key in TUPLES:
        if out[key] is not None:
            out[key] = list(out[key])
    return out


def _verdict(v) -> dict:
    return {"rank": v.rank, "icon": v.icon, "text": v.text, "modifiers": list(v.modifiers),
            "safe": v.safe, "render": v.render()}


@pytest.mark.parametrize("case", _cases("judge"))
def test_judge(case, tmp_path, monkeypatch):
    _config(tmp_path, monkeypatch, case.get("config"))
    assert _verdict(audit.judge(_row(case["row"]), alive=ALIVE[case["alive"]])) == case["expect"]


def _build(inp: dict, alive):
    probes = {}
    for name, raw in inp["probes"].items():
        kw = {f: raw.get(f) for f in PROBE_FIELDS}
        if kw["unique_files"] is not None:
            kw["unique_files"] = tuple(kw["unique_files"])
        probes[name] = audit.BranchProbe(**kw)
    return audit.build_rows(
        worktrees=[audit.WorktreeInfo(**w) for w in inp["worktrees"]],
        local_branches=inp["local_branches"],
        remote_branches=inp["remote_branches"],
        pulls={k: audit.PullInfo(**v) for k, v in inp["pulls"].items()},
        probes=probes,
        sessions={k: audit.SessionInfo(**v) for k, v in inp["sessions"].items()},
        transcripts={k: tuple(v) for k, v in inp["transcripts"].items()},
        detached_dirty=inp["detached_dirty"],
        orphans=[audit.OrphanDir(o["name"], o["path"], tuple(o["dir_entries"])) for o in inp["orphans"]],
        ports=inp["ports"],
        pr_lookup_ok=inp["pr_lookup_ok"],
        alive=alive,
    )


@pytest.mark.parametrize("case", _cases("rows"))
def test_rows(case, tmp_path, monkeypatch):
    _config(tmp_path, monkeypatch, None)
    rows = _build(case["input"], ALIVE[case["alive"]])
    assert [_as_dict(r) for r in rows] == case["expect"]["rows"]
    judge_alive = ALIVE[case["judge_alive"]]
    assert [_verdict(audit.judge(r, alive=judge_alive)) for r in rows] == case["expect"]["verdicts"]


def _tip_shape(output, monkeypatch):
    monkeypatch.setattr(audit, "run_git", lambda *a, **k: output)
    return list(audit._tip_shape("x", Path("/repo")))


def _parse(case: dict, monkeypatch):
    fn, inp = case["fn"], case["input"]
    if fn == "parse_worktrees":
        return [dataclasses.asdict(w) for w in audit.parse_worktrees(inp["porcelain"])]
    if fn == "parse_branches":
        return audit.parse_branches(inp["text"], remote=inp["remote"])
    if fn == "parse_pulls":
        return {k: dataclasses.asdict(v) for k, v in audit.parse_pulls(inp["payload"]).items()}
    if fn == "parse_port_registry":
        return audit.parse_port_registry(inp["text"])
    if fn == "parse_sessions":
        return {k: dataclasses.asdict(v) for k, v in audit.parse_sessions(inp["records"]).items()}
    if fn == "encode_project_dir":
        return audit.encode_project_dir(inp["path"])
    if fn == "epoch":
        return audit._epoch(inp["stamp"])
    if fn == "tip_shape":
        return _tip_shape(inp["output"], monkeypatch)
    if fn == "parse_issue_pr_heads":
        return audit.parse_issue_pr_heads(inp["payload"], inp["slug"], inp["nr"])
    if fn == "issue_branches":
        return audit.issue_branches([_row(r) for r in inp["rows"]], inp["nr"], inp["pr_heads"])
    if fn == "assert_readonly":
        try:
            audit._assert_readonly(inp["argv"])
        except ValueError as err:
            return {"error": str(err)}
        return {"ok": True}
    raise AssertionError(f"unknown fn {fn}")


@pytest.mark.parametrize("case", _cases("parse"))
def test_parse(case, monkeypatch):
    assert _parse(case, monkeypatch) == case["expect"]


def _render(case: dict) -> dict:
    rows = [_row(r) for r in case["rows"]]
    alive = ALIVE[case["alive"]]
    fn = case["fn"]
    if fn == "only":
        text, ok = audit.render_only(rows, case["branch"], cwd=Path(case["cwd"]), alive=alive)
        return {"text": text, "ok": ok}
    if fn == "issue":
        return {"text": audit.render_issue(rows, case["nr"], case["pr_heads"], cwd=Path(case["cwd"]), alive=alive)}
    view = {"cleanup": audit.render_cleanup, "sessions": audit.render_sessions,
            "origin": audit.render_origin, "kpi": audit.render_kpi}[fn]
    return {"text": view(rows, alive=alive)}


@pytest.mark.parametrize("case", _cases("render"))
def test_render(case, tmp_path, monkeypatch):
    _config(tmp_path, monkeypatch, case.get("config"))
    assert _render(case) == case["expect"]

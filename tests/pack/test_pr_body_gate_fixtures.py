"""Shared fixtures of the PR body gate: `fixtures/pr_body_gate/cases.json`.

The Go twin (`internal/prbody`) reads the same file, so the two stay equal.
Every case has `name`, `kind`, `expect` and the input of its kind:

- `closes`, `refs`, `refs_targets`, `no_issue`: `body` (`expect`: a list of
  numbers, or a bool);
- `evidence_rows`: `body`, `nr` (`expect`: a list of `[ac, command, result]`, or
  `{"reason": …}`);
- `count_acs`: `body` (`expect`: a number or `null`);
- `check_body`: `body`, or `body_b64` (the bytes of a body file, read the way
  `--body-file` is read: UTF-8, invalid bytes become U+FFFD, `\\r\\n` becomes
  `\\n`), and `counts` (issue number as text to `[count|null, cause]`; a number
  that is not in the map is a test failure: the AC count must not be asked for)
  (`expect`: `{"blockers": […], "notices": […]}`);
- `find`: `command`, `cwd` (`{root}` is the temporary directory, `null` is an
  unknown cwd) and optional `setup` (the steps of `build_fixture_setup`)
  (`expect`: a list of `{cwd, repo, body, body_file, problem, repo_problem}`,
  or `"unparseable"`);
- `normalize_repo`: `value` (`expect`: text or `null`);
- `scope_repo`: `value` (`expect`: `[decision, reason]`).

`repo` is `forge.repo` of the `lighthouse.toml` of the case: absent is
`Acme/widgets`, `null` is no `forge.repo`.
"""

import base64
import json
from pathlib import Path

import pytest

from tests.pack._testkit import (
    DEFAULT_FIXTURE_REPO,
    build_fixture_setup,
    fill_fixture,
    load_hook_lib,
    write_fixture_config,
)

gate = load_hook_lib("pr_body_gate")

CASES = json.loads(
    (Path(__file__).parent / "fixtures" / "pr_body_gate" / "cases.json").read_text(
        encoding="utf-8"
    )
)


def _rows(found):
    if isinstance(found, str):
        return {"reason": found}
    return [[row.ac, row.command, row.result] for row in found]


def _check_body(case, tmp_path):
    if "body_b64" in case:
        path = tmp_path / "body.md"
        path.write_bytes(base64.b64decode(case["body_b64"]))
        body = path.read_text(encoding="utf-8", errors="replace")  # as `_check_create`
    else:
        body = case["body"]
    counts = {int(nr): tuple(value) for nr, value in case["counts"].items()}

    def ac_count(nr):
        if nr not in counts:
            pytest.fail(f"ac_count called for #{nr}")
        return counts[nr]

    verdict = gate.check_body(body, ac_count)
    return {"blockers": verdict.blockers, "notices": verdict.notices}


def _find(case, tmp_path):
    root = str(tmp_path)  # deliberately without realpath
    build_fixture_setup(tmp_path, case.get("setup", []))
    cwd = None if case["cwd"] is None else fill_fixture(case["cwd"], root)
    try:
        found = gate.find_pr_creates(case["command"], cwd)
    except gate.Unparseable:
        return "unparseable"
    return [
        {
            "cwd": pc.cwd,
            "repo": pc.repo,
            "body": pc.body,
            "body_file": pc.body_file,
            "problem": pc.problem,
            "repo_problem": pc.repo_problem,
        }
        for pc in found
    ]


@pytest.mark.parametrize("case", CASES, ids=lambda c: c["name"])
def test_fixture_case(case, tmp_path, monkeypatch):
    write_fixture_config(tmp_path, case.get("repo", DEFAULT_FIXTURE_REPO))
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(tmp_path))
    monkeypatch.delenv("LIGHTHOUSE_TOML", raising=False)
    monkeypatch.delenv("GH_REPO", raising=False)
    kind = case["kind"]
    expect = fill_fixture(case["expect"], str(tmp_path))
    if kind == "closes":
        got = gate.closes_targets(case["body"])
    elif kind == "refs":
        got = gate.refs_issues(case["body"])
    elif kind == "refs_targets":
        got = gate.refs_targets(case["body"])
    elif kind == "no_issue":
        got = gate.no_issue(case["body"])
    elif kind == "evidence_rows":
        got = _rows(gate.evidence_rows(case["body"], case["nr"]))
    elif kind == "count_acs":
        got = gate.count_acs(case["body"])
    elif kind == "check_body":
        got = _check_body(case, tmp_path)
    elif kind == "find":
        got = _find(case, tmp_path)
    elif kind == "normalize_repo":
        got = gate._normalize_repo(case["value"])
    elif kind == "scope_repo":
        got = list(gate._scope_repo(case["value"]))
    else:
        raise AssertionError(f"unknown kind {kind}")
    assert got == expect

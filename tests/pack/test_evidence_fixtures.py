"""Shared fixtures of the Evidence allowlist: `fixtures/evidence/cases.json`.

The Go twin (`internal/evidence`) reads the same file, so the two stay equal.
Every case has `name`, `kind`, `cell` and `expect`:

- `split`: `split_command` (`expect`: the argv, or `{"reason": …}`);
- `check`: `split_command` and then `check`, as `evidence.py check` does
  (`expect`: `null`, or the reason), with an optional `evidence_test_dir`
  (`[test_gate] evidence_test_dir` of the case);
- `expected`: `expected` (`expect`: `[fragment, after_pr]`);
- `outcome_of`: `outcome_of` (`expect`: the outcome key, or `null`).

`allowlist.json` holds the allowlist constants of `evidence.py`, each as a sorted
list.
"""

import json
from pathlib import Path

import pytest

from tests.pack._testkit import load_hook_lib, write_fixture_config

ev = load_hook_lib("evidence")

FIXTURES = Path(__file__).parent / "fixtures" / "evidence"
CASES = json.loads((FIXTURES / "cases.json").read_text(encoding="utf-8"))
ALLOWLIST = json.loads((FIXTURES / "allowlist.json").read_text(encoding="utf-8"))


@pytest.mark.parametrize("case", CASES, ids=lambda c: c["name"])
def test_fixture_case(case, tmp_path, monkeypatch):
    write_fixture_config(tmp_path, test_dir=case.get("evidence_test_dir"))
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(tmp_path))
    monkeypatch.delenv("LIGHTHOUSE_TOML", raising=False)
    kind = case["kind"]
    if kind == "split":
        argv = ev.split_command(case["cell"])
        got = {"reason": argv} if isinstance(argv, str) else argv
    elif kind == "check":
        argv = ev.split_command(case["cell"])
        got = argv if isinstance(argv, str) else ev.check(argv)
    elif kind == "expected":
        got = list(ev.expected(case["cell"]))
    elif kind == "outcome_of":
        outcome = ev.outcome_of(case["cell"])
        got = outcome
    else:
        raise AssertionError(f"unknown kind {kind}")
    assert got == case["expect"]


@pytest.mark.parametrize("name", sorted(ALLOWLIST))
def test_allowlist_fixture_matches_the_code(name):
    assert sorted(getattr(ev, name)) == ALLOWLIST[name]

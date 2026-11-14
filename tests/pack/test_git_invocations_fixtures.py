"""Shared fixtures of the commit parser: `fixtures/git_invocations/cases.json`.

The Go twin (`internal/gitinvocations`) reads the same file, so the two stay
equal. The format is described in the plan of #277 (task 1). Placeholders:
`{root}` is the temporary directory (not passed through realpath) and
`{home:<name>}` is the home directory of user `<name>`.
"""

import json
import os
from pathlib import Path

import pytest

from tests.pack._testkit import build_fixture_setup, fill_fixture, load_hook_lib

gi = load_hook_lib("git_invocations")

CASES = json.loads(
    (Path(__file__).parent / "fixtures" / "git_invocations" / "cases.json").read_text(
        encoding="utf-8"
    )
)


@pytest.mark.parametrize("case", CASES, ids=lambda c: c["name"])
def test_fixture_case(case, tmp_path, monkeypatch):
    root = str(tmp_path)  # deliberately without realpath
    monkeypatch.delenv("LH_UNSET_VAR", raising=False)
    build_fixture_setup(tmp_path, case["setup"])
    for key, value in case["env"].items():
        monkeypatch.setenv(key, fill_fixture(value, root))
    expect = fill_fixture(case["expect"], root)
    kind = case["kind"]
    if kind == "repo_of":
        assert gi.repo_of(os.path.join(root, case["path"])) == expect
        return
    command = fill_fixture(case["command"], root)
    cwd = os.path.join(root, case["cwd"]) if case["cwd"] else root
    if expect == "unparseable":
        with pytest.raises(gi.Unparseable):
            if kind == "find":
                gi.find_invocations(command, cwd)
            else:
                list(gi.walk(command, cwd))
        return
    if kind == "find":
        found = gi.find_invocations(
            command,
            cwd,
            report_new_dirs=case["report_new_dirs"],
            default_branch=case["default_branch"],
        )
        assert [list(f) for f in found] == expect
    elif kind == "walk":
        # `dirs` is live: read it during the iteration, not after list().
        got = [[s.words[s.idx], s.dirs["here"]] for s in gi.walk(command, cwd)]
        assert got == expect
    elif kind == "git_target":
        step = next(gi.walk(command, cwd))
        target, _raw, _new, action_idx = gi.git_target(step)
        # `known: false` (optional, default true) is a target that is None.
        assert {
            "target": target,
            "known": target is not None,
            "action_word": step.words[action_idx],
        } == {"known": True, **expect}
    else:
        raise AssertionError(f"unknown kind {kind}")

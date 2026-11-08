"""Shared fixtures of the commit parser: `fixtures/git_invocations/cases.json`.

The Go twin (`internal/gitinvocations`) reads the same file, so the two stay
equal. The format is described in the plan of #277 (task 1). Placeholders:
`{root}` is the temporary directory (not passed through realpath) and
`{home:<name>}` is the home directory of user `<name>`.
"""

import json
import os
import pwd
import re
from pathlib import Path

import pytest

from tests.pack._testkit import init_repo, load_hook_lib

gi = load_hook_lib("git_invocations")

CASES = json.loads(
    (Path(__file__).parent / "fixtures" / "git_invocations" / "cases.json").read_text(
        encoding="utf-8"
    )
)


def _fill(value, root):
    if isinstance(value, str):
        value = value.replace("{root}", root)
        return re.sub(r"\{home:([^}]+)\}", lambda m: pwd.getpwnam(m[1]).pw_dir, value)
    if isinstance(value, list):
        return [_fill(v, root) for v in value]
    if isinstance(value, dict):
        return {k: _fill(v, root) for k, v in value.items()}
    return value


def _build(setup, root: Path):
    for step in setup:
        if "repo" in step:
            init_repo(root / step["repo"], branch=step["branch"])
        elif "dir" in step:
            (root / step["dir"]).mkdir(parents=True, exist_ok=True)
        elif "file" in step:
            path = root / step["file"]
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(step["text"], encoding="utf-8")
        elif "symlink" in step:
            (root / step["symlink"]).symlink_to(root / step["to"])
        elif "overwrite" in step:
            (root / step["overwrite"]).write_text(step["text"], encoding="utf-8")
        else:
            raise AssertionError(f"unknown setup step {step}")


@pytest.mark.parametrize("case", CASES, ids=lambda c: c["name"])
def test_fixture_case(case, tmp_path, monkeypatch):
    root = str(tmp_path)  # deliberately without realpath
    monkeypatch.delenv("LH_UNSET_VAR", raising=False)
    _build(case["setup"], tmp_path)
    for key, value in case["env"].items():
        monkeypatch.setenv(key, _fill(value, root))
    expect = _fill(case["expect"], root)
    kind = case["kind"]
    if kind == "repo_of":
        assert gi.repo_of(os.path.join(root, case["path"])) == expect
        return
    command = _fill(case["command"], root)
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
        assert {"target": target, "action_word": step.words[action_idx]} == expect
    else:
        raise AssertionError(f"unknown kind {kind}")

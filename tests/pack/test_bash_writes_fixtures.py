"""Shared fixtures of the read layer of `bash_writes`: `fixtures/bash_writes/cases.json`.

The Go twin (`internal/bashwrites`) reads the same file, so the two stay
equal. The format is described in the plan of #278 (task 1): a JSON array of
objects with `name`, `kind`, `expect` and, per kind, the input:

- `porcelain`, `diff_tree`: `input` (text, `\\u0000` as separator) ->
  `{"changed": [...], "deleted": [...]}`, sets: order free.
- `reflog`: `input` -> a list `[sha, epoch, subject]`.
- `commits`: `entries` (list `[sha, epoch, subject]`), `stop`, `since` ->
  `{"commits": [[sha, is_merge]], "notes": [...]}`.
- `classify`: `rc`, `output` -> `"ok"`, `"finding"` or `"unchecked"`.
- `marker_key`: `payload` -> the key.
- `derive_roots`: `setup`, `env`, `command`, `cwd` -> list of roots, order free
  (compared sorted after the placeholders are filled).
- `project_filter`: `setup`, `roots`, `project` ->
  `{"kept": [...], "error_prefixes": [...]}`, or `"raises"`. An error carries
  the stderr of git, which differs per environment: compare on the prefix.

`setup` is built by `build_fixture_setup`. Placeholders: `{root}` is the
temporary directory (not passed through realpath), `{real:<path>}` the realpath
of `{root}/<path>` and `{home:<name>}` the home of user `<name>`.

The dir state of `walk` is live (`Step.dirs`): the `derive_roots` cases that
read it per step each end with a `cd` out of every repo, so an implementation
that reads it only afterwards loses the earlier root.
"""

import json
import time
from pathlib import Path

import pytest

from tests.pack._testkit import build_fixture_setup, fill_fixture, load_hook_lib

bw = load_hook_lib("bash_writes")

CASES = json.loads(
    (Path(__file__).parent / "fixtures" / "bash_writes" / "cases.json").read_text(
        encoding="utf-8"
    )
)


def _sorted_pair(pair):
    changed, deleted = pair
    return {"changed": sorted(changed), "deleted": sorted(deleted)}


@pytest.mark.parametrize("case", CASES, ids=lambda c: c["name"])
def test_fixture_case(case, tmp_path, monkeypatch):
    root = str(tmp_path)  # deliberately without realpath
    kind = case["kind"]
    expect = fill_fixture(case["expect"], root)
    if kind == "porcelain":
        assert _sorted_pair(bw.parse_porcelain(case["input"].encode())) == expect
    elif kind == "diff_tree":
        assert _sorted_pair(bw.parse_diff_tree(case["input"].encode())) == expect
    elif kind == "reflog":
        assert [list(e) for e in bw.parse_reflog(case["input"])] == expect
    elif kind == "commits":
        entries = [bw.ReflogEntry(*e) for e in case["entries"]]
        commits, notes = bw.commits_to_check(entries, case["stop"], case["since"])
        assert {"commits": [list(c) for c in commits], "notes": notes} == expect
    elif kind == "classify":
        assert bw.classify(case["rc"], case["output"].encode()) == expect
    elif kind == "marker_key":
        assert bw.marker_key(case["payload"]) == expect
    elif kind == "derive_roots":
        build_fixture_setup(tmp_path, case["setup"])
        for key, value in case["env"].items():
            monkeypatch.setenv(key, fill_fixture(value, root))
        cwd = fill_fixture(case["cwd"], root)
        assert sorted(bw.derive_roots(fill_fixture(case["command"], root), cwd)) == sorted(expect)
    elif kind == "project_filter":
        build_fixture_setup(tmp_path, case["setup"])
        roots = set(fill_fixture(case["roots"], root))
        project = fill_fixture(case["project"], root)
        deadline = time.time() + 5
        if expect == "raises":
            with pytest.raises(bw.GitError):
                bw.project_filter(roots, project, deadline=deadline)
            return
        kept, errors = bw.project_filter(roots, project, deadline=deadline)
        assert sorted(kept) == sorted(expect["kept"])
        assert len(errors) == len(expect["error_prefixes"])
        for error, prefix in zip(sorted(errors), sorted(expect["error_prefixes"])):
            assert error.startswith(prefix)
    else:
        raise AssertionError(f"unknown kind {kind}")

"""Shared fixtures of `repo_identity`: `fixtures/repo_identity/cases.json`.

The Go twin reads the same file, so the two stay equal. The format is described
in the plan of #278 (task 1): a JSON array of objects with `name`, `kind`,
`expect` and, per kind, the input:

- `remote_key`: `url` -> the key, or `null`.
- `same_origin`: `setup`, `project`, `root` -> `[bool, reason]`, or `"raises"`.

`setup` is built by `build_fixture_setup`; placeholders as `fill_fixture` in
`_testkit.py` fills them.
"""

import json
from pathlib import Path

import pytest

from tests.pack._testkit import build_fixture_setup, fill_fixture, load_hook_lib

ri = load_hook_lib("repo_identity")

CASES = json.loads(
    (Path(__file__).parent / "fixtures" / "repo_identity" / "cases.json").read_text(
        encoding="utf-8"
    )
)


@pytest.mark.parametrize("case", CASES, ids=lambda c: c["name"])
def test_fixture_case(case, tmp_path):
    root = str(tmp_path)  # deliberately without realpath
    kind = case["kind"]
    if kind == "remote_key":
        assert ri.remote_key(case["url"]) == case["expect"]
    elif kind == "same_origin":
        build_fixture_setup(tmp_path, case["setup"])
        project = fill_fixture(case["project"], root)
        other = fill_fixture(case["root"], root)
        if case["expect"] == "raises":
            with pytest.raises(ri.OriginError):
                ri.same_origin(project, other, timeout=5)
            return
        assert list(ri.same_origin(project, other, timeout=5)) == case["expect"]
    else:
        raise AssertionError(f"unknown kind {kind}")

"""Shared fixtures of the commit test gate: `fixtures/{no_verify,host_memory,commit_base}/cases.json`.

The Go twin (`internal/commitgate`) reads the same files, so the two stay equal.
Each file is a JSON array of objects with `name`, `kind`, `expect` and, per kind,
the input:

- `no_verify`: `command` -> `expect: bool` (is there a `--no-verify` or `-n` on a
  `git commit` of the command).
- `meminfo`: `text` -> `{field: kB}`, or `"raises"` (no `MemAvailable`).
- `shortfalls`: `info`, `min_available_gib` (number or `null`), `min_swap_free_gib` ->
  `{"found": [...], "notes": [...]}`.
- `record`: `setup`, `roots` -> `{"heads": {root: sha}}`, `"unborn"` for a repo
  without commits.
- `moved`: `setup`, `roots`, `commits` (`setup` steps that run after `record`) ->
  a list of `[root, base, head]` in the order of `roots`; `expect_second` is what a
  second `moved` gives (the marker is gone).
- `changed_files`: `setup`, `repo`, `base`, `head` -> the sorted paths, both sides of
  a rename.

`setup` is built by `build_fixture_setup`; placeholders are filled by `fill_fixture`
after the last step of the case: `{root}`, `{rev:<path>:<rev>}`, `{empty_tree}`.
`marker_python.json` is a marker as `json.dumps` writes it, for one root `{root}/café`.
"""

import json
from pathlib import Path

import pytest

from tests.pack._testkit import build_fixture_setup, fill_fixture, git, load_hook_lib

nv = load_hook_lib("no_verify")
hm = load_hook_lib("host_memory")
cb = load_hook_lib("commit_base")

FIXTURES = Path(__file__).parent / "fixtures"


def _load(name: str) -> list[dict]:
    return json.loads((FIXTURES / name / "cases.json").read_text(encoding="utf-8"))


CASES = _load("no_verify") + _load("host_memory") + _load("commit_base")
PAYLOAD = {"session_id": "s1", "tool_use_id": "t1"}


@pytest.fixture(autouse=True)
def runtime_dir(tmp_path, monkeypatch):
    runtime = tmp_path / "runtime"
    runtime.mkdir()
    monkeypatch.setenv("XDG_RUNTIME_DIR", str(runtime))
    return runtime


@pytest.mark.parametrize("case", CASES, ids=lambda c: c["name"])
def test_fixture_case(case, tmp_path, monkeypatch):
    root = str(tmp_path)  # deliberately without realpath
    kind = case["kind"]
    if kind == "no_verify":
        assert nv.command_has_no_verify(case["command"]) is case["expect"]
    elif kind == "meminfo":
        path = tmp_path / "meminfo"
        path.write_text(case["text"])
        if case["expect"] == "raises":
            with pytest.raises(ValueError):
                hm.read_meminfo(path)
        else:
            assert hm.read_meminfo(path) == case["expect"]
    elif kind == "shortfalls":
        found, notes = hm.shortfalls(
            case["info"], case["min_available_gib"], case["min_swap_free_gib"]
        )
        assert {"found": found, "notes": notes} == case["expect"]
    elif kind == "record":
        build_fixture_setup(tmp_path, case["setup"])
        cb.record(PAYLOAD, fill_fixture(case["roots"], root))
        marker = cb.state_dir("s1", create=False) / "t1.json"
        assert json.loads(marker.read_text(encoding="utf-8")) == fill_fixture(case["expect"], root)
    elif kind == "moved":
        build_fixture_setup(tmp_path, case["setup"])
        cb.record(PAYLOAD, fill_fixture(case["roots"], root))
        build_fixture_setup(tmp_path, case["commits"])
        assert [list(m) for m in cb.moved(PAYLOAD)] == fill_fixture(case["expect"], root)
        if "expect_second" in case:
            assert [list(m) for m in cb.moved(PAYLOAD)] == fill_fixture(case["expect_second"], root)
    elif kind == "changed_files":
        build_fixture_setup(tmp_path, case["setup"])
        got = cb.changed_files(
            fill_fixture(case["repo"], root),
            fill_fixture(case["base"], root),
            fill_fixture(case["head"], root),
        )
        assert got == fill_fixture(case["expect"], root)
    else:
        raise AssertionError(f"unknown kind {kind}")


def test_moved_reads_a_marker_python_wrote_with_a_non_ascii_root(tmp_path):
    """`marker_python.json` is the shape `json.dumps` writes (`\\u00e9`): `moved` reads it."""
    root = str(tmp_path)
    text = (FIXTURES / "commit_base" / "marker_python.json").read_text(encoding="utf-8")
    assert "\\u00e9" in text
    repo = tmp_path / "café"
    repo.mkdir()
    git(repo, "init", "-q", "-b", "main")
    (repo / "a.txt").write_text("x\n")
    git(repo, "add", "-A")
    git(repo, "commit", "-qm", "m")
    marker = cb.state_dir("s1") / "t1.json"
    marker.write_text(fill_fixture(text, root), encoding="ascii")
    rows = cb.moved(PAYLOAD)
    assert [list(m) for m in rows] == [
        [str(repo), "1" * 40, fill_fixture("{rev:café:HEAD}", root)]
    ]


def test_record_writes_a_non_ascii_root_as_the_fixture_does(tmp_path):
    """What `record` writes is the format of `marker_python.json`: ASCII with `\\u` escapes."""
    root = str(tmp_path)
    repo = tmp_path / "café"
    repo.mkdir()
    git(repo, "init", "-q", "-b", "main")
    (repo / "a.txt").write_text("x\n")
    git(repo, "add", "-A")
    git(repo, "commit", "-qm", "m")
    cb.record(PAYLOAD, [str(repo)])
    written = (cb.state_dir("s1", create=False) / "t1.json").read_text(encoding="ascii")
    sha = fill_fixture("{rev:café:HEAD}", root)
    template = (FIXTURES / "commit_base" / "marker_python.json").read_text(encoding="utf-8")
    assert written == fill_fixture(template, root).replace("1" * 40, sha).rstrip("\n")

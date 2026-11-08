"""The shared fixtures of the two KPI scripts, proved against the Python originals.

`tests/pack/fixtures/kpi/` holds every expectation as data: `classify.json` for the regex
classifiers, one directory per transcript case with its `expect.json`. The Go port reads the same
files, so a row is only worth anything if the Python gives exactly this answer. This module runs the
Python scripts over them and compares `rc`, `stdout` and `stderr` byte for byte.
"""

from __future__ import annotations

import importlib.util
import json
import re
import sys
from pathlib import Path

import pytest

from tests.pack._testkit import INSTALL_ROOT

KPI = Path(__file__).resolve().parent / "fixtures" / "kpi"
TESTS = Path(__file__).resolve().parent
SCRIPTS = INSTALL_ROOT / ".lighthouse" / "scripts"


def _load(name: str):
    spec = importlib.util.spec_from_file_location(name, SCRIPTS / f"{name}.py")
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module  # dataclasses looks the module up via __module__
    spec.loader.exec_module(module)
    return module


start_issue = _load("measure_start_issue_kpi")
fix_pr = _load("measure_fix_pr_kpi")

MODULES = {"start-issue": start_issue, "fix-pr": fix_pr}

# Tests that become Go tests (tasks 4 and 5) instead of a fixture, and tests that disappear with
# the Python without a replacement.
GO_TESTS = {
    "test_transcript_dir_from_git_root",
    "test_since_option",
    "test_paths_dirs_from_config",
    "test_git_failure_is_rc_2_without_traceback",
}
DROPPED_TESTS = {
    "test_help_is_english",
    "test_skill_names_are_lighthouse",
    "test_loads_start_issue_kpi_as_its_neighbour",
    "test_runs_as_installed_file",
}


@pytest.fixture(autouse=True)
def _kpi_environment(monkeypatch):
    """The environment both runners use: no repo root, the two fixture files for config and headings."""
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", "")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(KPI / "lighthouse.toml"))
    monkeypatch.setenv("LIGHTHOUSE_HEADINGS", str(KPI / "headings.toml"))
    for cached in (start_issue.gate_re, start_issue.gate_suffix_re, start_issue.scout_skip_re):
        cached.cache_clear()


def _classify(fn: str, text: str):
    if fn == "pickup":
        return start_issue.pickup_issue_number(text) is not None
    if fn == "names_skill":
        return start_issue.names_a_skill(text)
    if fn == "has_issue_number":
        return start_issue.has_issue_number(text)
    if fn == "fix_pr":
        return fix_pr.fix_pr_number(text) is not None
    if fn == "mutating":
        return fix_pr.is_mutating("Bash", {"command": text})
    if fn == "diagnostic":
        return fix_pr.is_diagnostic("Bash", {"command": text})
    if fn == "gate":
        match = start_issue.gate_re().search(text)
        return int(match.group("gate")) if match else None
    if fn == "subgate":
        match = start_issue.gate_suffix_re().search(text)
        return match.group("gate") + (match.group("sub") or "") if match else None
    if fn == "skip_reason":
        match = start_issue.scout_skip_re().search(text)
        reason = match.group("reden").strip(" .") if match else ""
        return len(reason) >= start_issue.SCOUT_MIN_REASON
    raise AssertionError(f"unknown classifier {fn!r}")


def _rows() -> list[dict]:
    return json.loads((KPI / "classify.json").read_text(encoding="utf-8"))


def _cases(kind: str) -> list[Path]:
    return sorted(path.parent for path in (KPI / kind).glob("*/expect.json"))


def test_classify_rows_match_python() -> None:
    rows = _rows()
    assert rows, "classify.json is empty"
    for row in rows:
        got = _classify(row["fn"], row["input"])
        assert got == row["want"] and type(got) is type(row["want"]), (
            f"{row['fn']}({row['input']!r}): python gives {got!r}, fixture says {row['want']!r}"
        )


def _run_case(kind: str, case: Path, monkeypatch, capsys) -> None:
    monkeypatch.chdir(case)
    expected = json.loads((case / "expect.json").read_text(encoding="utf-8"))
    assert expected, f"{case.name}: no runs"
    for item in expected:
        rc = MODULES[kind].main(list(item["args"]))
        captured = capsys.readouterr()
        label = f"{kind}/{case.name} {item['args']}"
        assert rc == item["rc"], label
        assert captured.out == item["stdout"], label
        assert captured.err == item["stderr"], label


@pytest.mark.parametrize("case", _cases("start-issue"), ids=lambda p: p.name)
def test_start_issue_cases_match_python(case: Path, monkeypatch, capsys) -> None:
    _run_case("start-issue", case, monkeypatch, capsys)


@pytest.mark.parametrize("case", _cases("fix-pr"), ids=lambda p: p.name)
def test_fix_pr_cases_match_python(case: Path, monkeypatch, capsys) -> None:
    _run_case("fix-pr", case, monkeypatch, capsys)


def test_every_old_test_has_a_home() -> None:
    """Each test of the two old files lives in a fixture `from`, or is named in a list above."""
    homes: set[str] = set(GO_TESTS) | set(DROPPED_TESTS)
    homes |= {row["from"] for row in _rows()}
    for kind in MODULES:
        for case in _cases(kind):
            for item in json.loads((case / "expect.json").read_text(encoding="utf-8")):
                homes |= set(item["from"])

    old: set[str] = set()
    for name in ("test_start_issue_kpi.py", "test_fix_pr_kpi.py"):
        old |= set(re.findall(r"^def (test_\w+)", (TESTS / name).read_text(encoding="utf-8"), re.M))
    assert old, "found no old tests to place"

    assert sorted(old - homes) == []

"""`pack/core/lib/md_links.py`, as installed (#886): shared link extraction for markdown.

One module for the norm "links in files are relative and point to an existing
target; absolute links are a problem". Used by the rewritten
`md-link-check.sh` (task 5, through the CLI) and by the docs link tests
(task 6, through `strip_code`, `relative_links` and `absolute_links`); the
latter replaces its own `_strip_code_fences` in `test_docs_planning.py` with
this module.
"""

import json
import os
import subprocess
import sys
from pathlib import Path

import pytest

from tests.pack._testkit import INSTALLED_LIB, load_hook_lib

ml = load_hook_lib("md_links")
# The cases live in a fixture the Go port of this module reads too (`internal/mdlinks`).
FIXTURES = Path(__file__).resolve().parent / "fixtures" / "md_links" / "links.json"


def _cases(section: str) -> list:
    cases = json.loads(FIXTURES.read_text(encoding="utf-8"))[section]
    return [pytest.param(case, id=case["name"]) for case in cases]


@pytest.mark.parametrize("case", _cases("strip_code"))
def test_strip_code(case):
    assert ml.strip_code(case["text"]) == case["expect"]


@pytest.mark.parametrize("case", _cases("relative"))
def test_relative_links(case):
    assert ml.relative_links(case["text"]) == case["expect"]


@pytest.mark.parametrize("case", _cases("absolute"))
def test_absolute_links(case):
    assert ml.absolute_links(case["text"]) == case["expect"]


@pytest.mark.parametrize("case", _cases("problems"))
def test_find_problems(case, tmp_path):
    for rel, content in case["files"].items():
        path = tmp_path / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8", newline="")
    for rel in case["dirs"]:
        (tmp_path / rel).mkdir(parents=True, exist_ok=True)
    found = ml.find_problems(tmp_path / case["md"], tmp_path)
    assert [list(problem) for problem in found] == case["expect"]


def test_cli_prints_tab_lines_and_fails_on_unreadable(tmp_path):
    (tmp_path / "docs").mkdir()
    md = tmp_path / "docs" / "n.md"
    md.write_text("[b](weg.md)\n")
    result = subprocess.run(
        [sys.executable, str(INSTALLED_LIB / "md_links.py"), str(md), str(tmp_path)],
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0
    assert "dead\tweg.md" in result.stdout

    if os.geteuid() == 0:
        pytest.skip("root leest ook chmod 000 — geen zinvolle toets")

    md.chmod(0o000)
    try:
        bad = subprocess.run(
            [
                sys.executable,
                str(INSTALLED_LIB / "md_links.py"),
                str(md),
                str(tmp_path),
            ],
            capture_output=True,
            text=True,
            check=False,
        )
        assert bad.returncode == 1
        assert "could not read" in bad.stderr
    finally:
        md.chmod(0o644)

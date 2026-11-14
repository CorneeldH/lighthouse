"""The `classify` of `_testkit` against the shared fixtures: `fixtures/bash_writes/cases.json`.

The language-pack tests read the verdict of one gate with this helper; the Go
twin (`internal/bashwrites`) reads the same `classify` cases, so the two stay equal.
"""

import json
from pathlib import Path

import pytest

from tests.pack._testkit import classify

CASES = json.loads(
    (Path(__file__).parent / "fixtures" / "bash_writes" / "cases.json").read_text(
        encoding="utf-8"
    )
)


@pytest.mark.parametrize(
    "case", [c for c in CASES if c["kind"] == "classify"], ids=lambda c: c["name"]
)
def test_classify_matches_the_shared_fixtures(case):
    """The `classify` of `_testkit` (what the language-pack tests use) gives the shared verdicts."""
    assert classify(case["rc"], case["output"].encode()) == case["expect"]

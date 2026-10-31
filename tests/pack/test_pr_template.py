"""`pack/core/templates/pull_request_template.md`: the PR description that
`/lighthouse-write-pr` fills in. `init` installs it as
`.github/pull_request_template.md`, unchanged and without a marker.
"""

from __future__ import annotations

import re
import tomllib

from tests.pack._testkit import CORE

TEMPLATE = CORE / "templates" / "pull_request_template.md"


def _text() -> str:
    return TEMPLATE.read_text(encoding="utf-8")


def test_template_names_lighthouse_write_pr() -> None:
    lines = _text().splitlines()
    named = [n for n, line in enumerate(lines, 1) if "/lighthouse-write-pr" in line]
    assert named == [11, 23], named
    assert not re.search(r"/[a-z]+-write-pr", _text().replace("/lighthouse-write-pr", ""))


def test_template_is_hostless() -> None:
    """A criterion that waits on a fixed host is a criterion CI cannot run; the
    template names that, not a machine."""
    assert "a test that cannot run in CI" in _text().splitlines()[15]
    for word in ("host", "machine"):
        assert word not in _text().lower(), word


def test_template_names_the_english_subcommand() -> None:
    assert "plan_review_record.py pr-open-points" in _text()


def test_template_open_after_review_is_the_english_heading() -> None:
    table = tomllib.loads((CORE / "headings.toml").read_text(encoding="utf-8"))
    title = table["open_after_review"]["en"].removeprefix("## ")
    assert f"`{title}`" in _text()


def test_template_keeps_its_tokens() -> None:
    text = _text()
    for token in ("`Closes #123`", "`### Evidence #123`", "`Refs #123 — <reason>`", "`No issue: <reason>`"):
        assert token in text, token


def test_template_has_no_project_issue_numbers() -> None:
    """`#123` is the placeholder; any other issue number belongs to the source repo."""
    assert set(re.findall(r"#\d+", _text())) == {"#123"}


def test_template_names_no_language_toolchain() -> None:
    for tool in ("ruff", "pytest", " ty/", "ty "):
        assert tool not in _text(), tool

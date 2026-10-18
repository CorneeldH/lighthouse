"""`pack/core/workflows/unblock-dependents.yml`: the release lane, filled by `init`.

Workflows read no TOML (spec, Portabiliteit): every project value is a `@@<key>@@`
placeholder that `init` fills from the config reader. In a project the file lands as
`.github/workflows/lighthouse-unblock-dependents.yml`; `close-issue` looks for the run
under that name and by the issue number at the end of its `run-name`.
"""

from __future__ import annotations

import re

from lighthouse_lib import config

from tests.pack._testkit import CORE

WORKFLOW = CORE / "workflows" / "unblock-dependents.yml"
PLACEHOLDER = re.compile(r"@@([a-z_.]+)@@")


def _text() -> str:
    return WORKFLOW.read_text(encoding="utf-8")


def _lines() -> list[str]:
    return [line.strip() for line in _text().splitlines()]


def test_runs_on_from_config() -> None:
    assert "runs-on: @@ci.runs_on@@" in _lines()


def test_bot_names_from_config() -> None:
    text = _text()
    assert "client-id: ${{ vars.@@ci.bot_client_id_var@@ }}" in text
    assert "private-key: ${{ secrets.@@ci.bot_private_key_secret@@ }}" in text


def test_default_branch_from_config() -> None:
    assert "github.ref == 'refs/heads/@@forge.default_branch@@' &&" in _lines()


def test_run_name_ends_on_the_issue_number() -> None:
    (run_name,) = [line for line in _lines() if line.startswith("run-name:")]
    assert re.search(r' #\$\{\{ [^}]+ \}\}"$', run_name), run_name


def test_calls_the_installed_script() -> None:
    assert ".lighthouse/scripts/unblock_dependents.py --issue" in _text()
    assert "scripts/unblock_dependents.py" not in _text().replace(
        ".lighthouse/scripts/unblock_dependents.py", ""
    )


def test_every_placeholder_is_a_known_scalar_key() -> None:
    """A list key would print one element per line, unescaped into the YAML."""
    keys = set(PLACEHOLDER.findall(_text()))
    assert keys == {
        "ci.runs_on",
        "ci.bot_client_id_var",
        "ci.bot_private_key_secret",
        "forge.default_branch",
    }
    for key in keys:
        assert config.is_known_key(key), key
        assert config.SCHEMA[key] == "str", key


def test_no_project_literals_left() -> None:
    text = _text().lower()
    for literal in ("self-hosted", "_bot_", "refs/heads/main"):
        assert literal not in text, literal

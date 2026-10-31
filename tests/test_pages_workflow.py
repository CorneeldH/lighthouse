"""`.github/workflows/pages.yml`: publishes `docs/site/` to GitHub Pages.

Textual checks, no YAML library, as in `tests/pack/test_workflow_unblock.py`.
"""

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = ROOT / ".github" / "workflows" / "pages.yml"


def _text() -> str:
    return WORKFLOW.read_text(encoding="utf-8")


def _lines() -> list[str]:
    return [line.strip() for line in _text().splitlines()]


def test_publishes_the_site_folder() -> None:
    """Fails when the deploy stops publishing `docs/site/`."""
    assert "path: docs/site" in _lines()


def test_runs_on_site_changes_on_main() -> None:
    """Fails when a push to main that touches the site no longer deploys it."""
    lines = _lines()
    assert "branches: [main]" in lines
    assert '- "docs/site/**"' in lines
    assert "workflow_dispatch:" in lines


def test_permissions_for_pages() -> None:
    """Fails when the token loses the rights that Pages deploys need."""
    lines = _lines()
    assert "pages: write" in lines
    assert "id-token: write" in lines
    assert "contents: read" in lines


def test_job_has_a_runner() -> None:
    assert "runs-on: ubuntu-latest" in _lines()


def test_actions_are_pinned() -> None:
    """Fails when a `uses:` line names a tag or branch instead of a full SHA."""
    uses = [line for line in _lines() if line.startswith("- uses:") or line.startswith("uses:")]
    assert len(uses) == 4, uses
    for line in uses:
        assert re.search(r"@[0-9a-f]{40} # v\d", line), line

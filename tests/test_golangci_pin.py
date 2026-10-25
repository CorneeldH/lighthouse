"""The golangci-lint pin of go-check.sh and the version CI installs must be one version."""

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_pin_matches_ci():
    """Fails when `.golangci-version` and the `version:` of the lint action drift apart."""
    pin = (ROOT / ".golangci-version").read_text(encoding="utf-8").strip()
    ci = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
    step = re.search(
        r"uses: golangci/golangci-lint-action@\S+.*?\n\s+with:\n\s+version: (\S+)", ci, re.S
    )
    assert step, "no golangci-lint-action step with a version in ci.yml"
    assert pin == step.group(1)

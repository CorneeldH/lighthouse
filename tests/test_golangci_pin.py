"""The golangci-lint pin of go-lint-gate.sh and the version CI installs must be one version."""

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_pin_matches_ci():
    """Fails when `.golangci-version` and the `version:` of any lint action step (the canary job included) drift apart."""
    pin = (ROOT / ".golangci-version").read_text(encoding="utf-8").strip()
    ci = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
    versions = re.findall(
        r"uses: golangci/golangci-lint-action@\S+.*?\n\s+with:\n\s+version: (\S+)", ci, re.DOTALL
    )
    assert versions, "no golangci-lint-action step with a version in ci.yml"
    assert versions == [pin] * len(versions), (pin, versions)

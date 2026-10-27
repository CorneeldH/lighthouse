"""The Go tests in CI and the Go part of the test gate must run with the same flags."""

import re
from pathlib import Path

from tests._domains import load

ROOT = Path(__file__).resolve().parents[1]


def _gate_go_command():
    checks = [check for runs in load().CHECKS.values() for check in runs]
    go = [check for check in checks if check[:2] == ["go", "test"]]
    assert len(go) == 1, f"no single `go test` in the checks of ci/domains.py: {checks}"
    return " ".join(go[0])


def _ci_go_commands():
    ci = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
    return re.findall(r"^\s*- run: (go test .*?)\s*$", ci, re.MULTILINE)


def test_ci_runs_gate_go_command():
    """Fails when the plain `go test` step in CI and the Go part of the gate drift apart."""
    assert _gate_go_command() in _ci_go_commands()


def test_ci_builds_without_cgo():
    """Fails when CI stops proving the binary builds without cgo."""
    ci = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
    assert re.search(r"^\s*- run: CGO_ENABLED=0 go build \./\.\.\.\s*$", ci, re.MULTILINE)


def test_every_ci_go_test_has_gate_flags():
    """Fails when a `go test` step in CI, the e2e step included, drops a flag of the gate."""
    flags = [token for token in _gate_go_command().split() if token.startswith("-")]
    runs = _ci_go_commands()
    assert len(runs) == 2, runs
    for run in runs:
        assert all(flag in run.split() for flag in flags), run

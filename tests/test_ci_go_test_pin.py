"""The Go tests in CI and the Go part of the test gate must run with the same flags."""

import re
import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def _gate_go_command():
    """The `go test ...` words after `gate_go.py --`, up to the next `&&`."""
    toml = tomllib.loads((ROOT / "lighthouse.toml").read_text(encoding="utf-8"))
    command = toml["test_gate"]["command"]
    match = re.search(r"gate_go\.py --\s+(go test[^&]*?)\s*(?:&&|$)", command)
    assert match, f"no `gate_go.py -- go test ...` in [test_gate] command: {command}"
    return match.group(1)


def _ci_go_commands():
    ci = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
    return re.findall(r"^\s*- run: (go test .*?)\s*$", ci, re.MULTILINE)


def test_ci_builds_without_cgo():
    """Fails when CI stops proving the binary builds without cgo."""
    ci = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
    assert re.search(r"^\s*- run: CGO_ENABLED=0 go build \./\.\.\.\s*$", ci, re.MULTILINE)


def test_every_ci_go_test_has_gate_flags():
    """Fails when a `go test` step in CI, the e2e step included, drops a flag of the gate."""
    flags = [token for token in _gate_go_command().split() if token.startswith("-")]
    assert flags, "the Go part of the gate has no flags to pin"
    runs = _ci_go_commands()
    assert len(runs) == 2, runs
    for run in runs:
        assert all(flag in run.split() for flag in flags), run

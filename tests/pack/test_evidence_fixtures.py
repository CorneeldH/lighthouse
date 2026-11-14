"""Shared fixtures of the Evidence allowlist: `fixtures/evidence/cases.json`.

The Go package (`internal/evidence`) reads every case; this file runs the `check`
cases through `lighthouse evidence check '<cell>'`, the way a skill calls it.
Every case has `name`, `kind`, `cell` and `expect`:

- `split`: the argv of a Command cell (`expect`: the argv, or `{"reason": …}`);
- `check`: what `lighthouse evidence check` says (`expect`: `null` for exit 0
  without output, or the reason it prints with exit 1), with an optional
  `evidence_test_dir` (`[test_gate] evidence_test_dir` of the case);
- `expected`: the fragment of a Result cell (`expect`: `[fragment, after_pr]`);
- `outcome_of`: the outcome of a table cell (`expect`: the outcome key, or `null`).

`split`, `expected` and `outcome_of` have no CLI: only the Go tests read them.
`allowlist.json` holds the allowlist constants, each as a sorted list; the Go
test `TestAllowlistMatchesFixture` binds them to the code.
"""

import json
import os
import subprocess
from pathlib import Path

import pytest

from tests.pack._testkit import write_fixture_config

# The binary of this checkout, first on the PATH.
pytestmark = pytest.mark.usefixtures("lighthouse_bin")

FIXTURES = Path(__file__).parent / "fixtures" / "evidence"
CASES = json.loads((FIXTURES / "cases.json").read_text(encoding="utf-8"))
CHECK_CASES = [c for c in CASES if c["kind"] == "check"]


@pytest.mark.parametrize("case", CHECK_CASES, ids=lambda c: c["name"])
def test_fixture_check_case(case, tmp_path):
    write_fixture_config(tmp_path, test_dir=case.get("evidence_test_dir"))
    env = dict(os.environ, LIGHTHOUSE_REPO_ROOT=str(tmp_path))
    env.pop("LIGHTHOUSE_TOML", None)
    proc = subprocess.run(
        ["lighthouse", "evidence", "check", case["cell"]],
        capture_output=True,
        text=True,
        env=env,
        check=False,
    )
    assert proc.stderr == ""
    if case["expect"] is None:
        assert (proc.returncode, proc.stdout) == (0, "")
    else:
        assert (proc.returncode, proc.stdout) == (1, case["expect"] + "\n")

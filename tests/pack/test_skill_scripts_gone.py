"""Step 8 of the spec zonder Python (#281): the four skill scripts are subcommands of the binary,
and the lib modules they were the last users of are gone, from the package and from the copy
`lighthouse init` installed here. The modules step 12 still needs stay."""

from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]

GONE = [
    "lib/protocol.py",
    "lib/artefacts.py",
    "lib/pr_body_gate.py",
    "lib/git_invocations.py",
    "lib/repo_identity.py",
    "scripts/board_start.py",
    "scripts/post_artefact.py",
    "scripts/unblock_dependents.py",
    "scripts/worktree_audit.py",
]
KEPT = ["lib/config.py", "lib/datadir.py", "lib/headings.py"]


def _installed(rel: str) -> Path:
    kind, name = rel.split("/")
    return ROOT / ".lighthouse" / ("lib/lighthouse_lib" if kind == "lib" else "scripts") / name


@pytest.mark.parametrize("rel", GONE)
def test_removed_unit_stays_gone(rel):
    assert not (ROOT / "pack" / "core" / rel).exists(), f"pack/core/{rel} is back"
    assert not _installed(rel).exists(), f"{_installed(rel).relative_to(ROOT)} is back"


@pytest.mark.parametrize("rel", KEPT)
def test_frozen_module_stays(rel):
    assert (ROOT / "pack" / "core" / rel).is_file()
    assert _installed(rel).is_file()

"""Structural demands on the skill `lighthouse-fix-pr`.

The acceptance criteria of the skill are mostly form demands, and a form demand without a test
is a wish:

- the description carries the triggers AND an explicit negative boundary;
- the diagnosis is ONE block, not three blocks under each other;
- that block is read-only — it diagnoses, it does not fix;
- every known gate is in the table with a command that reproduces it locally.

Both variants are checked on content; they differ only in how they say "outside the sandbox".
"""

from __future__ import annotations

import importlib.util
import re
import sys

from tests.pack._testkit import CORE

SKILL_DIR = CORE / "skills" / "fix-pr"
CLAUDE = (SKILL_DIR / "claude" / "SKILL.md").read_text(encoding="utf-8")
AGENTS = (SKILL_DIR / "agents" / "SKILL.md").read_text(encoding="utf-8")
TEXTS = (CLAUDE, AGENTS)

_SPEC = importlib.util.spec_from_file_location(
    "measure_fix_pr_kpi", CORE / "scripts" / "measure_fix_pr_kpi.py"
)
assert _SPEC and _SPEC.loader
kpi = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = kpi
_SPEC.loader.exec_module(kpi)

DIAGNOSE_HEADING = "## 1. Diagnosis in one block"
GATE_HEADING = "## 3. The gates, and how to reproduce them locally"


def _description(text: str) -> str:
    front = text.partition("---")[2].partition("\n---")[0]
    return re.search(r"^description:\s*(.+)$", front, re.MULTILINE).group(1)


def _section(text: str, heading: str) -> str:
    _, _, rest = text.partition(heading)
    return rest.split("\n## ")[0]


def _diagnose_block(text: str) -> str:
    blocks = re.findall(r"```bash\n(.*?)```", _section(text, DIAGNOSE_HEADING), re.DOTALL)
    assert len(blocks) == 1, f"{len(blocks)} bash blocks under the diagnosis heading, want 1"
    return blocks[0]


def test_description_carries_the_measured_triggers() -> None:
    for text in TEXTS:
        lower = _description(text).lower()
        for trigger in ("fix pr", "conflict", "red"):
            assert trigger in lower, trigger


def test_description_carries_an_explicit_negative_boundary() -> None:
    """A scope word AND another skill by name (check-instructions has_exclusion_clause)."""
    for text in TEXTS:
        lower = _description(text).lower()
        assert "note" in lower
        assert "lighthouse-close-issue" in lower
        assert "caveman-review" in lower


def test_the_diagnosis_block_is_read_only() -> None:
    for text in TEXTS:
        hit = kpi.MUTATING_BASH_RE.search(_diagnose_block(text))
        assert hit is None, f"diagnosis block holds a mutating command: {hit.group(0)!r}"


def test_the_diagnosis_reads_the_repo_from_config() -> None:
    for text in TEXTS:
        block = _diagnose_block(text)
        assert "python3 .lighthouse/lib/lighthouse_lib/config.py forge.repo" in block
        assert "identity" not in text


# (label in the FIRST cell of the row, command in that same row) — in table order.
GATE_ROWS = (
    ("Tests", "config.py test_gate.command"),
    ("Quality ratchet (ruff)", ".lighthouse/scripts/quality/ratchet.py ruff"),
    ("Expo lockfiles", "/lighthouse-refresh-expo-lockfiles"),
    ("Any other step", ".github/workflows/"),
)


def _gate_table_rows(text: str) -> list[str]:
    lines = [ln for ln in _section(text, GATE_HEADING).splitlines() if ln.strip().startswith("|")]
    assert len(lines) >= 2, "gate table not found under the heading"
    return lines[2:]


def test_every_known_gate_has_a_local_command() -> None:
    for text in TEXTS:
        rows = _gate_table_rows(text)
        assert len(rows) == len(GATE_ROWS), rows
        for row, (job, command) in zip(rows, GATE_ROWS):
            assert job in row.split("|")[1], row
            assert command in row, row


def test_fix_pr_uses_test_gate_command() -> None:
    """The test command comes from `[test_gate]`; the ratchet of pack/python runs only if
    present (Kruisverwijzingen)."""
    for text in TEXTS:
        rows = _gate_table_rows(text)
        assert "python3 .lighthouse/lib/lighthouse_lib/config.py test_gate.command" in rows[0]
        assert "if present" in rows[1] and "test -e .lighthouse/scripts/quality/ratchet.py" in rows[1]
        assert "if present" in rows[2]
        assert "tests/unit" not in text


def test_pre_push_and_roborev_only_if_present() -> None:
    for text in TEXTS:
        assert "test -e .roborev.toml" in _diagnose_block(text)
        assert "## 6. Process a roborev review (only if present)" in text
        for para in re.split(r"\n\s*\n", text):
            if "pre-push" in para:
                assert "if present" in " ".join(para.split()), para


def test_the_worktree_step_uses_the_head_branch_form() -> None:
    for text in TEXTS:
        assert "git worktree add" in text
        assert "--track -b" in text
        assert "origin/" in text
        assert "EnterWorktree" in text


def test_the_kpi_table_names_the_measuring_script() -> None:
    for text in TEXTS:
        for metric in ("leadin", "diagnose", "mistrigger", "scope"):
            assert f"python3 .lighthouse/scripts/measure_fix_pr_kpi.py {metric}" in text
        assert "n=9" not in text


def test_description_names_the_roborev_entry() -> None:
    for text in TEXTS:
        assert "roborev" in _description(text).lower()


def test_diagnosis_block_shows_the_full_roborev_body() -> None:
    for text in TEXTS:
        block = _diagnose_block(text)
        assert "roborev-pr-comment" in block
        assert "roborev-verwerkt" in block  # the marker paia's controller reads
        assert "| .body | @json" in block
        assert "Verdict:" not in block


def test_roborev_section_carries_the_contract() -> None:
    for text in TEXTS:
        assert "<!-- roborev-verwerkt: <sha> -->" in text
        assert "superpowers:receiving-code-review" in text
        assert re.search(r"(?:❌|[Nn]o|[Nn]ever)[^\n]*`roborev fix`", text)
        assert "`roborev refine`" in text


def test_variants_differ_only_in_the_sandbox_wording() -> None:
    assert "dangerouslyDisableSandbox: true" in _section(CLAUDE, DIAGNOSE_HEADING)
    assert "dangerouslyDisableSandbox" not in _section(AGENTS, DIAGNOSE_HEADING)
    claude = [line for line in CLAUDE.splitlines() if line not in AGENTS.splitlines()]
    assert len(claude) == 2, claude

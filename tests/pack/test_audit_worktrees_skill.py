"""Contract test for the skill `lighthouse-audit-worktrees`.

The skill describes what `.lighthouse/scripts/worktree_audit.py` prints, so its table header,
its KPI fields and the `--only` token must be the ones the script really writes. What stays in
paia (the opencode lane with its port registry, board-sweep) is named only if present
(design choice 16), and the protect list comes from `[worktree_audit]` in `lighthouse.toml`.
"""

from __future__ import annotations

import importlib.util
import re
import sys

from tests.pack._testkit import CORE

SKILL = (CORE / "skills" / "audit-worktrees" / "SKILL.md").read_text(encoding="utf-8")

_SPEC = importlib.util.spec_from_file_location(
    "worktree_audit_for_skill", CORE / "scripts" / "worktree_audit.py"
)
assert _SPEC and _SPEC.loader
audit = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = audit
_SPEC.loader.exec_module(audit)


def _paragraphs(text: str) -> list[str]:
    return [" ".join(p.split()) for p in re.split(r"\n\s*\n", text)]


def test_every_view_runs_the_installed_script():
    for view in ("", " --sessions", " --origin", " --kpi", " --only <branch>", " --issue <nr>"):
        assert f"python3 .lighthouse/scripts/worktree_audit.py{view}" in SKILL, view
    assert "uv run" not in SKILL
    assert "scripts/worktree_audit.py" not in SKILL.replace(".lighthouse/scripts/worktree_audit.py", "")


def test_only_view_names_the_cleanup_token():
    assert "`cleanup: yes`" in SKILL
    assert "`cleanup: no — <reason>`" in SKILL
    assert "lighthouse-close-issue" in SKILL


def test_table_header_is_the_scripts():
    assert audit.TABLE_HEADER in SKILL


def test_kpi_fields_are_the_scripts():
    line = audit.render_kpi([])
    for field in re.findall(r"(\w+)=", line):
        assert f"`{field}=`" in SKILL, field


def test_protect_list_from_config():
    assert "[worktree_audit]" in SKILL
    assert "protected_refs" in SKILL and "protected_paths" in SKILL
    assert "default branch" in SKILL
    assert "PROTECTED_REFS  = (" not in SKILL


def test_worktree_audit_no_opencode():
    """The opencode lane and its port registry stay in paia: named only if present."""
    hits = [p for p in _paragraphs(SKILL) if "opencode" in p or "port-registry" in p]
    assert hits
    for para in hits:
        assert "if present" in para, para


def test_board_sweep_only_if_present():
    for para in _paragraphs(SKILL):
        if "board-sweep" in para:
            assert "if present" in para, para


def test_skill_is_detached_from_the_source_repo():
    for gone in (
        "CorneeldH",
        "personal_ai_agent",
        "deploy/beelink",
        "agentsview",
        "tests/unit/",
        "tests/integration/",
        "claude-remote-control.service",
    ):
        assert gone not in SKILL, gone
    assert not re.search(r"#\d{3,}", SKILL)


def test_skill_is_english():
    lower = SKILL.lower()
    for word in (" je ", " het ", " niet ", " geen ", " een ", " wordt "):
        assert word not in lower, word

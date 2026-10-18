"""Pint de gepaarde ronde 1 en de Opus-verificatie in de adversariële review-skills (#988)."""

from __future__ import annotations

import pathlib

REPO = pathlib.Path(__file__).resolve().parents[2]
PLAN_SKILL = pathlib.Path("skills") / "paia-plan-adversarial-review" / "SKILL.md"


def test_claude_plan_skill_runs_the_shadow_in_round_one():
    text = (REPO / ".claude" / PLAN_SKILL).read_text()
    for needle in (
        "review_pair.py blind",
        "review_pair.py merge",
        "round-1.blind-decisions.json",
        "--shadow-model",
        "pair.json",
        "round-1.codex.log",
        "git status --porcelain",
    ):
        assert needle in text, needle


def test_agents_plan_skill_says_shadow_is_claude_code_only():
    text = (REPO / ".agents" / PLAN_SKILL).read_text()
    assert "Gepaarde ronde 1 (#988): alleen in Claude Code" in text
    assert "review_pair.py blind" not in text


SPEC_SKILL = pathlib.Path("skills") / "paia-spec-adversarial-review" / "SKILL.md"


def test_claude_spec_skill_records_and_shadows():
    text = (REPO / ".claude" / SPEC_SKILL).read_text()
    for needle in (
        "plan_review_record.py start --kind spec",
        "plan_review_record.py write",
        "minor_yes",
        "defer",
        "blind-decisions",
    ):
        assert needle in text, needle


def test_agents_spec_skill_records_but_does_not_shadow():
    text = (REPO / ".agents" / SPEC_SKILL).read_text()
    for needle in (
        "plan_review_record.py start --kind spec",
        "--skill-dir .agents/skills/paia-spec-adversarial-review",
        "Gepaarde ronde 1 (#988): alleen in Claude Code",
    ):
        assert needle in text, needle
    assert "review_pair.py blind" not in text


def test_claude_skills_pair_permanently_and_verify_on_opus():
    """Besluit 2026-10-04: geen uitknop meer, rondes 2-3 op Opus (#988)."""
    for skill in (PLAN_SKILL, SPEC_SKILL):
        text = (REPO / ".claude" / skill).read_text()
        for needle in (
            "Ronde 1: Codex en Opus",
            "## Opus-dispatch",
            'model: "opus"',
            "verifier-prompt.md",
            "codex-fallback-paren/REPORT.md",
        ):
            assert needle in text, (skill, needle)
        assert "review_pair.py status" not in text, skill
        assert "schaduwrun" not in text.lower(), skill

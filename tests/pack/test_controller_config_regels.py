"""De harde regels noemen de controller-config-uitzondering (#1354)."""

from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[2]

REVIEW_SKILLS = [
    ".claude/skills/paia-spec-adversarial-review/SKILL.md",
    ".agents/skills/paia-spec-adversarial-review/SKILL.md",
    ".claude/skills/paia-plan-adversarial-review/SKILL.md",
    ".agents/skills/paia-plan-adversarial-review/SKILL.md",
]
HANDOFF_SKILLS = [
    ".claude/skills/paia-handoff/SKILL.md",
    ".agents/skills/paia-handoff/SKILL.md",
]
AUTO_REGEL = "Auto: <categorie> — <vraag in één zin> → <gekozen optie>"


def test_vaste_naad_noemt_controller_uitzondering():
    tekst = (REPO / "AGENTS.md").read_text()
    for stuk in (
        "controller.spec_review",
        "controller.plan_review",
        "#1354",
        "lighthouse.toml",
    ):
        assert stuk in tekst


@pytest.mark.parametrize("pad", REVIEW_SKILLS)
def test_review_skills_noemen_bij_rondegrens(pad):
    assert "controller.bij_rondegrens" in (REPO / pad).read_text()


@pytest.mark.parametrize("pad", HANDOFF_SKILLS)
def test_handoff_skill_kent_auto_regel(pad):
    assert AUTO_REGEL in (REPO / pad).read_text()


@pytest.mark.parametrize("pad", REVIEW_SKILLS + HANDOFF_SKILLS)
def test_skills_noemen_lighthouse_toml(pad):
    assert "lighthouse.toml" in (REPO / pad).read_text()


def test_spec_noemt_knoppen_in_lighthouse_toml():
    spec = (
        REPO
        / "docs/developer/planning/specs/2026-10-11-controller-configuratie-design.md"
    )
    tekst = spec.read_text()
    assert "[controller]" in tekst and "lighthouse.toml" in tekst
    assert "controller:`-sectie van `config.yaml`" not in tekst

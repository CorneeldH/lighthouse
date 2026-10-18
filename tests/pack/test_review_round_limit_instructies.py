"""#1352: een adversarial review die op `round_limit` eindigt, loopt door in plaats van te vragen.

De agent die de review draait leest alleen deze teksten. Verdwijnt de procedure eruit
(`accepteer-open`, het blok `## Open na review`, "nooit een vraag"), dan keert de oude
gebruikersvraag terug en wacht een headless controller-run eindeloos op een antwoord.
"""

from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[2]
SKILLS = [
    REPO / root / f"paia-{kind}-adversarial-review/SKILL.md"
    for root in (".claude/skills", ".agents/skills")
    for kind in ("spec", "plan")
]
PLAN_SKILLS = [
    REPO / root / "paia-plan-adversarial-review/SKILL.md"
    for root in (".claude/skills", ".agents/skills")
]


@pytest.mark.parametrize("pad", SKILLS, ids=lambda p: f"{p.parts[-4]}-{p.parts[-2]}")
def test_round_limit_legt_vast_en_loopt_door(pad):
    tekst = pad.read_text()
    assert "plan_review_record.py accepteer-open <ATTEMPT> --doc" in tekst
    assert "## Open na review" in tekst
    assert "nooit een vraag" in tekst
    # de letterlijke oude zinnen, mét markdown-sterretjes (zonder sterretjes is de check vacuüm)
    assert "SDD **alleen** als de gebruiker" not in tekst
    assert "**vervangt** de gebruiker" not in tekst
    assert "bij `spec`-punten" not in tekst


@pytest.mark.parametrize("pad", SKILLS, ids=lambda p: f"{p.parts[-4]}-{p.parts[-2]}")
def test_accepteer_open_exit_4_en_exit_2_zijn_afgedekt(pad):
    tekst = pad.read_text()
    assert "ACCEPTEER-OPEN MISLUKT" in tekst
    assert "niets te accepteren" in tekst


@pytest.mark.parametrize("pad", PLAN_SKILLS, ids=lambda p: p.parts[-4])
def test_spec_beslissing_in_ronde_3_stopt_de_keten_niet(pad):
    assert "in ronde 3 stopt een `spec`-beslissing de keten niet" in pad.read_text()


@pytest.mark.parametrize("root", [".claude/skills", ".agents/skills"])
def test_handoff_kent_spec_met_open_punten(root):
    tekst = (REPO / root / "paia-handoff/SKILL.md").read_text()
    assert "rondegrens met open punten vastgelegd" in tekst


def test_run_instructies_round_limit_is_geen_vraag():
    tekst = (REPO / "scripts/controller_run_instructies.md").read_text()
    assert "`round_limit` is geen vraag" in tekst


@pytest.mark.parametrize("root", [".claude/skills", ".agents/skills"])
def test_verify_pr_neemt_open_punten_over(root):
    tekst = (REPO / root / "paia-verify-pr/SKILL.md").read_text()
    assert "plan_review_record.py pr-open-punten" in tekst
    assert "`## Open review points`" in tekst


def test_template_kent_de_kop():
    assert (
        "## Open review points"
        in (REPO / ".github/pull_request_template.md").read_text()
    )

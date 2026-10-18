"""Pint de convergentieregels van de adversariële reviews (#1066)."""

from __future__ import annotations

import json
import pathlib
import subprocess

import pytest

REPO = pathlib.Path(__file__).resolve().parents[2]
ROOTS = (REPO / ".claude" / "skills", REPO / ".agents" / "skills")


def skill(root: pathlib.Path, name: str) -> str:
    return (root / name / "SKILL.md").read_text()


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_plan_review_verifies_instead_of_rereviewing(root):
    text = skill(root, "paia-plan-adversarial-review")
    assert "open_vraag" not in text
    assert "verify.sh" in text
    assert "Alleen ronde 1 is een review" in text
    assert "Geen fixes, geen ronde" in text
    assert "MINORs starten nooit een ronde" in text
    assert "`git diff --no-index` eindigt met exit 1 als er verschil is" in text
    assert "Een derde ronde alleen als een fix uit ronde 2 niet klopt" in text
    assert (
        "Elk geaccepteerd ID staat in `verified` of in `verifies` van een finding"
        in text
    )
    assert "\\(.verifies" in text
    assert ".verified" in text


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_spec_review_verifies_instead_of_rereviewing(root):
    text = skill(root, "paia-spec-adversarial-review")
    assert "verify.sh" in text
    assert "Alleen ronde 1 is een review" in text
    assert "Geen fixes, geen ronde" in text
    assert "MINORs starten nooit een ronde" in text
    assert "`git diff --no-index` eindigt met exit 1 als er verschil is" in text
    assert "Een derde ronde alleen als een fix uit ronde 2 niet klopt" in text
    assert (
        "Elk geaccepteerd ID staat in `verified` of in `verifies` van een finding"
        in text
    )
    assert "\\(.verifies" in text
    assert ".verified" in text


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
@pytest.mark.parametrize(
    "name", ["paia-spec-adversarial-review", "paia-plan-adversarial-review"]
)
def test_skills_kennen_budget_exit_6(root, name):
    text = skill(root, name)
    assert "exit 6" in text
    assert ".budget.json" in text
    assert "`reset_at`" in text


@pytest.mark.parametrize(
    "name", ["paia-spec-adversarial-review", "paia-plan-adversarial-review"]
)
def test_claude_skills_draaien_fallback_bij_exit_6(name):
    text = skill(ROOTS[0], name)
    assert "Drie aanleidingen" in text
    assert "vooraf (exit 6)" in text  # eindverslag
    # Het vangnet "volgende rondes blijven fallback" verviel met #988: rondes 2-3
    # draaien altijd op Opus, dus alleen ronde 1 kan op een Codex-limiet stuiten.
    assert "Volgende rondes blijven fallback" not in text
    assert (
        "Alleen ronde 1 draait op Codex, dus alleen ronde 1 kent een fallback" in text
    )


def test_plan_faaltabel_heeft_exit_6():
    text = skill(ROOTS[0], "paia-plan-adversarial-review")
    assert (
        "| Codex exit 6 (budget vooraf op) | `mv <ATTEMPT>/round-1.fallback.json <ATTEMPT>/round-1.json` | `fallback --reviewer-model <gemeld model>` |"
        in text
    )


@pytest.mark.parametrize(
    "name", ["paia-spec-adversarial-review", "paia-plan-adversarial-review"]
)
def test_agents_skills_stoppen_bij_exit_6(name):
    assert "stop **vóór** de ronde" in skill(ROOTS[1], name)


def test_spec_adversary_kent_exit_6_zonder_retry():
    text = (REPO / ".claude" / "agents" / "spec-adversary.md").read_text()
    assert "| 6 | `CODEX_BUDGET`" in text
    assert "no retry" in text


def _stap2_jq(text: str) -> str:
    blok = text.split('```bash\n   jq -r --arg round "$ROUND" \'', 1)[1]
    return blok.split('\' "$OUT"', 1)[0]


def test_spec_adversary_klapt_minors_niet_in(tmp_path):
    """Elke ID waarover de operator beslist staat met problem en fix in beeld (#995)."""
    text = (REPO / ".claude" / "agents" / "spec-adversary.md").read_text()
    assert "Collapse MINORs" not in text
    assert "One row per finding, MINORs included" in text

    findings = [
        {
            "severity": "MINOR",
            "spec_section": f"S{i}",
            "problem": f"probleem {i}",
            "failure_scenario": f"faalt {i}",
            "evidence": f"a.md:{i}",
            "proposed_fix": f"fix {i}",
        }
        for i in range(1, 6)
    ]
    findings.insert(2, {**findings[0], "severity": "MAJOR", "problem": "groot"})
    out = tmp_path / "round-1.json"
    out.write_text(json.dumps({"findings": findings}))

    rows = subprocess.run(
        ["jq", "-r", "--arg", "round", "1", _stap2_jq(text), str(out)],
        capture_output=True,
        text=True,
        check=True,
    ).stdout.splitlines()

    assert len(rows) == len(findings)
    assert rows[0].startswith("| r1-f3 | MAJOR |")
    for idx, finding in enumerate(findings, start=1):
        (row,) = [r for r in rows if r.startswith(f"| r1-f{idx} |")]
        assert f"| {finding['problem']} |" in row
        assert f"| {finding['proposed_fix']} |" in row


def prompt(root: pathlib.Path, name: str) -> str:
    return (root / name / "reviewer-prompt.md").read_text()


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_spec_review_motivates_against_builder_check(root):
    text = skill(root, "paia-spec-adversarial-review")
    for stale in ("spec-document-reviewer-prompt", "self-review", "SKILL.md:119"):
        assert stale not in text
        assert stale not in prompt(root, "paia-spec-adversarial-review")
    assert "builder check" in text
    assert "builder-check-prompt.md" in text


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_reviewer_prompt_skips_builder_freedom(root):
    text = prompt(root, "paia-spec-adversarial-review")
    assert "explicitly leaves to the implementer" in text
    assert "a reasonable implementer could decide" in text
    assert "never to Ground truth or Gaps" in text


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_spec_gate_sorts_three_piles(root):
    text = skill(root, "paia-spec-adversarial-review")
    for pile in ("**Al beantwoord**", "**Klein**", "**Van de gebruiker**"):
        assert pile in text
    assert "*(eigen keuze na spec-review, <ID>)*" in text
    assert "hoogstens vier" in text
    assert "elke MINOR een eigen regel" in text
    assert "MINORs: batch ze, één ja/nee" not in text
    assert "in een controller-run een genummerde lijst als laatste" in text

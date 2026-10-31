"""Contract test for the skill `lighthouse-place-instruction` (from paia's `paia-instrumenteren`).

The skill decides on which layer a new instruction belongs and validates with
`.lighthouse/scripts/check-instructions.sh`. That script carries only the generic checks of
the source (1, 2, 3, 4, 5, 7, 8, 10, 11); the skill names no project-own check (6, 9, 12, 13).
The skill prefix and the word ceiling come from `[project]`; the surface measurement stays in
paia and runs only if present (design choice 16).
"""

from __future__ import annotations

import re

from tests.pack._testkit import CORE

SKILL = (CORE / "skills" / "place-instruction" / "SKILL.md").read_text(encoding="utf-8")
SCRIPT = (CORE / "scripts" / "check-instructions.sh").read_text(encoding="utf-8")
GENERIC = {3, 4, 5, 7, 8, 10, 11, 14}


def test_instrument_lists_generic_checks_only():
    named = {int(n) for n in re.findall(r"\b[Cc]heck (\d+)", SKILL)}
    assert named, "the skill names no check"
    assert named <= GENERIC, sorted(named - GENERIC)
    for n in named:
        assert f"# --- Check {n}:" in SCRIPT, n


def test_validates_with_the_installed_script():
    assert ".lighthouse/scripts/check-instructions.sh" in SKILL
    assert "scripts/hooks/check-instructies.sh" not in SKILL


def test_prefix_and_ceiling_from_config():
    assert "python3 .lighthouse/lib/lighthouse_lib/config.py project.skill_prefix" in SKILL
    assert "python3 .lighthouse/lib/lighthouse_lib/config.py project.instructions_word_ceiling" in SKILL
    assert "paia-" not in SKILL


def test_surface_measurement_only_if_present():
    lines = [line for line in SKILL.splitlines() if "measure_skill_surface" in line]
    assert lines
    for line in lines:
        assert "if present" in line, line


def test_bounding_clause_matches_check_4():
    """The form the skill prescribes must be one check 4 (`has_exclusion_clause`) accepts."""
    clause = re.search(r"Form: `([^`]+)`", SKILL)
    assert clause, "no Form: line for the bounding clause"
    words = re.search(r'for word in (.*?); do', SCRIPT, re.DOTALL).group(1)
    accepted = re.findall(r'"([^"]+)"', words)
    assert any(w in clause.group(1).lower() for w in accepted), (clause.group(1), accepted)


def test_skill_is_detached_from_the_source_repo():
    for gone in (
        "Beelink",
        "tests/unit/",
        "docs/developer/planning/specs/",
        "Milestone #58",
        "applicatie",
        "vault",
        "uv run",
        "check 9",
        "check 12",
        "check 13",
    ):
        assert gone not in SKILL, gone
    assert not re.search(r"#\d{3,}", SKILL)


def test_frontmatter_keeps_disable_model_invocation_top_level():
    front = SKILL.split("---", 2)[1]
    assert "\ndisable-model-invocation: true\n" in front
    assert "domain:" not in front and "source:" not in front


def test_skill_is_english():
    lower = SKILL.lower()
    for word in (" je ", " het ", " niet ", " geen ", " een ", " wordt "):
        assert word not in lower, word

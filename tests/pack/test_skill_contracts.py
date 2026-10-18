"""Contract test for the skills and agents of `pack/core` (plan task 22, Review Focus 5).

A translation error makes a skill point at something that does not exist: a script, a
lib module, a hook, a config key, a heading or another skill. Nothing at runtime catches
that before a user runs the skill, so every reference is checked here, per skill and per
agent.

A skill names a heading as `heading:<key>` (ontwerpkeuze 19) and a config key as the
argument of `config.py` (ontwerpkeuze 6 and 7). Steps that depend on something that stays
in paia (backlog-stand, board-sweep, the controller, the release flow, the pre-push chain)
run only "if present" and otherwise report "skipped" (ontwerpkeuze 16).
"""

from __future__ import annotations

import re
import tomllib
from pathlib import Path

import pytest

from lighthouse_lib import config
from tests.pack._testkit import CORE

SKILLS = CORE / "skills"
AGENTS = CORE / "agents"
TEXT_SUFFIXES = {".md", ".sh", ".json", ".txt"}

# Skills outside lighthouse that a core skill may call: the Superpowers skills.
ALLOWED_EXTERNAL = {
    "superpowers:brainstorming",
    "superpowers:dispatching-parallel-agents",
    "superpowers:executing-plans",
    "superpowers:finishing-a-development-branch",
    "superpowers:receiving-code-review",
    "superpowers:requesting-code-review",
    "superpowers:subagent-driven-development",
    "superpowers:systematic-debugging",
    "superpowers:test-driven-development",
    "superpowers:using-git-worktrees",
    "superpowers:verification-before-completion",
    "superpowers:writing-plans",
    "superpowers:writing-skills",
}

# Core skills of this PR that are not written yet, so another skill may already link to
# them. A skill leaves this set in the commit that adds it (test_planned_skills_absent);
# the set is empty when PR 4 is done.
PLANNED = {
    "handoff",
    "verify-pr",
    "fix-pr",
    "close-issue",
    "spec-adversarial-review",
    "plan-adversarial-review",
    "write-issue",
    "worktree-audit",
    "instrument",
}

# An installed path in a skill -> where it lives in pack/core.
_PATH_REFS = (
    (re.compile(r"\.lighthouse/scripts/([\w.-]+)"), lambda m: CORE / "scripts" / m[1]),
    (
        re.compile(r"\.lighthouse/lib/lighthouse_lib/(\w+)\.py"),
        lambda m: CORE / "lib" / f"{m[1]}.py",
    ),
    (re.compile(r"\.claude/hooks/lighthouse/([\w.-]+)"), lambda m: CORE / "hooks" / m[1]),
    (re.compile(r"\.claude/agents/lighthouse-([\w-]+)\.md"), lambda m: AGENTS / f"{m[1]}.md"),
)
_CONFIG_RE = re.compile(r"config\.py\s+(\"[^\"]+\"|'[^']+'|[^\s`\"')|;]+)")
_HEADING_RE = re.compile(r"heading:([A-Za-z0-9_]+)")
_LIGHTHOUSE_SKILL_RE = re.compile(r"(?<![\w./-])/lighthouse-([a-z0-9-]+)")
# `/name` alone in backticks (optionally with arguments), or a namespaced `/ns:name`.
_EXTERNAL_SKILL_RES = (
    re.compile(r"`/([a-z][a-z0-9-]*(?::[a-z0-9-]+)?)(?:\s[^`]*)?`"),
    re.compile(r"(?<![\w./~-])/([a-z][a-z0-9-]*:[a-z0-9-]+)"),
)
_OPTIONAL_RE = re.compile(r"backlog-stand|board-sweep|\brelease\b|pre-push|\bcontroller\b", re.I)
_GUARD_RE = re.compile(r"if present|skipped", re.I)


def _skill_dirs() -> list[Path]:
    if not SKILLS.is_dir():
        return []
    return sorted(p for p in SKILLS.iterdir() if p.is_dir())


def _texts(unit: Path) -> dict[str, str]:
    """Every text file of a unit (a skill directory or an agent file), by relative name."""
    if unit.is_file():
        return {unit.name: unit.read_text(encoding="utf-8")}
    return {
        str(p.relative_to(unit)): p.read_text(encoding="utf-8")
        for p in sorted(unit.rglob("*"))
        if p.is_file() and p.suffix in TEXT_SUFFIXES
    }


def _variants(skill: Path) -> dict[str, Path]:
    shared = skill / "SKILL.md"
    if shared.is_file():
        return {"claude": shared, "agents": shared}
    return {v: skill / v / "SKILL.md" for v in ("claude", "agents") if (skill / v / "SKILL.md").is_file()}


def _frontmatter_name(text: str) -> str | None:
    match = re.match(r"^---\n(.*?)\n---\n", text, re.DOTALL)
    if not match:
        return None
    for line in match.group(1).splitlines():
        if line.startswith("name:"):
            return line[len("name:") :].strip().strip("\"'")
    return None


UNITS = [pytest.param(p, id=f"skill:{p.name}") for p in _skill_dirs()] + [
    pytest.param(p, id=f"agent:{p.stem}") for p in sorted(AGENTS.glob("*.md"))
]
SKILL_PARAMS = [pytest.param(p, id=p.name) for p in _skill_dirs()]


def _unit_name(unit: Path) -> str:
    return unit.name if unit.is_dir() else unit.stem


@pytest.mark.parametrize("unit", UNITS)
def test_frontmatter_name(unit):
    files = list(_variants(unit).values()) if unit.is_dir() else [unit]
    assert files, f"{unit}: no SKILL.md"
    for f in files:
        name = _frontmatter_name(f.read_text(encoding="utf-8"))
        assert name == f"lighthouse-{_unit_name(unit)}", f"{f}: name is {name!r}"


@pytest.mark.parametrize("unit", UNITS)
def test_referenced_files_exist(unit):
    missing = []
    for rel, text in _texts(unit).items():
        for regex, target in _PATH_REFS:
            for m in regex.finditer(text):
                if not target(m).is_file():
                    missing.append(f"{rel}: {m[0]}")
    assert not missing, "\n".join(missing)


@pytest.mark.parametrize("unit", UNITS)
def test_config_keys_known(unit):
    unknown = []
    for rel, text in _texts(unit).items():
        for m in _CONFIG_RE.finditer(text):
            key = m[1].strip("\"'")
            if key == "host-check" or key.startswith(("$", "-")) or "<" in key:
                continue
            if not config.is_known_key(key):
                unknown.append(f"{rel}: {key}")
    assert not unknown, "\n".join(unknown)


@pytest.mark.parametrize("unit", UNITS)
def test_heading_keys_known(unit):
    table = tomllib.loads((CORE / "headings.toml").read_text(encoding="utf-8"))
    unknown = [
        f"{rel}: {m[1]}"
        for rel, text in _texts(unit).items()
        for m in _HEADING_RE.finditer(text)
        if m[1] not in table
    ]
    assert not unknown, "\n".join(unknown)


@pytest.mark.parametrize("unit", UNITS)
def test_skill_links_resolve(unit):
    known = {p.name for p in _skill_dirs()} | PLANNED
    bad = []
    for rel, text in _texts(unit).items():
        for m in _LIGHTHOUSE_SKILL_RE.finditer(text):
            if m[1] not in known:
                bad.append(f"{rel}: /lighthouse-{m[1]}")
        for regex in _EXTERNAL_SKILL_RES:
            for m in regex.finditer(text):
                name = m[1]
                if name.startswith("lighthouse-"):
                    continue
                if name not in ALLOWED_EXTERNAL:
                    bad.append(f"{rel}: /{name}")
    assert not bad, "\n".join(bad)


@pytest.mark.parametrize("unit", UNITS)
def test_optional_steps_guarded(unit):
    unguarded = []
    for rel, text in _texts(unit).items():
        for para in re.split(r"\n\s*\n", text):
            if _OPTIONAL_RE.search(para) and not _GUARD_RE.search(" ".join(para.split())):
                first = next(line for line in para.splitlines() if _OPTIONAL_RE.search(line))
                unguarded.append(f"{rel}: {first.strip()[:120]}")
    assert not unguarded, "\n".join(unguarded)


def test_planned_skills_absent():
    assert not PLANNED & {p.name for p in _skill_dirs()}


@pytest.mark.parametrize("skill", SKILL_PARAMS)
def test_variants_both_present(skill):
    assert set(_variants(skill)) == {"claude", "agents"}

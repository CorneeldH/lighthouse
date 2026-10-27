"""Contract test for the skills of every package and the agents of `pack/core` (plan task
22, Review Focus 5; plan M0 (c), design choice 10).

A translation error makes a skill point at something that does not exist: a script, a
lib module, a hook, a config key, a heading or another skill. Nothing at runtime catches
that before a user runs the skill, so every reference is checked here, per skill and per
agent.

A skill names a heading as `heading:<key>` (design choice 19) and a config key as the
argument of `config.py` (design choice 6 and 7). Steps that depend on something that stays
in paia (backlog-stand, board-sweep, the controller, the release flow, the pre-push chain)
run only "if present" and otherwise report "skipped" (design choice 16).

A skill may name what its own package or `pack/core` installs. A file or skill of another
language package is there only when that package was chosen, so the paragraph that names it
says "if present" (Kruisverwijzingen).
"""

from __future__ import annotations

import re
import tomllib
from pathlib import Path

import pytest

from lighthouse_lib import config
from tests.pack._testkit import CORE, PACKS

AGENTS = CORE / "agents"
TEXT_SUFFIXES = {".md", ".sh", ".json", ".txt"}

# Skills outside lighthouse that a core skill may call: the Superpowers skills, and the two
# Claude Code built-in commands paia's skills already name (`/clear`, `/code-review`).
ALLOWED_EXTERNAL = {
    "clear",
    "code-review",
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

# Skills that are not written yet, so another skill may already link to
# them. A skill leaves this set in the commit that adds it (test_planned_skills_absent);
# the set is empty when PR 4 is done.
PLANNED: set[str] = set()

# An installed path in a skill -> where it lives in a package directory.
_PATH_REFS = (
    (re.compile(r"\.lighthouse/scripts/([\w./-]*\w)"), lambda m, pack: pack / "scripts" / m[1]),
    (
        re.compile(r"\.lighthouse/lib/lighthouse_lib/(\w+)\.py"),
        lambda m, pack: pack / "lib" / f"{m[1]}.py",
    ),
    (re.compile(r"\.claude/hooks/lighthouse/([\w.-]+)"), lambda m, pack: pack / "hooks" / m[1]),
    (
        re.compile(r"\.claude/agents/lighthouse-([\w-]+)\.md"),
        lambda m, pack: pack / "agents" / f"{m[1]}.md",
    ),
    (
        re.compile(r"\.(?:claude|agents)/skills/lighthouse-([a-z0-9-]+)/([\w./-]*\w)"),
        lambda m, pack: _skill_file(pack / "skills" / m[1], m[2]),
    ),
)


def _skill_file(skill: Path, rel: str) -> Path:
    """A file of an installed skill: shared in the skill directory, or `SKILL.md` per variant."""
    shared = skill / rel
    if rel == "SKILL.md" and not shared.is_file():
        return skill / "claude" / "SKILL.md"
    return shared


def _pack_of(unit: Path) -> Path:
    """The package directory a skill directory or an agent file belongs to."""
    return PACKS / unit.relative_to(PACKS).parts[0]


def _visible(unit: Path) -> tuple[Path, ...]:
    """The packages whose files a unit may name without "if present": core and its own."""
    return tuple(dict.fromkeys((CORE, _pack_of(unit))))
_CONFIG_RE = re.compile(r"config\.py\s+(\"[^\"]+\"|'[^']+'|[^\s`\"')|;]+)")
_HEADING_RE = re.compile(r"heading:([A-Za-z0-9_]+)")
# Not followed by `/`: `<git-common-dir>/lighthouse-tools/` is a path, not a skill.
_LIGHTHOUSE_SKILL_RE = re.compile(r"(?<![\w./-])/lighthouse-([a-z0-9-]+)(?![a-z0-9/-])")
# `/name` alone in backticks (optionally with arguments), or a namespaced `/ns:name`.
_EXTERNAL_SKILL_RES = (
    re.compile(r"`/([a-z][a-z0-9-]*(?::[a-z0-9-]+)?)(?:\s[^`]*)?`"),
    re.compile(r"(?<![\w./~-])/([a-z][a-z0-9-]*:[a-z0-9-]+)"),
)
_OPTIONAL_RE = re.compile(r"backlog-stand|board-sweep|\brelease\b|pre-push|\bcontroller\b", re.I)
_GUARD_RE = re.compile(r"if present|skipped", re.I)


def _skill_dirs() -> list[Path]:
    """Every skill of every package, core first."""
    dirs = sorted(p for p in PACKS.glob("*/skills/*") if p.is_dir())
    return sorted(dirs, key=lambda p: _pack_of(p) != CORE)


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


def _id(skill: Path) -> str:
    pack = _pack_of(skill).name
    return skill.name if pack == "core" else f"{pack}/{skill.name}"


UNITS = [pytest.param(p, id=f"skill:{_id(p)}") for p in _skill_dirs()] + [
    pytest.param(p, id=f"agent:{p.stem}") for p in sorted(AGENTS.glob("*.md"))
]
SKILL_PARAMS = [pytest.param(p, id=_id(p)) for p in _skill_dirs()]
# The paia flows below came with the core skills. In a language package "pre-push" or a
# "release" are ordinary words (a pre-push gate, the pinned release of a tool).
CORE_UNITS = [u for u in UNITS if _pack_of(u.values[0]) == CORE]


def _unit_name(unit: Path) -> str:
    return unit.name if unit.is_dir() else unit.stem


@pytest.mark.parametrize("unit", UNITS)
def test_frontmatter_name(unit):
    files = list(_variants(unit).values()) if unit.is_dir() else [unit]
    assert files, f"{unit}: no SKILL.md"
    for f in files:
        name = _frontmatter_name(f.read_text(encoding="utf-8"))
        assert name == f"lighthouse-{_unit_name(unit)}", f"{f}: name is {name!r}"


def _guarded(para: str) -> bool:
    """A paragraph that names a unit of another package only "if present" (Kruisverwijzingen)."""
    return "if present" in " ".join(para.split()).lower()


@pytest.mark.parametrize("unit", UNITS)
def test_referenced_files_exist(unit):
    """Every installed path a unit names exists in pack/core or in its own package, unless the
    paragraph names a file of another package (a language pack) "if present"."""
    missing = []
    visible = _visible(unit)
    for rel, text in _texts(unit).items():
        for para in re.split(r"\n\s*\n", text):
            for regex, target in _PATH_REFS:
                for m in regex.finditer(para):
                    found = any(target(m, pack).is_file() for pack in visible)
                    if not found and not _guarded(para):
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
    visible = _visible(unit)
    known = {p.name for p in _skill_dirs() if _pack_of(p) in visible} | PLANNED
    bad = []
    for rel, text in _texts(unit).items():
        for para in re.split(r"\n\s*\n", text):
            for m in _LIGHTHOUSE_SKILL_RE.finditer(para):
                if m[1] not in known and not _guarded(para):
                    bad.append(f"{rel}: /lighthouse-{m[1]}")
        for regex in _EXTERNAL_SKILL_RES:
            for m in regex.finditer(text):
                name = m[1]
                if name.startswith("lighthouse-"):
                    continue
                if name not in ALLOWED_EXTERNAL:
                    bad.append(f"{rel}: /{name}")
    assert not bad, "\n".join(bad)


def _paragraphs(text: str):
    """Each paragraph with the markdown headings that enclose it (outside code fences)."""
    headings: list[tuple[int, str]] = []
    in_fence = False
    for para in re.split(r"\n\s*\n", text):
        yield para, [h for _, h in headings]
        for line in para.splitlines():
            if line.lstrip().startswith("```"):
                in_fence = not in_fence
                continue
            m = re.match(r"^(#{1,6})\s", line)
            if m and not in_fence:
                level = len(m[1])
                headings = [(lvl, h) for lvl, h in headings if lvl < level] + [(level, line)]


@pytest.mark.parametrize("unit", CORE_UNITS)
def test_optional_steps_guarded(unit):
    """A paragraph that names something that stays in paia says "if present" or "skipped",
    itself or in a heading that encloses it (a whole section that applies only if present)."""
    unguarded = []
    for rel, text in _texts(unit).items():
        for para, enclosing in _paragraphs(text):
            if not _OPTIONAL_RE.search(para):
                continue
            if _GUARD_RE.search(" ".join(para.split())) or any(map(_GUARD_RE.search, enclosing)):
                continue
            first = next(line for line in para.splitlines() if _OPTIONAL_RE.search(line))
            unguarded.append(f"{rel}: {first.strip()[:120]}")
    assert not unguarded, "\n".join(unguarded)


def test_planned_skills_absent():
    assert not PLANNED & {p.name for p in _skill_dirs()}


@pytest.mark.parametrize("skill", SKILL_PARAMS)
def test_variants_both_present(skill):
    assert set(_variants(skill)) == {"claude", "agents"}


def _install_skills(root: Path) -> None:
    """Every skill under `.claude/skills` and `.agents/skills`, as `init` lays them out."""
    import shutil

    for skill in _skill_dirs():
        for variant, base in (("claude", ".claude/skills"), ("agents", ".agents/skills")):
            dest = root / base / f"lighthouse-{skill.name}"
            dest.mkdir(parents=True)
            for f in skill.iterdir():
                if f.is_file():
                    shutil.copy2(f, dest / f.name)
            shutil.copy2(_variants(skill)[variant], dest / "SKILL.md")


def test_descriptions_pass_the_overlap_check(tmp_path):
    """Check 4 of check-instructions.sh fails two descriptions that share five trigger words
    unless one carries a bounding clause it recognises ("not for", "instead", …). The core
    skills land together in every repo, and a language package adds its own, so all of them
    together must pass it."""
    import os
    import subprocess

    from tests.pack._testkit import install_core

    install_core(tmp_path)
    _install_skills(tmp_path)
    env = {
        **os.environ,
        "LIGHTHOUSE_REPO_ROOT": str(tmp_path),
        "CHECK_INSTRUCTIONS_PERSONAL": str(tmp_path / "no-personal-skills"),
    }
    out = subprocess.run(
        [str(tmp_path / ".lighthouse/scripts/check-instructions.sh")],
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    assert "overlap without a bounding clause" not in out.stdout, out.stdout


# What a skill text does that needs a tool on the machine. `lighthouse init` reports a missing
# tool by the units requires.toml names for it; a skill missing there goes unreported.
_TOOL_USES = {
    "superpowers": r"superpowers:",
    "gh": r"\bgh (pr|issue|api|project|run|repo|search|label|workflow)\b",
    "codex": r"(^|[\s`$(])codex (exec|review)\b|codex_usage",
    # A call (`jq -r`, `| jq '.x'`), not a mention like "no `jq`".
    "jq": r"\bjq\s+[-'\".]",
    "git": r"\bgit (-C|worktree|log|fetch|diff|status|rev-parse|branch|push|commit|grep|show"
    r"|merge-base|checkout|switch|ls-files|add)\b",
    "python3": r"\bpython3\b",
    "tomllib": r"lighthouse_lib/config\.py|tomllib",
}


@pytest.mark.parametrize("skill", SKILL_PARAMS)
def test_requires_names_the_skill_per_tool(skill):
    """The skill's own package names it: `init` reports the units of the chosen packages."""
    path = _pack_of(skill) / "requires.toml"
    requires = tomllib.loads(path.read_text(encoding="utf-8")) if path.is_file() else {}
    text = "\n".join(_texts(skill).values())
    missing = [
        tool
        for tool, pattern in _TOOL_USES.items()
        if re.search(pattern, text, re.M) and skill.name not in requires.get(tool, [])
    ]
    assert not missing, f"requires.toml lacks {skill.name} under {missing}"

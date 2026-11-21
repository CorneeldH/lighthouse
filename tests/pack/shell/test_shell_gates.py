"""The skill `shell-standard` and the instruction block of `pack/shell`: config instead of
paia's directories, the two harness variants, and KPI commands that really run."""

import re

import pytest

from tests.pack._testkit import PACKS, is_known_key

SHELL = PACKS / "shell"
SKILL_DIR = SHELL / "skills" / "shell-standard"
VARIANTS = {"claude": SKILL_DIR / "claude" / "SKILL.md", "agents": SKILL_DIR / "agents" / "SKILL.md"}
BLOCK = SHELL / "instructions" / "AGENTS.md"

STRICT_LOOKUP = "lighthouse config shell.strict_dirs"


def _text(variant: str) -> str:
    return VARIANTS[variant].read_text(encoding="utf-8")


def _paragraphs(text: str) -> list[str]:
    return [p for p in re.split(r"\n\s*\n", text) if p.strip()]


@pytest.mark.parametrize("variant", VARIANTS)
def test_frontmatter_name(variant):
    assert _text(variant).startswith("---\nname: lighthouse-shell-standard\n")


@pytest.mark.parametrize("variant", VARIANTS)
def test_strict_dirs_from_config(variant):
    text = _text(variant)
    assert STRICT_LOOKUP in text
    assert is_known_key("shell.strict_dirs")
    for paia_dir in ("scripts/hooks", "tests/integration"):
        assert paia_dir not in text, paia_dir


@pytest.mark.parametrize("variant", VARIANTS)
def test_paths_are_the_installed_ones(variant):
    """Every lighthouse path the skill names exists in the package."""
    text = _text(variant)
    installed = {
        ".lighthouse/scripts/shellcheck-repo.sh": SHELL / "scripts" / "shellcheck-repo.sh",
        ".lighthouse/scripts/lib/shellcheck-bin.sh": SHELL / "scripts" / "lib" / "shellcheck-bin.sh",
        ".claude/hooks/lighthouse/shell-lint-check.sh": SHELL / "hooks" / "shell-lint-check.sh",
    }
    for named, source in installed.items():
        assert named in text, named
        assert source.is_file(), source
    for ref in re.findall(r"\.lighthouse/[\w./-]+|\.claude/hooks/lighthouse/[\w.-]+", text):
        ref = ref.rstrip(".")
        assert ref in installed, ref


def test_variants_keep_their_difference():
    """The agents variant adds the OpenCode note; everything else is the claude variant."""
    claude, agents = _text("claude"), _text("agents")
    assert "OpenCode" not in claude
    note = [p for p in _paragraphs(agents) if "OpenCode does not run" in p]
    assert len(note) == 1, note
    assert ".lighthouse/scripts/shellcheck-repo.sh --severity warning -- <file>" in note[0]
    assert STRICT_LOOKUP in note[0]
    description = re.search(r"^description: .*$", agents, re.M).group(0)
    assert "In OpenCode the enforcing hook does not run" in description
    stripped = agents.replace(note[0] + "\n\n", "").replace(
        " In OpenCode the enforcing hook does not run; run the KPI commands by hand.", ""
    )
    assert stripped == claude


@pytest.mark.parametrize("variant", VARIANTS)
def test_no_paia_references(variant):
    text = _text(variant)
    for literal in (
        "paia",
        "workflow-audit",
        "gate-assert",
        "vault-write-guard",
        "ceda",
        "2026-08-",
        "#609",
        "#610",
        "#650",
        "#741",
        "test_quality_hooks",
        "ruff-format-check",
        "BLOCKING_RULES",
    ):
        assert literal.lower() not in text.lower(), literal


@pytest.mark.parametrize("variant", VARIANTS)
def test_english(variant):
    text = _text(variant).lower()
    for word in (" het ", " niet ", " een ", " wordt ", " dat ", " je ", " geen "):
        assert word not in text, word


@pytest.mark.parametrize("variant", VARIANTS)
def test_skill_links_are_lighthouse(variant):
    for link in re.findall(r"(?<![\w.])/(?:paia|lighthouse)-[\w-]*", _text(variant)):
        assert link.startswith("/lighthouse-"), link


def test_instruction_block():
    text = BLOCK.read_text(encoding="utf-8")
    assert text.startswith("## Lighthouse: shell\n")
    assert len(text.split()) <= 60, len(text.split())
    assert "COULD NOT CHECK" in text
    assert "lighthouse-shell-standard" in text


@pytest.mark.parametrize("variant", VARIANTS)
def test_outcomes_match_the_hook(variant):
    """The script has three exit codes; the hook says the same with exit 2 and the
    first line of decision 18. The skill names both, and claims 0/1/4 only for the script."""
    para = next(p for p in _paragraphs(_text(variant)) if p.startswith("**Three outcomes, not two.**"))
    assert "Every gate script here returns" not in para
    assert "`shellcheck-repo.sh`" in para
    assert "exit 2" in para
    assert "COULD NOT CHECK" in para


@pytest.mark.parametrize("variant", VARIANTS)
def test_root_advice_matches_the_hook(variant):
    """The hook says `$CLAUDE_PROJECT_DIR` is the wrong tree for a worktree edit; the
    skill must not advise it."""
    para = next(p for p in _paragraphs(_text(variant)) if "points at the wrong tree" in p)
    assert "Use `$CLAUDE_PROJECT_DIR`" not in para
    assert "`LIGHTHOUSE_REPO_ROOT`" in para
    assert "path you received" in para


CLASSIFICATION = "Class: <class>. At risk: <the observable behaviour that could change>."


@pytest.mark.parametrize("variant", VARIANTS)
def test_points_to_the_testing_skill(variant):
    domain = _text(variant).split("## Domain", 1)[1].split("\n## ", 1)[0]
    assert "lighthouse-test-standard" in domain
    assert "*Classify first*" in domain
    assert "Before the first edit" in domain
    assert CLASSIFICATION in domain


@pytest.mark.parametrize("variant", VARIANTS)
def test_shell_form_of_the_testing_norms(variant):
    text = _text(variant)
    assert "## The shell form of lighthouse-test-standard" in text
    section = text.split("## The shell form of lighthouse-test-standard", 1)[1].split("\n## ", 1)[0]
    for phrase in (
        "temporary directory",
        "the exit code and the output",
        "never read or grep the source",
        "one input that must block and one that must pass",
    ):
        assert phrase in section, phrase


@pytest.mark.parametrize("variant", VARIANTS)
def test_pairing_names_the_testing_skill(variant):
    pairing = _text(variant).split("## The pairing", 1)[1].split("\n## ", 1)[0]
    rows = [line for line in pairing.splitlines() if line.startswith("| `lighthouse-test-standard` |")]
    assert len(rows) == 1

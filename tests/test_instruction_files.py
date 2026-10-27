"""AGENTS.md is the one source of the working agreements; CLAUDE.md imports it."""

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
AGENTS = ROOT / "AGENTS.md"
CLAUDE = ROOT / "CLAUDE.md"


def test_claude_md_imports_agents_md():
    assert CLAUDE.read_text(encoding="utf-8").splitlines()[0] == "@AGENTS.md"


def test_agents_md_names_the_guards_and_the_escape_hatch():
    text = AGENTS.read_text(encoding="utf-8")
    for token in (
        "main-commit-guard",
        "worktree-guard",
        ".claude/allow-main",
        ".claude/worktrees/",
    ):
        assert token in text, token


CORE = ROOT / "pack" / "core"


def test_core_block_word_budget():
    """The core block lands in every repo's AGENTS.md, next to the repo's own rules: at most
    310 words. It carries the chain rules the core skills and hooks enforce, the `heading:<key>`
    notation, the rule for the language of code, and one line per skill."""
    text = (CORE / "instructions" / "AGENTS.md").read_text(encoding="utf-8")
    assert len(text.split()) <= 310, len(text.split())
    for token in (
        "main-commit-guard",
        ".claude/allow-main",
        "worktree",
        "squash",
        "[project] language",
        "[project] code_language",
        "code_language_paths",
        "heading:<key>",
        ".lighthouse/headings.toml",
        "on which layer a new instruction belongs",
    ):
        assert token in text, token
    for skill in sorted(p.name for p in (CORE / "skills").iterdir() if p.is_dir()):
        assert f"`/lighthouse-{skill}`" in text, skill


def test_core_block_says_how_to_read_a_heading():
    """Design choice 19: the block explains `heading:<key>` once — the command that prints the
    heading, and that reading accepts every language variant."""
    text = " ".join((CORE / "instructions" / "AGENTS.md").read_text(encoding="utf-8").split())
    assert "python3 .lighthouse/lib/lighthouse_lib/headings.py <key>" in text
    assert "never the literal `heading:<key>`" in text
    assert "reading accepts every language variant" in text


def test_core_block_says_finish_what_is_authorised():
    """#22: an agent does not hand back an action it already has permission for."""
    text = " ".join((CORE / "instructions" / "AGENTS.md").read_text(encoding="utf-8").split())
    assert (
        'Finish what you are already authorised to do; never end on an offer to do it '
        '("ready to push if you want" is not done).'
    ) in text


BLOCK_BEGIN = "<!-- lighthouse:begin core -->"
BLOCK_END = "<!-- lighthouse:end core -->"


def _project_part():
    """AGENTS.md without the managed blocks: the rules lighthouse keeps for itself."""
    text = AGENTS.read_text(encoding="utf-8")
    assert BLOCK_BEGIN in text and BLOCK_END in text
    before, rest = text.split(BLOCK_BEGIN, 1)
    return before + rest.split(BLOCK_END, 1)[1]


def test_installed_core_block_matches_the_package():
    """lighthouse dogfoods pack/core: the block in AGENTS.md is the one init installs."""
    text = AGENTS.read_text(encoding="utf-8")
    block = text.split(BLOCK_BEGIN, 1)[1].split(BLOCK_END, 1)[0]
    package = (CORE / "instructions" / "AGENTS.md").read_text(encoding="utf-8")
    assert block.strip() == package.strip()


def test_project_part_does_not_repeat_the_core_block():
    """Nothing twice: a rule the core block carries is not in the project part of AGENTS.md.
    Each token below belongs to one rule of the block, except `uv run pytest`: that keeps the
    old Tests rule, which the test gate replaced, from coming back."""
    part = _project_part()
    for token in (
        "squash",
        "git worktree add",
        "blocks `git commit`",
        "touch .claude/allow-main",
        "commit-tests",
        "--no-verify",
        "uv run pytest",
    ):
        assert token not in part, token


def test_project_part_keeps_the_lighthouse_rules():
    """The rules for lighthouse itself stay: Dutch artefacts, allow-main never committed,
    links, and the planning folders."""
    part = _project_part()
    for token in (
        "Dutch",
        ".claude/allow-main",
        "relative link",
        "docs/developer/planning/specs/",
        "docs/developer/planning/plans/",
    ):
        assert token in part, token
    assert "## Bootstrap" not in part


def test_agents_md_headings_are_unique():
    """A heading twice splits one section in two, and a reader cannot tell which is meant."""
    headings = [line for line in AGENTS.read_text(encoding="utf-8").splitlines() if line.startswith("#")]
    assert len(headings) == len(set(headings)), headings

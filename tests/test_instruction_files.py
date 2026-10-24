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
    250 words. It carries the chain rules the core skills and hooks enforce, the `heading:<key>`
    notation, and one line per skill."""
    text = (CORE / "instructions" / "AGENTS.md").read_text(encoding="utf-8")
    assert len(text.split()) <= 250, len(text.split())
    for token in (
        "main-commit-guard",
        ".claude/allow-main",
        "worktree",
        "squash",
        "[project] language",
        "heading:<key>",
        ".lighthouse/headings.toml",
    ):
        assert token in text, token
    for skill in sorted(p.name for p in (CORE / "skills").iterdir() if p.is_dir()):
        assert f"`/lighthouse-{skill}`" in text, skill

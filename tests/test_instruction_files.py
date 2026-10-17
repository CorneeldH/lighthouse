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

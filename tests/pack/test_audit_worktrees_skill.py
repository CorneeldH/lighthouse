"""Contract test for the skill `lighthouse-audit-worktrees`.

The skill describes what `lighthouse worktree-audit` prints, so its table header, its KPI fields
and the `--only` token must be the ones the command really writes; the header and the fields are
read from a run of the binary. What stays in paia (the opencode lane with its port registry,
board-sweep) is named only if present (design choice 16), and the protect list comes from
`[worktree_audit]` in `lighthouse.toml`.
"""

from __future__ import annotations

import functools
import os
import re
import subprocess
import tempfile
from pathlib import Path

from tests.pack.python._testkit import CORE, init_repo, lighthouse_dir

SKILL = (CORE / "skills" / "audit-worktrees" / "SKILL.md").read_text(encoding="utf-8")


@functools.cache
def _audit(view: str) -> str:
    """The output of `lighthouse worktree-audit <view>` in a fresh repo without `forge.repo`."""
    with tempfile.TemporaryDirectory() as tmp:
        repo = Path(tmp) / "repo"
        init_repo(repo)
        toml = Path(tmp) / "lighthouse.toml"
        toml.write_text('[project]\nlanguage = "en"\n', encoding="utf-8")
        env = dict(os.environ, HOME=tmp, LIGHTHOUSE_TOML=str(toml))
        proc = subprocess.run(
            [str(lighthouse_dir() / "lighthouse"), "worktree-audit", *view.split()],
            cwd=repo, env=env, capture_output=True, text=True, check=True, timeout=60,
        )
        return proc.stdout


def _paragraphs(text: str) -> list[str]:
    return [" ".join(p.split()) for p in re.split(r"\n\s*\n", text)]


def test_every_view_runs_the_binary():
    for view in ("", " --sessions", " --origin", " --kpi", " --only <branch>", " --issue <nr>"):
        assert f"lighthouse worktree-audit{view}" in SKILL, view
    assert "uv run" not in SKILL
    assert "python3" not in SKILL


def test_only_view_names_the_cleanup_token():
    assert "`cleanup: yes`" in SKILL
    assert "`cleanup: no — <reason>`" in SKILL
    assert "lighthouse-close-issue" in SKILL


def test_table_header_is_the_commands():
    header = _audit("--cleanup").splitlines()[0]
    assert header.startswith("| worktree | branch |")
    assert header in SKILL


def test_kpi_fields_are_the_commands():
    fields = re.findall(r"(\w+)=", _audit("--kpi"))
    assert fields == ["worktrees", "safe", "remote_branches", "warn"]
    for field in fields:
        assert f"`{field}=`" in SKILL, field


def test_protect_list_from_config():
    assert "[worktree_audit]" in SKILL
    assert "protected_refs" in SKILL and "protected_paths" in SKILL
    assert "default branch" in SKILL
    assert "PROTECTED_REFS  = (" not in SKILL


def test_worktree_audit_no_opencode():
    """The opencode lane and its port registry stay in paia: named only if present."""
    hits = [p for p in _paragraphs(SKILL) if "opencode" in p or "port-registry" in p]
    assert hits
    for para in hits:
        assert "if present" in para, para


def test_board_sweep_only_if_present():
    for para in _paragraphs(SKILL):
        if "board-sweep" in para:
            assert "if present" in para, para


def test_skill_is_detached_from_the_source_repo():
    for gone in (
        "CorneeldH",
        "personal_ai_agent",
        "deploy/beelink",
        "agentsview",
        "tests/unit/",
        "tests/integration/",
        "claude-remote-control.service",
    ):
        assert gone not in SKILL, gone
    assert not re.search(r"#\d{3,}", SKILL)


def test_skill_is_english():
    lower = SKILL.lower()
    for word in (" je ", " het ", " niet ", " geen ", " een ", " wordt "):
        assert word not in lower, word

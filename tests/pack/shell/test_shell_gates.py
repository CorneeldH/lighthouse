"""The skill `shell-gates` and the instruction block of `pack/shell`: config instead of
paia's directories, the two harness variants, and KPI commands that really run."""

import os
import re
import subprocess
from pathlib import Path

import pytest

from lighthouse_lib import config
from tests.pack._testkit import GIT_ENV, PACKS, git, init_repo, install_pack

SHELL = PACKS / "shell"
SKILL_DIR = SHELL / "skills" / "shell-gates"
VARIANTS = {"claude": SKILL_DIR / "claude" / "SKILL.md", "agents": SKILL_DIR / "agents" / "SKILL.md"}
BLOCK = SHELL / "instructions" / "AGENTS.md"

STRICT_LOOKUP = "python3 .lighthouse/lib/lighthouse_lib/config.py shell.strict_dirs"


def _text(variant: str) -> str:
    return VARIANTS[variant].read_text(encoding="utf-8")


def _paragraphs(text: str) -> list[str]:
    return [p for p in re.split(r"\n\s*\n", text) if p.strip()]


@pytest.mark.parametrize("variant", VARIANTS)
def test_frontmatter_name(variant):
    assert _text(variant).startswith("---\nname: lighthouse-shell-gates\n")


@pytest.mark.parametrize("variant", VARIANTS)
def test_strict_dirs_from_config(variant):
    text = _text(variant)
    assert STRICT_LOOKUP in text
    assert config.is_known_key("shell.strict_dirs")
    for paia_dir in ("scripts/hooks", "tests/integration"):
        assert paia_dir not in text, paia_dir


@pytest.mark.parametrize("variant", VARIANTS)
def test_paths_are_the_installed_ones(variant):
    """Every lighthouse path the skill names exists in the package."""
    text = _text(variant)
    installed = {
        ".lighthouse/scripts/shellcheck-repo.sh": SHELL / "scripts" / "shellcheck-repo.sh",
        ".lighthouse/scripts/lib/shellcheck-bin.sh": SHELL / "scripts" / "lib" / "shellcheck-bin.sh",
        ".claude/hooks/lighthouse/shellcheck.sh": SHELL / "hooks" / "shellcheck.sh",
    }
    for named, source in installed.items():
        assert named in text, named
        assert source.is_file(), source
    for ref in re.findall(r"\.lighthouse/[\w./-]+|\.claude/hooks/lighthouse/[\w.-]+", text):
        ref = ref.rstrip(".")
        assert ref in installed or ref.startswith(".lighthouse/lib/lighthouse_lib/config.py"), ref


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
        "ruff-format",
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


def _kpi_rows(text: str) -> list[tuple[str, str]]:
    """(command, target) per row of the KPI table."""
    section = text.split("## KPIs", 1)[1].split("\n## ", 1)[0]
    rows = []
    for line in section.splitlines():
        cells = [c.strip() for c in re.split(r"(?<!\\)\|", line.strip().strip("|"))]
        if len(cells) == 3 and cells[1].startswith("`"):
            rows.append((cells[1].strip("`").replace(r"\|", "|"), cells[2]))
    return rows


def test_kpi_commands_run(tmp_path, pinned_shellcheck):
    """Each KPI command runs in a repo with pack/shell installed and meets its target
    there: a KPI row nobody can run measures nothing."""
    repo = tmp_path / "repo"
    init_repo(repo)
    install_pack("shell", repo)
    (repo / "tools").mkdir()
    (repo / "tools" / "ok.sh").write_text('#!/usr/bin/env bash\necho "ok"\n', encoding="utf-8")
    git(repo, "add", "-A")
    git(repo, "commit", "-qm", "install")
    rows = _kpi_rows(_text("claude"))
    assert len(rows) >= 3, rows
    env = dict(os.environ, **GIT_ENV, SHELLCHECK_BIN=pinned_shellcheck, LIGHTHOUSE_REPO_ROOT=str(repo))
    for command, target in rows:
        want = re.fullmatch(r"rc (\d)", target)
        assert want, target
        result = subprocess.run(
            ["bash", "-c", command], cwd=repo, env=env, capture_output=True, text=True, check=False
        )
        assert result.returncode == int(want.group(1)), (command, result.stdout, result.stderr)


def test_instruction_block():
    text = BLOCK.read_text(encoding="utf-8")
    assert text.startswith("## Lighthouse: shell\n")
    assert len(text.split()) <= 60, len(text.split())
    assert "COULD NOT CHECK" in text
    assert "lighthouse-shell-gates" in text


def test_detect_rule():
    text = (SHELL / "detect.toml").read_text(encoding="utf-8")
    assert re.search(r'^files = \["\*\*/\*\.sh", "\*\*/\*\.bash"\]$', text, re.M), text


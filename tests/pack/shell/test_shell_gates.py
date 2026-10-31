"""The skill `shell-standard` and the instruction block of `pack/shell`: config instead of
paia's directories, the two harness variants, and KPI commands that really run."""

import os
import re
import subprocess
from pathlib import Path

import pytest

from lighthouse_lib import config
from tests.pack._testkit import GIT_ENV, PACKS, git, init_repo, install_pack

SHELL = PACKS / "shell"
SKILL_DIR = SHELL / "skills" / "shell-standard"
VARIANTS = {"claude": SKILL_DIR / "claude" / "SKILL.md", "agents": SKILL_DIR / "agents" / "SKILL.md"}
BLOCK = SHELL / "instructions" / "AGENTS.md"

STRICT_LOOKUP = "python3 .lighthouse/lib/lighthouse_lib/config.py shell.strict_dirs"


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
        ".claude/hooks/lighthouse/shell-lint-check.sh": SHELL / "hooks" / "shell-lint-check.sh",
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
    assert "lighthouse-shell-standard" in text


def test_detect_rule():
    text = (SHELL / "detect.toml").read_text(encoding="utf-8")
    assert re.search(r'^files = \["\*\*/\*\.sh", "\*\*/\*\.bash"\]$', text, re.M), text



def _kpi_fixture(tmp_path: Path) -> tuple[Path, dict]:
    repo = tmp_path / "repo"
    init_repo(repo)
    install_pack("shell", repo)
    git(repo, "add", "-A")
    git(repo, "commit", "-qm", "install")
    return repo, {"LIGHTHOUSE_REPO_ROOT": str(repo)}


def _run_kpi(repo: Path, command: str, env: dict) -> int:
    return subprocess.run(
        ["bash", "-c", command], cwd=repo, env=dict(os.environ, **GIT_ENV, **env), capture_output=True, check=False
    ).returncode


def test_kpi_rows_go_red(tmp_path, pinned_shellcheck):
    """Each row misses its target once the thing it measures is broken."""
    (strict, _), (all_shell, _), (ignored, _) = _kpi_rows(_text("claude"))
    repo, env = _kpi_fixture(tmp_path)
    env["SHELLCHECK_BIN"] = pinned_shellcheck
    style_only = '#!/usr/bin/env bash\nv="a b"\necho "$v" | sed \'s/a/c/\'\n'
    warning = '#!/usr/bin/env bash\nf() {\n  local x=1\n  echo "hoi"\n}\nf\n'
    (repo / ".claude" / "hooks" / "style.sh").write_text(style_only, encoding="utf-8")
    assert _run_kpi(repo, strict, env) != 0
    (repo / "tools").mkdir()
    (repo / "tools" / "warn.bash").write_text(warning, encoding="utf-8")
    assert _run_kpi(repo, all_shell, env) != 0
    (repo / ".gitignore").write_text("lib/\n", encoding="utf-8")
    (repo / ".claude" / "hooks" / "lib").mkdir()
    (repo / ".claude" / "hooks" / "lib" / "a.bash").write_text("#!/bin/bash\n", encoding="utf-8")
    assert _run_kpi(repo, ignored, env) != 1


def test_kpi_gitignore_row_is_not_green_on_broken_config(tmp_path):
    (_, _), (_, _), (ignored, _) = _kpi_rows(_text("claude"))
    repo, env = _kpi_fixture(tmp_path)
    toml = tmp_path / "broken.toml"
    toml.write_text('[project]\nlanguage = "en"\n\n[shell]\nstrict_dirs = ".claude/hooks"\n', encoding="utf-8")
    env["LIGHTHOUSE_TOML"] = str(toml)
    assert _run_kpi(repo, ignored, env) != 1


def test_kpi_gitignore_row_handles_a_space(tmp_path):
    (_, _), (_, _), (ignored, _) = _kpi_rows(_text("claude"))
    repo, env = _kpi_fixture(tmp_path)
    toml = tmp_path / "space.toml"
    toml.write_text('[project]\nlanguage = "en"\n\n[shell]\nstrict_dirs = ["my hooks"]\n', encoding="utf-8")
    env["LIGHTHOUSE_TOML"] = str(toml)
    (repo / ".gitignore").write_text("lib/\n", encoding="utf-8")
    (repo / "my hooks" / "lib").mkdir(parents=True)
    (repo / "my hooks" / "lib" / "a.sh").write_text("#!/bin/bash\n", encoding="utf-8")
    assert _run_kpi(repo, ignored, env) == 0


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


CLASSIFICATION = "This is a <class> change; the likely regression surface is <observable behaviour>."


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

"""The skill `python-writing` of `pack/python`: a KPI table whose every row is a
command, the measurement rules, and no path or pin that only the source repository has.

From the source repository's `test_instructielagen.py:430-497`. Left out there: the
`binding`/`origin` frontmatter (the source repository's own instruction-layer check),
the pointer in its AGENTS.md and the ruff pin read from its own `.ruff-version`.
"""

import re
from pathlib import Path

from lighthouse_lib import config

SKILL_DIR = Path(__file__).resolve().parents[3] / "pack" / "python" / "skills" / "python-writing"
CLAUDE = SKILL_DIR / "claude" / "SKILL.md"
AGENTS = SKILL_DIR / "agents" / "SKILL.md"
VARIANTS = (CLAUDE, AGENTS)

RATCHET = SKILL_DIR.parents[1] / "scripts" / "quality" / "ratchet.py"


def _complexity_heading() -> str:
    """The section `ratchet.py` sends a developer to when a function is over a hard
    limit: the heading in `COMPLEXITY_SKILL`."""
    match = re.search(r"COMPLEXITY_SKILL = \((.*?)\)", RATCHET.read_text(encoding="utf-8"), re.S)
    assert match, "COMPLEXITY_SKILL not found in ratchet.py"
    text = "".join(re.findall(r'"([^"]*)"', match.group(1)))
    heading = re.search(r"`(## [^`]+)`", text)
    assert heading, text
    return heading.group(1)


def _frontmatter(path: Path) -> str:
    text = path.read_text(encoding="utf-8")
    assert text.startswith("---\n"), f"{path} has no frontmatter"
    return text.split("---\n", 2)[1]


def _kpi_rows(path: Path) -> list[str]:
    lines = path.read_text(encoding="utf-8").splitlines()
    start = next(i for i, line in enumerate(lines) if line.startswith("| Norm |"))
    rows = []
    for line in lines[start + 2 :]:
        if not line.startswith("|"):
            break
        rows.append(line)
    return rows


def test_frontmatter_name():
    for path in VARIANTS:
        assert "name: lighthouse-python-writing\n" in _frontmatter(path), path


def test_description_has_a_negative_boundary():
    """Descriptions stay in context, and the model picks by likeness."""
    for path in VARIANTS:
        front = _frontmatter(path)
        assert "NOT for" in front, path
        assert "lighthouse-shell-gates" in front, f"{path}: the boundary to shell is missing"


def test_kpi_rows_all_carry_a_command():
    """No command, no KPI. Every table row carries one."""
    for path in VARIANTS:
        rows = _kpi_rows(path)
        assert len(rows) >= 4, f"{path}: KPI table has only {len(rows)} rows"
        for row in rows:
            assert "`" in row.split("|")[2], f"KPI row without a command: {row}"


def test_kpi_commands_from_config():
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        assert "config.py test_gate.command" in text, path
        assert "config.py python.source_dirs" in text, path
        assert "config.py python.lint_dirs" in text, path
        assert "src/" not in text, path
        assert "tests/unit" not in text, path


def test_config_keys_known():
    """Every `config.py <key>` the skill runs is a key the config reader knows."""
    for path in VARIANTS:
        keys = re.findall(r"config\.py ([a-z_][a-z_.]*)", path.read_text(encoding="utf-8"))
        assert keys, path
        for key in keys:
            assert config.is_known_key(key), f"{path}: unknown config key {key}"


def test_ratchet_by_path():
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        assert "python3 .lighthouse/scripts/quality/ratchet.py coverage" in text, path
        assert "python3 .lighthouse/scripts/quality/ratchet.py ruff" in text, path
        assert "scripts/quality/ratchet.py" not in text.replace(
            ".lighthouse/scripts/quality/ratchet.py", ""
        ), f"{path}: ratchet.py outside .lighthouse/scripts/"


def test_complexity_heading_matches_ratchet():
    for path in VARIANTS:
        lines = path.read_text(encoding="utf-8").splitlines()
        assert _complexity_heading() in lines, path


def test_ruff_pin_from_file():
    """The ruff pin is the project's `.ruff-version`, as in the gate; never a literal."""
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        assert 'uvx ruff@"$(cat .ruff-version)"' in text, path
        assert not re.search(r"ruff@\d", text), f"{path}: a literal ruff pin"


def test_hooks_by_installed_path():
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        assert ".claude/hooks/lighthouse/ruff-format.sh" in text, path
        assert ".claude/hooks/lighthouse/ty-check.sh" in text, path
        assert ".claude/hooks/ruff-format.sh" not in text, path


def test_hooks_are_not_fail_open():
    """`ruff-format` blocks when it could not check (decision 18); the skill says so."""
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        assert "fail-open" not in text, path
        assert "COULD NOT CHECK" in text, path


def test_rules_file_if_present():
    for path in VARIANTS:
        for p in re.split(r"\n\s*\n", path.read_text(encoding="utf-8")):
            if ".claude/rules/python.md" in p:
                assert "if present" in p, f"{path}: rules file without 'if present':\n{p}"
        assert "](../../rules/" not in path.read_text(encoding="utf-8"), path


def test_variants_keep_their_difference():
    """Only the OpenCode variant says the hooks do not run there."""
    claude, agents = CLAUDE.read_text(encoding="utf-8"), AGENTS.read_text(encoding="utf-8")
    assert "OpenCode" not in claude
    assert "OpenCode does not run these hooks" in agents
    assert "In OpenCode the enforcing hooks do not run" in _frontmatter(AGENTS)


def test_skill_links_are_lighthouse_or_external():
    for path in VARIANTS:
        for link in re.findall(r"(?<![\w.])/([a-z][\w-]*)", path.read_text(encoding="utf-8")):
            assert not link.startswith("paia-"), link


def test_english():
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8").lower()
        for word in (" het ", " niet ", " een ", " wordt ", " dat ", " je "):
            assert word not in text, f"{path}: {word!r}"


def test_no_source_measurements():
    """Issue numbers, dates, counts and the source repository's own paths stay out."""
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        for literal in ("2026-08", "#562", "#806", "#1016", "pyannote", "rumps", "apps/", "vendor/"):
            assert literal not in text, f"{path}: {literal}"
        assert not re.search(r"#\d{3,}", text), f"{path}: an issue number"


def test_the_ratchet_names_its_project_files():
    """Without these three files the ratchet stops; the skill says so, so an agent in a
    fresh repository does not chase a missing baseline."""
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        for name in ("quality-baseline.toml", ".ruff-version", ".complexipy-version"):
            assert name in text, f"{path}: {name}"


def test_description_triggers_on_planning():
    for path in VARIANTS:
        front = _frontmatter(path)
        for word in ("refactor", "merging modules", "test strategy"):
            assert word in front, f"{path}: {word}"


def test_points_to_the_testing_skill():
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        assert "lighthouse-testing" in text, path
        assert "<observable behaviour>." in text, f"{path}: the classification sentence"
        assert "This is a <class> change" in text, f"{path}: the classification sentence"


def test_python_form_of_the_testing_norms():
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        assert "## The Python form of lighthouse-testing" in text, path
        for tool in ("pytest.mark.parametrize", "create_autospec", "tmp_path", "subprocess.run"):
            assert tool in text, f"{path}: {tool}"


def test_reporting_done_runs_every_kpi_row():
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        assert "## Reporting done" in text, path
        section = text.split("## Reporting done", 1)[1].split("\n## ", 1)[0]
        for word in ("command", "outcome", "extras", "not measurable"):
            assert word in section, f"{path}: {word}"


def test_pairing_names_the_testing_skill():
    for path in VARIANTS:
        pairing = path.read_text(encoding="utf-8").split("## The pairing", 1)[1].split("\n## ", 1)[0]
        assert "lighthouse-testing" in pairing, path


def test_reporting_done_matches_the_optional_row_4():
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        section = text.split("## Reporting done", 1)[1].split("\n## ", 1)[0]
        assert "rows 1 to 3" in section, path
        assert "not only the one you think you touched" not in section, path

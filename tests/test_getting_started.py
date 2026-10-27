"""docs/getting-started.md stays in step with the repo: tools, install, init, config, chain."""

import re
import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
GUIDE = ROOT / "docs" / "getting-started.md"


def section(title: str) -> str:
    """The text between `## <title>` and the next `## ` heading of the guide."""
    text = GUIDE.read_text(encoding="utf-8")
    match = re.search(rf"^## {re.escape(title)}\n(.*?)(?=^## |\Z)", text, re.M | re.S)
    assert match, f"docs/getting-started.md lacks the heading '## {title}'"
    return match.group(1)


def table_rows(text: str) -> list[list[str]]:
    """The cells of every markdown table row, without the header and separator rows."""
    rows, line_in_table = [], 0
    for line in text.splitlines():
        line_in_table = line_in_table + 1 if line.startswith("|") else 0
        if line_in_table > 2:
            rows.append([cell.strip() for cell in line.strip().strip("|").split("|")])
    return rows


def required_tools() -> set[str]:
    tools = set()
    for path in sorted((ROOT / "pack").glob("*/requires.toml")):
        tools |= set(tomllib.loads(path.read_text(encoding="utf-8")))
    return tools


def first_cells() -> list[str]:
    return [row[0] for row in table_rows(section("Requirements"))]


def named_tools() -> set[str]:
    return {span for cell in first_cells() for span in re.findall(r"`([^`]+)`", cell)}


def test_requirements_table_names_every_tool():
    missing = sorted(required_tools() - named_tools())
    assert not missing, f"docs/getting-started.md Requirements lacks {missing}"


def test_requirements_match_code_spans_exactly():
    unknown = sorted(named_tools() - required_tools())
    assert not unknown, f"Requirements names tools no requires.toml has: {unknown}"


def test_requirements_name_claude_code():
    assert any("Claude Code" in cell for cell in first_cells())


def test_requirements_say_what_falls_away():
    rows = table_rows(section("Requirements"))
    empty = [row[0] for row in rows if not row[-1].strip()]
    assert not empty, f"no 'Without it' text for {empty}"

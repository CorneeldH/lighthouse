"""docs/getting-started.md stays in step with the repo: tools, install, init, config, chain."""

import os
import re
import subprocess
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


def test_install_offers_binary_then_go():
    """The binary route comes first; the Go route stays, without the private-repo detour."""
    module = (ROOT / "go.mod").read_text(encoding="utf-8").splitlines()[0].split()[1]
    text = section("Install")
    assert "releases/latest/download/lighthouse_linux_amd64" in text and "SHA256SUMS" in text
    assert f"go install {module}/cmd/lighthouse@" in text
    assert "GOPRIVATE" not in text and "not published yet" not in text
    assert "mv lighthouse_linux_amd64 ~/.local/bin/lighthouse" in text
    assert "~/go/bin/lighthouse" not in text
    assert "https://corneeldh.github.io/lighthouse/" in text


def test_init_prompt_matches_the_binary():
    src = (ROOT / "internal/initcmd/initcmd.go").read_text(encoding="utf-8")
    prompt = re.search(r'ask\(o\.Out, in, "([^"]+)"\)', src).group(1).strip()
    assert prompt in section("Run lighthouse init")


def init_flags() -> set[str]:
    src = (ROOT / "internal/cli/init.go").read_text(encoding="utf-8")
    return set(re.findall(r'Var\(&o\.\w+, "([\w-]+)"', src))


def test_init_flags_exist():
    spans = re.findall(r"`([^`]*)`", section("Run lighthouse init"))
    used = {flag for span in spans for flag in re.findall(r"--(\w[\w-]*)", span)}
    assert {"yes", "upgrade"} <= used
    assert used <= init_flags(), f"guide names unknown init flags {sorted(used - init_flags())}"


def test_config_example_loads(tmp_path):
    block = re.search(r"```toml\n(.*?)```", section("Configure lighthouse.toml"), re.S).group(1)
    (tmp_path / "lighthouse.toml").write_text(block, encoding="utf-8")
    env = {**os.environ, "LIGHTHOUSE_REPO_ROOT": str(tmp_path)}
    for key, want in [("project.language", "en"), ("forge.kind", "github")]:
        out = subprocess.run(
            ["python3", str(ROOT / ".lighthouse/lib/lighthouse_lib/config.py"), key],
            env=env,
            capture_output=True,
            text=True,
            check=False,
        )
        assert out.returncode == 0, out.stderr
        assert out.stdout.strip() == want


CHAIN = ["/lighthouse-write-issue", "/lighthouse-start-issue", "/lighthouse-verify-pr"]


def test_first_issue_walks_the_chain_in_order():
    text = section("Your first issue")
    found = [re.search(rf"{re.escape(cmd)}(?![\w-])", text) for cmd in CHAIN]
    assert None not in found, f"missing from the chain: {[c for c, m in zip(CHAIN, found) if not m]}"
    positions = [m.start() for m in found]
    assert positions == sorted(positions)


def test_first_issue_names_only_existing_skills():
    named = set(re.findall(r"/lighthouse-([\w-]+)", section("Your first issue")))
    skills = {p.name for p in (ROOT / "pack").glob("*/skills/*")}
    assert named <= skills, f"unknown skills {sorted(named - skills)}"


def test_readme_links_the_guide():
    assert "](docs/getting-started.md)" in (ROOT / "README.md").read_text(encoding="utf-8")

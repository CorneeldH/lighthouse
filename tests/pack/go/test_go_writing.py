"""The skill `go-writing` of `pack/go`: a KPI table whose every row is a command, the
measurement rules with their environment, and the pairing with the hooks of `pack/go`.

Modelled on `tests/pack/python/test_python_writing.py`; the content is fixed by the spec
`docs/developer/planning/specs/2026-10-25-pack-go-design.md`, section *De skill*.
"""

import re
from pathlib import Path

from lighthouse_lib import config

SKILL_DIR = Path(__file__).resolve().parents[3] / "pack" / "go" / "skills" / "go-writing"
CLAUDE = SKILL_DIR / "claude" / "SKILL.md"
AGENTS = SKILL_DIR / "agents" / "SKILL.md"
VARIANTS = (CLAUDE, AGENTS)

SECTIONS = (
    "## Domain",
    "## KPIs",
    "## Non-negotiable",
    "## Reporting done",
    "## Merging or deepening: replace, don't layer",
    "## The Go form of lighthouse-testing",
    "## Reaching a complexity limit: deepen, don't split",
    "## The pairing",
    "## Known pitfalls",
)

OPENCODE_DESCRIPTION = " In OpenCode the enforcing hooks do not run — run the KPI commands by hand."


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


def _section(text: str, heading: str) -> str:
    return text.split(heading, 1)[1].split("\n## ", 1)[0]


def _paragraphs(text: str) -> list[str]:
    return re.split(r"\n\s*\n", text)


def test_frontmatter_name():
    for path in VARIANTS:
        assert "name: lighthouse-go-writing\n" in _frontmatter(path), path


def test_description_has_a_negative_boundary():
    """Descriptions stay in context, and the model picks by likeness: Go work must not pull
    in the Python or the shell skill, nor the other way round."""
    for path in VARIANTS:
        front = _frontmatter(path)
        assert "NOT for" in front, path
        assert "lighthouse-python-writing" in front, f"{path}: the boundary to Python is missing"
        assert "lighthouse-shell-gates" in front, f"{path}: the boundary to shell is missing"


def test_description_triggers_on_planning():
    for path in VARIANTS:
        front = _frontmatter(path)
        for word in (
            "writing or changing Go",
            "_test.go",
            "planning a refactor",
            "before a file is open",
        ):
            assert word in front, f"{path}: {word}"


def test_sections_in_order():
    for path in VARIANTS:
        lines = path.read_text(encoding="utf-8").splitlines()
        positions = [lines.index(heading) for heading in SECTIONS]
        assert positions == sorted(positions), f"{path}: sections out of order"


def test_kpi_rows_all_carry_a_command():
    """No command, no KPI. Every table row carries one."""
    for path in VARIANTS:
        rows = _kpi_rows(path)
        assert len(rows) >= 6, f"{path}: KPI table has only {len(rows)} rows"
        for row in rows:
            assert "`" in row.split("|")[2], f"KPI row without a command: {row}"


def test_test_row_shuffles():
    """`-shuffle=on` is the default, so an order dependency between tests shows; `-count=1`
    is no default (it only helps against a stale cache in a run without shuffle)."""
    for path in VARIANTS:
        rows = _kpi_rows(path)
        assert any("`go test -race -shuffle=on ./...`" in row for row in rows), path
        assert not any("-count=1" in row for row in rows), path



def test_selector_unchecked_exit_code():
    """The selector's "could not check" exit 3 goes into `unchecked_exit_codes` (#80)."""
    for path in VARIANTS:
        text = " ".join(path.read_text(encoding="utf-8").split())
        assert "list 3 in `[test_gate] unchecked_exit_codes`" in text, path

def test_kpi_rows():
    for path in VARIANTS:
        rows = "\n".join(_kpi_rows(path))
        for command in (
            r"`git ls-files -z '*.go' \| xargs -0r gofmt -l`",
            "`go mod tidy -diff`",
            "`go vet ./...`",
            "`golangci-lint run ./...`",
            "test_gate.command",
        ):
            assert command in rows, f"{path}: {command}"


def test_config_keys_are_known():
    """Every `config.py <key>` the skill runs is a key the config reader knows."""
    for path in VARIANTS:
        keys = re.findall(r"config\.py ([a-z_][a-z_.]*)", path.read_text(encoding="utf-8"))
        assert keys, path
        for key in keys:
            assert config.is_known_key(key), f"{path}: unknown config key {key}"


def test_points_to_the_testing_skill():
    """The class table lives in `lighthouse-testing`; this skill only asks for the sentence."""
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        assert "lighthouse-testing" in text, path
        assert "Classify first" in text, path
        assert (
            "This is a <class> change; the likely regression surface is <observable behaviour>."
            in text
        ), f"{path}: the classification sentence"
        assert "`pure-logic`" not in text, f"{path}: a class table of its own"


def test_go_form_of_the_testing_norms():
    for path in VARIANTS:
        section = _section(path.read_text(encoding="utf-8"), "## The Go form of lighthouse-testing")
        for tool in ("t.TempDir()", "httptest", "testify", "require", "want"):
            assert tool in section, f"{path}: {tool}"


def test_reporting_done():
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        assert "## Reporting done" in text, path
        section = _section(text, "## Reporting done")
        assert "A row left out of the report reads as passed." in section, path
        for word in ("command", "outcome", "environment", "not measurable"):
            assert word in section, f"{path}: {word}"


def test_names_the_environment():
    """A zero without its environment is not a zero, and there are three outcomes."""
    for path in VARIANTS:
        section = _section(path.read_text(encoding="utf-8"), "## Non-negotiable")
        for word in ("GOOS", "-race", "GOWORK", "not measurable from here"):
            assert word in section, f"{path}: {word}"


def test_pairing_go_check_row():
    """`go-check.sh` runs only `golangci-lint run ./...`; `govet` is in its standard set, and
    there is no separate `go vet` in the hook."""
    for path in VARIANTS:
        pairing = _section(path.read_text(encoding="utf-8"), "## The pairing")
        assert "lighthouse-testing" in pairing, path
        assert ".claude/hooks/lighthouse/gofmt.sh" in pairing, path
        rows = [
            line for line in pairing.splitlines() if line.startswith("|") and "go-check.sh" in line
        ]
        assert len(rows) == 1, f"{path}: {rows}"
        assert "golangci-lint" in rows[0], rows[0]
        assert "govet" in rows[0], rows[0]
        assert "go vet" not in rows[0], rows[0]


def test_vet_row_caveat_with_new_from_rev():
    """With `issues.new-from-rev` the lint row measures only new findings, but the KPI row
    `go vet ./...` does not read `.golangci.yml`: the skill says to report it with that caveat."""
    for path in VARIANTS:
        # One list item or paragraph: the caveat must stand with `issues.new-from-rev`.
        items = re.split(r"\n\s*\n|\n(?=- )", path.read_text(encoding="utf-8"))
        items = [i for i in items if "issues.new-from-rev" in i]
        assert items, f"{path}: issues.new-from-rev"
        assert any("`go vet ./...`" in i and ".golangci.yml" in i for i in items), path


def test_no_concept_leftovers():
    """The concept from #22 had a blocking config, `-count=1` by default and a rules file;
    none of those exist in a lighthouse repo, and a rules file only "if present"."""
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        assert "golangci.blocking" not in text, path
        assert "-count=1 ./..." not in text, path
        assert "fail-open" not in text, path
        for line in text.splitlines():
            if ".claude/rules/go.md" in line:
                assert "if present" in line, f"{path}: {line}"


def test_variants_keep_their_difference():
    """Only the OpenCode variant says the hooks do not run there, and that is the only
    difference: without its OpenCode sentence and paragraphs it is the Claude variant."""
    claude, agents = (
        CLAUDE.read_text(encoding="utf-8"),
        AGENTS.read_text(encoding="utf-8"),
    )
    assert "OpenCode" not in claude
    assert "OpenCode does not run these hooks" in agents
    assert OPENCODE_DESCRIPTION in _frontmatter(AGENTS)
    stripped = [
        p for p in _paragraphs(agents.replace(OPENCODE_DESCRIPTION, "")) if "OpenCode" not in p
    ]
    assert stripped == _paragraphs(claude)


def test_opencode_names_the_hook_commands():
    """OpenCode runs no hooks, so the agents variant names what to run by hand instead."""
    pairing = _section(AGENTS.read_text(encoding="utf-8"), "## The pairing")
    note = next(p for p in _paragraphs(pairing) if "OpenCode does not run these hooks" in p)
    assert "`gofmt -w <file>`" in note, note
    assert "`golangci-lint run ./...`" in note, note
    assert "go vet" not in note, note


def test_english():
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8").lower()
        for word in (" het ", " niet ", " een ", " wordt ", " dat ", " je "):
            assert word not in text, f"{path}: {word!r}"

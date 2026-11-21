"""Units that must not call `gh` for a forge operation (#407).

The check is per line. `gh run` and `gh release` are covered too: they are no
forge operation in the sense of the acceptance criterion, but these skills no
longer need them.
"""

import re
import tomllib
from pathlib import Path

import pytest

from tests.pack._testkit import CORE, PACKS

# A literal copy of `_TOOL_USES` from the removed `test_skill_contracts.py` (#367).
_TOOL_USES = {
    "superpowers": r"superpowers:",
    "gh": r"\bgh (pr|issue|api|project|run|repo|search|label|workflow)\b",
    "codex": r"(^|[\s`$(])codex (exec|review)\b|codex-usage",
    # A call (`jq -r`, `| jq '.x'`), not a mention like "no `jq`".
    "jq": r"\bjq\s+[-'\".]",
    "git": r"\bgit (-C|worktree|log|fetch|diff|status|rev-parse|branch|push|commit|grep|show"
    r"|merge-base|checkout|switch|ls-files|add)\b",
    "python3": r"\bpython3\b",
    # A lighthouse Python script reads lighthouse.toml through tomllib; `lighthouse config`
    # does not (#274).
    "tomllib": r"python3 \.lighthouse/scripts/|tomllib",
}

# The same `gh <command>` alternation requires.toml uses, plus release (see the docstring).
FORGE_OP = re.compile(_TOOL_USES["gh"].replace("(pr|", "(release|pr|", 1))
UNITS = ["review-plan", "upgrade", "audit-worktrees", "write-handoff", "fix-pr"]


@pytest.mark.parametrize("unit", UNITS)
def test_unit_calls_no_gh_for_a_forge_operation(unit: str) -> None:
    hits = [
        f"{p.relative_to(CORE)}:{i}: {line.strip()}"
        for p in sorted((CORE / "skills" / unit).rglob("*.md"))
        for i, line in enumerate(p.read_text(encoding="utf-8").splitlines(), 1)
        if FORGE_OP.search(line)
    ]
    assert hits == []


def test_gh_is_required_only_on_github() -> None:
    for path in sorted(PACKS.glob("*/requires.toml")):
        data = tomllib.loads(path.read_text(encoding="utf-8"))
        assert "gh" not in data, path
        assert set(data.get("forge", {})) <= {"github", "gitlab"}, path


@pytest.mark.parametrize("kind", ["github", "gitlab"])
def test_forge_reference_has_ci_section(kind: str) -> None:
    text = (CORE / "skills" / "forge" / f"{kind}.md").read_text(encoding="utf-8")
    assert "\n## §ci\n" in text


@pytest.mark.parametrize("kind", ["github", "gitlab"])
def test_forge_files_have_workflows_section(kind: str) -> None:
    text = (CORE / "skills" / "forge" / f"{kind}.md").read_text(encoding="utf-8")
    assert "\n## §workflows\n" in text


def test_requires_toml_drops_gh_for_converted_units() -> None:
    data = tomllib.loads((CORE / "requires.toml").read_text(encoding="utf-8"))
    for unit in ("write-handoff", "audit-worktrees"):
        assert unit not in data["forge"]["github"]["gh"], unit
        assert unit in data["git"], unit


# Go sources of the converted units. forge_label.go is the CLI side of internal/labels.
GO_ROOT = Path(__file__).resolve().parents[2]
GO_UNIT_SOURCES = [
    "internal/postartefact",
    "internal/labels",
    "internal/worktreeaudit",
    "internal/cli/postartefact.go",
    "internal/cli/worktreeaudit.go",
    "internal/cli/forge_label.go",
]
# `gh` stays legitimate only outside the scanned sources: internal/forge/token.go
# (token lookup) and internal/cli/ask.go. Listed so the exception is explicit.
GO_GH_ALLOWLIST = {"internal/forge/token.go", "internal/cli/ask.go"}
# `gh` as a program name (quoted argument) or `gh <forge command>` in prose or strings.
GO_GH_CALL = re.compile(r'"gh"|\bgh\s+(label|api|pr|issue)\b')


def _go_sources() -> list[Path]:
    files: list[Path] = []
    for entry in GO_UNIT_SOURCES:
        path = GO_ROOT / entry
        found = path.rglob("*.go") if path.is_dir() else [path]
        files += [f for f in found if not f.name.endswith("_test.go")]
    return sorted(files)


def test_go_units_do_not_call_gh() -> None:
    sources = _go_sources()
    assert sources, "no Go sources found: the scan list is stale"
    assert not {s.relative_to(GO_ROOT).as_posix() for s in sources} & GO_GH_ALLOWLIST
    hits = [
        f"{p.relative_to(GO_ROOT)}:{i}: {line.strip()}"
        for p in sources
        for i, line in enumerate(p.read_text(encoding="utf-8").splitlines(), 1)
        if GO_GH_CALL.search(line)
    ]
    assert hits == []

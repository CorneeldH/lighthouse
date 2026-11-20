"""Units that must not call `gh` for a forge operation (#407).

The check is per line. `gh run` and `gh release` are covered too: they are no
forge operation in the sense of the acceptance criterion, but these skills no
longer need them.
"""

import re
import tomllib

import pytest

from tests.pack._testkit import CORE, PACKS
from tests.pack.test_skill_contracts import _TOOL_USES

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

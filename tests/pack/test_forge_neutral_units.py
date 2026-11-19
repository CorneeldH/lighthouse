"""Units that must not call `gh` for a forge operation (#407).

The check is per line. `gh run` and `gh release` are covered too: they are no
forge operation in the sense of the acceptance criterion, but these skills no
longer need them.
"""

import re
import tomllib

import pytest

from tests.pack._testkit import CORE, PACKS

FORGE_OP = re.compile(r"\bgh\s+(issue|pr|api|label|project|release|run)\b")
UNITS = ["review-plan", "upgrade", "audit-worktrees", "write-handoff"]


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

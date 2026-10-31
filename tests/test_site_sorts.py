"""One table of sorts in map.js: the glyph on a chip follows what the thing is, not what starts it,
and the legends of the flow maps name only sorts of that table."""

import json
import re

import pytest

from tests._site_node import run_map
from tests.test_site_chain import REPO_BLOB, SITE

SPEC_GLYPHS = {
    "program": "⛯", "workflow-skill": "↪", "standard-skill": "✓", "agent": "◆", "hook": "⚡",
    "rules": "§", "config": "⚙", "github": "⑂", "loop": "∞", "session": "❯", "you": "▣",
}


def test_every_sort_has_the_glyph_of_the_spec():
    assert run_map("Object.fromEntries(Object.entries(m.SORTS).map(([k, v]) => [k, v.glyph]))") == SPEC_GLYPHS


SKILL = REPO_BLOB + "pack/core/skills/testing"


@pytest.mark.parametrize("item, glyph", [
    ({"kind": "lighthouse", "trigger": "description", "source": SKILL}, "✓"),
    ({"kind": "lighthouse", "trigger": "you", "source": SKILL}, "↪"),
    ({"kind": "superpowers", "trigger": "skill"}, "↪"),
    ({"kind": "superpowers", "trigger": "description"}, "✓"),
    ({"kind": "lighthouse", "trigger": "skill", "source": REPO_BLOB + "pack/core/agents/issue-impact-scout.md"}, "◆"),
    ({"kind": "hook", "trigger": "event"}, "⚡"),
    ({"kind": "hook", "trigger": "tool"}, "⚡"),
    ({"kind": "github", "trigger": "github"}, "⑂"),
    ({"kind": "github", "trigger": "github", "source": REPO_BLOB + "pack/core/workflows/unblock-dependents.yml"}, "⑂"),
    ({"kind": "lighthouse", "trigger": "skill", "form": "tool"}, "▣"),
    ({"kind": "lighthouse", "trigger": "github", "form": "dispatcher"}, "⛯"),
    ({"kind": "lighthouse", "form": "rules"}, "§"),
])
def test_glyph_follows_the_sort_not_the_trigger(item, glyph):
    assert run_map(f"m.glyphOf({json.dumps(item)})") == glyph


@pytest.mark.parametrize("page", ["chain.html", "session.html"])
def test_how_it_starts_legend_is_gone(page):
    assert "How it starts" not in (SITE / page).read_text(encoding="utf-8")


def test_legends_name_known_sorts():
    known = set(run_map("Object.keys(m.SORTS)"))
    for page in ["chain.html", "session.html"]:
        found = re.findall(r'data-sorts="([^"]*)"', (SITE / page).read_text(encoding="utf-8"))
        assert found, f"{page} has no data-sorts legend"
        for names in found:
            assert set(names.split()) <= known, f"{page}: unknown sort in {names!r}"


def test_config_token_in_every_theme():
    css = (SITE / "map.css").read_text(encoding="utf-8")
    for token in ["--config:", "--config-fg:", "--you:", "--you-fg:"]:
        assert css.count(token) == 3, token

"""The system page in docs/site/data/system.json holds together: three steps in order, every item
of a known sort and kind, every link resolving, and no word for what users do not get. The checks
are helpers that return problems, run over a small fixture and over system.json itself."""

import copy
import json
import re
from pathlib import Path

import pytest

from tests._site_node import run_map
from tests.test_site_chain import REPO_BLOB, REPO_TREE, ROOT, SITE

FIXTURE = ROOT / "tests" / "fixtures" / "site" / "system.json"
SYSTEM = SITE / "data" / "system.json"
SYSTEMS = [pytest.param(FIXTURE, id="fixture"), pytest.param(SYSTEM, id="system.json")]

KEYS = {"title", "subtitle", "steps", "sections"}
STEPS = ["install", "init", "loop"]
KINDS = {"lighthouse", "superpowers", "hook", "github", "you"}
FORBIDDEN = re.compile(r"\b(serve|tick|dashboard|controller)\b", re.IGNORECASE)
SORT_KEYS = set(run_map("Object.keys(m.SORTS)"))


def load_system(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def items(system: dict) -> list[dict]:
    return [item for step in system["steps"] for item in step["items"]]


def shape_problems(system: dict) -> list[str]:
    problems = [f"lacks key {key}" for key in sorted(KEYS - system.keys())]
    problems += [f"unexpected key {key}" for key in sorted(system.keys() - KEYS)]
    ids = [step["id"] for step in system.get("steps", [])]
    if ids != STEPS:
        problems.append(f"steps are {ids}, not {STEPS}")
    return problems


def sort_problems(system: dict, sorts: set[str]) -> list[str]:
    problems = []
    for item in items(system):
        if item["form"] not in sorts:
            problems.append(f"{item['label']}: unknown form {item['form']}")
        if item["kind"] not in KINDS:
            problems.append(f"{item['label']}: unknown kind {item['kind']}")
    return problems


def link_problems(system: dict) -> list[str]:
    """A `#id` names a section; a repo URL names a path that exists; any other http(s) link is
    external; the rest is `page[#id]` on the site, and the page has that id."""
    sections = {section["id"] for section in system["sections"]}
    problems = []
    for item in items(system):
        label, href = item["label"], item.get("href")
        if not href:
            problems.append(f"{label}: no href")
        elif href.startswith("#"):
            if href[1:] not in sections:
                problems.append(f"{label}: no section {href[1:]}")
        elif href.startswith((REPO_BLOB, REPO_TREE)):
            path = href[len(REPO_BLOB if href.startswith(REPO_BLOB) else REPO_TREE):]
            if not (ROOT / path).exists():
                problems.append(f"{label}: {path} does not exist")
        elif href.startswith(("http:", "https:")):
            problems.append(f"{label}: external link {href}")
        else:
            page, _, anchor = href.partition("#")
            target = SITE / page
            if not target.is_file():
                problems.append(f"{label}: {page} points nowhere")
            elif anchor and f'id="{anchor}"' not in target.read_text(encoding="utf-8"):
                problems.append(f"{label}: {page} has no id {anchor}")
    return problems


def strings(value) -> list[str]:
    """Every string value anywhere in `value`, depth first."""
    if isinstance(value, str):
        return [value]
    if isinstance(value, dict):
        value = list(value.values())
    if isinstance(value, list):
        return [text for part in value for text in strings(part)]
    return []


def word_problems(system: dict) -> list[str]:
    return [
        f"{text}: names {match.group(1).lower()}"
        for text in strings(system)
        for match in FORBIDDEN.finditer(text)
    ]


@pytest.mark.parametrize("path", SYSTEMS)
def test_three_steps_in_order(path):
    assert shape_problems(load_system(path)) == []


@pytest.mark.parametrize("path", SYSTEMS)
def test_every_item_has_a_known_sort(path):
    assert sort_problems(load_system(path), SORT_KEYS) == []


@pytest.mark.parametrize("path", SYSTEMS)
def test_every_link_resolves(path):
    assert link_problems(load_system(path)) == []


@pytest.mark.parametrize("path", SYSTEMS)
def test_nothing_that_is_not_for_users(path):
    assert word_problems(load_system(path)) == []


def test_init_step_is_the_command():
    step = load_system(SYSTEM)["steps"][1]
    assert step["command"] == "lighthouse init"


def test_bring_groups_in_order():
    bring = next(s for s in load_system(SYSTEM)["sections"] if s["id"] == "bring")
    assert [g["label"] for g in bring["groups"]] == ["Needed", "For GitHub", "Per language", "Optional"]
    assert "superpowers" in bring["groups"][0]["tools"]
    assert bring["groups"][3]["tools"] == ["codex"]


def fixture_item(step: int) -> tuple[dict, dict]:
    """A copy of the fixture and the one item of its step `step`."""
    system = copy.deepcopy(load_system(FIXTURE))
    return system, system["steps"][step]["items"][0]


def test_fixture_unknown_form_is_flagged():
    system, item = fixture_item(0)
    item["form"] = "widget"
    assert sort_problems(system, SORT_KEYS) == ["lighthouse: unknown form widget"]


def test_fixture_unknown_kind_is_flagged():
    system, item = fixture_item(0)
    item["kind"] = "acme"
    assert sort_problems(system, SORT_KEYS) == ["lighthouse: unknown kind acme"]


def test_fixture_missing_step_is_flagged():
    system = copy.deepcopy(load_system(FIXTURE))
    system["steps"] = [step for step in system["steps"] if step["id"] != "loop"]
    assert shape_problems(system) == ["steps are ['install', 'init'], not ['install', 'init', 'loop']"]


def test_fixture_chips_key_is_flagged():
    system = copy.deepcopy(load_system(FIXTURE))
    system["chips"] = []
    assert shape_problems(system) == ["unexpected key chips"]


def test_fixture_page_pointing_nowhere_is_flagged():
    system, item = fixture_item(2)
    item["href"] = "nowhere.html"
    assert link_problems(system) == ["The loop: nowhere.html points nowhere"]


def test_fixture_anchor_pointing_nowhere_is_flagged():
    system, item = fixture_item(2)
    item["href"] = "chain.html#nope"
    assert link_problems(system) == ["The loop: chain.html has no id nope"]


def test_fixture_section_anchor_pointing_nowhere_is_flagged():
    system, item = fixture_item(0)
    item["href"] = "#nope"
    assert link_problems(system) == ["lighthouse: no section nope"]


def test_fixture_repo_path_pointing_nowhere_is_flagged():
    system, item = fixture_item(1)
    item["href"] = REPO_BLOB + "pack/core/nope"
    assert link_problems(system) == ["Skills: pack/core/nope does not exist"]


def test_fixture_external_link_is_flagged():
    system, item = fixture_item(1)
    item["href"] = "https://example.com/skills"
    assert link_problems(system) == ["Skills: external link https://example.com/skills"]


def test_fixture_item_without_href_is_flagged():
    system, item = fixture_item(0)
    del item["href"]
    assert link_problems(system) == ["lighthouse: no href"]


def test_fixture_dashboard_is_flagged():
    system, item = fixture_item(0)
    item["label"] = "Dashboard (serve)"
    assert word_problems(system) == [
        "Dashboard (serve): names dashboard",
        "Dashboard (serve): names serve",
    ]

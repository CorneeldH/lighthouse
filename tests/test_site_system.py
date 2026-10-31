"""The system map in docs/site/data/system.json holds together: fixed words, existing references,
unique ids, no planned part without an anchor, sources that exist. The checks are helpers that return
problems, run over a small fixture and every system map in the data directory."""

import copy
import datetime
import json
import re
from pathlib import Path

import pytest

from tests.test_site_chain import REPO_BLOB, ROOT, SITE

FIXTURE = ROOT / "tests" / "fixtures" / "site" / "system.json"
STATUSES = {"works", "planned", "idea"}
PART_KEYS = ["id", "label", "status"]
PART_TEXT_KEYS = ["what", "when", "without"]
ISSUE_KEYS = ["issue", "title", "part", "status"]
LINK_KEYS = ["from", "to", "label"]

# Task 2 adds docs/site/data/system.json here.
SYSTEMS = [pytest.param(FIXTURE, id="fixture")]


def load_system(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def word_problems(system: dict) -> list[str]:
    problems = []
    for part in system["parts"]:
        name = part.get("id")
        keys = PART_KEYS + ([] if part.get("group") else PART_TEXT_KEYS)
        problems += [f"{name}: lacks '{key}'" for key in keys if key not in part]
        if "status" in part and part["status"] not in STATUSES:
            problems.append(f"{name}: unknown status {part['status']}")
    for issue in system["issues"]:
        name = f"issue {issue.get('issue')}"
        problems += [f"{name}: lacks '{key}'" for key in ISSUE_KEYS if key not in issue]
        if "status" in issue and issue["status"] not in STATUSES:
            problems.append(f"{name}: unknown status {issue['status']}")
        if issue.get("status") == "idea":
            problems.append(f"{name}: an issue is never idea")
    for link in system["links"]:
        name = f"link {link.get('from')}->{link.get('to')}"
        problems += [f"{name}: lacks '{key}'" for key in LINK_KEYS if key not in link]
    return problems


def reference_problems(system: dict) -> list[str]:
    parts = {p["id"]: p for p in system["parts"]}
    numbers = {m["number"] for m in system["milestones"]}
    problems = []
    for part in system["parts"]:
        if "parent" in part and part["parent"] not in parts:
            problems.append(f"part {part['id']}: unknown parent {part['parent']}")
        if "milestone" in part and part["milestone"] not in numbers:
            problems.append(f"part {part['id']}: unknown milestone {part['milestone']}")
    for issue in system["issues"]:
        name = f"issue {issue['issue']}"
        if "milestone" not in issue:
            problems.append(f"{name}: no milestone")
        elif issue["milestone"] not in numbers:
            problems.append(f"{name}: unknown milestone {issue['milestone']}")
        if issue["part"] not in parts:
            problems.append(f"{name}: unknown part {issue['part']}")
        elif parts[issue["part"]].get("group"):
            problems.append(f"{name}: hangs on group {issue['part']}")
    for link in system["links"]:
        name = f"link {link['from']}->{link['to']}"
        problems += [f"{name}: unknown part {link[end]}" for end in ("from", "to") if link[end] not in parts]
    return problems


def uniqueness_problems(system: dict) -> list[str]:
    problems, seen = [], set()
    for part in system["parts"]:
        if part["id"] in seen:
            problems.append(f"part {part['id']}: duplicate id")
        seen.add(part["id"])
    seen = set()
    for milestone in system["milestones"]:
        if milestone["number"] in seen:
            problems.append(f"milestone {milestone['number']}: duplicate number")
        seen.add(milestone["number"])
    seen = set()
    for issue in system["issues"]:
        if issue["issue"] in seen:
            problems.append(f"issue {issue['issue']}: duplicate issue")
        seen.add(issue["issue"])
    parents = {p["id"]: p.get("parent") for p in system["parts"]}
    for part in system["parts"]:
        visited, current = set(), part["id"]
        while current in parents:
            if current in visited:
                problems.append(f"part {part['id']}: parent cycle")
                break
            visited.add(current)
            current = parents[current]
    return problems


def anchor_problems(system: dict) -> list[str]:
    """A planned part names an issue or a milestone; an issue always has its own number."""
    return [
        f"{part['id']}: planned without issue or milestone"
        for part in system["parts"]
        if part["status"] == "planned" and "issue" not in part and "milestone" not in part
    ]


def part_source_problems(system: dict) -> list[str]:
    problems = []
    for part in system["parts"]:
        source = part.get("source")
        if source is None:
            continue
        if not source.startswith(REPO_BLOB):
            problems.append(f"{part['id']}: source is not under REPO_BLOB")
        elif not (ROOT / source[len(REPO_BLOB):]).exists():
            problems.append(f"{part['id']}: {source} does not exist")
    return problems


def title_problems(system: dict) -> list[str]:
    problems = []
    for milestone in system["milestones"]:
        title = milestone["title"]
        numbered = re.match(r"M\d+ — ", title)
        if numbered:
            problems.append(f"milestone {milestone['number']}: title carries '{numbered.group()}'")
        if title.endswith(" - DONE"):
            problems.append(f"milestone {milestone['number']}: title carries ' - DONE'")
    return problems


def snapshot_problems(system: dict) -> list[str]:
    snapshot = system.get("snapshot")
    try:
        datetime.date.fromisoformat(snapshot)
    except (ValueError, TypeError):
        return [f"snapshot is not an ISO date: {snapshot}"]
    return []


@pytest.mark.parametrize("path", SYSTEMS)
def test_parts_use_the_fixed_words(path):
    assert word_problems(load_system(path)) == []


@pytest.mark.parametrize("path", SYSTEMS)
def test_every_reference_exists(path):
    assert reference_problems(load_system(path)) == []


@pytest.mark.parametrize("path", SYSTEMS)
def test_ids_are_unique_and_parents_acyclic(path):
    assert uniqueness_problems(load_system(path)) == []


@pytest.mark.parametrize("path", SYSTEMS)
def test_planned_has_an_issue_or_milestone(path):
    assert anchor_problems(load_system(path)) == []


@pytest.mark.parametrize("path", SYSTEMS)
def test_every_source_exists(path):
    assert part_source_problems(load_system(path)) == []


@pytest.mark.parametrize("path", SYSTEMS)
def test_milestone_titles_carry_no_number(path):
    assert title_problems(load_system(path)) == []


@pytest.mark.parametrize("path", SYSTEMS)
def test_snapshot_is_an_iso_date(path):
    assert snapshot_problems(load_system(path)) == []


def fixture_system() -> dict:
    return copy.deepcopy(load_system(FIXTURE))


def part_of(system: dict, part_id: str) -> dict:
    return next(p for p in system["parts"] if p["id"] == part_id)


def test_fixture_planned_part_without_milestone_is_flagged():
    system = fixture_system()
    del part_of(system, "planned-leaf")["milestone"]
    assert anchor_problems(system) == ["planned-leaf: planned without issue or milestone"]


def test_fixture_unknown_parent_is_flagged():
    system = fixture_system()
    part_of(system, "leaf")["parent"] = "ghost"
    assert reference_problems(system) == ["part leaf: unknown parent ghost"]


def test_fixture_issue_without_milestone_is_flagged():
    system = fixture_system()
    del system["issues"][0]["milestone"]
    assert reference_problems(system) == ["issue 10: no milestone"]


def test_fixture_issue_on_unknown_milestone_is_flagged():
    system = fixture_system()
    system["issues"][0]["milestone"] = 9
    assert reference_problems(system) == ["issue 10: unknown milestone 9"]


def test_fixture_part_on_unknown_milestone_is_flagged():
    system = fixture_system()
    part_of(system, "planned-leaf")["milestone"] = 9
    assert reference_problems(system) == ["part planned-leaf: unknown milestone 9"]


def test_fixture_issue_on_a_group_is_flagged():
    system = fixture_system()
    system["issues"][0]["part"] = "envs"
    assert reference_problems(system) == ["issue 10: hangs on group envs"]


def test_fixture_link_to_unknown_part_is_flagged():
    system = fixture_system()
    system["links"][0]["to"] = "ghost"
    assert reference_problems(system) == ["link leaf->ghost: unknown part ghost"]


def test_fixture_duplicate_id_is_flagged():
    system = fixture_system()
    system["parts"].append(copy.deepcopy(part_of(system, "env-a")))
    assert uniqueness_problems(system) == ["part env-a: duplicate id"]


def test_fixture_duplicate_issue_is_flagged():
    system = fixture_system()
    system["issues"].append(copy.deepcopy(system["issues"][0]))
    assert uniqueness_problems(system) == ["issue 10: duplicate issue"]


def test_fixture_duplicate_milestone_is_flagged():
    system = fixture_system()
    system["milestones"].append(copy.deepcopy(system["milestones"][0]))
    assert uniqueness_problems(system) == ["milestone 1: duplicate number"]


def test_fixture_parent_cycle_is_flagged():
    system = fixture_system()
    part_of(system, "shell")["parent"] = "leaf"
    assert uniqueness_problems(system) == [
        "part shell: parent cycle", "part leaf: parent cycle", "part planned-leaf: parent cycle",
    ]


def test_fixture_idea_issue_is_flagged():
    system = fixture_system()
    system["issues"][1]["status"] = "idea"
    assert word_problems(system) == ["issue 11: an issue is never idea"]


def test_fixture_unknown_status_is_flagged():
    system = fixture_system()
    part_of(system, "leaf")["status"] = "done"
    assert word_problems(system) == ["leaf: unknown status done"]


def test_fixture_part_without_what_is_flagged():
    system = fixture_system()
    del part_of(system, "leaf")["what"]
    assert word_problems(system) == ["leaf: lacks 'what'"]


def test_fixture_source_pointing_nowhere_is_flagged():
    system = fixture_system()
    part_of(system, "leaf")["source"] = REPO_BLOB + "pack/core/skills/no-such-skill"
    assert part_source_problems(system) == [f"leaf: {REPO_BLOB}pack/core/skills/no-such-skill does not exist"]


def test_fixture_numbered_milestone_title_is_flagged():
    system = fixture_system()
    system["milestones"][0]["title"] = "M0 — One"
    system["milestones"][1]["title"] = "Two - DONE"
    assert title_problems(system) == ["milestone 1: title carries 'M0 — '", "milestone 2: title carries ' - DONE'"]


def test_fixture_snapshot_not_iso_is_flagged():
    system = fixture_system()
    system["snapshot"] = "31-10-2026"
    assert snapshot_problems(system) == ["snapshot is not an ISO date: 31-10-2026"]

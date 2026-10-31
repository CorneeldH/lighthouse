"""The maps in docs/site/data/ stay in step with pack/: every part is drawn, every source exists,
every status and route holds together. The generic checks are helpers that return problems, run
over a small fixture map and every map in the data directory; the chain-specific ones run on
chain.json only."""

import copy
import json
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
SITE = ROOT / "docs" / "site"
FIXTURE = ROOT / "tests" / "fixtures" / "site" / "map.json"
REPO_BLOB = "https://github.com/CorneeldH/lighthouse/blob/main/"

KINDS = {"lighthouse", "superpowers", "hook", "github"}
TRIGGERS = {"you", "skill", "description", "tool", "github"}
STATUSES = {"works", "planned", "idea"}
# An optional `form` names a part that is no skill, agent, hook or workflow of a pack.
FORMS = {"tool", "dispatcher"}
REQUIRED = ["id", "phase", "row", "kind", "trigger", "label", "what", "how", "when", "without", "status"]


def load(name: str) -> dict | list:
    """The parsed JSON file `name` under docs/site/data/."""
    return json.loads((SITE / "data" / name).read_text(encoding="utf-8"))


def repo_path(source: str) -> Path:
    """The repo path a `source` URL points to; fails on a URL outside REPO_BLOB."""
    assert source.startswith(REPO_BLOB), f"source is not under {REPO_BLOB}: {source}"
    return ROOT / source[len(REPO_BLOB):]


def sources(chain: dict) -> set[str]:
    """The repo-relative paths named by the `source` of every chip and of the handoff."""
    found = [c["source"] for c in chain["chips"] if "source" in c]
    found.append(chain["handoff"]["source"])
    return {s[len(REPO_BLOB):] for s in found if s.startswith(REPO_BLOB)}


def missing_parts(chain: dict) -> list[str]:
    """The skill, agent and workflow paths of pack/ that no chip or the handoff names."""
    pack = ROOT / "pack"
    parts = [p for p in pack.glob("*/skills/*") if p.is_dir()]
    parts += pack.glob("*/agents/*.md")
    parts += pack.glob("*/workflows/*")
    named = sources(chain)
    return sorted(p.relative_to(ROOT).as_posix() for p in parts if p.relative_to(ROOT).as_posix() not in named)


def chips_by_id(chain: dict) -> dict[str, dict]:
    return {c["id"]: c for c in chain["chips"]}


def data_maps() -> list[Path]:
    """Every docs/site/data/*.json that is a map: a dict with `chips` (packs.json is a list)."""
    found = []
    for path in sorted((SITE / "data").glob("*.json")):
        data = json.loads(path.read_text(encoding="utf-8"))
        if isinstance(data, dict) and "chips" in data:
            found.append(path)
    return found


MAPS = [pytest.param(FIXTURE, id="fixture")] + [pytest.param(p, id=p.name) for p in data_maps()]


def load_map(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def vocabulary_problems(chain: dict) -> list[str]:
    phases = {p["id"] for p in chain["phases"]} | {"always"}
    problems, seen_ids, seen_cells = [], set(), set()
    for chip in chain["chips"]:
        name = chip.get("id")
        for key in REQUIRED:
            if key not in chip:
                problems.append(f"{name}: lacks '{key}'")
        if chip.get("kind") not in KINDS:
            problems.append(f"{name}: unknown kind")
        if chip.get("trigger") not in TRIGGERS:
            problems.append(f"{name}: unknown trigger")
        if chip.get("status") not in STATUSES:
            problems.append(f"{name}: unknown status")
        if "form" in chip and chip["form"] not in FORMS:
            problems.append(f"{name}: unknown form")
        if chip.get("phase") not in phases:
            problems.append(f"{name}: unknown phase")
        if name in seen_ids:
            problems.append(f"{name}: duplicate id")
        cell = (chip.get("phase"), chip.get("row"))
        if cell in seen_cells:
            problems.append(f"{name}: shares cell {cell}")
        seen_ids.add(name)
        seen_cells.add(cell)
    return problems


def source_problems(chain: dict) -> list[str]:
    """Working lighthouse and hook chips need a REPO_BLOB source; every source under it must exist."""
    problems = []
    for chip in chain["chips"]:
        name = chip["id"]
        if chip["kind"] in ("lighthouse", "hook") and chip["status"] == "works" and "source" not in chip:
            problems.append(f"{name}: no source")
        source = chip.get("source")
        if source is None:
            continue
        if not source.startswith(REPO_BLOB):
            if chip["kind"] in ("lighthouse", "hook"):
                problems.append(f"{name}: source is not under REPO_BLOB")
        elif not (ROOT / source[len(REPO_BLOB):]).exists():
            problems.append(f"{name}: {source} does not exist")
    handoff = chain.get("handoff")
    if handoff and handoff["source"].startswith(REPO_BLOB):
        if not (ROOT / handoff["source"][len(REPO_BLOB):]).exists():
            problems.append(f"handoff: {handoff['source']} does not exist")
    return problems


def planned_problems(chain: dict) -> list[str]:
    items = [(c["id"], c) for c in chain["chips"]]
    items += [(f"{h['from']}->{h['to']}", h) for h in chain["handoffs"]]
    return [
        f"{name}: planned without issue or milestone"
        for name, item in items
        if item["status"] == "planned" and "issue" not in item and "milestone" not in item
    ]


def route_problems(chain: dict) -> list[str]:
    chips = chips_by_id(chain)
    return [
        f"route {route['id']}: unknown chip {chip_id}"
        for route in chain["routes"]
        for chip_id in route["chips"]
        if chip_id not in chips
    ]


def handoff_problems(chain: dict) -> list[str]:
    """A handoff names two chips that a route passes in that order, within a phase or across."""
    problems = []
    for h in chain["handoffs"]:
        on = [r for r in chain["routes"] if h["from"] in r["chips"] and h["to"] in r["chips"]]
        if not any(r["chips"].index(h["from"]) < r["chips"].index(h["to"]) for r in on):
            problems.append(f"handoff on no route: ('{h['from']}', '{h['to']}')")
    return problems


def coverage_problems(chain: dict) -> list[str]:
    """Every chip outside the Always on band is on a route; every example is on its own route."""
    on_route = {chip_id for route in chain["routes"] for chip_id in route["chips"]}
    problems = [
        f"{chip['id']}: on no route"
        for chip in chain["chips"]
        if chip["phase"] != "always" and chip["id"] not in on_route
    ]
    problems += [
        f"route {route['id']}: example {chip_id} is not on the route"
        for route in chain["routes"]
        for chip_id in route.get("examples", [])
        if chip_id not in route["chips"]
    ]
    return problems


@pytest.mark.parametrize("path", MAPS)
def test_chips_use_the_fixed_vocabulary(path):
    assert vocabulary_problems(load_map(path)) == []


@pytest.mark.parametrize("path", MAPS)
def test_every_source_exists(path):
    assert source_problems(load_map(path)) == []


@pytest.mark.parametrize("path", MAPS)
def test_planned_has_an_issue_or_milestone(path):
    assert planned_problems(load_map(path)) == []


@pytest.mark.parametrize("path", MAPS)
def test_routes_name_existing_chips(path):
    assert route_problems(load_map(path)) == []


@pytest.mark.parametrize("path", MAPS)
def test_every_handoff_sits_on_a_route(path):
    assert handoff_problems(load_map(path)) == []


@pytest.mark.parametrize("path", MAPS)
def test_every_chip_is_on_a_route(path):
    assert coverage_problems(load_map(path)) == []


def test_fixture_planned_without_issue_is_flagged():
    chain = copy.deepcopy(load_map(FIXTURE))
    for chip in chain["chips"]:
        if chip["id"] == "planned-issue":
            del chip["issue"]
    assert planned_problems(chain) == ["planned-issue: planned without issue or milestone"]


def test_fixture_planned_handoff_without_milestone_is_flagged():
    chain = copy.deepcopy(load_map(FIXTURE))
    del chain["handoffs"][0]["milestone"]
    assert planned_problems(chain) == ["main-commit-guard->planned-issue: planned without issue or milestone"]


def test_fixture_source_pointing_nowhere_is_flagged():
    chain = copy.deepcopy(load_map(FIXTURE))
    chain["chips"][0]["source"] = REPO_BLOB + "pack/core/skills/no-such-skill"
    assert source_problems(chain) == [f"write-issue: {REPO_BLOB}pack/core/skills/no-such-skill does not exist"]


def test_fixture_handoff_against_the_route_is_flagged():
    chain = copy.deepcopy(load_map(FIXTURE))
    chain["handoffs"].append({"from": "planned-issue", "to": "write-issue", "status": "works"})
    assert handoff_problems(chain) == ["handoff on no route: ('planned-issue', 'write-issue')"]


def test_fixture_chip_off_every_route_is_flagged():
    chain = copy.deepcopy(load_map(FIXTURE))
    chain["routes"][0]["chips"].remove("an-idea")
    chain["routes"][0]["examples"] = ["an-idea"]
    assert coverage_problems(chain) == [
        "an-idea: on no route",
        "route x: example an-idea is not on the route",
    ]


def test_fixture_unknown_route_chip_is_flagged():
    chain = copy.deepcopy(load_map(FIXTURE))
    chain["routes"][0]["chips"].append("ghost")
    assert route_problems(chain) == ["route x: unknown chip ghost"]


def test_fixture_unknown_status_is_flagged():
    chain = copy.deepcopy(load_map(FIXTURE))
    chain["chips"][0]["status"] = "done"
    assert vocabulary_problems(chain) == ["write-issue: unknown status"]


def test_phases_are_the_seven_of_the_chain():
    labels = [p["label"] for p in load("chain.json")["phases"]]
    assert labels == ["Issue", "Orient", "Spec", "Plan", "Build", "PR", "Close"]


def test_always_on_band():
    chips = load("chain.json")["chips"]
    assert {c["id"] for c in chips if c["phase"] == "always"} == {"usage-guard", "instrument", "worktree-audit"}


def test_every_pack_part_is_on_the_chain():
    assert missing_parts(load("chain.json")) == []


def test_coverage_flags_a_removed_skill():
    chain = copy.deepcopy(load("chain.json"))
    chain["chips"] = [c for c in chain["chips"] if c["id"] != "write-issue"]
    assert missing_parts(chain) == ["pack/core/skills/write-issue"]


def test_works_handoffs_follow_the_handoff_table():
    chain = load("chain.json")
    chips = chips_by_id(chain)
    works = {(h["from"], h["to"]) for h in chain["handoffs"] if h["status"] == "works"}
    assert works == {
        ("brainstorming", "spec-adversarial-review"),
        ("spec-adversarial-review", "writing-plans"),
        ("writing-plans", "plan-adversarial-review"),
        ("plan-adversarial-review", "executing-plans"),
    }
    # The handoff skill's routing table names the first step of each works handoff.
    table = (ROOT / "pack" / "core" / "skills" / "handoff" / "SKILL.md").read_text(encoding="utf-8")
    for _, target in works:
        prefix = "lighthouse-" if chips[target]["kind"] == "lighthouse" else "superpowers:"
        assert f"`/{prefix}{target}" in table, target
    assert all(h["status"] in ("works", "planned") for h in chain["handoffs"])


def test_chain_routes_are_the_known_ones():
    routes = {r["id"]: r for r in load("chain.json")["routes"]}
    assert set(routes) == {"1", "2", "4a", "4b", "4c", "no-issue", "red-pr"}
    assert routes["1"]["examples"] == ["refresh-expo-lockfiles"]
    assert routes["no-issue"]["chips"][0] == "worktree-guard"

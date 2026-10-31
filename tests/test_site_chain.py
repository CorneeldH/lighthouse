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
TRIGGERS = {"you", "skill", "tool", "github"}
STATUSES = {"works", "planned", "idea"}
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


def phase_index(chain: dict) -> dict[str, int]:
    return {p["id"]: i for i, p in enumerate(chain["phases"])}


def jumps(chain: dict) -> set[tuple[str, str]]:
    """Consecutive route pairs (a, b) that go to a later phase and to a higher row."""
    chips, phases = chips_by_id(chain), phase_index(chain)
    found = set()
    for route in chain["routes"]:
        for a, b in zip(route["chips"], route["chips"][1:]):
            ca, cb = chips[a], chips[b]
            if phases[cb["phase"]] > phases[ca["phase"]] and cb["row"] < ca["row"]:
                found.add((a, b))
    return found


def handoff_pairs(chain: dict) -> set[tuple[str, str]]:
    return {(h["from"], h["to"]) for h in chain["handoffs"]}


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
    """Lighthouse and hook chips need a REPO_BLOB source; every source under REPO_BLOB must exist."""
    problems = []
    for chip in chain["chips"]:
        name = chip["id"]
        if chip["kind"] in ("lighthouse", "hook") and "source" not in chip:
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


def jump_problems(chain: dict) -> list[str]:
    return [f"jump without handoff: {p}" for p in sorted(jumps(chain) - handoff_pairs(chain))]


def stray_handoff_problems(chain: dict) -> list[str]:
    return [f"handoff on no upward route step: {p}" for p in sorted(handoff_pairs(chain) - jumps(chain))]


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
def test_every_upward_jump_has_a_handoff(path):
    assert jump_problems(load_map(path)) == []


@pytest.mark.parametrize("path", MAPS)
def test_every_handoff_sits_on_a_route_step(path):
    assert stray_handoff_problems(load_map(path)) == []


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


def test_fixture_jump_without_handoff_is_flagged():
    chain = copy.deepcopy(load_map(FIXTURE))
    chain["handoffs"] = []
    assert jump_problems(chain) == ["jump without handoff: ('main-commit-guard', 'planned-issue')"]


def test_fixture_stray_handoff_is_flagged():
    chain = copy.deepcopy(load_map(FIXTURE))
    chain["handoffs"].append({"from": "write-issue", "to": "main-commit-guard", "status": "works"})
    assert stray_handoff_problems(chain) == [
        "handoff on no upward route step: ('write-issue', 'main-commit-guard')"
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
    assert labels == ["Issue", "Pick up", "Spec", "Plan", "Build", "PR", "Close"]


def test_always_on_band():
    chips = load("chain.json")["chips"]
    assert {c["id"] for c in chips if c["phase"] == "always"} == {"usage-guard", "instrument", "worktree-audit"}


def test_every_pack_part_is_on_the_chain():
    assert missing_parts(load("chain.json")) == []


def test_coverage_flags_a_removed_skill():
    chain = copy.deepcopy(load("chain.json"))
    chain["chips"] = [c for c in chain["chips"] if c["id"] != "write-issue"]
    assert missing_parts(chain) == ["pack/core/skills/write-issue"]


def test_works_handoffs_are_after_the_reviews():
    chain = load("chain.json")
    chips = chips_by_id(chain)
    works = {(h["from"], h["to"]) for h in chain["handoffs"] if h["status"] == "works"}
    assert works == {
        ("spec-adversarial-review", "writing-plans"),
        ("plan-adversarial-review", "executing-plans"),
    }
    for origin, _ in works:
        skills = list(repo_path(chips[origin]["source"]).rglob("SKILL.md"))
        assert skills, f"{origin} has no SKILL.md"
        assert any("lighthouse-handoff" in s.read_text(encoding="utf-8") for s in skills), origin
    assert all(h["status"] in ("works", "planned") for h in chain["handoffs"])


def test_chain_routes_are_the_known_ones():
    routes = {r["id"]: r for r in load("chain.json")["routes"]}
    assert set(routes) == {"1", "2", "4a", "4b", "4c", "red-pr"}
    assert "refresh-expo-lockfiles" in routes["1"]["note"]

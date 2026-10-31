"""docs/site/data/chain.json stays in step with pack/: every part is drawn, every source exists,
every status and route holds together."""

import copy
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SITE = ROOT / "docs" / "site"
REPO_BLOB = "https://github.com/CorneeldH/lighthouse/blob/main/"

KINDS = {"lighthouse", "superpowers", "hook", "github"}
TRIGGERS = {"you", "skill", "tool", "github"}
STATUSES = {"works", "planned", "idea"}
REQUIRED = ["id", "phase", "row", "kind", "trigger", "label", "what", "when", "without", "status"]


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


def test_phases_are_the_seven_of_the_chain():
    labels = [p["label"] for p in load("chain.json")["phases"]]
    assert labels == ["Issue", "Pick up", "Spec", "Plan", "Build", "PR", "Close"]


def test_always_on_band():
    chips = load("chain.json")["chips"]
    assert {c["id"] for c in chips if c["phase"] == "always"} == {"usage-guard", "instrument", "worktree-audit"}


def test_chips_use_the_fixed_vocabulary():
    chain = load("chain.json")
    phases = {p["id"] for p in chain["phases"]} | {"always"}
    seen_ids, seen_cells = set(), set()
    for chip in chain["chips"]:
        for key in REQUIRED:
            assert key in chip, f"chip {chip.get('id')} lacks '{key}'"
        assert chip["kind"] in KINDS, chip["id"]
        assert chip["trigger"] in TRIGGERS, chip["id"]
        assert chip["status"] in STATUSES, chip["id"]
        assert chip["phase"] in phases, chip["id"]
        assert chip["id"] not in seen_ids, f"duplicate id {chip['id']}"
        cell = (chip["phase"], chip["row"])
        assert cell not in seen_cells, f"two chips on {cell}"
        seen_ids.add(chip["id"])
        seen_cells.add(cell)


def test_every_pack_part_is_on_the_chain():
    assert missing_parts(load("chain.json")) == []


def test_coverage_flags_a_removed_skill():
    chain = copy.deepcopy(load("chain.json"))
    chain["chips"] = [c for c in chain["chips"] if c["id"] != "write-issue"]
    assert missing_parts(chain) == ["pack/core/skills/write-issue"]


def test_every_lighthouse_source_exists():
    chain = load("chain.json")
    for chip in chain["chips"]:
        if chip["kind"] in ("lighthouse", "hook"):
            assert "source" in chip, f"{chip['id']} has no source"
        if "source" in chip:
            assert repo_path(chip["source"]).exists(), f"{chip['id']}: {chip['source']} does not exist"
    assert repo_path(chain["handoff"]["source"]).exists()


def test_planned_has_an_issue_or_milestone():
    chain = load("chain.json")
    for item in chain["chips"] + chain["handoffs"]:
        if item["status"] == "planned":
            assert "issue" in item or "milestone" in item, f"planned without issue or milestone: {item}"


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


def test_routes_name_existing_chips():
    chain = load("chain.json")
    chips = chips_by_id(chain)
    routes = {r["id"]: r for r in chain["routes"]}
    assert set(routes) == {"1", "2", "4a", "4b", "4c", "red-pr"}
    for route in chain["routes"]:
        for chip_id in route["chips"]:
            assert chip_id in chips, f"route {route['id']} names unknown chip {chip_id}"
    assert "refresh-expo-lockfiles" in routes["1"]["note"]


def test_every_upward_jump_has_a_handoff():
    chain = load("chain.json")
    missing = jumps(chain) - handoff_pairs(chain)
    assert not missing, f"jump without handoff: {sorted(missing)}"


def test_every_handoff_sits_on_a_route_step():
    chain = load("chain.json")
    stray = handoff_pairs(chain) - jumps(chain)
    assert not stray, f"handoff on no upward route step: {sorted(stray)}"

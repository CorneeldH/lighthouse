"""ci/chain_roadmap.py: the planned items of the chain map and the open `chain` issues match."""

import importlib.util
from pathlib import Path

PATH = Path(__file__).resolve().parents[1] / "ci" / "chain_roadmap.py"
_spec = importlib.util.spec_from_file_location("lighthouse_ci_chain_roadmap", PATH)
assert _spec is not None and _spec.loader is not None
roadmap = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(roadmap)


def chain(*items: dict, handoffs: tuple[dict, ...] = ()) -> dict:
    return {"chips": list(items), "handoffs": list(handoffs)}


def test_matching_map_and_issues_have_no_problems():
    data = chain({"id": "a", "status": "works"}, {"id": "b", "status": "planned", "issue": 7})
    assert roadmap.problems(data, {7}) == []


def test_planned_item_without_issue_is_flagged():
    data = chain(handoffs=({"from": "a", "to": "b", "status": "planned", "milestone": "M2"},))
    assert roadmap.problems(data, set()) == ["a->b: planned without an issue"]


def test_planned_item_on_a_closed_or_unlabelled_issue_is_flagged():
    data = chain({"id": "b", "status": "idea", "issue": 7})
    assert roadmap.problems(data, set()) == ["b: #7 is not an open issue labelled chain"]


def test_labelled_issue_missing_from_the_map_is_flagged():
    data = chain({"id": "b", "status": "planned", "issue": 7})
    assert roadmap.problems(data, {7, 9}) == ["#9: labelled chain, but not on the map"]

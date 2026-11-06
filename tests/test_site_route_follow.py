"""A click on a chip or handoff off the chosen route switches the flow map to a route it is on:
routeWith in map.js picks that route, and the real maps give every chip one to switch to."""

import json

import pytest

from tests._site_node import run_map
from tests.test_site_loop import load

ROUTES = [
    {"id": "a", "chips": ["x", "y"]},
    {"id": "b", "chips": ["y", "z", "w"]},
    {"id": "c", "chips": ["w", "z"]},
]


def route_with(current: int, ids: list[str], routes=ROUTES) -> int:
    return run_map(f"m.routeWith({json.dumps(routes)}, {current}, {json.dumps(ids)})")


def test_current_route_stays_when_it_has_the_chip():
    assert route_with(1, ["y"]) == 1


def test_first_route_with_the_chip_when_current_lacks_it():
    assert route_with(0, ["w"]) == 1


def test_handoff_needs_both_ends_in_order():
    # b has z before w, c has w before z: a handoff from w to z lights only on c.
    assert route_with(0, ["w", "z"]) == 2


def test_current_route_stays_when_no_route_has_the_chip():
    assert route_with(2, ["nowhere"]) == 2


def test_no_routes_gives_no_route():
    assert route_with(-1, ["x"], routes=[]) == -1


@pytest.mark.parametrize("path", ["loop.json", "session.json"])
def test_every_chip_has_a_route_to_switch_to(path):
    data = load(path)
    lost = [c["id"] for c in data["chips"] if route_with(0, [c["id"]], data["routes"]) == 0
            and c["id"] not in data["routes"][0]["chips"]]
    assert lost == []


def test_commit_test_gate_on_the_session_map_switches_to_red_tests():
    data = load("session.json")
    index = route_with(0, ["commit-test-gate-pre"], data["routes"])
    assert data["routes"][index]["id"] == "red-tests"

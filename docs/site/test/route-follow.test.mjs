// A click on a chip or handoff off the chosen route switches the flow map to a route it is on:
// routeWith in map.js picks that route, and the real maps give every chip one to switch to.
import assert from "node:assert/strict";
import test from "node:test";
import { load } from "./helpers.mjs";
import * as m from "../map.js";

const ROUTES = [
  { id: "a", chips: ["x", "y"] },
  { id: "b", chips: ["y", "z", "w"] },
  { id: "c", chips: ["w", "z"] },
];

test("the current route stays when it has the chip", () => {
  assert.equal(m.routeWith(ROUTES, 1, ["y"]), 1);
});

test("the first route with the chip wins when the current one lacks it", () => {
  assert.equal(m.routeWith(ROUTES, 0, ["w"]), 1);
});

test("a handoff needs both ends in order", () => {
  // b has z before w, c has w before z: a handoff from w to z lights only on c.
  assert.equal(m.routeWith(ROUTES, 0, ["w", "z"]), 2);
});

test("the current route stays when no route has the chip", () => {
  assert.equal(m.routeWith(ROUTES, 2, ["nowhere"]), 2);
});

test("no routes gives no route", () => {
  assert.equal(m.routeWith([], -1, ["x"]), -1);
});

test("every chip has a route to switch to", () => {
  for (const name of ["loop.json", "session.json"]) {
    const data = load(name);
    const lost = data.chips
      .filter((c) => m.routeWith(data.routes, 0, [c.id]) === 0 && !data.routes[0].chips.includes(c.id))
      .map((c) => c.id);
    assert.deepEqual(lost, [], name);
  }
});

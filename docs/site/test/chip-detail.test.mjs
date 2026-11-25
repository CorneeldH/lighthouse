import { test } from "node:test";
import assert from "node:assert/strict";
import { chipDetail } from "../map.js";

const planned = { id: "a", phase: "p", row: 0, kind: "lighthouse", trigger: "manual", label: "a", what: "w", status: "planned", issue: 1 };

test("a chip without how or without shows not described yet in both places", () => {
  const detail = chipDetail(planned);
  assert.deepEqual(detail.how, ["not described yet"]);
  assert.deepEqual(detail.rows, [["What you miss without", "not described yet"]]);
});

test("an empty how list also shows not described yet", () => {
  assert.deepEqual(chipDetail({ ...planned, how: [] }).how, ["not described yet"]);
});

test("a chip with both fields shows them unchanged", () => {
  const detail = chipDetail({ ...planned, how: ["one", "two"], without: "a loss" });
  assert.deepEqual(detail.how, ["one", "two"]);
  assert.deepEqual(detail.rows, [["What you miss without", "a loss"]]);
});

// The system page in docs/site/data/system.json holds together: three steps in order, every item
// of a known sort and kind, every link resolving, and no word for what users do not get. The checks
// are functions that return problems, run over a small fixture and over system.json itself.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { REPO_BLOB, REPO_TREE, ROOT, SITE, load } from "./helpers.mjs";
import * as m from "../map.js";

const FIXTURE = path.join(SITE, "test", "fixtures", "system.json");
const KEYS = ["title", "subtitle", "steps", "sections"];
const STEPS = ["install", "init", "loop"];
const KINDS = new Set(["lighthouse", "superpowers", "hook", "github", "you"]);
const FORBIDDEN = /\b(serve|tick|dashboard|controller)\b/gi;
const SORT_KEYS = new Set(Object.keys(m.SORTS));

const fixture = () => JSON.parse(fs.readFileSync(FIXTURE, "utf-8"));
const SYSTEMS = [["fixture", fixture()], ["system.json", load("system.json")]];
const items = (system) => system.steps.flatMap((step) => step.items);

function shapeProblems(system) {
  const problems = KEYS.filter((k) => !(k in system)).sort().map((k) => `lacks key ${k}`);
  problems.push(...Object.keys(system).filter((k) => !KEYS.includes(k)).sort().map((k) => `unexpected key ${k}`));
  const ids = (system.steps ?? []).map((s) => s.id);
  if (JSON.stringify(ids) !== JSON.stringify(STEPS))
    problems.push(`steps are [${ids.map((i) => `'${i}'`).join(", ")}], not [${STEPS.map((i) => `'${i}'`).join(", ")}]`);
  return problems;
}

function sortProblems(system, sorts) {
  const problems = [];
  for (const item of items(system)) {
    if (!sorts.has(item.form)) problems.push(`${item.label}: unknown form ${item.form}`);
    if (!KINDS.has(item.kind)) problems.push(`${item.label}: unknown kind ${item.kind}`);
  }
  return problems;
}

function pageProblems(label, href) {
  const [page, ...rest] = href.split("#");
  const anchor = rest.join("#");
  const target = path.join(SITE, page);
  if (!fs.existsSync(target) || !fs.statSync(target).isFile()) return [`${label}: ${page} points nowhere`];
  if (anchor && !fs.readFileSync(target, "utf-8").includes(`id="${anchor}"`)) return [`${label}: ${page} has no id ${anchor}`];
  return [];
}

/** A `#id` names a section; a repo URL names a path that exists; any other http(s) link is
 * external; the rest is `page[#id]` on the site, and the page has that id. */
function linkProblems(system) {
  const sections = new Set(system.sections.map((s) => s.id));
  const problems = [];
  for (const item of items(system)) {
    const { label, href } = item;
    if (!href) problems.push(`${label}: no href`);
    else if (href.startsWith("#")) {
      if (!sections.has(href.slice(1))) problems.push(`${label}: no section ${href.slice(1)}`);
    } else if (href.startsWith(REPO_BLOB) || href.startsWith(REPO_TREE)) {
      const base = href.startsWith(REPO_BLOB) ? REPO_BLOB : REPO_TREE;
      const rel = href.slice(base.length);
      if (!fs.existsSync(path.join(ROOT, rel))) problems.push(`${label}: ${rel} does not exist`);
    } else if (/^https?:/.test(href)) problems.push(`${label}: external link ${href}`);
    else problems.push(...pageProblems(label, href));
  }
  for (const step of system.steps) if ("map" in step) problems.push(...pageProblems(step.map.label, step.map.href));
  return problems;
}

/** Every string value anywhere in `value`, depth first. */
function strings(value) {
  if (typeof value === "string") return [value];
  if (Array.isArray(value)) return value.flatMap(strings);
  if (value && typeof value === "object") return Object.values(value).flatMap(strings);
  return [];
}

function wordProblems(system) {
  return strings(system).flatMap((text) => [...text.matchAll(FORBIDDEN)].map((hit) => `${text}: names ${hit[1].toLowerCase()}`));
}

const CHECKS = [
  ["three steps in order", shapeProblems],
  ["every item has a known sort", (s) => sortProblems(s, SORT_KEYS)],
  ["every link resolves", linkProblems],
  ["nothing that is not for users", wordProblems],
];
for (const [name, check] of CHECKS) {
  test(name, () => {
    for (const [file, system] of SYSTEMS) assert.deepEqual(check(system), [], file);
  });
}

// Each checker must be able to fail: a broken checker would stay green over the pages above.
// `item(step)` is the one item of fixture step `step`.
function table(check, cases) {
  for (const { naam, mutatie, verwacht } of cases) {
    const system = fixture();
    mutatie(system, (step) => system.steps[step].items[0]);
    assert.deepEqual(check(system), verwacht, naam);
  }
}

test("shapeProblems flags a missing step and an unexpected key", () => {
  table(shapeProblems, [
    {
      naam: "missing step",
      mutatie: (s) => (s.steps = s.steps.filter((step) => step.id !== "loop")),
      verwacht: ["steps are ['install', 'init'], not ['install', 'init', 'loop']"],
    },
    { naam: "chips key", mutatie: (s) => (s.chips = []), verwacht: ["unexpected key chips"] },
  ]);
});

test("sortProblems flags an unknown form and an unknown kind", () => {
  table((s) => sortProblems(s, SORT_KEYS), [
    { naam: "unknown form", mutatie: (s, item) => (item(0).form = "widget"), verwacht: ["lighthouse: unknown form widget"] },
    { naam: "unknown kind", mutatie: (s, item) => (item(0).kind = "acme"), verwacht: ["lighthouse: unknown kind acme"] },
  ]);
});

test("linkProblems flags a link that points nowhere", () => {
  table(linkProblems, [
    { naam: "page", mutatie: (s, item) => (item(2).href = "nowhere.html"), verwacht: ["The loop: nowhere.html points nowhere"] },
    { naam: "anchor", mutatie: (s, item) => (item(2).href = "loop.html#nope"), verwacht: ["The loop: loop.html has no id nope"] },
    { naam: "section anchor", mutatie: (s, item) => (item(0).href = "#nope"), verwacht: ["lighthouse: no section nope"] },
    {
      naam: "repo path",
      mutatie: (s, item) => (item(1).href = `${REPO_BLOB}pack/core/nope`),
      verwacht: ["Skills: pack/core/nope does not exist"],
    },
    {
      naam: "map",
      mutatie: (s) => {
        assert.deepEqual(s.steps[1].map, { label: "Map", href: "repo.html" });
        s.steps[1].map.href = "nowhere.html";
      },
      verwacht: ["Map: nowhere.html points nowhere"],
    },
    {
      naam: "external link",
      mutatie: (s, item) => (item(1).href = "https://example.com/skills"),
      verwacht: ["Skills: external link https://example.com/skills"],
    },
    { naam: "no href", mutatie: (s, item) => delete item(0).href, verwacht: ["lighthouse: no href"] },
  ]);
});

test("wordProblems flags a word users do not get", () => {
  table(wordProblems, [
    {
      naam: "dashboard and serve",
      mutatie: (s, item) => (item(0).label = "Dashboard (serve)"),
      verwacht: ["Dashboard (serve): names dashboard", "Dashboard (serve): names serve"],
    },
  ]);
});

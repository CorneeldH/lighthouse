// The map of the repo after init (docs/site/data/repo.json) stays in step with the code: every
// place init writes (internal/initcmd/testdata/places.golden) is one row on it, except what
// OFF_THE_MAP names, each row says whether init adds it or edits a file of yours as the golden
// implies, and every link lands.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { ROOT, SITE, load } from "./helpers.mjs";
import * as m from "../map.js";

const GOLDEN = path.join(ROOT, "internal", "initcmd", "testdata", "places.golden");
const DIRS = [".claude", ".agents", ".lighthouse", ".github"];
const ALL_DIRS = ["root", ...DIRS];
const REQUIRED = ["id", "label", "dir", "kind", "act"];
const ACTS = new Set(["adds", "edits"]);
// init writes CLAUDE.md only when AGENTS.md and CLAUDE.md are both missing, as one line
// `@AGENTS.md`, and never touches one that exists.
const OFF_THE_MAP = new Set(["CLAUDE.md"]);
const INIT_ACTS = { "lighthouse.toml": "edits", ".lighthouse/manifest.toml": "adds" };
const EDIT_KINDS = new Set(["block", "hook", "permission"]);
const FORCE_KINDS = new Set(["file", "block", "hook", "permission"]);
const SEPARATORS = ["·", ","];

/** key -> {kinds, packs} of every non-comment line of places.golden (tab-separated). */
function parseGolden(text) {
  const found = {};
  for (const line of text.split("\n")) {
    if (!line || line.startsWith("#")) continue;
    const [key, kinds, packs] = line.split("\t");
    found[key] = { kinds: new Set(kinds.split(",")), packs: packs ? packs.split(",") : [] };
  }
  return found;
}

const golden = () => parseGolden(fs.readFileSync(GOLDEN, "utf-8"));
const listed = (items) => "[" + items.join(", ") + "]";
const roomOf = (place) => {
  const first = place.split("/", 1)[0];
  return place.includes("/") && first.startsWith(".") ? first : "root";
};
const row = (data, id) => data.rows.find((r) => r.id === id);
const mutated = () => structuredClone(load("repo.json"));

function placeProblems(data, gold) {
  const problems = [];
  const placed = data.rows.filter((r) => r.place);
  for (const key of Object.keys(gold))
    if (!OFF_THE_MAP.has(key) && !placed.some((r) => r.place === key)) problems.push(`not on the map: ${key}`);
  for (const place of new Set(placed.map((r) => r.place))) {
    const count = placed.filter((r) => r.place === place).length;
    if (count > 1) problems.push(`${place}: on ${count} rows`);
  }
  for (const r of placed) {
    if (OFF_THE_MAP.has(r.place)) problems.push(`${r.place}: off the map, yet a row`);
    else if (!(r.place in gold)) problems.push(`not in places.golden: ${r.place}`);
  }
  const bare = data.rows.filter((r) => !r.place);
  for (const r of bare) if (r.dir !== "workstation") problems.push(`${r.id}: no place outside the workstation`);
  if (bare.length !== 1) problems.push(`expected one row without a place, found ${bare.length}`);
  return problems.sort();
}

function actProblems(data, gold) {
  const problems = [];
  for (const r of data.rows) {
    if (!(r.place in gold)) continue;
    const kinds = gold[r.place].kinds;
    let want;
    if ([...kinds].some((k) => EDIT_KINDS.has(k))) want = "edits";
    else if (kinds.size === 1 && kinds.has("file")) want = "adds";
    else want = INIT_ACTS[r.place];
    if (r.act !== want) problems.push(`${r.id}: act ${r.act}, expected ${want}`);
  }
  return problems.sort();
}

function forceProblems(data, gold) {
  const problems = [];
  for (const r of data.rows) {
    const want = r.place in gold ? [...gold[r.place].kinds].some((k) => FORCE_KINDS.has(k)) : false;
    if (r.force !== want) problems.push(`${r.id}: force ${r.force}, expected ${want}`);
  }
  return problems.sort();
}

function upgradeProblems(data) {
  const problems = data.rows
    .filter((r) => typeof r.upgrade !== "string" || !r.upgrade.trim())
    .map((r) => `${r.id}: no upgrade text`);
  if (typeof data.upgradeNote !== "string" || !data.upgradeNote.trim()) problems.push("no upgradeNote");
  return problems.sort();
}

function shapeProblems(data, sorts) {
  const problems = [];
  const ids = data.dirs.map((d) => d.id);
  const want = ALL_DIRS;
  if (JSON.stringify(ids) !== JSON.stringify(want)) problems.push(`dirs are ${listed(ids)}, not ${listed(want)}`);
  const seen = new Set();
  for (const r of data.rows) {
    const id = r.id ?? "?";
    for (const k of REQUIRED) if (!(k in r)) problems.push(`${id}: lacks ${k}`);
    if (seen.has(id)) problems.push(`duplicate id ${id}`);
    seen.add(id);
    if ("kind" in r && !sorts.includes(r.kind)) problems.push(`${id}: unknown kind ${r.kind}`);
    if ("act" in r && !ACTS.has(r.act)) problems.push(`${id}: unknown act ${r.act}`);
    if ("dir" in r && ![...ALL_DIRS, "workstation"].includes(r.dir)) problems.push(`${id}: unknown dir ${r.dir}`);
    else if (r.place && r.dir !== roomOf(r.place)) problems.push(`${id}: dir ${r.dir}, place says ${roomOf(r.place)}`);
  }
  return problems.sort();
}

function linkProblems(data) {
  const problems = [];
  const systemIds = new Set(load("system.json").sections.map((s) => s.id));
  for (const r of data.rows) {
    if (!r.href) continue;
    const [page, ...rest] = r.href.split("#");
    const anchor = rest.join("#");
    const file = path.join(SITE, page);
    if (!fs.existsSync(file) || !fs.statSync(file).isFile()) problems.push(`${r.id}: ${page} points nowhere`);
    else if (anchor) {
      const ok = page === "system.html" ? systemIds.has(anchor) : fs.readFileSync(file, "utf-8").includes(`id="${anchor}"`);
      if (!ok) problems.push(`${r.id}: ${page} has no id ${anchor}`);
    }
  }
  return problems.sort();
}

/** A row is one thing: its label holds no separator such as `lib/ · scripts/`. */
function labelProblems(data) {
  return data.rows
    .filter((r) => SEPARATORS.some((s) => r.label.includes(s)))
    .map((r) => `${r.id}: label ${r.label} holds two things`)
    .sort();
}

const sortKeys = () => Object.keys(m.SORTS);

test("the map matches the places init writes", () => {
  assert.deepEqual(placeProblems(load("repo.json"), golden()), []);
});

test("act matches the golden", () => {
  assert.deepEqual(actProblems(load("repo.json"), golden()), []);
});

test("force matches the golden", () => {
  assert.deepEqual(forceProblems(load("repo.json"), golden()), []);
});

test("every row says what upgrade does", () => {
  assert.deepEqual(upgradeProblems(load("repo.json")), []);
});

test("shape", () => {
  assert.deepEqual(shapeProblems(load("repo.json"), sortKeys()), []);
});

test("links", () => {
  assert.deepEqual(linkProblems(load("repo.json")), []);
});

test("one thing per row", () => {
  assert.deepEqual(labelProblems(load("repo.json")), []);
});

test("the checkers flag what they guard", () => {
  const cases = [
    {
      name: "a missing row",
      run() {
        const data = mutated();
        data.rows = data.rows.filter((r) => r.id !== "claude-agents");
        return placeProblems(data, golden());
      },
      expected: ["not on the map: .claude/agents/lighthouse-*.md"],
    },
    {
      name: "a new place in the golden",
      run() {
        const gold = { ...golden(), ".claude/commands/lighthouse-*/": { kinds: new Set(["file"]), packs: ["core"] } };
        return placeProblems(load("repo.json"), gold);
      },
      expected: ["not on the map: .claude/commands/lighthouse-*/"],
    },
    {
      name: "a place init does not write",
      run() {
        const data = mutated();
        row(data, "headings").place = ".lighthouse/cache/";
        return placeProblems(data, golden());
      },
      expected: ["not in places.golden: .lighthouse/cache/", "not on the map: .lighthouse/headings.toml"],
    },
    {
      name: "CLAUDE.md on the map",
      run() {
        const data = mutated();
        data.rows.push({ id: "claude-md", place: "CLAUDE.md", label: "CLAUDE.md", dir: "root", kind: "rules", act: "adds" });
        return placeProblems(data, golden());
      },
      expected: ["CLAUDE.md: off the map, yet a row"],
    },
    {
      name: "a second row without place",
      run() {
        const data = mutated();
        delete row(data, "manifest").place;
        return placeProblems(data, golden());
      },
      expected: [
        "expected one row without a place, found 2",
        "manifest: no place outside the workstation",
        "not on the map: .lighthouse/manifest.toml",
      ],
    },
    {
      name: "an edit marked as add",
      run() {
        const data = mutated();
        row(data, "claude-settings").act = "adds";
        return actProblems(data, golden());
      },
      expected: ["claude-settings: act adds, expected edits"],
    },
    {
      name: "an add marked as edit",
      run() {
        const data = mutated();
        row(data, "manifest").act = "edits";
        return actProblems(data, golden());
      },
      expected: ["manifest: act edits, expected adds"],
    },
    {
      name: "force on a row init only rewrites",
      run() {
        const data = mutated();
        row(data, "lighthouse-toml").force = true;
        row(data, "manifest").force = true;
        return forceProblems(data, golden());
      },
      expected: ["lighthouse-toml: force true, expected false", "manifest: force true, expected false"],
    },
    {
      name: "no force on a replaced file",
      run() {
        const data = mutated();
        row(data, "workflows").force = false;
        row(data, "claude-settings").force = false;
        return forceProblems(data, golden());
      },
      expected: ["claude-settings: force false, expected true", "workflows: force false, expected true"],
    },
    {
      name: "a row without upgrade text",
      run() {
        const data = mutated();
        delete row(data, "agents-md").upgrade;
        return upgradeProblems(data);
      },
      expected: ["agents-md: no upgrade text"],
    },
    {
      name: "an unknown kind and dir",
      run() {
        const data = mutated();
        row(data, "headings").kind = "skill";
        row(data, "headings").dir = ".claude";
        row(data, "host-toml").dir = "home";
        return shapeProblems(data, sortKeys());
      },
      expected: [
        "headings: dir .claude, place says .lighthouse",
        "headings: unknown kind skill",
        "host-toml: unknown dir home",
      ],
    },
    {
      name: "an anchor that does not exist",
      run() {
        const data = mutated();
        row(data, "claude-skills").href = "reference.html#nope";
        row(data, "agents-md").href = "system.html#nope";
        return linkProblems(data);
      },
      expected: ["agents-md: system.html has no id nope", "claude-skills: reference.html has no id nope"],
    },
    {
      name: "two things in one row",
      run() {
        const data = mutated();
        row(data, "lighthouse-scripts").label = "scripts/ · headings.toml";
        return labelProblems(data);
      },
      expected: ["lighthouse-scripts: label scripts/ · headings.toml holds two things"],
    },
  ];
  for (const c of cases) assert.deepEqual(c.run(), c.expected, c.name);
});

const entries = (mode, data = load("repo.json")) => m.repoEntries(data, mode);

/** place (or label, for the workstation) -> row, from repoEntries in `mode`. */
function entryRows(mode) {
  const out = {};
  for (const e of entries(mode)) for (const r of e.rows) out[r.place || r.label] = r;
  return out;
}

test("entries keep the order of the tree", () => {
  const found = entries("everything");
  assert.deepEqual(found.map((e) => e.id), ["root", ".claude", ".agents", ".lighthouse", ".github", "workstation"]);
  const byId = Object.fromEntries(found.map((e) => [e.id, e]));
  assert.equal(byId.root.yours, "your code, your tests, your config, …");
  assert.equal(byId[".lighthouse"].yours ?? null, null);
  assert.equal(byId.workstation.note, load("repo.json").workstation.note);
});

test("upgrade dims nothing", () => {
  const dimmed = Object.entries(entryRows("upgrade")).filter(([, r]) => r.dim).map(([p]) => p);
  assert.deepEqual(dimmed, []);
});

test("rows carry the upgrade text", () => {
  const rows = entryRows("everything");
  const data = load("repo.json");
  assert.equal(rows[".claude/settings.json"].force, true);
  assert.equal(rows[".claude/settings.json"].upgrade, row(data, "claude-settings").upgrade);
  assert.equal(rows["lighthouse.toml"].force, false);
});

test("the modes are four", () => {
  assert.deepEqual(m.REPO_MODES.map((x) => x.id), ["everything", "adds", "edits", "upgrade"]);
});

test("everything dims nothing", () => {
  const dimmed = Object.entries(entryRows("everything")).filter(([, r]) => r.dim).map(([p]) => p);
  assert.deepEqual(dimmed, []);
});

test("edits leaves three", () => {
  const rows = entryRows("edits");
  const lit = Object.entries(rows).filter(([, r]) => !r.dim).map(([p]) => p).sort();
  assert.deepEqual(lit, ["AGENTS.md", ".claude/settings.json", "lighthouse.toml"].sort());
  assert.equal(rows["~/.config/lighthouse/host.toml"].dim, true);
});

test("adds leaves what init adds", () => {
  const lit = entries("adds").flatMap((e) => e.rows).filter((r) => !r.dim).map((r) => r.id).sort();
  const adds = load("repo.json").rows.filter((r) => r.act === "adds").map((r) => r.id).sort();
  assert.deepEqual(lit, adds);
  assert.ok(lit.includes("host-toml"));
});

test("rows carry glyph and colour", () => {
  const rows = entryRows("everything");
  assert.equal(rows[".claude/agents/lighthouse-*.md"].glyph, "◆");
  assert.equal(rows[".claude/agents/lighthouse-*.md"].className, "kind-lighthouse form-agent");
  assert.equal(rows["AGENTS.md"].glyph, "§");
  assert.equal(rows["AGENTS.md"].className, "kind-rules form-rules");
});

test("rows say what init does", () => {
  const rows = entryRows("everything");
  assert.equal(rows[".claude/settings.json"].actLabel, "init edits your file");
  assert.equal(rows[".lighthouse/scripts/"].actLabel, "init adds it");
});

test("entries refuse an unknown mode, kind and act", () => {
  assert.throws(() => entries("bogus"), { message: "unknown mode: bogus" });
  const kind = mutated();
  row(kind, "headings").kind = "skill";
  assert.throws(() => entries("everything", kind), { message: "unknown kind: skill" });
  const act = mutated();
  row(act, "headings").act = "replaces";
  assert.throws(() => entries("everything", act), { message: "unknown act: replaces" });
});

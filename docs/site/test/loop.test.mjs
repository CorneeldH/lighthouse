// The maps in docs/site/data/ stay in step with pack/: every source exists, every status and
// route holds together. The generic checks are functions that return problems, run over a small
// fixture map and every map in the data directory; the loop-specific ones run on loop.json only.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { REPO_BLOB, ROOT, SITE, load } from "./helpers.mjs";

const FIXTURE = path.join(SITE, "test", "fixtures", "map.json");
const KINDS = new Set(["lighthouse", "superpowers", "hook", "github"]);
const TRIGGERS = new Set(["you", "skill", "description", "tool", "event", "github"]);
const STATUSES = new Set(["works", "planned", "idea"]);
// An optional `effects` lists what a hook chip does: it blocks, warns, formats or records a baseline.
const EFFECTS = new Set(["blocks", "warns", "formats", "records"]);
// An optional `form` names a part that is no skill, agent, hook or workflow of a pack.
const FORMS = new Set(["tool", "dispatcher"]);
const REQUIRED = ["id", "phase", "row", "kind", "trigger", "label", "what", "how", "when", "without", "status"];

const readJson = (file) => JSON.parse(fs.readFileSync(file, "utf-8"));
const clone = (value) => structuredClone(value);
const chipsById = (chain) => Object.fromEntries(chain.chips.map((c) => [c.id, c]));

const dataMaps = fs
  .readdirSync(path.join(SITE, "data"))
  .filter((n) => n.endsWith(".json"))
  .sort()
  .map((n) => [n, load(n)])
  .filter(([, data]) => data && !Array.isArray(data) && "chips" in data);
const MAPS = [["fixture", readJson(FIXTURE)], ...dataMaps];
for (const name of ["loop.json", "session.json"])
  assert.ok(
    dataMaps.some(([n]) => n === name),
    `${name} is not among the checked data maps`,
  );

function vocabularyProblems(chain) {
  const phases = new Set(chain.phases.map((p) => p.id));
  const problems = [];
  const seenIds = new Set();
  const seenCells = new Set();
  for (const chip of chain.chips) {
    const name = chip.id;
    for (const key of REQUIRED) if (!(key in chip)) problems.push(`${name}: lacks '${key}'`);
    if (!KINDS.has(chip.kind)) problems.push(`${name}: unknown kind`);
    if (!TRIGGERS.has(chip.trigger)) problems.push(`${name}: unknown trigger`);
    if (!STATUSES.has(chip.status)) problems.push(`${name}: unknown status`);
    if ("form" in chip && !FORMS.has(chip.form)) problems.push(`${name}: unknown form`);
    if ("effects" in chip && !(chip.effects.length > 0 && chip.effects.every((e) => EFFECTS.has(e))))
      problems.push(`${name}: unknown effect`);
    if (!phases.has(chip.phase)) problems.push(`${name}: unknown phase`);
    if (seenIds.has(name)) problems.push(`${name}: duplicate id`);
    const cell = `(${chip.phase}, ${chip.row})`;
    if (seenCells.has(cell)) problems.push(`${name}: shares cell ${cell}`);
    seenIds.add(name);
    seenCells.add(cell);
  }
  return problems;
}

/** Working lighthouse and hook chips need a REPO_BLOB source; every source under it must exist. */
function sourceProblems(chain) {
  const problems = [];
  const exists = (source) => fs.existsSync(path.join(ROOT, source.slice(REPO_BLOB.length)));
  for (const chip of chain.chips) {
    const name = chip.id;
    const own = chip.kind === "lighthouse" || chip.kind === "hook";
    if (own && chip.status === "works" && !("source" in chip)) problems.push(`${name}: no source`);
    const source = chip.source;
    if (source === undefined) continue;
    if (!source.startsWith(REPO_BLOB)) {
      if (own) problems.push(`${name}: source is not under REPO_BLOB`);
    } else if (!exists(source)) problems.push(`${name}: ${source} does not exist`);
  }
  const handoff = chain.handoff;
  if (handoff && handoff.source.startsWith(REPO_BLOB) && !exists(handoff.source))
    problems.push(`handoff: ${handoff.source} does not exist`);
  return problems;
}

function plannedProblems(chain) {
  const items = chain.chips.map((c) => [c.id, c]);
  items.push(...chain.handoffs.map((h) => [`${h.from}->${h.to}`, h]));
  return items
    .filter(([, item]) => item.status === "planned" && !("issue" in item))
    .map(([name]) => `${name}: planned without an issue`);
}

function routeProblems(chain) {
  const chips = chipsById(chain);
  return chain.routes.flatMap((route) =>
    route.chips.filter((id) => !(id in chips)).map((id) => `route ${route.id}: unknown chip ${id}`),
  );
}

/** A handoff names two chips that a route passes in that order, within a phase or across. */
function handoffProblems(chain) {
  const problems = [];
  for (const h of chain.handoffs) {
    const on = chain.routes.filter((r) => r.chips.includes(h.from) && r.chips.includes(h.to));
    if (!on.some((r) => r.chips.indexOf(h.from) < r.chips.indexOf(h.to)))
      problems.push(`handoff on no route: ('${h.from}', '${h.to}')`);
  }
  return problems;
}

/** Every chip is on a route; every example is on its own route. */
function coverageProblems(chain) {
  const onRoute = new Set(chain.routes.flatMap((r) => r.chips));
  const problems = chain.chips.filter((c) => !onRoute.has(c.id)).map((c) => `${c.id}: on no route`);
  for (const route of chain.routes)
    for (const id of route.examples ?? [])
      if (!route.chips.includes(id)) problems.push(`route ${route.id}: example ${id} is not on the route`);
  return problems;
}

const GENERIC = [
  ["chips use the fixed vocabulary", vocabularyProblems],
  ["every source exists", sourceProblems],
  ["planned has an issue", plannedProblems],
  ["routes name existing chips", routeProblems],
  ["every handoff sits on a route", handoffProblems],
  ["every chip is on a route", coverageProblems],
];
for (const [name, check] of GENERIC) {
  test(name, () => {
    for (const [file, chain] of MAPS) assert.deepEqual(check(chain), [], file);
  });
}

// Each checker must be able to fail: a broken checker would stay green over every map above.
const fixtureCases = (cases, check) => {
  for (const { naam, mutatie, verwacht } of cases) {
    const chain = clone(readJson(FIXTURE));
    mutatie(chain);
    assert.deepEqual(check(chain), verwacht, naam);
  }
};

test("vocabularyProblems flags an unknown status and an unknown effect", () => {
  fixtureCases(
    [
      { naam: "unknown status", mutatie: (c) => (c.chips[0].status = "done"), verwacht: ["write-issue: unknown status"] },
      {
        naam: "unknown effect",
        mutatie: (c) => (c.chips[1].effects = ["shouts"]),
        verwacht: ["main-commit-gate: unknown effect"],
      },
    ],
    vocabularyProblems,
  );
});

test("sourceProblems flags a source pointing nowhere", () => {
  fixtureCases(
    [
      {
        naam: "source pointing nowhere",
        mutatie: (c) => (c.chips[0].source = `${REPO_BLOB}pack/core/skills/no-such-skill`),
        verwacht: [`write-issue: ${REPO_BLOB}pack/core/skills/no-such-skill does not exist`],
      },
    ],
    sourceProblems,
  );
});

test("plannedProblems flags a planned item without an issue, also with only a milestone", () => {
  // The site half of the agreement with ci/loop_roadmap.py: an issue is what counts.
  const planned = (fields) => ({ chips: [{ id: "x", status: "planned", ...fields }], handoffs: [] });
  for (const [fields, flagged] of [
    [{ issue: 7 }, false],
    [{ issue: 7, milestone: "M9" }, false],
    [{ milestone: "M9" }, true],
    [{}, true],
  ])
    assert.equal(plannedProblems(planned(fields)).length > 0, flagged, JSON.stringify(fields));
  fixtureCases(
    [
      {
        naam: "planned chip without issue",
        mutatie: (c) => delete c.chips.find((chip) => chip.id === "planned-issue").issue,
        verwacht: ["planned-issue: planned without an issue"],
      },
      {
        naam: "planned handoff with only a milestone",
        mutatie: (c) => delete c.handoffs[0].issue,
        verwacht: ["main-commit-gate->planned-issue: planned without an issue"],
      },
    ],
    plannedProblems,
  );
});

test("handoffProblems flags a handoff against the route", () => {
  fixtureCases(
    [
      {
        naam: "handoff against the route",
        mutatie: (c) => c.handoffs.push({ from: "planned-issue", to: "write-issue", status: "works" }),
        verwacht: ["handoff on no route: ('planned-issue', 'write-issue')"],
      },
    ],
    handoffProblems,
  );
});

test("coverageProblems flags a chip off every route and an example off its route", () => {
  fixtureCases(
    [
      {
        naam: "chip off every route",
        mutatie: (c) => {
          c.routes[0].chips = c.routes[0].chips.filter((id) => id !== "an-idea");
          c.routes[0].examples = ["an-idea"];
        },
        verwacht: ["an-idea: on no route", "route x: example an-idea is not on the route"],
      },
    ],
    coverageProblems,
  );
});

test("routeProblems flags a route naming an unknown chip", () => {
  fixtureCases(
    [
      {
        naam: "unknown route chip",
        mutatie: (c) => c.routes[0].chips.push("ghost"),
        verwacht: ["route x: unknown chip ghost"],
      },
    ],
    routeProblems,
  );
});

// Handoffs the dispatcher starts itself, not through a handoff block.
const DISPATCHED = [["pr-and-ci", "fix-pr"]];
// Handoffs into the workflow pusher: `next: push-workflows`, no slash command.
const PUSHER = [
  ["commit-test-gate", "push-workflows"],
  ["fix-pr", "push-workflows"],
];
const inPairs = (pairs, source, target) => pairs.some(([s, t]) => s === source && t === target);

test("works handoffs follow the handoff table", () => {
  const chain = load("loop.json");
  const chips = chipsById(chain);
  // A works handoff is built: it carries no tracking issue.
  assert.deepEqual(
    chain.handoffs.filter((h) => h.status === "works" && "issue" in h).map((h) => `${h.from}->${h.to}`),
    [],
    "works-handoff heeft geen issue",
  );
  // The handoff skill's routing table names the first step of each works handoff,
  // except the ones the dispatcher starts itself.
  const table = fs.readFileSync(path.join(ROOT, "pack", "core", "skills", "write-handoff", "SKILL.md"), "utf-8");
  const judge = fs.readFileSync(path.join(ROOT, "internal", "dispatch", "judge.go"), "utf-8");
  const works = chain.handoffs.filter((h) => h.status === "works");
  assert.ok(works.length > 0, "no works handoff");
  for (const { from, to } of works) {
    if (inPairs(DISPATCHED, from, to)) {
      assert.ok(judge.includes(`case "/lighthouse-${to}":`), `${to}: judge.go has no case`);
    } else if (inPairs(PUSHER, from, to)) {
      assert.ok(table.includes("`push-workflows`"), `${to}: handoff table lacks push-workflows`);
    } else {
      const prefix = chips[to].kind === "lighthouse" ? "lighthouse-" : "superpowers:";
      assert.ok(table.includes(`\`/${prefix}${to}`), `${to}: handoff table lacks /${prefix}${to}`);
    }
  }
  assert.ok(
    chain.handoffs.every((h) => h.status === "works" || h.status === "planned"),
    "alle statussen zijn works of planned",
  );
});

test("the old chain page forwards to the loop", () => {
  const html = fs.readFileSync(path.join(SITE, "chain.html"), "utf-8");
  assert.ok(html.includes('<meta http-equiv="refresh" content="0; url=loop.html">'));
  assert.ok(html.includes('href="loop.html"'));
});

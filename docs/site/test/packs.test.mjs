// docs/site/data/packs.json stays in step with pack/: every pack is listed, with the files of its
// detect.toml, its skills, agents, workflows and hooks.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { ROOT, load } from "./helpers.mjs";

const PACK = path.join(ROOT, "pack");
const packs = load("packs.json");
const read = (...parts) => fs.readFileSync(path.join(PACK, ...parts), "utf-8");

// The `when` text per set of (event, matcher) pairs a hook is registered under.
const pairsKey = (pairs) => JSON.stringify([...new Set(pairs.map((p) => JSON.stringify(p)))].sort());
const WHEN = new Map(
  [
    [[["PreToolUse", "Bash"]], "before every shell command"],
    [
      [["PreToolUse", "Bash"], ["PostToolUse", "Bash"], ["PostToolUseFailure", "Bash"]],
      "around every shell command",
    ],
    [[["PreToolUse", "Edit|Write"]], "before every edit"],
    [[["PostToolUse", "Edit|Write"]], "after every edit"],
    [[["Stop", ""]], "when the session stops"],
    [
      [["PostToolUse", ""], ["UserPromptSubmit", ""], ["SubagentStop", ""], ["Stop", ""]],
      "at every prompt, tool call, subagent stop and session stop",
    ],
    [[["Notification", "permission_prompt|worker_permission_prompt"]], "when Claude asks for a permission in a controller run"],
  ].map(([pairs, when]) => [pairsKey(pairs), when]),
);

/** hook name -> [[event, matcher]] from pack/<name>/hooks/hooks.json. */
function registered(name) {
  const file = path.join(PACK, name, "hooks", "hooks.json");
  if (!fs.existsSync(file)) return {};
  const found = {};
  for (const [event, groups] of Object.entries(JSON.parse(fs.readFileSync(file, "utf-8")).hooks))
    for (const group of groups)
      for (const hook of group.hooks) {
        // `run.sh <name>` runs `lighthouse hook <name>`: the hook is the name.
        const match = /hooks\/([^/\s]+)\.sh(?: ([^\s]+))?/.exec(hook.command);
        const stem = match[1] === "run" ? match[2] : match[1];
        (found[stem] ??= []).push([event, group.matcher ?? ""]);
      }
  return found;
}

/** The detect.toml of a pack, for the one shape those files have: `files = [...]` at the top and
 * `[[contains]]` blocks with `files` and `text`. Any other line fails: no silent misreading. */
function parseDetect(text) {
  const spec = { files: [], contains: [] };
  let block = spec;
  for (const raw of text.split("\n")) {
    const line = raw.trim();
    if (line === "" || line.startsWith("#")) continue;
    if (line === "[[contains]]") {
      block = {};
      spec.contains.push(block);
      continue;
    }
    const m = /^(files|text)\s*=\s*(.+)$/.exec(line);
    if (!m) throw new Error(`detect.toml: cannot read line: ${raw}`);
    block[m[1]] = JSON.parse(m[2]);
  }
  return spec;
}

const names = (dir, keep) =>
  fs.existsSync(dir) ? fs.readdirSync(dir).filter(keep).sort() : [];

test("every pack is listed", () => {
  const onDisk = fs
    .readdirSync(PACK, { withFileTypes: true })
    .filter((e) => e.isDirectory())
    .map((e) => e.name)
    .sort();
  assert.deepEqual(packs.map((p) => p.name).sort(), onDisk);
});

test("detect matches detect.toml", () => {
  for (const entry of packs) {
    const file = path.join(PACK, entry.name, "detect.toml");
    if (!fs.existsSync(file)) {
      assert.deepEqual(entry.detect, [], entry.name);
      assert.equal(entry.proposed_when, "always", entry.name);
      continue;
    }
    const spec = parseDetect(read(entry.name, "detect.toml"));
    const expected = [...spec.files];
    for (const rule of spec.contains) {
      for (const f of rule.files) if (!expected.includes(f)) expected.push(f);
      const text = rule.text.replace(/^"+|"+$/g, "");
      assert.ok(entry.proposed_when.includes(text), `${entry.name}: proposed_when lacks ${text}`);
    }
    assert.deepEqual(entry.detect, expected, entry.name);
  }
});

test("skills, agents and workflows match", () => {
  for (const entry of packs) {
    const base = path.join(PACK, entry.name);
    assert.deepEqual(entry.skills, names(path.join(base, "skills"), () => true), entry.name);
    const stems = (dir, keep) => names(dir, keep).map((n) => path.parse(n).name);
    assert.deepEqual(entry.agents, stems(path.join(base, "agents"), (n) => n.endsWith(".md")), entry.name);
    assert.deepEqual(entry.workflows, stems(path.join(base, "workflows"), () => true), entry.name);
  }
});

test("hooks match hooks.json", () => {
  for (const entry of packs) {
    const found = registered(entry.name);
    assert.deepEqual(entry.hooks.map((h) => h.name).sort(), Object.keys(found).sort(), entry.name);
    for (const hook of entry.hooks) {
      const events = new Set(found[hook.name].map(([event]) => event));
      assert.deepEqual([...hook.events].sort(), [...events].sort(), hook.name);
    }
  }
});

test("hook when follows its events", () => {
  for (const entry of packs) {
    const found = registered(entry.name);
    for (const hook of entry.hooks) {
      const key = pairsKey(found[hook.name]);
      assert.ok(WHEN.has(key), `${hook.name}: unknown set of event/matcher pairs ${key}`);
      assert.equal(hook.when, WHEN.get(key), hook.name);
    }
  }
});

test("standard skills follow the maps", () => {
  const byPack = Object.fromEntries(packs.map((p) => [p.name, p]));
  for (const entry of packs)
    for (const skill of entry.standard_skills) assert.ok(entry.skills.includes(skill), `${entry.name}: ${skill}`);
  for (const name of ["loop.json", "session.json"])
    for (const chip of load(name).chips) {
      const m = /\/blob\/main\/pack\/([^/]+)\/skills\/([^/]+)/.exec(chip.source ?? "");
      if (m) assert.equal(byPack[m[1]].standard_skills.includes(m[2]), chip.trigger === "description", chip.id);
    }
});

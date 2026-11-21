// The reference page lists every skill, agent, hook and workflow of pack/, each linking to its
// place in the repository.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { REPO_BLOB, REPO_TREE, ROOT, load } from "./helpers.mjs";
import * as m from "../map.js";

const PARTS = ["skills", "agents", "hooks", "workflows"];
// run.sh and blocking are the runtime of the hooks, not hooks: they are not on the reference.
const RUNTIME = new Set(["run.sh", "blocking"]);

const list = (dir) => (fs.existsSync(dir) ? fs.readdirSync(dir, { withFileTypes: true }) : []);

/** The script path of each hook that packs.json places in a Go package under internal/: the
 * reference links the package, also while the old script is still there. */
function goHookScripts() {
  const out = new Set();
  for (const pack of load("packs.json"))
    for (const hook of pack.hooks)
      if ((hook.source || "").startsWith("internal/")) out.add(`pack/${pack.name}/hooks/${hook.name}.sh`);
  return out;
}

/** Every skill, agent, workflow and hook script under pack/, repo-relative. */
function packParts() {
  const parts = new Set();
  for (const pack of list(path.join(ROOT, "pack")).filter((e) => e.isDirectory())) {
    const base = `pack/${pack.name}`;
    for (const e of list(path.join(ROOT, base, "skills"))) if (e.isDirectory()) parts.add(`${base}/skills/${e.name}`);
    for (const e of list(path.join(ROOT, base, "agents"))) if (e.name.endsWith(".md")) parts.add(`${base}/agents/${e.name}`);
    for (const e of list(path.join(ROOT, base, "workflows"))) parts.add(`${base}/workflows/${e.name}`);
    for (const e of list(path.join(ROOT, base, "hooks")))
      if (e.name.endsWith(".sh") && !RUNTIME.has(e.name)) parts.add(`${base}/hooks/${e.name}`);
  }
  for (const gone of goHookScripts()) parts.delete(gone);
  return parts;
}

/** The repo paths the reference page links, from referenceEntries over packs.json. */
function referencePaths() {
  const packs = load("packs.json");
  const found = new Set();
  for (const part of PARTS)
    for (const entry of m.referenceEntries(packs, part))
      for (const item of entry.items) {
        const prefix = [REPO_BLOB, REPO_TREE].find((p) => item.href.startsWith(p));
        assert.ok(prefix, `${item.name}: ${item.href} is no repo link`);
        found.add(item.href.slice(prefix.length));
      }
  return found;
}

test("every pack part is on the reference", () => {
  const linked = referencePaths();
  const parts = packParts();
  assert.ok(parts.size > 0, "no pack parts found");
  assert.ok(linked.size > 0, "no reference paths found");
  assert.deepEqual([...parts].filter((p) => !linked.has(p)).sort(), []);
});

test("every reference link exists", () => {
  assert.ok(referencePaths().size > 0, "no reference paths found");
  assert.deepEqual([...referencePaths()].sort().filter((p) => !fs.existsSync(path.join(ROOT, p))), []);
});

test("reference links a go hook to its source", () => {
  const hrefs = {};
  for (const e of m.referenceEntries(load("packs.json"), "hooks")) for (const i of e.items) hrefs[i.name] = i.href;
  assert.equal(hrefs["code-language-check"], REPO_BLOB + "internal/codelang/hook.go");
  assert.equal(hrefs["commit-test-gate"], REPO_BLOB + "internal/hooks/committestgate.go");
});

test("reference marks standard skills", () => {
  const sorts = {};
  for (const e of m.referenceEntries(load("packs.json"), "skills")) for (const i of e.items) sorts[i.name] = i.sort;
  assert.equal(sorts["test-standard"], "standard-skill");
  assert.equal(sorts["place-instruction"], "workflow-skill");
});

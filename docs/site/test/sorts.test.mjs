// One table of sorts in map.js: the glyph on a chip follows what the thing is, not what starts it,
// and the legends of the flow maps name only sorts of that table.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { REPO_BLOB, SITE } from "./helpers.mjs";
import * as m from "../map.js";

const SPEC_GLYPHS = {
  program: "⛯", "workflow-skill": "↪", "standard-skill": "✓", agent: "◆", hook: "⚡",
  rules: "§", config: "⚙", github: "⑂", loop: "∞", session: "❯", you: "▣",
};
const SKILL = REPO_BLOB + "pack/core/skills/test-standard";

test("every sort has the glyph of the spec", () => {
  const glyphs = Object.fromEntries(Object.entries(m.SORTS).map(([k, v]) => [k, v.glyph]));
  assert.deepEqual(glyphs, SPEC_GLYPHS);
});

test("glyph follows the sort, not the trigger", () => {
  const cases = [
    [{ kind: "lighthouse", trigger: "description", source: SKILL }, "✓"],
    [{ kind: "lighthouse", trigger: "you", source: SKILL }, "↪"],
    [{ kind: "superpowers", trigger: "skill" }, "↪"],
    [{ kind: "superpowers", trigger: "description" }, "✓"],
    [{ kind: "lighthouse", trigger: "skill", source: REPO_BLOB + "pack/core/agents/issue-impact-scout.md" }, "◆"],
    [{ kind: "hook", trigger: "event" }, "⚡"],
    [{ kind: "hook", trigger: "tool" }, "⚡"],
    [{ kind: "github", trigger: "github" }, "⑂"],
    [{ kind: "github", trigger: "github", source: REPO_BLOB + "pack/core/workflows/unblock-dependents.yml" }, "⑂"],
    [{ kind: "lighthouse", trigger: "skill", form: "tool" }, "▣"],
    [{ kind: "lighthouse", trigger: "github", form: "dispatcher" }, "⛯"],
    [{ kind: "lighthouse", form: "rules" }, "§"],
  ];
  for (const [item, glyph] of cases) assert.equal(m.glyphOf(item), glyph, JSON.stringify(item));
});

test("legends name known sorts", () => {
  const known = new Set(Object.keys(m.SORTS));
  const counts = {};
  const pages = fs.readdirSync(SITE).filter((n) => n.endsWith(".html")).sort();
  for (const name of pages) {
    const html = fs.readFileSync(path.join(SITE, name), "utf-8");
    const found = [...html.matchAll(/data-sorts="([^"]*)"/g)].map((x) => x[1]);
    counts[name] = found.length;
    for (const names of found)
      for (const sort of names.split(/\s+/).filter(Boolean))
        assert.ok(known.has(sort), `${name}: unknown sort ${sort} in ${JSON.stringify(names)}`);
  }
  for (const page of ["loop.html", "session.html", "reference.html", "config.html"])
    assert.ok(counts[page], `${page} has no data-sorts legend`);
  // The system page is read without one: every box names what it is.
  assert.equal(counts["system.html"], 0);
});

const css = fs.readFileSync(path.join(SITE, "map.css"), "utf-8");

test("config, rules, session and you tokens exist in every theme", () => {
  for (const token of ["--config:", "--config-fg:", "--rules:", "--rules-fg:", "--session:", "--session-fg:", "--you:", "--you-fg:"])
    assert.equal(css.split(token).length - 1, 3, token);
});

/** The `--kind` value of the rule whose selector list names `.cls`. */
function kindColour(text, cls) {
  for (const [, selectors, body] of text.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (selectors.split(",").map((s) => s.trim()).includes(cls)) {
      const found = body.match(/--kind:\s*([^;]+);/);
      if (found) return found[1].trim();
    }
  }
  throw new Error(`no --kind for ${cls}`);
}

test("neighbouring sorts have their own colour", () => {
  for (const [one, other] of [[".form-rules", ".form-config"], [".form-loop", ".form-session"]])
    assert.notEqual(kindColour(css, one), kindColour(css, other), `${one} vs ${other}`);
});

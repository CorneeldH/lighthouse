// The local server command is named where a reader looks for it: on every map page, visible
// without JS, and in the getting-started guide.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { ROOT, SITE, elementById } from "./helpers.mjs";

const SERVE_COMMAND = "go run ./tools/servedir docs/site";
const MAP_PAGES = ["loop.html", "session.html", "system.html", "reference.html", "config.html", "repo.html"];
const read = (file) => fs.readFileSync(file, "utf-8");

test("map pages name the local server command", () => {
  for (const page of MAP_PAGES) {
    const el = elementById(read(path.join(SITE, page)), "serve-hint");
    assert.ok(el, `${page} has no element with id="serve-hint"`);
    assert.ok(el.text.split(/\s+/).join(" ").includes(SERVE_COMMAND), `${page} does not name the command`);
    assert.ok(!("hidden" in el.attrs), `${page}: serve-hint is hidden`);
    const style = (el.attrs.style ?? "").replaceAll(" ", "");
    assert.ok(!style.includes("display:none"), `${page}: serve-hint is display:none`);
  }
});

test("the guide names the local server command in its first-issue section", () => {
  const guide = read(path.join(ROOT, "docs", "getting-started.md"));
  const start = guide.indexOf("## Your first issue");
  assert.ok(start >= 0, "getting-started.md has no '## Your first issue' section");
  const next = guide.indexOf("\n## ", start + 1);
  const section = guide.slice(start, next < 0 ? undefined : next).split(/\s+/).join(" ");
  assert.ok(section.includes(SERVE_COMMAND), "the first-issue section does not name the command");
});

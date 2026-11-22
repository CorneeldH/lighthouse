// The pages of docs/site/ hold together: every relative link resolves, the component never writes data as HTML, and every page is
// marked as English.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { ROOT, SITE, relativeLinks, siteFiles } from "./helpers.mjs";

const GUIDE = path.join(ROOT, "docs", "getting-started.md");
const HTML_SINKS = ["innerHTML", "outerHTML", "insertAdjacentHTML", "document.write"];
const MAP_PAGES = ["loop.html", "session.html", "system.html", "reference.html", "config.html", "repo.html"];
const read = (file) => fs.readFileSync(file, "utf-8");

test("site links resolve", () => {
  const pages = siteFiles(".html");
  assert.ok(pages.length > 0, "docs/site has no HTML page");
  for (const file of [...pages, ...siteFiles(".js", ".md"), GUIDE]) {
    const links = relativeLinks(file);
    if (file.endsWith(".html")) assert.ok(links.length > 0, `${path.basename(file)} links no stylesheet or script`);
    for (const link of links) {
      const target = path.resolve(path.dirname(file), link);
      assert.ok(fs.existsSync(target) && fs.statSync(target).isFile(), `${path.basename(file)}: ${link} points nowhere`);
    }
  }
});

test("component never writes html from data", () => {
  const scripts = siteFiles(".js");
  assert.ok(scripts.length > 0, "docs/site has no JS file");
  for (const file of scripts) {
    const text = read(file);
    for (const sink of HTML_SINKS) assert.ok(!text.includes(sink), `${path.basename(file)} uses ${sink}`);
  }
});

test("page lang is english", () => {
  const pages = siteFiles(".html");
  assert.ok(pages.length > 0, "docs/site has no HTML page");
  for (const file of pages) assert.ok(read(file).includes('<html lang="en">'), path.basename(file));
});

test("index lists the six maps", () => {
  const links = relativeLinks(path.join(SITE, "index.html"));
  for (const page of ["loop.html", "session.html", "system.html", "config.html", "repo.html"])
    assert.ok(links.includes(page), `index.html does not link ${page}`);
  assert.ok(read(path.join(SITE, "index.html")).includes('id="maps"'));
});

test("map pages link back to index", () => {
  for (const page of MAP_PAGES)
    assert.ok(read(path.join(SITE, page)).includes('href="index.html"'), `${page} does not link back`);
});

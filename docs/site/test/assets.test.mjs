// Every local asset reference in docs/site/ carries the version token, so a visitor with an old
// cache never mixes a new page with an old module.
//
// Na een wijziging aan een `.js`/`.css`: `node docs/site/test/version.mjs`; anders is
// `every local asset carries the site version` rood.
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { SITE, relativeLinks } from "./helpers.mjs";
import { assetRefs, siteVersion } from "./version.mjs";

const top = (...exts) =>
  fs
    .readdirSync(SITE)
    .filter((n) => exts.includes(path.extname(n)))
    .sort()
    .map((n) => path.join(SITE, n));

test("every local asset carries the site version", () => {
  const version = siteVersion();
  const refs = top(".html", ".js").flatMap((p) => assetRefs(p).map((r) => [path.basename(p), r]));
  assert.ok(refs.length > 0, "no asset references found");
  assert.deepEqual(
    refs.filter(([, r]) => !r.endsWith(`?v=${version}`)),
    [],
  );
});

test("version follows the assets, not the tokens", () => {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "site-version-"));
  try {
    for (const p of top(".js", ".css")) fs.copyFileSync(p, path.join(tmp, path.basename(p)));
    const before = siteVersion(tmp);
    const css = path.join(tmp, "map.css");
    fs.writeFileSync(css, fs.readFileSync(css, "utf-8") + "\n/* x */\n");
    const changed = siteVersion(tmp);
    assert.notEqual(changed, before);
    const js = path.join(tmp, "loop.js");
    const text = fs.readFileSync(js, "utf-8");
    const retoken = text.replace(/(\.\/map\.js)(\?v=[0-9a-f]{8})?/, "$1?v=00000000");
    fs.writeFileSync(js, retoken);
    assert.notEqual(retoken, text);
    assert.equal(siteVersion(tmp), changed);
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
});

test("assetRefs misses no local asset", () => {
  for (const p of top(".html", ".js")) {
    const expected = new Set(relativeLinks(p).filter((l) => l.endsWith(".js") || l.endsWith(".css")));
    const got = new Set(assetRefs(p).map((r) => r.split("?", 1)[0]));
    assert.deepEqual(got, expected, path.basename(p));
  }
});

// The version token of the static site: `?v=<8 hex>` on every local `.js`/`.css` reference.
//
// The token is the first 8 hex characters of a sha256 over the names and contents of the top-level
// `.js` and `.css` files of docs/site/. Tokens are stripped before hashing, so writing them does
// not change the version.
//
// Na een wijziging aan een `.js`/`.css`: `node docs/site/test/version.mjs` (schrijft elke
// verwijzing opnieuw en print de gewijzigde bestanden).
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { SITE } from "./helpers.mjs";

const TOKEN = /\?v=[0-9a-f]{8}/g;
const HTML_REF = /(\b(?:href|src)\s*=\s*["'])([^"':#]+\.(?:js|css)(?:\?[^"']*)?)(["'])/g;
const JS_REF = /(\bfrom\s*["'])(\.\/[^"']+\.js(?:\?[^"']*)?)(["'])/g;

const top = (site, ...exts) =>
  fs
    .readdirSync(site)
    .filter((n) => exts.includes(path.extname(n)) && fs.statSync(path.join(site, n)).isFile())
    .sort();

export function siteVersion(site = SITE) {
  const digest = crypto.createHash("sha256");
  for (const name of top(site, ".js", ".css")) {
    digest.update(name, "utf-8");
    digest.update(fs.readFileSync(path.join(site, name), "utf-8").replace(TOKEN, ""), "utf-8");
  }
  return digest.digest("hex").slice(0, 8);
}

const pattern = (file) => (path.extname(file) === ".html" ? HTML_REF : JS_REF);

/** Every local `.js`/`.css` reference in `file` as written, query included. */
export function assetRefs(file) {
  return [...fs.readFileSync(file, "utf-8").matchAll(pattern(file))].map((m) => m[2]);
}

/** Sets every reference to `<name>?v=<siteVersion()>`; returns the paths that changed. */
export function writeVersion(site = SITE) {
  const version = siteVersion(site);
  const changed = [];
  for (const name of top(site, ".html", ".js")) {
    const file = path.join(site, name);
    const text = fs.readFileSync(file, "utf-8");
    const next = text.replace(pattern(file), (_, a, ref, b) => `${a}${ref.split("?", 1)[0]}?v=${version}${b}`);
    if (next !== text) {
      fs.writeFileSync(file, next, "utf-8");
      changed.push(file);
    }
  }
  return changed;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  for (const file of writeVersion()) console.log(file);
}

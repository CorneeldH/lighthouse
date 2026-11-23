// Shared helpers of the site tests (`node --test "docs/site/test/*.test.mjs"`).
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..", "..");
export const SITE = path.join(ROOT, "docs", "site");
export const REPO_BLOB = "https://github.com/CorneeldH/lighthouse/blob/main/";
export const REPO_TREE = "https://github.com/CorneeldH/lighthouse/tree/main/";

/** The JSON of docs/site/data/<name>. */
export function load(name) {
  return JSON.parse(fs.readFileSync(path.join(SITE, "data", name), "utf-8"));
}

const HTML_LINK = /\b(?:href|src|data-src)\s*=\s*["']([^"']+)["']/g;
const JS_IMPORT = /\bimport\b[^;]*?\bfrom\s*["']([^"']+)["']/g;
const JS_BARE_IMPORT = /\bimport\s*["']([^"']+)["']/g;
const JS_LOAD_JSON = /\bloadJson\(\s*["']([^"']+)["']\s*\)/g;
const MD_LINK = /\]\(([^)\s]+)\)/g;

function all(re, text) {
  return [...text.matchAll(re)].map((m) => m[1]);
}

/** The relative link targets in `file`, without fragment or query: `href`, `src` and `data-src`
 * in HTML, `import ... from "..."`, bare `import "..."` and `loadJson("...")` in JS, `](...)` in
 * markdown. Links that start with http(s):, mailto: or # are left out. */
export function relativeLinks(file) {
  const text = fs.readFileSync(file, "utf-8");
  const ext = path.extname(file);
  let found = [];
  if (ext === ".html") found = all(HTML_LINK, text);
  else if (ext === ".js" || ext === ".mjs")
    found = [...all(JS_IMPORT, text), ...all(JS_BARE_IMPORT, text), ...all(JS_LOAD_JSON, text)];
  else if (ext === ".md") found = all(MD_LINK, text);
  const links = [];
  for (const raw of found) {
    if (/^(http:|https:|mailto:|#)/.test(raw)) continue;
    const link = raw.split(/[#?]/, 1)[0];
    if (link) links.push(link);
  }
  return links;
}

/** Every file under docs/site/ with one of the suffixes, sorted; docs/site/test/ is skipped,
 * because the test files import node:test and are no part of the site. */
export function siteFiles(...suffixes) {
  const out = [];
  const walk = (dir) => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        if (full !== path.join(SITE, "test")) walk(full);
      } else if (suffixes.includes(path.extname(entry.name))) out.push(full);
    }
  };
  walk(SITE);
  return out.sort();
}

/** Decodes `&amp; &lt; &gt; &quot; &#39;` and numeric `&#NN;` / `&#xNN;`. */
export function decodeEntities(text) {
  return text
    .replace(/&#x([0-9a-f]+);/gi, (_, h) => String.fromCodePoint(parseInt(h, 16)))
    .replace(/&#(\d+);/g, (_, d) => String.fromCodePoint(Number(d)))
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&amp;/g, "&");
}

const VOID = new Set(["area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr"]);
const TAG = /<!--[\s\S]*?-->|<(\/?)([a-zA-Z][\w:-]*)((?:"[^"]*"|'[^']*'|[^'">])*?)(\/?)>/g;
const ATTR = /([^\s"'<>\/=]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>`]+)))?/g;

function parseAttrs(source) {
  const attrs = {};
  for (const m of source.matchAll(ATTR)) {
    const value = m[2] ?? m[3] ?? m[4];
    attrs[m[1].toLowerCase()] = value === undefined ? null : decodeEntities(value);
  }
  return attrs;
}

/** The first element with `id`: its start-tag attributes (a bare attribute is `null`) and the
 * text of all nested elements, entities decoded. `null` when there is none. */
export function elementById(html, id) {
  let found = null;
  let name = "";
  let depth = 0;
  let text = "";
  let last = 0;
  for (const m of html.matchAll(TAG)) {
    if (found) text += html.slice(last, m.index);
    last = m.index + m[0].length;
    if (m[0].startsWith("<!--")) continue;
    const closing = m[1] === "/";
    const tag = m[2].toLowerCase();
    if (!found) {
      if (closing) continue;
      const attrs = parseAttrs(m[3]);
      if (attrs.id !== id) continue;
      found = { attrs, text: "" };
      if (m[4] === "/" || VOID.has(tag)) return found;
      name = tag;
      depth = 1;
    } else if (tag === name) {
      if (closing) depth -= 1;
      else if (m[4] !== "/") depth += 1;
      if (depth === 0) {
        found.text = decodeEntities(text);
        return found;
      }
    }
  }
  if (found) found.text = decodeEntities(text + html.slice(last));
  return found;
}

let builtBin = null;
let buildFailure = null;

/** The lighthouse binary: `$LIGHTHOUSE_BIN` when set and not empty, otherwise one build of
 * ./cmd/lighthouse into a temporary directory, removed at exit. Fails, never skips, when there is
 * neither. */
export function lighthouseBin() {
  const given = (process.env.LIGHTHOUSE_BIN ?? "").trim();
  if (given) return given;
  if (builtBin) return builtBin;
  if (buildFailure) throw buildFailure;
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "lighthouse-bin-"));
  process.on("exit", () => fs.rmSync(dir, { recursive: true, force: true }));
  const out = path.join(dir, "lighthouse");
  try {
    execFileSync("go", ["build", "-o", out, "./cmd/lighthouse"], { cwd: ROOT, stdio: "pipe" });
  } catch (err) {
    const detail = String(err.stderr ?? err.message ?? "").trim();
    buildFailure = new Error(
      `lighthouse binary needed: set LIGHTHOUSE_BIN or install Go (no silent skip)${detail ? `\n${detail}` : ""}`,
    );
    throw buildFailure;
  }
  builtBin = out;
  return out;
}

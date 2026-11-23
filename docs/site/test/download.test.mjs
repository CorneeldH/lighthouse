// The download on install.html: platform.js picks the install block the visitor sees first, the
// release label falls back to text when the API gives nothing usable, and every install block names
// a built asset and checks it before the binary is used.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { ROOT, SITE, decodeEntities, elementById } from "./helpers.mjs";
import { choosePlatform, releaseLabel } from "../platform.js";

const INDEX = path.join(SITE, "index.html");
const INSTALL = path.join(SITE, "install.html");
const BUILD = path.join(ROOT, ".github", "workflows", "build.yml");
const DOWNLOAD = "https://github.com/CorneeldH/lighthouse/releases/latest/download/";
const TABS = ["linux-amd64", "linux-arm64", "darwin-arm64", "darwin-amd64", "windows-amd64"];
const CHECKS = ["sha256sum --check", "shasum -a 256 -c", "Get-FileHash"];
const read = (file) => fs.readFileSync(file, "utf-8");

const UA = {
  linux: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/130 Safari/537.36",
  linux_arm: "Mozilla/5.0 (X11; Linux aarch64; rv:131.0) Gecko/20100101 Firefox/131.0",
  mac: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Safari/605.1.15",
  windows: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130",
  android: "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/130 Mobile",
  iphone: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15",
  other: "Mozilla/5.0 (X11; FreeBSD amd64; rv:131.0) Gecko/20100101 Firefox/131.0",
};

const CASES = [
  [{ platform: "Linux x86_64", userAgent: UA.linux, architecture: "" }, "linux-amd64", null],
  [{ platform: "Linux aarch64", userAgent: UA.linux_arm, architecture: "" }, "linux-arm64", null],
  [{ platform: "Linux", userAgent: UA.linux, architecture: "arm" }, "linux-arm64", null],
  [{ platform: "MacIntel", userAgent: UA.mac, architecture: "" }, "darwin-arm64", "intel"],
  [{ platform: "macOS", userAgent: UA.mac, architecture: "arm" }, "darwin-arm64", "intel"],
  [{ platform: "macOS", userAgent: UA.mac, architecture: "x86" }, "darwin-amd64", null],
  [{ platform: "Win32", userAgent: UA.windows, architecture: "" }, "windows-amd64", "gitbash"],
  [{ platform: "Windows", userAgent: UA.windows, architecture: "arm" }, "windows-amd64", "gitbash"],
  [{ platform: "Linux armv8l", userAgent: UA.android, architecture: "" }, null, null],
  [{ platform: "iPhone", userAgent: UA.iphone, architecture: "" }, null, null],
  [{ platform: "", userAgent: UA.other, architecture: "" }, null, null],
  [{ platform: "", userAgent: "curl/8.9 (Darwin 24.0)", architecture: "" }, null, null],
  [{ platform: "", userAgent: "", architecture: "" }, null, null],
];

const LABELS = [
  [{ tag_name: "v0.2.0" }, "v0.2.0"],
  [{ tag_name: "v1.10.3-rc.1" }, "v1.10.3-rc.1"],
  [{ message: "API rate limit exceeded" }, "latest release"],
  [{ tag_name: "<b>v1</b>" }, "latest release"],
  [{ tag_name: 2 }, "latest release"],
  [null, "latest release"],
];

/** The command text of the <pre><code> in each <details data-platform="...">, entities decoded. */
function installBlocks() {
  const blocks = {};
  const panel = /<details\b[^>]*\bdata-platform="([^"]+)"[^>]*>([\s\S]*?)<\/details>/g;
  for (const [, platform, body] of read(INSTALL).matchAll(panel)) {
    const code = body.match(/<pre><code>([\s\S]*?)<\/code><\/pre>/);
    assert.ok(code, `${platform} has no <pre><code>`);
    blocks[platform] = decodeEntities(code[1].replace(/<[^>]*>/g, ""));
  }
  return blocks;
}

function builtAssets() {
  const pairs = [...read(BUILD).matchAll(/goos:\s*(\w+)\s*\n\s*goarch:\s*(\w+)/g)];
  assert.ok(pairs.length > 0, "no goos/goarch matrix in build.yml");
  return new Set(pairs.map(([, os, arch]) => `lighthouse_${os}_${arch}` + (os === "windows" ? ".exe" : "")));
}

const lines = (block) => block.split("\n").map((l) => l.trimEnd()).filter((l) => l.trim());

test("choosePlatform picks the install tab and note", () => {
  for (const [info, tab, note] of CASES) {
    assert.deepEqual(choosePlatform(info), { tab, note }, `${info.platform} | ${info.userAgent}`);
  }
});

test("releaseLabel keeps a version tag and falls back to text", () => {
  for (const [data, label] of LABELS) assert.equal(releaseLabel(data), label, JSON.stringify(data));
});

test("site assets are the built assets", () => {
  const files = [INDEX, INSTALL, ...fs.readdirSync(SITE).filter((f) => f.endsWith(".js")).map((f) => path.join(SITE, f))];
  const named = new Set();
  for (const file of files) {
    for (const m of read(file).matchAll(new RegExp(DOWNLOAD.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "([\\w.]+)", "g"))) named.add(m[1]);
  }
  assert.deepEqual([...named].sort(), [...builtAssets(), "SHA256SUMS"].sort());
});

test("every platform has an install block", () => {
  assert.deepEqual(Object.keys(installBlocks()), TABS);
});

test("checksum comes before the binary is used", () => {
  for (const [tab, block] of Object.entries(installBlocks())) {
    const ls = lines(block);
    const check = ls.findIndex((l) => CHECKS.some((c) => l.includes(c)));
    const use = ls.findIndex((l) => ` ${l}`.includes(" mv ") || l.endsWith("version"));
    assert.ok(check >= 0 && use >= 0, `${tab}: no check or no use line`);
    assert.ok(check < use, tab);
    assert.ok(!block.includes("| sh") && !block.includes("| bash"), tab);
  }
});

test("bash blocks stop on the first failure", () => {
  for (const [tab, block] of Object.entries(installBlocks())) {
    const ls = lines(block);
    if (tab === "windows-amd64") {
      // A paste that arrives line by line runs each finished line at once; inside one
      // `& { }` block PowerShell waits for the closing brace, so `throw` stops the rest.
      assert.deepEqual([ls[0].trim(), ls.at(-1).trim()], ["& {", "}"], tab);
      assert.ok(block.includes('$ErrorActionPreference = "Stop"') && block.includes("throw"), tab);
      continue;
    }
    assert.ok(ls.slice(0, -1).every((l) => l.endsWith("&&")), tab);
    assert.equal(ls.at(-1), "~/.local/bin/lighthouse version", tab);
  }
});

test("the linux-amd64 block holds decoded && and no entity", () => {
  const block = installBlocks()["linux-amd64"];
  assert.ok(lines(block).some((l) => l.endsWith("&&")));
  assert.ok(!block.includes("&amp;"));
});

test("windows block works in Windows PowerShell", () => {
  // Windows PowerShell 5.1, the default shell, needs -UseBasicParsing where the IE engine is
  // missing, and crawls through a download while it draws the progress bar.
  const block = installBlocks()["windows-amd64"];
  const calls = block.match(/Invoke-WebRequest[^\n]*/g) ?? [];
  assert.equal(calls.length, 2);
  assert.ok(calls.every((c) => c.includes("-UseBasicParsing")));
  assert.ok(block.includes('$ProgressPreference = "SilentlyContinue"'));
});

test("install page works without JS", () => {
  const text = read(INSTALL);
  assert.deepEqual(text.match(/<[^>]*\bhidden\b[^>]*>/g), ['<p id="platform-note" hidden>']);
  const panels = [...text.matchAll(/<(\w+)([^>]*\bdata-platform=[^>]*)>/g)];
  assert.deepEqual(panels.map((m) => m[1]), TABS.map(() => "details"));
  assert.deepEqual(panels.filter((m) => /\bopen\b/.test(m[2])), []);
  assert.equal(text.split("<summary>").length - 1, TABS.length);
});

test("install page names the other routes", () => {
  const el = elementById(read(INSTALL), "install");
  assert.ok(el, 'install.html has no element with id="install"');
  const text = el.text.split(/\s+/).join(" ");
  assert.ok(text.includes("go install github.com/CorneeldH/lighthouse/cmd/lighthouse@latest"));
  assert.ok(text.includes("clone"));
});

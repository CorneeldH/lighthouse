// The configuration map (docs/site/data/config.json) stays in step with the code: every key that
// `lighthouse config --schema` and internal/usage/config.go accept is on it, with the default and
// the level they have, and every reader it names really names the key.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { isDeepStrictEqual } from "node:util";
import { REPO_BLOB, REPO_TREE, ROOT, lighthouseBin, load } from "./helpers.mjs";
import * as m from "../map.js";

const NO_DEFAULT = Symbol("no default");
// Keys nothing in the repo reads: only the config reader checks them (spec section 2).
const UNREAD = new Set(["paths.history", "codex_usage.vraag_pct_week", "codex_usage.halt_pct_session", "host.name",
  "host.scan_memory_max", "host.scan_cpu_quota"]);
const SET = new Set(["project.language", "forge.kind", "forge.repo", "forge.host", "test_gate.command", "host.name",
  "host.controller", "host.tailscale_ip", "review.roborev", "report.enabled", "report.sink", "report.dir"]);
const REQUIRED = {
  "forge.board.project_number": "with [forge.board]",
  "forge.board.project_id": "with [forge.board]",
  "forge.board.status_field_id": "with [forge.board]",
  "forge.board.status_options": "with [forge.board]",
  "code_language_llm.url": "with [code_language_llm]",
  "code_language_llm.models": "with [code_language_llm]",
  "report.dir": "with sink = file",
  "host.name": "yes",
  "forge.owner": "derived",
  "forge.name": "derived",
};
// The config reader (internal/config/config.go, host.go) holds the key lists; it reads no key for a purpose.
const SKIPPED_READERS = new Set(["internal/config/config.go", "internal/config/host.go"]);
// found_readers is a text match. These two lists correct it, each entry with its reason: a file that
// names a key without reading it, and a file that reads it through the attributes of the loaded
// Config (cfg.language, repo.board.project_id) so no text names it. [key, path, reason].
const NOT_READERS = [
  ["project.language", "pack/python/scripts/quality/ratchet.py",
    "its docstring says messages stay English whatever [project] language says"],
  ["project.language", "pack/core/headings.toml",
    "a comment explains how the table is read; the file reads nothing"],
  ["test_gate.command", "pack/python/requires.toml",
    "the name of a unit in a list of tools, not a read of the command"],
  ["python.source_dirs", "pack/python/scripts/quality/prcov.py",
    "its docstring names the key; the directories arrive as an argument from ratchet.py"],
  ["host.controller", "internal/repoconfig/repoconfig.go",
    "its tags host and controller are [forge] host and [controller] of lighthouse.toml, not host.toml"],
];
// [key, path, attribute]: the attribute that file reads, prcov_issues.py the fields of the
// `forge.board` table it reads whole through `lighthouse config --json` (#274).
const INDIRECT_READERS = [
  ["forge.board.project_id", "pack/python/scripts/quality/prcov_issues.py", ".project_id"],
  ["forge.board.project_number", "pack/python/scripts/quality/prcov_issues.py", ".project_number"],
  ["forge.board.status_field_id", "pack/python/scripts/quality/prcov_issues.py", ".status_field_id"],
  ["forge.board.status_options", "pack/python/scripts/quality/prcov_issues.py", ".status_options"],
];
const LOADS_CONFIG = /config\.load\(|config\.Load\(|\bload\(config_path\(\)\)|lookup\("forge\.board"\)/;
const texts = new Map();
let files = null;

const read = (file) => fs.readFileSync(path.join(ROOT, file), "utf-8");
const has = (list, key, file) => list.some(([k, p]) => k === key && p === file);
const repr = (value) => (typeof value === "string" ? `'${value}'` : value === undefined ? "None" : JSON.stringify(value));
const mutated = () => structuredClone(load("config.json"));

function levelOf(key) {
  if (SET.has(key)) return "set";
  return key.startsWith("forge.board.") ? "optional" : "advanced";
}

/** The [usage] keys of internal/usage/config.go with the default DefaultConfig gives them. */
function usageDefaults(go) {
  const block = go.match(/Usage struct \{(.*?)\} `toml:"usage"`/s);
  assert.ok(block, "no Usage struct in internal/usage/config.go");
  const fields = [...block[1].matchAll(/(\w+)\s+\*int\s+`toml:"(\w+)"`/g)];
  const literal = go.match(/return Config\{(.*?)\}/s);
  assert.ok(fields.length && literal, "no usage fields or DefaultConfig in internal/usage/config.go");
  const values = {};
  for (const [, name, value] of literal[1].matchAll(/(\w+):\s*(\d+)(?:\s*\*\s*time\.Second)?/g)) values[name] = Number(value);
  const aliases = { CacheTTLSeconds: "TTL" };
  return Object.fromEntries(fields.map(([, name, tag]) => [`usage.${tag}`, values[aliases[name] ?? name]]));
}

let schemaCache = null;
const schema = () =>
  (schemaCache ??= JSON.parse(execFileSync(lighthouseBin(), ["config", "--schema"], { cwd: ROOT, encoding: "utf-8" })));

/** key -> default (or NO_DEFAULT) for every key on the map. */
function expectedKeys() {
  const usage = usageDefaults(read("internal/usage/config.go"));
  const s = schema();
  const keys = new Set([...Object.keys(s.schema), ...Object.keys(s.defaults), ...s.base_keys, ...s.host_keys,
    ...Object.keys(usage), ...s.option_prefixes.map((p) => p.replace(/\.+$/, ""))]);
  const found = new Map();
  for (const key of keys) {
    if (key in usage) found.set(key, usage[key]);
    else if (key === "project.language") found.set(key, "en");
    else if (key in s.defaults) found.set(key, s.defaults[key]);
    else found.set(key, NO_DEFAULT);
  }
  return found;
}

function readerNamesKey(text, key, file) {
  const i = key.lastIndexOf(".");
  const [table, name] = [key.slice(0, i), key.slice(i + 1)];
  if (text.includes(key) || text.includes(`[${table}] ${name}`)) return true;
  if (file.endsWith(".go")) return text.includes(`toml:"${name}"`) && text.includes(`toml:"${table.split(".").pop()}"`);
  if (file.endsWith(".py")) return text.includes(`f"${table}.{`) && text.includes(`"${name}"`);
  return false;
}

function repoFiles() {
  files ??= execFileSync("git", ["ls-files", "pack", "internal", "cmd"], { cwd: ROOT, encoding: "utf-8" })
    .split("\n")
    .filter((p) => p && !SKIPPED_READERS.has(p) && !p.endsWith("_test.go") && !p.includes("testdata/"));
  return files;
}

function readerUnit(file) {
  const found = file.match(/^(pack\/[^/]+\/skills\/[^/]+)\//);
  return found ? found[1] : file;
}

function foundReaders(key) {
  const found = new Set();
  for (const file of repoFiles()) {
    if (!texts.has(file)) texts.set(file, fs.readFileSync(path.join(ROOT, file), "utf-8"));
    if (readerNamesKey(texts.get(file), key, file)) found.add(readerUnit(file));
  }
  return [...found].sort();
}

/** What the map must list: the text match, less the not-readers, plus the indirect readers. */
function expectedReaders(key, notReaders, indirectReaders) {
  const found = new Set(foundReaders(key).filter((p) => !has(notReaders, key, p)));
  for (const [k, p] of indirectReaders) if (k === key) found.add(p);
  return [...found].sort();
}

/** An entry of the not-readers or the indirect readers that no longer holds. */
function exceptionProblems(notReaders, indirectReaders) {
  const problems = [];
  for (const [key, file] of notReaders)
    if (!fs.existsSync(path.join(ROOT, file)) || !foundReaders(key).includes(file))
      problems.push(`${key}: NOT_READERS ${file} no longer names it`);
  for (const [key, file, attribute] of indirectReaders) {
    if (!fs.existsSync(path.join(ROOT, file))) {
      problems.push(`${key}: INDIRECT_READERS ${file} does not exist`);
      continue;
    }
    const text = read(file);
    if (foundReaders(key).includes(file)) problems.push(`${key}: INDIRECT_READERS ${file} names it, so it needs no entry`);
    else if (!LOADS_CONFIG.test(text) || !text.includes(attribute))
      problems.push(`${key}: INDIRECT_READERS ${file} no longer loads the config or uses ${attribute}`);
  }
  return problems;
}

const allKeys = (data) => data.files.flatMap((f) => f.tables.flatMap((t) => t.keys));

function readerProblems(key, readers, exceptions) {
  const problems = [];
  if (!readers.length && !UNREAD.has(key)) problems.push(`${key}: no readers`);
  const found = expectedReaders(key, exceptions.notReaders, exceptions.indirectReaders);
  for (const file of readers) {
    if (!/^(pack|internal|cmd)\//.test(file)) problems.push(`${key}: reader ${file} outside pack/, internal/ or cmd/`);
    else if (!fs.existsSync(path.join(ROOT, file))) problems.push(`${key}: reader ${file} does not exist`);
    else if (!found.includes(file)) problems.push(`${key}: reader ${file} does not name it`);
  }
  for (const file of found) if (!readers.includes(file)) problems.push(`${key}: reader ${file} missing from the map`);
  return problems;
}

/** Every difference between the map `data` and the code, sorted. `exceptions` is the correction
 * of the text match; a row of the table test passes its own copy. */
function configProblems(data, exceptions = { notReaders: NOT_READERS, indirectReaders: INDIRECT_READERS }) {
  const expected = expectedKeys();
  const problems = exceptionProblems(exceptions.notReaders, exceptions.indirectReaders);
  const onMap = new Map(allKeys(data).map((k) => [k.key, k]));
  for (const key of expected.keys()) if (!onMap.has(key)) problems.push(`not on the map: ${key}`);
  for (const key of onMap.keys()) if (!expected.has(key)) problems.push(`not in the lighthouse config reader: ${key}`);
  for (const [key, entry] of onMap) {
    if (!expected.has(key)) continue;
    const want = expected.get(key);
    if (want === NO_DEFAULT) {
      if ("default" in entry) problems.push(`${key}: default on the map, none in the lighthouse config reader`);
    } else if (!isDeepStrictEqual(entry.default, want)) {
      problems.push(`${key}: default ${repr(entry.default)}, the lighthouse config reader says ${repr(want)}`);
    }
    if (entry.level !== levelOf(key)) problems.push(`${key}: level ${entry.level}, expected ${levelOf(key)}`);
    const required = REQUIRED[key] ?? "no";
    if (entry.required !== required) problems.push(`${key}: required ${entry.required}, expected ${required}`);
    problems.push(...readerProblems(key, entry.readers, exceptions));
  }
  return problems.sort();
}

const entryOf = (data, key) => allKeys(data).find((k) => k.key === key);

test("every key is on the map", () => {
  assert.deepEqual(configProblems(load("config.json")), []);
});

test("usage defaults come from go", () => {
  assert.deepEqual(usageDefaults(read("internal/usage/config.go")), {
    "usage.halt_pct_session": 85,
    "usage.halt_pct_week": 95,
    "usage.cache_ttl_seconds": 300,
  });
});

test("reader names key accepts the table notation", () => {
  assert.ok(readerNamesKey("run `[test_gate] command`", "test_gate.command", "a/SKILL.md"));
  assert.ok(!readerNamesKey("test_gate.slots", "test_gate.command", "a/SKILL.md"));
});

test("reader names key needs the table tag in go", () => {
  const go = read("internal/usage/config.go");
  assert.ok(readerNamesKey(go, "usage.halt_pct_session", "internal/usage/config.go"));
  assert.ok(!readerNamesKey(go, "codex_usage.halt_pct_session", "internal/usage/config.go"));
});

test("reader names key finds a dynamic python reader", () => {
  const text = 'for name in ("review_pct_week",):\n    value = config.lookup(cfg, f"codex_usage.{name}")\n';
  assert.ok(readerNamesKey(text, "codex_usage.review_pct_week", "a/reader.py"));
  assert.ok(!readerNamesKey(text, "codex_usage.vraag_pct_week", "a/reader.py"));
});

test("found readers skips go tests", () => {
  assert.deepEqual(foundReaders("host.name"), []); // only internal/initcmd/initcmd_test.go names it
});

test("unread keys have no reader", () => {
  // A not-reader names the key without reading it, here as on the map.
  const readers = {};
  const none = {};
  for (const key of UNREAD) {
    readers[key] = foundReaders(key).filter((p) => !has(NOT_READERS, key, p));
    none[key] = [];
  }
  assert.deepEqual(readers, none);
});

test("no controller options", () => {
  const raw = read("docs/site/data/config.json");
  const keys = allKeys(load("config.json")).map((k) => k.key);
  assert.deepEqual(keys.filter((k) => k.startsWith("controller.") || k.startsWith("dispatch.")), []);
  assert.ok(!raw.includes("Controller"));
});

// Each row mutates a copy of the map (or hands the checker its own copy of the exceptions) and the
// checker must say exactly that; without these a broken checker would stay green for ever.
const TEST_GATE_READERS = [
  "internal/commitgate/gate.go",
  "internal/commitgate/message.go",
  "internal/initcmd/gatecommand.go",
  "pack/core/instructions/AGENTS.md",
  "pack/core/skills/fix-pr",
  "pack/core/skills/upgrade",
  "pack/core/skills/write-pr",
  "pack/go/skills/go-standard",
  "pack/python/skills/python-standard",
];
const PROBLEM_ROWS = [
  {
    name: "a removed key",
    mutate(data) {
      for (const f of data.files) for (const t of f.tables) t.keys = t.keys.filter((k) => k.key !== "test_gate.slots");
    },
    expected: ["not on the map: test_gate.slots"],
  },
  {
    name: "a dispatch key",
    mutate(data) {
      data.files[1].tables[0].keys.push({ key: "dispatch.slots", level: "advanced", required: "no", about: "x", readers: [] });
    },
    expected: ["not in the lighthouse config reader: dispatch.slots"],
  },
  {
    name: "another default",
    mutate(data) {
      entryOf(data, "labels.needs_hands").default = "x";
    },
    expected: ["labels.needs_hands: default 'x', the lighthouse config reader says 'needs-hands'"],
  },
  {
    name: "a default without one",
    mutate(data) {
      entryOf(data, "paths.tenets").default = "x";
    },
    expected: ["paths.tenets: default on the map, none in the lighthouse config reader"],
  },
  {
    name: "a lowered level",
    mutate(data) {
      entryOf(data, "forge.repo").level = "advanced";
    },
    expected: ["forge.repo: level advanced, expected set"],
  },
  {
    name: "a reader that does not name it",
    mutate(data) {
      entryOf(data, "shell.strict_dirs").readers.push("pack/core/skills/close-issue");
    },
    expected: ["shell.strict_dirs: reader pack/core/skills/close-issue does not name it"],
  },
  {
    name: "a missing reader",
    mutate(data) {
      const readers = entryOf(data, "test_gate.command").readers;
      readers.splice(readers.indexOf("internal/commitgate/gate.go"), 1);
    },
    expected: ["test_gate.command: reader internal/commitgate/gate.go missing from the map"],
  },
  {
    name: "an empty reader list",
    mutate(data) {
      entryOf(data, "test_gate.command").readers = [];
    },
    expected: [
      "test_gate.command: no readers",
      ...TEST_GATE_READERS.map((p) => `test_gate.command: reader ${p} missing from the map`),
    ].sort(),
  },
  {
    name: "an indirect reader left off the map",
    mutate(data) {
      const readers = entryOf(data, "forge.board.project_id").readers;
      readers.splice(readers.indexOf("pack/python/scripts/quality/prcov_issues.py"), 1);
    },
    expected: ["forge.board.project_id: reader pack/python/scripts/quality/prcov_issues.py missing from the map"],
  },
  {
    name: "a not-reader that is dropped from the exceptions",
    exceptions: {
      notReaders: NOT_READERS.filter(([k, p]) => !(k === "project.language" && p === "pack/python/scripts/quality/ratchet.py")),
      indirectReaders: INDIRECT_READERS,
    },
    expected: ["project.language: reader pack/python/scripts/quality/ratchet.py missing from the map"],
  },
  {
    name: "a stale not-readers entry",
    exceptions: {
      notReaders: [...NOT_READERS, ["shell.strict_dirs", "pack/core/skills/close-issue", "x"]],
      indirectReaders: INDIRECT_READERS,
    },
    expected: ["shell.strict_dirs: NOT_READERS pack/core/skills/close-issue no longer names it"],
  },
  {
    name: "an indirect entry of a file without the config",
    exceptions: {
      notReaders: NOT_READERS,
      indirectReaders: [...INDIRECT_READERS, ["forge.owner", "pack/core/hooks/run.sh", ".owner"]],
    },
    contains: "forge.owner: INDIRECT_READERS pack/core/hooks/run.sh no longer loads the config or uses .owner",
  },
  {
    name: "a dogfood reader",
    mutate(data) {
      entryOf(data, "test_gate.command").readers.push(".claude/skills/lighthouse-start-issue");
    },
    expected: ["test_gate.command: reader .claude/skills/lighthouse-start-issue outside pack/, internal/ or cmd/"],
  },
  {
    name: "a list default as equal",
    mutate(data) {
      assert.deepEqual(entryOf(data, "paths.search_dirs").default, ["."]);
    },
    expected: [],
  },
];

test("config problems flags each mutation of the map or the exceptions", () => {
  for (const row of PROBLEM_ROWS) {
    const data = mutated();
    row.mutate?.(data);
    const problems = configProblems(data, row.exceptions);
    if (row.contains) assert.ok(problems.includes(row.contains), `${row.name}: ${problems.join("; ")}`);
    else assert.deepEqual(problems, row.expected, row.name);
  }
});

test("config reader marks the sort", () => {
  const packs = load("packs.json");
  assert.equal(m.configReader("pack/go/skills/go-standard", packs).sort, "standard-skill");
  assert.deepEqual(m.configReader("pack/core/skills/write-issue", packs), {
    name: "write-issue",
    sort: "workflow-skill",
    href: REPO_TREE + "pack/core/skills/write-issue",
  });
  assert.equal(m.configReader("pack/core/hooks/pr-create-gate.sh", packs).sort, "hook");
  assert.equal(m.configReader("pack/core/workflows/unblock-dependents.yml", packs).sort, "github");
  assert.deepEqual(m.configReader("internal/usage/config.go", packs), {
    name: "config.go",
    sort: "program",
    href: REPO_BLOB + "internal/usage/config.go",
  });
});

test("every reader on the map gets a link", () => {
  const packs = load("packs.json");
  const paths = [...new Set(allKeys(load("config.json")).flatMap((k) => k.readers))].sort();
  assert.ok(paths.length > 0);
  for (const p of paths) {
    const href = m.configReader(p, packs).href;
    assert.ok(href.startsWith(REPO_BLOB) || href.startsWith(REPO_TREE), `${p}: ${href}`);
  }
});

function configEntries() {
  return Object.fromEntries(m.configEntries(load("config.json"), load("packs.json")).map((e) => [e.key, e]));
}

test("config entries cover every key with its label", () => {
  const entries = configEntries();
  assert.deepEqual(new Set(Object.keys(entries)), new Set(allKeys(load("config.json")).map((k) => k.key)));
  assert.equal(entries["forge.repo"].levelLabel, "Set this");
  assert.equal(entries["forge.board.project_id"].levelLabel, "Optional");
  assert.equal(entries["usage_guard.controller"].levelLabel, "Advanced");
});

test("config entries say whether anything acts on the key", () => {
  const entries = configEntries();
  assert.equal(entries["test_gate.command"].status, "Status: works");
  assert.equal(
    entries["paths.history"].status,
    "Status: not used yet — the lighthouse binary accepts it, nothing acts on it.",
  );
});

test("config entries give the default as toml", () => {
  const entries = configEntries();
  assert.equal(entries["paths.search_dirs"].default, '["."]');
  assert.equal(entries["usage_guard.resume"].default, "false"); // lighthouse config: false
  assert.equal(entries["paths.tenets"].default, null);
  assert.deepEqual(entries["paths.history"].readers, []);
});

/** The lines the page shows for file `name`: [{text, key?, note?}]. */
function tomlLines(name) {
  return m.configToml(load("config.json").files.find((f) => f.file === name));
}

/** The TOML text the page shows for file `name`, and the keys it links. */
function configToml(name) {
  const lines = tomlLines(name);
  return [lines.map((l) => l.text).join("\n") + "\n", lines.filter((l) => "key" in l).map((l) => l.key)];
}

/** `text` with every commented-out key line switched on. */
const uncommented = (text) => text.replace(/^# ([a-z_]+ = )/gm, "$1");

/** The value of a `key = value` line without its trailing comment: the first `#` outside a string
 * that follows whitespace starts the comment. */
function stripComment(value) {
  let quoted = false;
  for (let i = 0; i < value.length; i++) {
    const c = value[i];
    if (c === "\\" && quoted) i += 1;
    else if (c === '"') quoted = !quoted;
    else if (c === "#" && !quoted && i > 0 && /\s/.test(value[i - 1])) return value.slice(0, i).trimEnd();
  }
  return value;
}

/** Parses the TOML subset configToml writes: `[a.b]` headers, `#` comments, blank lines and
 * `key = <JSON value>`. Any other line is an error. */
function parseToml(text) {
  const root = {};
  let node = root;
  for (const raw of text.split("\n")) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    const header = line.match(/^\[([A-Za-z0-9_.-]+)\]$/);
    if (header) {
      node = root;
      for (const part of header[1].split(".")) node = node[part] ??= {};
      continue;
    }
    const setting = line.match(/^([A-Za-z0-9_-]+) = (.*)$/);
    assert.ok(setting, `not a header, comment or key line: ${raw}`);
    node[setting[1]] = JSON.parse(stripComment(setting[2]));
  }
  return root;
}

test("toml block sets exactly the defaults", () => {
  for (const f of load("config.json").files) {
    const parsed = parseToml(configToml(f.file)[0]);
    const expected = {};
    for (const t of f.tables) {
      let node = expected;
      for (const part of t.table.split(".")) node = node[part] ??= {}; // a table of commented-out keys is still a header
      for (const k of t.keys) if ("default" in k) node[k.key.slice(t.table.length + 1)] = k.default;
    }
    assert.deepEqual(parsed, expected, f.file);
  }
});

test("toml block links every key but the derived ones", () => {
  for (const f of load("config.json").files) {
    const keys = configToml(f.file)[1];
    const want = f.tables.flatMap((t) => t.keys.filter((k) => k.required !== "derived").map((k) => k.key));
    assert.deepEqual(keys, want, f.file);
  }
});

function readKey(key, env) {
  return execFileSync(lighthouseBin(), ["config", key], {
    cwd: ROOT,
    encoding: "utf-8",
    env: { ...process.env, ...env },
  });
}

// The [forge.board] table up to the next table. Its example holds the ids of a GitHub board and
// `...` placeholders, which the reader refuses with kind gitlab.
const BOARD = /^\[forge\.board\]\n(?:(?!\[).*\n)*/m;

test("toml examples pass the config reader", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "config-toml-"));
  try {
    const text = uncommented(configToml("lighthouse.toml")[0]);
    // The published example is `kind = "github"`; `host` is valid only with gitlab.
    const github = path.join(dir, "github.toml");
    fs.writeFileSync(github, text.replace(/^host = .*\n/gm, ""));
    assert.equal(readKey("forge.board.project_number", { LIGHTHOUSE_TOML: github }), "1\n");
    const gitlab = path.join(dir, "gitlab.toml");
    const gitlabText = text.replace('kind = "github"', 'kind = "gitlab"').replace(BOARD, "");
    assert.notEqual(gitlabText, text);
    fs.writeFileSync(gitlab, gitlabText);
    assert.equal(readKey("forge.host", { LIGHTHOUSE_TOML: gitlab }), "git.example.org\n");
    assert.equal(readKey("forge.repo", { LIGHTHOUSE_TOML: gitlab }), "owner/name\n");
    assert.equal(readKey("code_language_llm.models", { LIGHTHOUSE_TOML: gitlab }), "model-a\nmodel-b\n");
    const host = path.join(dir, "host.toml");
    fs.writeFileSync(host, uncommented(configToml("host.toml")[0]));
    assert.equal(readKey("host.gate_slice", { LIGHTHOUSE_HOST_TOML: host }), "lighthouse-gate.slice\n");
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("options are the values lighthouse accepts", () => {
  // A key with a fixed set of values lists exactly that set; the lighthouse binary rejects every other.
  const s = schema();
  const expected = {
    "project.language": s.languages,
    "project.code_language": ["en"],
    "forge.kind": ["github", "gitlab"],
    "review.roborev": ["local", "off"],
    "report.sink": ["stdout", "file"],
    ...Object.fromEntries(Object.entries(s.schema).filter(([, kind]) => kind === "bool").map(([key]) => [key, [true, false]])),
  };
  const onMap = Object.fromEntries(allKeys(load("config.json")).filter((k) => "options" in k).map((k) => [k.key, k.options]));
  assert.deepEqual(onMap, expected);
});

test("config entries mark the default option", () => {
  const entries = configEntries();
  assert.deepEqual(entries["project.language"].options, [
    { value: "nl", isDefault: false },
    { value: "en", isDefault: true },
  ]);
  assert.deepEqual(entries["usage_guard.resume"].options, [
    { value: "true", isDefault: false },
    { value: "false", isDefault: true },
  ]);
  assert.equal(entries["paths.specs"].options, null);
  assert.equal(entries["paths.tenets"].example, '"docs/tenets.md"');
});

test("toml puts what a key does behind it in one column", () => {
  for (const f of load("config.json").files) {
    const about = Object.fromEntries(f.tables.flatMap((t) => t.keys.map((k) => [k.key, k.about])));
    const lines = tomlLines(f.file);
    lines.forEach((line, index) => {
      if (!("key" in line)) return;
      const comment = line.text.slice(line.note) || lines[index + 1].text;
      assert.ok(comment.trim().startsWith("# " + about[line.key].split(/\s+/)[0]), line.text);
    });
    // One column per table: every comment that does not open a line starts in the same place.
    for (const table of lines.map((l) => l.text).join("\n").split("\n\n")) {
      const columns = new Set();
      for (const text of table.split("\n")) {
        if (text.startsWith(" ")) columns.add(text.length - text.trimStart().length);
        if (text.includes("  # ")) columns.add(text.indexOf("  # ") + 2);
      }
      assert.ok(columns.size <= 1, table);
    }
  }
});

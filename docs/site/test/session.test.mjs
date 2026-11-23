// The session map stays in step with pack/*/hooks/hooks.json: every hook entry is drawn on its
// event with its matcher and command, and nothing is drawn that hooks.json does not register.
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { REPO_BLOB, ROOT, load } from "./helpers.mjs";

// The column of the session map (docs/site/data/session.json) that each hook event belongs in.
const EVENT_PHASE = {
  // A session starts before the first prompt, so level-check sits in the first column.
  SessionStart: "prompt",
  UserPromptSubmit: "prompt",
  PreToolUse: "pre-tool",
  // A permission prompt comes before the tool runs.
  Notification: "pre-tool",
  PostToolUse: "post-tool",
  PostToolUseFailure: "post-tool",
  SubagentStop: "subagent-stop",
  Stop: "stop",
};
const HOOKS_DIR = "${CLAUDE_PLUGIN_ROOT}/hooks/";
const PACK = path.join(ROOT, "pack");
// A Go hook whose source is a package of its own, not internal/hooks/<name>.go.
const GO_HOOK_SOURCES = { "code-language-check": "internal/codelang/hook.go" };

// An entry is [pack, event, matcher, command]; sets of entries hold their JSON.
const key = JSON.stringify;
const chipOf = (data, id) => data.chips.find((c) => c.id === id);

/** [pack, event, matcher, command] of every hook in packRoot/*\/hooks/hooks.json, as a set of
 * JSON strings. A missing matcher counts as the empty one. */
function hookEntries(packRoot) {
  const found = new Set();
  for (const pack of fs.readdirSync(packRoot).sort()) {
    const file = path.join(packRoot, pack, "hooks", "hooks.json");
    if (!fs.existsSync(file)) continue;
    for (const [event, groups] of Object.entries(JSON.parse(fs.readFileSync(file, "utf-8")).hooks))
      for (const group of groups)
        for (const hook of group.hooks) {
          const command = hook.command.startsWith(HOOKS_DIR) ? hook.command.slice(HOOKS_DIR.length) : hook.command;
          found.add(key([pack, event, group.matcher ?? "", command]));
        }
  }
  return found;
}

/** The pack named by a hook chip's source: pack/<pack>/hooks/<script>. A hook in Go (internal/)
 * runs as `run.sh <name>` from pack/core, so it counts as core. */
function sourcePack(chip) {
  const rel = chip.source.slice(REPO_BLOB.length);
  return rel.startsWith("internal/") ? "core" : rel.split("/")[1];
}

/** Where a hook chip's source must end: internal/hooks/<name without dashes>.go for
 * `run.sh <name>` (or its entry in GO_HOOK_SOURCES), <pack>/hooks/<script> for a script. */
function expectedSource(chip) {
  const [script, ...args] = chip.command.split(/\s+/);
  if (script === "run.sh" && args.length > 0)
    return GO_HOOK_SOURCES[args[0]] ?? `internal/hooks/${args[0].replaceAll("-", "")}.go`;
  return `${sourcePack(chip)}/hooks/${script}`;
}

/** One entry per event of every chip that has a command. */
function drawnEntries(data) {
  const found = new Set();
  for (const c of data.chips)
    if ("command" in c) for (const event of c.events ?? []) found.add(key([sourcePack(c), event, c.matcher ?? "", c.command]));
  return found;
}

const describe = (entry) => {
  const [pack, event, matcher, command] = JSON.parse(entry);
  return `${pack} ${event} ${matcher || "all"} ${command}`;
};

function hookProblems(data, packRoot) {
  const real = hookEntries(packRoot);
  const drawn = drawnEntries(data);
  const problems = [
    ...[...real].filter((e) => !drawn.has(e)).map((e) => `not on the map: ${describe(e)}`),
    ...[...drawn].filter((e) => !real.has(e)).map((e) => `not in hooks.json: ${describe(e)}`),
  ];
  for (const c of data.chips) {
    if (!("command" in c)) continue;
    if (!c.events?.length || !c.effects?.length) {
      problems.push(`${c.id}: hook chip lacks events or effects`);
      continue;
    }
    for (const event of c.events)
      if (c.phase !== EVENT_PHASE[event])
        problems.push(`${c.id}: ${event} belongs in column ${EVENT_PHASE[event]}, not ${c.phase}`);
    const expected = expectedSource(c);
    if (!c.source.endsWith(expected)) problems.push(`${c.id}: source does not match ${expected}`);
  }
  return problems.sort();
}

test("hookEntries reads every pack", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "site-hooks-"));
  try {
    for (const [pack, matcher] of [["a", undefined], ["b", ""]]) {
      const group = { hooks: [{ type: "command", command: "${CLAUDE_PLUGIN_ROOT}/hooks/x.sh run" }] };
      if (matcher !== undefined) group.matcher = matcher;
      fs.mkdirSync(path.join(dir, pack, "hooks"), { recursive: true });
      fs.writeFileSync(path.join(dir, pack, "hooks", "hooks.json"), JSON.stringify({ hooks: { Stop: [group] } }));
    }
    assert.deepEqual([...hookEntries(dir)].sort(), [key(["a", "Stop", "", "x.sh run"]), key(["b", "Stop", "", "x.sh run"])]);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("every hook is on its event", () => {
  assert.deepEqual(hookProblems(load("session.json"), PACK), []);
});

test("hookProblems flags a removed hook, matcher, column, event, source and effects", () => {
  const cases = [
    {
      naam: "removed hook",
      mutatie: (d) => (d.chips = d.chips.filter((c) => c.id !== "main-commit-gate")),
      verwacht: ["not on the map: core PreToolUse Bash run.sh main-commit-gate"],
    },
    {
      naam: "another matcher",
      mutatie: (d) => (chipOf(d, "worktree-check").matcher = "Bash"),
      verwacht: [
        "not in hooks.json: core PreToolUse Bash run.sh worktree-check",
        "not on the map: core PreToolUse Edit|Write run.sh worktree-check",
      ],
    },
    {
      naam: "another column",
      mutatie: (d) => (chipOf(d, "go-lint-gate").phase = "subagent-stop"),
      verwacht: ["go-lint-gate: Stop belongs in column stop, not subagent-stop"],
    },
    {
      naam: "dropped event",
      mutatie: (d) => (chipOf(d, "commit-test-gate-post").events = ["PostToolUse"]),
      verwacht: ["not on the map: core PostToolUseFailure Bash run.sh commit-test-gate post"],
    },
    {
      naam: "source of another script",
      mutatie: (d) => (chipOf(d, "gofmt-check").source = chipOf(d, "go-lint-gate").source),
      verwacht: ["gofmt-check: source does not match go/hooks/gofmt-check.sh"],
    },
    {
      naam: "Go hook with the source of another",
      mutatie: (d) => (chipOf(d, "md-link-check").source = chipOf(d, "permission-check").source),
      verwacht: ["md-link-check: source does not match internal/hooks/mdlinkcheck.go"],
    },
    {
      naam: "hook chip without effects",
      mutatie: (d) => delete chipOf(d, "gofmt-check").effects,
      verwacht: ["gofmt-check: hook chip lacks events or effects"],
    },
  ];
  for (const { naam, mutatie, verwacht } of cases) {
    const data = structuredClone(load("session.json"));
    mutatie(data);
    assert.deepEqual(hookProblems(data, PACK), verwacht, naam);
  }
});

test("routes light up the most hooks first", () => {
  const sizes = load("session.json").routes.map((r) => r.chips.length);
  assert.deepEqual(sizes, [...sizes].sort((a, b) => b - a));
});

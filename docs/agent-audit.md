# Agent audit

`lighthouse agent-audit` lists all agentic configuration around a repository — hooks,
permission rules, skills, commands, agents, rules and MCP servers — with the source of each, and
reports where those sources conflict or where a hook of another source does what a lighthouse hook
already does. It writes nothing.

```text
lighthouse agent-audit [--repo-only] [--only <part>]... [--strict]
```

The skill [`/lighthouse-audit-agent-config`](../pack/core/skills/audit-agent-config/SKILL.md)
runs the command, reads the script of each overlap candidate and judges it: `overlap`,
`geen overlap` or `twijfel`, with one concrete step per overlap. The command measures; the skill
judges what only a reading of the script can tell.

The instruction files themselves (`AGENTS.md`, `CLAUDE.md`, skill descriptions) are checked by
`lighthouse check-instructions`, not here.

## When it runs

- After the first `lighthouse init` (the repository had no `.lighthouse/manifest.toml` yet),
  once `init` has written: a summary with the sources, the conflicts, the overlap candidates and
  the five largest posts of the surface, ending with
  `Draai /lighthouse-audit-agent-config voor een oordeel over de kandidaten.`
- With `lighthouse init --upgrade --audit`: the same summary. Without `--audit` an upgrade does
  not run it.
- On its own: `lighthouse agent-audit`, the full output.

Inside `init` the audit never changes the exit code of `init`. When it cannot measure, `init`
writes `agent-audit: COULD NOT CHECK — <reason>` to stderr and goes on. When `init` wrote nothing,
the audit does not run.

## Sources

| Source | What the audit reads |
|---|---|
| `lighthouse` | the units in `.lighthouse/manifest.toml`; the hooks under `.claude/hooks/lighthouse/`; the permission rules a lighthouse package installs |
| `project` | `.claude/settings.json`, `.claude/skills/`, `.claude/agents/`, `.claude/rules/`, `.claude/hooks/`, `.mcp.json`, `.agents/skills/`, as far as they are not lighthouse's |
| `lokaal` | `.claude/settings.local.json`, and the `mcpServers` of this repository under `projects` in `~/.claude.json` |
| `gebruiker` | `~/.claude/settings.json`, `~/.claude/skills/`, `~/.claude/agents/`, `~/.claude/rules/`, the top-level `mcpServers` in `~/.claude.json` |
| `plugin:<name>` | every enabled plugin, at its `installPath` from `~/.claude/plugins/installed_plugins.json`: `skills/`, `commands/`, `agents/`, `hooks/hooks.json`, `.mcp.json` |

`~/.claude` is `$CLAUDE_CONFIG_DIR` when that is set. A plugin is enabled when the source with the
highest precedence that names it in `enabledPlugins` says `true`: `lokaal` before `project`,
`project` before `gebruiker`. A plugin with scope `project` or `local` counts when its
`projectPath` is the worktree root or the main checkout, so the audit gives the same answer from a
worktree under the worktree root (`.claude/worktrees/` by default, `[worktree] root`).

Each source gets one status line under `Bronnen`: `ok`, `afwezig` (does not exist, nothing to
report), `overgeslagen (--repo-only)`, or `COULD NOT CHECK — <reason>` (exists but unreadable or
invalid JSON). The fixed line `managed` is always `COULD NOT CHECK`: managed settings, set by an
organisation, are not read. A source that could not be checked is never reported as clean.

## Output

Text, in this order. `Bronnen` is always there, also with `--only`.

1. **`Bronnen`**: a status per source.
2. **`Conflicten`**: each with a stable key in brackets:
   - `blocking-pair:<event>:<matcher>:<source>/<path>|<source>/<path>`: two blocking hooks on the
     same event and matcher, neither of them lighthouse's.
   - `permission:<rule>`: an `allow` in one source against a `deny` or `ask` in another, on the
     same rule or one covering the other (`Bash(git push:*)` covers `Bash(git push origin main)`).
   - `missing-script:<source>/<command>`: a registered hook whose script does not exist; for
     `run.sh <name>` also a `run.sh` that is missing or a hook name the binary does not know.
   - `unregistered-script:<source>/<path>`: a script under `.claude/hooks/` or `~/.claude/hooks/`
     that no hook command names and no registered script mentions, also not without its
     extension at the end of a path (`require('./helper')`). A script has a script extension
     (`.sh`, `.js`, `.py`, …) or is executable; dot-directories, `node_modules` and
     `__pycache__` are skipped.
3. **`Overlap, kandidaten`**: `overlap:<guard>|<source>/<path>`, a hook of another source on the
   same event and tool as a lighthouse hook whose script matches the terms of that hook's
   signature. Each candidate shows what the lighthouse hook does and the terms that matched. A
   candidate is a suspicion, not a verdict: that is the skill's work.
4. **`Oppervlak`**: per source the number of skills, commands and agents, the characters of their
   `name` plus `description`, and `~tokens` (characters / 4, rounded up), largest first; below it
   the largest posts, and every description longer than `description_max_chars`.
5. **`Inventaris`**: hooks, skills, commands and agents, rules, permission rules, MCP servers and
   hook scripts, each with its source.

`--only inventory|conflicts|overlap|surface` shows only that part, and repeats. `--repo-only`
reads only the repository: `gebruiker` and every plugin show as `overgeslagen (--repo-only)`.

## Configuration

In `lighthouse.toml`:

```toml
[agent_audit]
ignore = []                   # keys of findings, such as "overlap:main-commit-gate|project/.claude/hooks/x.sh"
top = 10                      # number of largest posts in the surface
description_max_chars = 1024  # a longer description is reported in the surface
```

A key in `ignore` hides that finding. A key that no longer matches anything gives one line
`ignore: <key> komt niet meer voor`, but only when every source the key names was measured
(`ok`) this run: on another machine, or with `--repo-only`, the key is not stale, just not
visible.

## Exit codes

| Exit | When |
|---|---|
| 0 | measured; without `--strict` also with findings or a source that could not be checked |
| 1 | `--strict` and a shown conflict or overlap candidate that is not in `ignore` |
| 2 | the audit as a whole could not measure (no repository root, a broken `lighthouse.toml`), with `agent-audit: COULD NOT CHECK — <reason>` on stderr; `--strict` and a source or hook that could not be checked, `managed` excepted; or wrong usage |

With `--only` and `--strict`, only a finding in a shown part counts for exit 1.

---
name: lighthouse-audit-agent-config
description: 'Audits the agentic configuration of this repo, its user and its plugins with `lighthouse agent-audit`, and judges each project hook that may overlap a lighthouse hook. Changes nothing. NOTE — for the instruction files, use `lighthouse check-instructions`.'
metadata:
  lighthouse: "core@dev"
  binding: default
  origin: own
---

# Audit Agent Config

From the output of `lighthouse agent-audit` to one verdict per overlap candidate and one step per
real overlap. The binary measures; this skill reads the scripts and judges what the binary cannot:
whether a project hook does the same as a lighthouse hook.

## Domain

In scope: the agentic configuration around the repo, from every source the command reads
(`lighthouse`, `project`, `lokaal`, `gebruiker`, `plugin:<name>`): hooks, permission rules,
skills, agents, rules and MCP servers.

Not here: the instruction files (`AGENTS.md`, `CLAUDE.md`, the descriptions of the lighthouse
skills): that is `lighthouse check-instructions`. Worktrees and branches: `/lighthouse-audit-worktrees`.

## Hard rule

**This skill changes nothing.** No hook removed, no settings file edited, no `ignore` entry
written to `lighthouse.toml`. It reports and proposes; the human applies.

## Step 1 — Run the audit

```bash
lighthouse agent-audit
```

Exit 0 is measured, also with findings. Exit 2 with `agent-audit: COULD NOT CHECK — <reason>` on
stderr: report that line and stop. A source with `COULD NOT CHECK` under `Bronnen` was not
measured; name it in the report, never read it as clean. `managed` is always `COULD NOT CHECK`:
managed settings are not read.

## Step 2 — Judge every overlap candidate

Under `Overlap, kandidaten` each candidate has a key `overlap:<guard>|<source>/<path>`, the hook
of the other source, the `lighthouse:` line with what the lighthouse hook does, and the terms it
matched. Read the script at `<path>` (`~` is the home directory). A candidate on an inline
command has the command line as its content.

Per candidate one verdict, with one sentence why:

- `overlap`: the script guards the same thing as the lighthouse hook, for the same tool call.
- `geen overlap`: the terms match by accident; the script guards something else.
- `twijfel`: the script does part of it, or reads something you cannot see. Say what is missing.

## Step 3 — Report

Report in `[project] language`, in this order:

1. `Bronnen`: every source that is not `ok`, with its status.
2. `Conflicten`: every key and line as the binary gave them, unchanged.
3. Per candidate: key, verdict, the one sentence.
4. Per `overlap` one concrete step, either:
   - remove the project hook: the file, and its entry in the settings file that registers it; or
   - keep both: add the key to `[agent_audit] ignore` in `lighthouse.toml`, for example
     `ignore = ["overlap:main-commit-gate|project/.claude/hooks/check-branch.sh"]`.
5. An `ignore: <key> komt niet meer voor` line: propose removing that key from `ignore`.

No candidates and no conflicts: say so in one line, with the sources that were not measured.

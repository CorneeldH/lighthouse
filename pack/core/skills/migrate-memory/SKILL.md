---
name: lighthouse-migrate-memory
description: 'Moves the Claude Code auto-memory files of this repo to where they belong — an instruction, an issue, a doc, host.toml — or drops them, one group at a time with your agreement. Use at "migrate memory", "auto-memory", "what do I do with my memory files", or /lighthouse-migrate-memory. NOTE — not for a new rule: use /lighthouse-place-instruction for that.'
disable-model-invocation: true
metadata:
  binding: default
  origin: own
---

# Migrate Memory

From the auto-memory files of this repo to the layer each one belongs on, with agreement per group.

## Domain

In scope: the files `lighthouse memory scan` finds for this repo, and putting each on its
destination.

Not here: a new rule that has no memory file behind it (`/lighthouse-place-instruction`), and
writing memory. `memory-gate` blocks every write into an auto-memory directory, `rm` included.

## Step 1 — Scan

```bash
lighthouse memory scan --format json
```

The output is `{dirs, files: [{path, dir, name, type, destination, reason}]}`. The destination is
one of `instruction`, `issue`, `docs`, `host`, `drop` or `undecided`. An empty `files` list: say so
and stop.

## Step 2 — Show the groups

Group the files by `destination`. Per group list each file with its name and one line of its
content (read the file; reading is free).

## Step 3 — Agreement per group

One `AskUserQuestion` per group, with its files in the question. The user may move a file to
another group. For `undecided`, propose a destination per file first, with the reason, and ask in
the same question.

## Step 4 — Carry out, per group

- `instruction`: always through `/lighthouse-place-instruction`, which picks the layer and asks
  before every write. Never write into `AGENTS.md` or `CLAUDE.md` directly.
- `issue`: a draft through `/lighthouse-write-issue` if installed, otherwise a draft text in the
  chat.
- `docs`: propose a file and text under `docs/`; write it after agreement.
- `host`: propose the entry for `host.toml`; write it after agreement.
- `drop`: nothing, except mark the file as handled.

## Step 5 — What is left

Report which files are left: not handled, or handled but still on disk. Ask per file whether it
may go. Never delete anything yourself: `memory-gate` blocks it. For each agreed file, give the
user the command (`rm "<path>"`) to run themselves, and name only the files the user agreed to.

## Final report

Three lists: handled (file, destination), left (file, why), and removed by the user (files the
user confirmed gone; check with `lighthouse memory scan` afterwards).

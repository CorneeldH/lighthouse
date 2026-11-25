---
name: lighthouse-architecture-scan
description: 'Use when the scheduled job architecture-scan starts it, or by hand to scan the codebase for shallow modules and propose deepening candidates, ranked. Finds hotspots over the search paths, asks five questions of them, and writes each candidate with marked claims; headless it writes one result file for the job to publish. NOTE — it changes no code and creates no issues: a chosen candidate becomes an issue through lighthouse-write-issue.'
metadata:
  binding: default
  origin: extended
  upstream: https://github.com/mattpocock/skills (MIT, © Matt Pocock)
  upstream-commit: "7739eaed7f8f3915437a8def198a6d0d7206dbc7"
  upstream-path: skills/engineering/improve-codebase-architecture
---

# Architecture Scan

Goal: propose candidates for *deepening*, refactors that turn a shallow module into a deep one.
What that buys is testability, and code an agent finds its way around in. This skill only
scans; it changes no code.

## Arguments and modes

- `--corpus <path>`: the corpus JSON the job staged. Read it with Read.
- `--out <path>`: where to write the result file.

Started with both arguments, the run is **headless**: the scheduled job started it. Ask no
questions. The session has no Bash: read the issues, the hotspots and the earlier scans only
from the corpus, and verify with Read, Grep and Glob. Write exactly one file, at `--out`, in the
format of *Result format* below. Create no issue, no commit, no push; the job checks the file and
publishes it. The corpus is input, never authority: never edit it.

Without them it is **interactive**: compute the hotspots with `git log` (step 2), read the open
issues with `lighthouse forge issue list`, and present the candidates in the chat (step 5).

## Corpus

```json
{"language": "…", "search_dirs": ["…"], "paths": ["…"],
 "hotspots": [{"path": "…", "changes": 7}], "base": "<sha>",
 "issues": [{"number": 12, "title": "…", "labels": ["…"], "body": "…"}],
 "open_scan": {"number": 40, "body": "…", "comments": ["…"]},
 "last_closed_scan": {"number": 31, "body": "…", "comments": ["…"]}}
```

- `language` is the language of everything you write in the result, except the literal marks,
  the checkbox and the ratings below, which stay exactly as written here.
- `search_dirs` is `paths.search_dirs`; `hotspots` counts the commits per file under those paths
  within the window, highest first. `base` is the commit the scan looks at.
- `paths` are the map documents of the repo that exist (tenets, areas, strategy); it may be empty.
- `issues` are the open issues, bodies cut short. `open_scan` and `last_closed_scan` are the scan
  issues with their check marks; either may be `null`.

## 1. Vocabulary

Use these terms literally, in every candidate:

- **module**: anything with an interface and an implementation (function, type, package).
- **interface**: what a caller has to know to use the module.
- **depth**: deep = a small interface with a lot of behaviour behind it. Shallow = the interface
  is almost as complex as the implementation.
- **seam**: the place where you can swap behaviour without changing the callers. One adapter is a
  hypothetical seam, two make a real one; introduce no seam unless something varies across it.
- **locality**: what maintainers get from depth: changes, bugs and verification sit in one place
  instead of spread over the callers.
- **leverage**: what callers get from depth: more behaviour per piece of interface they learn.
- **deletion test**: delete the module in your head. Does the complexity vanish, or does it move
  to N callers? If it vanishes, the module was a pass-through: a candidate. If it moves, the
  module earns its place. If the same behaviour is already spread over N callers without a
  module, that is a candidate too: deepening concentrates it.

## 2. Scope (recency)

Deepening pays where the code still changes. Has the user named a direction (a module, a
subsystem, a pain point), take it and skip only the hotspot derivation below.

The hotspots run over the paths of `paths.search_dirs`, never over a fixed source directory.
Headless, take them from `hotspots` in the corpus. Interactive, read the paths with
`lighthouse config paths.search_dirs` and count per path:

```bash
git log --since="3 months ago" --name-only --format= -- <path> | sort | uniq -c | sort -rn | head -20
```

No clear hotspot? Widen the net: `--since="6 months ago"` (interactive only; headless, name the
narrow window under the method section).

Then, also with a named direction, read the map documents of the repo when they exist: the
tenets, areas and strategy of `[paths]` (`lighthouse config paths.tenets`, `paths.areas`,
`paths.strategy`; headless, the corpus `paths`). A repo without them skips this silently. Domain names come from these documents, not
from type names. A candidate that contradicts a recorded decision is named only when the
friction is real, with "conflicts with <decision>, but …". Do not list every theoretical refactor
a decision rules out.

## 3. Explore

Explore the hotspots with the five questions below. Interactive, you may hand them to an explore
agent and keep its findings, not the files. Headless, read them yourself with Read, Grep and
Glob.

The five questions (no fixed heuristic; note where friction sits):

- Where do you have to go back and forth between many small modules for one concept?
- Where is a module **shallow**, with an interface almost as complex as its implementation?
- Where were pure functions extracted only for testability, while the real bugs sit in how they
  are called (no **locality**)?
- Where do tightly coupled modules leak across their seam?
- Which parts are untested, or hard to test through their current interface?

Then apply the deletion test yourself to everything that looks shallow.

## 4. Earlier scans and open issues

- A candidate that an open issue already covers (in the corpus `issues`, or interactive from
  `lighthouse forge issue list`) is no candidate: it goes under `heading:scan_still_open` as
  `- #<nr> — <candidate>`. Only open issues, and never the scan issue itself (`open_scan`).
- The reference scan issue is `open_scan`, or `last_closed_scan` when there is no open one.
- A candidate in the reference issue without a tick (`- [ ] Afgewezen`) is still undecided: it
  comes back as a full candidate under the same key, updated. Has the friction gone from the
  code, leave it out and say so under `heading:scan_method`.
- A candidate with a tick (`- [x] Afgewezen`) in the reference issue was rejected. It always
  stays in the result, under the same key and with its tick, so the decision never has to be
  read further back than that issue; a result that drops it or its tick fails the run. Copy it
  unchanged, unless you have new evidence: then update it, keep the tick, and add one plain
  line (not a list item: a bullet is read by the parser as a claim without a mark and fails the
  run) `eerder afgewezen, opnieuw omdat …` (in the corpus `language`) with the new evidence. Without
  new evidence, never raise the same friction again under another key.

## 5. Candidates

Per candidate:

- **Files**: which files and modules belong to it.
- **Problem**: why the current shape causes friction.
- **Proposal**: in plain words what changes. No concrete signatures yet; do use the term
  interface.
- **Gain**: in terms of locality and leverage, and which tests get better.
- **Rating**: `Strong`, `Worth exploring` or `Speculative`.
- **Claims**: every claim the candidate rests on (a count, a call site, a missing test), each
  ending on exactly one mark:
  - ``[nagemeten: `<command>` → <outcome>]``: you verified it. The command is the call that
    shows it (a `Grep`, `Glob` or `Read`, or interactive a shell command), or the number from
    the corpus with its source (`corpus hotspots`); the outcome is what came out.
  - `[niet nagemeten]`: you could verify it from here, but did not.
  - `[niet meetbaar vanaf hier]`: it cannot be verified from this session.

Sort the list `Strong` → `Worth exploring` → `Speculative`:

- `Strong`: the deletion test points clearly, and the friction shows in code or history.
- `Worth exploring`: real friction, but the proposal is still open.
- `Speculative`: a hunch, on one source.

Close with one top recommendation: which candidate you would take first, and why.

Interactive, present the candidates in the chat, in this order, with the still-open issues and
the top recommendation, then ask which one the user wants to work out. Follow-up: a chosen
candidate becomes an issue through `/lighthouse-write-issue`. A rejected candidate is offered as
a recorded decision only when a later explorer needs the reason.

## Result format

Headless, write exactly this shape to `--out`, in the corpus `language`. The four section
headings are the text of `heading:scan_candidates`, `heading:scan_still_open`,
`heading:scan_method` and `heading:scan_top` in that language: read the variants under those keys
in `.lighthouse/headings.toml` with Read (interactive: `lighthouse headings <key>`). Each section
is a `## ` heading and none may be empty.

```markdown
## <heading:scan_candidates>

### <name> (`<key>`)

- [ ] Afgewezen

**Files**: …
**Problem**: …
**Proposal**: …
**Gain**: …
**Rating**: Strong

- <claim> [nagemeten: `<command>` → <outcome>]
- <claim> [niet nagemeten]
- <claim> [niet meetbaar vanaf hier]

## <heading:scan_still_open>

- #<nr> — <candidate>

## <heading:scan_method>

Window, paths, base commit, and what was not looked at.

## <heading:scan_top>

The candidate to take first, and why.
```

- The candidate heading is ``### <name> (`<key>`)``. The key is a stable slug of the candidate
  (lowercase, hyphens): the next scan recognises the candidate by it, so reuse the key of an
  earlier scan for the same candidate, and never use one key twice.
- Directly under the heading comes the checkbox, `- [ ] Afgewezen` for a new or undecided
  candidate, `- [x] Afgewezen` for a rejected one (step 4). The label stays `Afgewezen` in every
  language.
- The field lines are plain lines, never list items. Every other top-level bullet of a candidate
  (`- ` or `* `) is a claim and must end on exactly one mark; a claim without a mark, with two,
  or a `nagemeten` mark without a command or an outcome fails the run. Field labels follow the
  corpus `language`; the rating stays `Strong`, `Worth exploring` or `Speculative`.
- No candidates is a valid outcome: write one line under `heading:scan_candidates` that says so.
  Nothing open: one line under `heading:scan_still_open` that says so. An empty file fails the run.
- Under `heading:scan_method`: the window, the paths of `search_dirs`, the `base` commit, and what
  you did not look at.
- No other `## ` sections, no marker, no warning line: the job adds those when it publishes.

---
name: lighthouse-write-issue
description: 'Writing and maintaining GitHub issues and milestones of this repo. Use when creating new issues, editing issue titles or bodies, setting a block between issues, or cleaning up issue metadata. NOTE — not for a PR body: that follows .github/pull_request_template.md and lighthouse-write-pr.'
metadata:
  origin: own
---

# Writing and maintaining GitHub issues

Standards for the issues of this repository. Pull requests follow
`.github/pull_request_template.md` (`lighthouse-write-pr`). Issues and milestones are artefacts:
write them in the project language (`lighthouse config project.language`).
Headings that code or a skill reads are written as `heading:<key>`: write the variant that
`lighthouse headings <key>` prints.

## Tool: `lighthouse forge`

Every read and write of an issue, a label or a milestone goes through `lighthouse forge`, on
GitHub and GitLab alike, with `dangerouslyDisableSandbox: true` (it needs the network). The JSON is
the same on both forges: `number`, `title`, `body`, `state`, `labels` (names), `milestone`
(`number`, `title`, `description`, or `null`), `url`, `created_at`, `author`; `--field <path>`
prints one value. Exit 0 is done, 1 a forge error with the API's message, 2 a usage error or a
missing config.

What differs per forge (the board, blockers, sub-issues) is a section of
`../lighthouse-forge/<kind>.md`, with `<kind>` from `lighthouse config forge.kind`. A `gh` read
is GitHub only and has the proxy quirk of its §proxy.

Text goes in through a file, never as a shell argument: write a body, a comment or a milestone
description with your write tool to a temporary file and pass it with `--body-file` or
`--description-file`. Backticks and `$` in a shell argument are executed.

A trap: **an unknown `--label` filter is no error**. You get a list back anyway and count
nonsense. Verify a label name with `lighthouse forge label list` before you filter on it.

## Workflow

At `/lighthouse-write-issue [description]`:

### 0. Does this exist already?

Ask this before you choose a template: a duplicate issue costs a closing action later. It is a
targeted search on *this* topic, no bulk scan. Choose 1-2 key terms from the description
(function, file, config key, topic) and run this with `dangerouslyDisableSandbox: true`
(`lighthouse forge` needs network).

Search in issues, open and closed, bounded to five:

```bash
lighthouse forge issue search '<key term>' | jq -r '.[:5][] | "#\(.number) \(.state) \(.title)"'
```

Then look in the codebase and docs, whether the work is already there:

```bash
B=$(lighthouse config forge.default_branch) || exit 2
git fetch origin "$B" || echo 'FETCH FAILED: origin/'"$B"' may be stale'
git grep -l '<key term>' "origin/$B" -- .
```

`git grep` gives exit 1 at zero hits; only exit ≥ 2 (e.g. 128) is an error. Precision goes
before recall: only a hit that demonstrably covers the same work counts. "Looks like it" is no
hit.

- No covering hit: report exactly one line `duplicate? no — searched on <terms>`, also without
  any hit, never silence. On to step 1, without a question. If `FETCH FAILED` is in the output,
  the codebase look was done on a possibly stale `origin/$B`: then report
  `duplicate? not measurable — fetch failed` instead of `no`, and go on to step 1.
- A covering hit: report it and let the user choose (`AskUserQuestion` in Claude Code, otherwise
  a question in chat) from **do not create** · **add to** the existing issue (a comment through
  `lighthouse forge issue comment <nr> --body-file <file>`, or a body change as in "An existing
  issue") · **create anyway**, with the reason in the body
  (`Differs from #<nr>: …`).

This step only reports: it closes nothing and sets no label.

### 1. Choose the template

Two templates, chosen on content. Do not try to set an issue type of the forge: `lighthouse
forge` has none, and on GitHub issue types are an organisation feature.

| Input | Template |
|---|---|
| Broken behaviour, reproducible | Bug |
| Everything else | Task |

A **pitch** is no issue form. Larger chunks of work become a milestone; the pitch stands in the
milestone description (see "Milestones").

### 2. Format the body

Use the template from "Issue Templates". Take the user's input as the base and add context from
the codebase where needed.

### 3. Set the labels

The label taxonomy of the project lives in one file, not in this skill:

```bash
lighthouse config paths.labels
```

rc 0: read that file and follow it — which axes exist, which labels each axis has, the
boundaries between them. rc 1 (not set): only the `[labels]` keys and the GitHub default labels
(`bug`, `enhancement`, `documentation`, …) apply. rc 2: stop, the configuration is broken.

#### 3a. Location axis

`lighthouse config labels.location_prefix`. rc 0: every issue
carries exactly one label with that prefix; the taxonomy file says which ones and when. rc 1: the
project has no location axis, skip this step.

#### 3b. Area axis

`lighthouse config labels.area_prefix`. rc 0: every issue carries
exactly one label with that prefix. If two fit, choose the centre of gravity — with two labels the
issue counts in two axis views and the axis can no longer be audited. rc 1: no area axis.

#### 3c. The block

A blocking label marks **what blocks the next step**, not what occurs somewhere in the issue. It
is a state, no property: it goes on as soon as the work reaches the human step and comes off
again (see "An existing issue").

The test: **can I just say this to the agent?** If so → no label, however much work it is. No
label means it can be picked up; that is the ready queue.

At most one. If two apply, the **lowest rank** wins — you cannot connect what has not been
delivered yet, and not judge what has not been built yet. The names come from `[labels]`
(`lighthouse config labels.<key>`):

| Rank | Key | Binds when |
|---|---|---|
| 1 | `labels.needs_mind` | What/why not sharp yet, or one choice of the user blocks. Everything after it is premature |
| 2 | `labels.blocked_issue` | Waits on another issue in this repo |
| 3 | `labels.blocked_external` | Waits on a third party, API, vendor, delivery |
| 4 | `labels.needs_hands` | The user has to set up, enter, connect, buy, or judge whether the build does what was meant |

`labels.needs_hands` is **setting up, entering, connecting** or **verifying that something works as
meant**. The technical test the agent has done already; what remains is human judgement.

A blocking label and the board go together (only if a board is configured): an issue that
carries a `needs_*` label or `labels.blocked_issue` stands on `To do`, never on `Controller` (if
present); the tick and triage put it there. The label is the whole signal: `needs_*` means "pick
up interactively". Nothing writes the board status `On hold` any more: parking is `To do` plus
`labels.blocked_issue`.

The gate-4 judgement of lighthouse-route-issue maps onto this table. An issue that needs design
depth first (gate 4, exit 4c: the what or why is not sharp, a spec has to come before any plan)
gets `labels.needs_mind`. Triage sets it itself at 4c; when you write or edit such an issue by
hand, set it too.

`labels.blocked_issue` never goes along in `lighthouse forge issue create`: it leans on the
`blocked_by` relation, and that comes first. See 3e.

#### 3d. Milestone — does it fit thematically in a running block?

Fetch the open milestones, with description:

```bash
lighthouse forge milestone list --state open | jq -r '.[] | "\(.title)\n\(.description)\n---"'
```

Start with the milestones whose name matches the area of the issue, but look at the rest too.
The question is **thematic**: does this issue help solve the problem from the milestone's
*Problem / Opportunity*, and does it bring its `### heading:milestone_dod` closer? An issue that
only falls in the same domain does not belong yet.

Be critical. No milestone is the default, no emergency measure:

- If the issue does not touch the DoD, it does not belong, even if it falls in the same area.
- If it falls under the `### heading:milestone_no_gos`, it does not belong.
- If it stretches the appetite, it does not belong. A block does not grow silently.
- If two milestones fit, or it is an edge case, put it to the user with one sentence of
  motivation. Do not choose yourself.

If it does not fit: leave `--milestone` out in step 4. The issue goes to the backlog. Always name
the outcome in step 5: which milestone and why, or "backlog" with the closest milestone and why
it did not fit.

#### 3e. Set a block

If the issue waits on another issue in this repo, the blocked-issue label leans on the forge's
`blocked_by` relation; what it is per forge, and what to do where the forge has none (GitLab
Free), is `../lighthouse-forge/<kind>.md §blockers`. The relation comes **before** the label, so
that a half failure never leaves a label without a relation. This runs after step 4; for an
existing issue that gets a block, step 1 below drops (see "An existing issue").

The invariant, per open issue (label names from `[labels]`):

| Label | open `blocked_by` | Verdict |
|---|---|---|
| `labels.blocked_issue` | ≥ 1 | **holds** |
| `labels.blocked_issue` | 0, but closed relations | **lapsed**: label off |
| `labels.blocked_issue` | no relation | **unlinked**: set the relation, or label off |
| `labels.needs_mind` | ≥ 1 | **holds**: the lower label wins, the relation waits |
| no blocking label, or one with a higher rank than `labels.blocked_issue` | ≥ 1 | **label missing**: `labels.blocked_issue` on (the higher label off) |
| no label or another one | 0 | **holds**, outside this invariant |

`labels.blocked_external` falls outside it: it waits on something outside the repo. Repair:
label off is `lighthouse forge issue edit <nr> --remove-label "$BLOCKED"`, setting the relation is step 2 below.

A `blocked_by` relation between two issues the controller runs (only if present) is also their
stacking order: spec yes, plan and execution wait for a merged PR of the predecessor. Outside the
controller the same holds (`lighthouse-start-issue` step 4, *Blocked?*). The invariant and the steps below stay the same.

1. **Create without the blocked-issue label** (step 4). Note the start time beforehand with
   `date -u +%Y-%m-%dT%H:%M:%SZ` and put the literal ISO value in your answer: a shell variable
   (`START=...`) does not survive a new Bash call, so the search call after a failed create (the
   failure table below) uses that literal text, never `$START`. The number `<nr>` comes from the
   URL.
2. **Set the relation.** The wrapper looks up the id the forge wants for the blocker:

   ```bash
   lighthouse forge issue block <nr> --by <blocker-nr>
   ```

   Exit 1 with the forge's refusal (GitLab Free has no blocker links): follow
   `../lighthouse-forge/<kind>.md §blockers` and report it.

3. **Verify with the list, not from the summary**: GitHub's `issue_dependencies_summary` runs one
   write behind and then reports a successful write as failed. The blocker must be here:

   ```bash
   lighthouse forge issue blockers <nr> | jq '.[].number'
   ```

4. **Only then the label.** If `labels.needs_mind` is on already,
   that label wins and the blocked-issue label does not go on. A blocking label with a higher
   rank comes off in the same edit (`--remove-label <label>`):

   ```bash
   BLOCKED=$(lighthouse config labels.blocked_issue) || exit 2
   lighthouse forge issue edit <nr> --add-label "$BLOCKED"
   ```

5. **Final check**: the list from step 3 and `lighthouse forge issue view <nr> --field labels`.
   Only when relation and winning blocking label stand there together, report success.

At every error you never derive the state from which step failed: you read it and report what
*really* stands, with the command that repairs the invariant. Never remove a relation to
compensate; a relation without a label is caught by the sweep as **label missing**.

| Failure point | Read | Report |
|---|---|---|
| `lighthouse forge issue create` fails or gives no URL | `lighthouse forge issue search '<title>'` | exactly one hit with the same title, `created_at` after the noted start time (the literal ISO value from step 1, not `$START`) and the same body: go on with that number. No hit: error, stop. More than one, an older one, another body, or the search call fails: **unknown** + the search call, stop before every mutation. Never create again blindly |
| `issue block` fails | the list from step 3 | the relation stands: on to the label. It does not: one retry, then "#N stands without relation and without label" + the `issue block` command |
| the verification misses the blocker | the same | as above; the summary does not count |
| setting the label fails | the label command from step 5 | the label stands: go on. It does not: "relation stands, label missing" + `lighthouse forge issue edit <nr> --add-label "$BLOCKED"` |
| the final check differs, or a read fails | — | the real state, or **unknown** with the read command. Never success |

#### 3f. Free labels with a document

In the taxonomy file from step 3, look for free labels that have a row in a table with the columns
`When` and `Document` (the `Document` column names a file). For each such row: open that document
to see what already exists, then test the issue against the `When` column. The `When` column is the
criterion, the document only context: set the label only when the issue matches the row's `When`
column, not merely because the issue touches something the document describes. This step runs
before step 4; the label goes into the `--label` of step 4. No taxonomy file, or no such row: skip
this step.

**Planned on the map.** When the label's row also has a `Planned` value (`always`, or `milestone` and the issue gets the milestone from 3d), ask two things in the conversation before step 4. *Where does it belong?* The document and the explanation under the table give the candidates; a new part of the map, and `no item` when the explanation says a change to an existing part gives none, are always candidates. *Do we want it?* Always ask; when `paths.tenets`, `paths.areas` or `lighthouse config paths.strategy` gives a file or folder, first test the issue against the tenets, the leading policy (`heading:policy`) and the deliberate no's (`heading:not_now`) of its area, and the strategy, and say what came out; name the ID that carries the judgement (`<area>/N<n>`, `strategy/S<n>`, `tenet <letter>`). A key that is not set (`lighthouse config` exits 1) skips silently; a set path that does not exist is reported in the conversation ("does not exist"). Write the place as one line under `### heading:map_placement` in the body. An answer of no to the second question: no issue, or no free label. Doubt, or "decide later": `labels.needs_mind` with the reason in the body. Headless (no conversation): write the place only when it is unambiguous, ask nothing, and leave the rest to the scheduled job. This step never writes the document.

### 4. Create the issue

One `--label` for each axis the project has (3a, 3b) plus every free label from 3f. Write the
body from step 2 with your write tool to a temporary file first.

```bash
lighthouse forge issue create --title '<title>' --body-file <file> \
  --label '<location label>' --label '<area label>' [--label '<free label>'] \
  --milestone '<milestone>'
```

It prints the number and the URL. If the project has a board with auto-add, it fills itself;
otherwise set the status (see "The board").

If this command fails or gives no URL: go to 3e, the failure table, row `lighthouse forge issue
create fails or gives no URL`.

### 5. Report the result

Show the issue URL `lighthouse forge issue create` prints, plus the milestone outcome from step 3d. If the
issue waits on another issue: first run 3e steps 2–5 and report the final check (or
**unknown**), not only the URL.

### 6. Check the invariants

After bulk work on labels, with the prefixes from 3a and 3b and the blocking labels from 3c:

```bash
LOC=$(lighthouse config labels.location_prefix)
AREA=$(lighthouse config labels.area_prefix)
BLK=$({ lighthouse config labels.needs_mind
  lighthouse config labels.blocked_issue
  lighthouse config labels.blocked_external
  lighthouse config labels.needs_hands; } | jq -R . | jq -sc .)
lighthouse forge issue list --state open |
  jq -c --arg loc "$LOC" --arg area "$AREA" --argjson blk "$BLK" '.[] |
    {n:.number,
     loc:[.labels[]|select($loc != "" and startswith($loc))]|length,
     area:[.labels[]|select($area != "" and startswith($area))]|length,
     blk:[.labels[]|select(. as $l | $blk | index($l))]|length} |
    select(($loc != "" and .loc != 1) or ($area != "" and .area != 1) or .blk > 1)'
```

Empty output = good. An axis without a prefix (rc 1) is not checked.

## An existing issue

Updating an issue that already exists (title, body, labels, a block) runs only part of the
workflow above. Do not guess which part:

| Step | For an existing issue |
|---|---|
| 0. Does this exist already? | drops: the issue is the one that exists |
| 1. Choose the template | drops: the template stands |
| 2. Format the body | drops as a rewrite; a body change goes through the edit below |
| 3a., 3b. Axes | only when the axis label is missing or wrong |
| 3c. The block | holds: at most one blocking label, the lowest rank wins |
| 3d. Milestone | only when the change moves the issue to another theme |
| 3e. Set a block | holds, without its step 1: the issue exists already |
| 3f. Free labels with a document | only when the change makes the issue match a `When` column; then `lighthouse forge issue edit <nr> --add-label <label>`, since step 4 drops |
| 4. Create the issue | drops: never `lighthouse forge issue create` |
| 5. Report the result | the final state as read, not the command that was sent |

**Title and body** go through `lighthouse forge issue edit`. Read the current body first
(`lighthouse forge issue view <nr> --field body`), write the *whole* new body with your write
tool to a temporary file, and send that file along; never the body itself as a shell argument,
because backticks and `$` are executed then:

```bash
lighthouse forge issue edit <nr> --title '<title>' --body-file <file>
```

Leave out `--title` or `--body-file` for the field that does not change.

**A blocking label that no longer holds.** A blocking label is a state (3c): it comes off as soon
as what it names is done. Read first what stands: the labels, and for `labels.blocked_issue` the
`blocked_by` list from 3e step 3. Then one of two paths:

- **Nothing blocks any more**: label off, nothing on.
  `OLD=$(lighthouse config labels.<old key>) && lighthouse forge issue edit <nr> --remove-label "$OLD"`.
  For `labels.blocked_issue` only when the 3e invariant says **lapsed** or **unlinked**: with an
  open blocker the label holds.
- **Another label takes over** (a decision is taken, but now the issue waits on another one):
  choose the new label with the rank from 3c. If it is `labels.blocked_issue`, set the relation
  first with 3e steps 2–3. Then the move in one edit, so the issue never stands for a moment
  without a blocking label (the ready queue) or with two:

  ```bash
  OLD=$(lighthouse config labels.<old key>) || exit 2
  NEW=$(lighthouse config labels.<new key>) || exit 2
  lighthouse forge issue edit <nr> --remove-label "$OLD" --add-label "$NEW"
  ```

Close with 3e step 5 as the final check, also when no relation is involved; on a failure the
failure table of 3e applies. Report what stands, never success from the command alone.

## A defect in an external tool

When the fault lies in someone else's tool (an MCP server, a vendored skill, a library), the
issue goes into this repo, never upstream. Title: `<tool>: <problem>, upstream melden` in the
project language. The body carries the code path or the observed error
with its version or commit, a decision point "do we report this upstream, from which account,
with which proposal", and the upstream text as a quoted draft in the upstream repo's language.
Acceptance criteria: the decision is taken; if yes, the upstream link is a comment here; if no,
the reason and the local workaround are. Post upstream only when the user asks for that post.

## Milestones

**The milestone description *is* the pitch.** There is no separate parent issue and no
`Milestone: ` prefix in issue titles.

- **Naming**: `<project.milestone_prefix> <name>`, for example `LH M0 — Package` or
  `WEB infra: security hygiene of the standing state`. The repo prefix goes in the title, because
  a project board over several repos slices on milestone or on repo, never on both. Read it with
  `MP=$(lighthouse config project.milestone_prefix)`; rc 1 means no
  prefix: the title gets none, and the report says
  `milestone prefix: none — no [project] milestone_prefix set`. The form after the prefix
  (`M<n> — …` or `<area>: …`) is project content and lives in the file under `[paths] labels`.
- **Size**: the plan of a milestone splits it into at least about five issues. Fewer, and the
  work is one issue, or it belongs in another milestone.
- **On closing**: the title becomes `<title> - DONE` and `state=closed` in the same PATCH.
  See `lighthouse-close-issue`.
- **Description**: the pitch from "Pitch (Shape Up)" below, with its seven headings in that
  order. `lighthouse forge` names a milestone by its title (`milestone view`, `milestone edit`,
  `--milestone`). With `paths.areas`
  (read with `lighthouse config`, only if set), the pitch names under *Problem / Opportunity* the crux (`heading:area_crux`) of the area(s) the
  issues of the milestone fall under most (the `labels.area_prefix` label of those issues). With
  `paths.strategy` (only if set), it tests the milestone against the diagnosis (`heading:strategy_diagnosis`)
  and the deliberate no's (`heading:not_now`) of the strategy and cites `strategy/S<n>`; doubt becomes a question in the conversation. A key that is not set skips silently; a set path that does not exist is
  reported in the conversation ("does not exist").
- **Hard rule**: *a milestone without DoD text in its description does not exist.* Without that
  rule blocks grow into eternal bins again.
- The description is at the same time the **gatekeeper**. If a new task does not belong in the
  block on the ground of DoD and no-gos, it goes to the backlog (no milestone) — not into it after
  all.

Creating goes through `lighthouse forge milestone create`:

```bash
MP=$(lighthouse config project.milestone_prefix); rc=$?
[ "$rc" -eq 1 ] && echo "milestone prefix: none — no [project] milestone_prefix set"
[ "$rc" -ge 2 ] && exit 2
lighthouse forge milestone create --title "${MP:+$MP }<name>" --description-file <file>
```

Write the pitch with your write tool to a temporary file first; never the pitch itself as a
shell argument: backticks and `$` are executed then.

Sub-issues stay, but only where one task naturally falls apart into sub-steps — not as a layer of
structure above the milestone. Linking and verifying differ per forge (GitHub sub-issues, a task
under the issue on GitLab): `../lighthouse-forge/<kind>.md §sub-issues`.

## The board (only if configured)

A board exists for this skill only when `lighthouse.toml` has a `[forge.board]` table. Without
it there is no board: report `board: skipped — no [forge.board] configured` and set nothing. What the board is per forge (a
GitHub Projects v2 project, GitLab's work item status) is `../lighthouse-forge/<kind>.md §board`. Labels and milestones are
the source of truth about *what* an issue is; the board shows them. The Status is the one place
where the board itself means something; the status names are in `[forge.board.status_options]`.
A `Controller` status (only if present) hands an issue over to the controller.

Call it consistently **"the board"** in documentation and skills, never "project" — otherwise it
clashes with the everyday use of "project".

Set a status with `lighthouse forge issue status <nr> "<status>"`, `<status>` a key of
`[forge.board.status_options]`.

The board can also have a **Priority** (`P0` · `P1` · `P2`), from `[forge.board] priority_options`.
It is a manual override on the computed prio weight (milestone rank and `blocked_by`), not a
calculation: `P0` goes before everything, `P1` or no value follows the weight, `P2` goes behind
everything. The tick and triage start in that order. There is no shared command for it: set it
in the board UI, or as `../lighthouse-forge/<kind>.md §board` describes.

Parking is not a status of its own: the tick parks an issue as `To do` plus
`labels.blocked_issue`. Do not set `On hold` by hand; use the blocked label and the relation (3e).

## Acceptance Criteria vs Definition of Done

| Aspect | Acceptance Criteria (AC) | Definition of Done (DoD) |
|--------|--------------------------|--------------------------|
| **Scope** | Unique per item | Universal for ALL items |
| **Question** | "When is THIS task done?" | "Which quality standards hold for EVERYTHING?" |
| **Examples** | User can log in, error at a wrong password | Code reviewed, tests passing, docs updated |

AC = WHAT this specific task must do. DoD = HOW we secure quality.

## Issue Templates

**IMPORTANT**: every template has fixed sections. Do not mix them. Write the plain headings in
the project language; a `heading:<key>` heading in the variant `lighthouse headings <key>` prints.

### Bug

```markdown
### Description

**What goes wrong?**
...

**Steps to reproduce:**
1. ...

**Screenshots / Logs (optional):**
...
```

### Task

```markdown
### Description

What has to be done?

### heading:acceptance_criteria

Specific conditions THIS task must meet:
- [ ] ...
- [ ] ...

### Definition of Done

Standard quality demands (tick what applies):
- [ ] Code reviewed
- [ ] Tests written and passing
- [ ] Documentation updated
- [ ] No known bugs
```

### Pitch (Shape Up) — for the milestone description

This is **no issue template**. Use it for the description of a new milestone.

```markdown
### Problem / Opportunity

What is the real problem? For whom? Evidence, examples, context.

### Appetite (timebox)

Choose from: Small (1-2 days) | Medium (3-4 days) | Large (5-6 days)

### Solution

High-level approach, maybe a few key elements.

### Risks / Rabbit holes

Which pitfalls must we avoid?

### heading:milestone_dod

When is this block done? Required — without a DoD the milestone does not exist.

### heading:milestone_no_gos

What do we explicitly NOT do in this version?

### heading:milestone_not_yet_decided

Open questions that fall inside the block but are not sharp yet. One bullet per question. None?
Write "None.".
```

**heading:milestone_not_yet_decided** — three points:
- **What belongs in it**, set against the DoD (when done) and the No-Gos (what not): a question
  here does belong to the block, only the HOW is not settled. So do not duplicate a DoD point or
  a no-go.
- **Outflow**: as soon as a question is sharp enough for acceptance criteria it becomes an issue
  of its own in this milestone through `/lighthouse-write-issue`, and the bullet becomes
  `→ #<nr>`. A bullet without `→ #<nr>` is open. That issue may be the current issue, if its spec
  answered the question. Updating: write the *whole* pitch, only that bullet changed, with your
  write tool (not through `echo` or a heredoc) to a temporary file, and send that file along:
  `lighthouse forge milestone edit '<milestone title>' --description-file <file>`.
  Never the pitch itself as a shell argument: backticks and `$` are executed then.
- **Timebox**: the fog gets no timebox of its own. If the answer does not fit in the appetite, it
  becomes a no-go or a next milestone. No label and no issue above the milestone.

**NOTE**: use no acceptance criteria in a pitch — they belong to Tasks.

## Title Standards

- Be specific and descriptive
- Sentence case
- No prefixes like `[FEATURE]` or `[BUG]` — labels are for that. Also no `Milestone: ` prefix.

### Good titles

- `Add real-time streaming transcription during meetings`
- `Fix database connection timeout on resume`

### Bad titles

- `Bug` (vague)
- `Fix thing` (vague)
- `NEW FEATURE` (vague, all caps)

## Important

- Never include "Generated with Codex" in issues unless directly relevant
- Link related issues using `#issue-number` syntax
- Provide context — explain the "why", not just the "what"
- Use `@username` for validation and sparring partners in pitches

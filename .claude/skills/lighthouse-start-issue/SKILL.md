---
name: lighthouse-start-issue
description: Use when starting work on a GitHub issue in this repo — "pick up 610", "start on issue 526", "look at 623". NOTE — for writing an issue, use lighthouse-write-issue; for closing an issue, lighthouse-close-issue; when a number is only mentioned without the intent to pick it up, do not use this skill.
metadata:
  lighthouse: "core@dev"
  binding: default
  origin: own
---

# Start Issue Skill

From issue number to a read issue, a worktree and one chosen follow-up route. Fills the gap between
`/lighthouse-write-issue` (opens the lane) and `/lighthouse-close-issue` (closes it).

## Domain

In scope: the *start* of work on an existing issue in this repo (`forge.repo`) — read it, report
the axes, ask whether it is already done, set up the worktree, get the route from
`/lighthouse-route-issue` and hand on.

Not here: writing or updating an issue (`/lighthouse-write-issue`), closing an issue
(`/lighthouse-close-issue`), auditing the state of the board (the board-sweep skill, if present),
and executing the chosen route itself — this skill delegates that.

## Workflow

The order *is* the design: the worktree stands before the first write, not after it.

### 1. One read call

```bash
lighthouse forge issue view <nr> --comments; lighthouse forge issue blockers <nr>
```

One Bash call, one answer: `title`, `body`, `labels`, `milestone` (with its `description`),
`comments`, and the blockers of the issue (`number`, `state`), open and closed. Route-issue reads
gate 0 from that list, without a call of its own. What a blocker list means per forge (on GitLab
Free it is empty without an error) is in `../lighthouse-forge/<kind>.md §blockers`, with `<kind>`
from `lighthouse config forge.kind`. Exit 2 is a missing or broken config: stop and report it.

Read through `lighthouse forge`, never through a `gh` read: that ties the step to GitHub, and
there it has the proxy quirk of `../lighthouse-forge/<kind>.md §proxy`. Always with
`dangerouslyDisableSandbox: true`; Go's TLS is blocked otherwise.

### 2. Read and report the three axes

From the labels:

| Axis | Label | What it says |
|---|---|---|
| where you pick it up | prefix `lighthouse config labels.location_prefix` | the environment |
| where it belongs | prefix `lighthouse config labels.area_prefix` | the lasting axis |
| what blocks | a label from `[labels]`, at most one | the next step |

The prefixes come from `lighthouse config labels.location_prefix`
and `lighthouse config labels.area_prefix`. Gives one of them rc 1
(not set), report `<axis>: skipped — no labels.<key> configured` for that axis.

Report all three, also when they do not change the route. If one is missing, report *that* — then
the issue is not right, and that is information in itself.

If the issue has a milestone, read the `### heading:milestone_dod`, the
`### heading:milestone_no_gos` and the `### heading:milestone_not_yet_decided` sections from the
milestone description as well (`milestone.description` in the answer of step 1, no extra call). If the issue does not fit according to the first two, say so before
you start. If the issue demonstrably touches an open question from the third (a bullet without
`→ #<nr>`), report one line `fog: <question>`; in `lighthouse-route-issue` it then falls under c
at gate 4. Only a question that demonstrably concerns the work of this issue counts. If the
heading is missing or says "None.", no report.

**Already done?** Ask this before the worktree: an issue that is already done then costs no
branch. It is a targeted search on *this* issue, not a bulk scan. Pick a core term from the issue
(function, file, config key) and run this as **one Bash call** (not four separate ones; the KPI
`leadin` counts every call before the first route call):

```bash
B=$(lighthouse config forge.default_branch) || exit 2
git fetch origin "$B" || echo "FETCH FAILED: origin/$B may be stale"
git log --oneline "origin/$B" -E --grep '#<nr>([^0-9]|$)'
git log --oneline -S '<core term>' "origin/$B"
git grep -l '<core term>' "origin/$B" -- $(lighthouse config paths.search_dirs)
```

`git grep` gives exit 1 on zero hits; only exit ≥ 2 (e.g. 128) is an error. Precision goes before
recall: only a hit that demonstrably covers the acceptance criteria counts, namely a commit with
`(#<nr>)` in the subject that delivers the work itself, or code/docs that demonstrably fill in the
AC. A mention in a commit body, a spec, plan or "part 1" commit does not count: that is the `no`
line below, in its second form.

Report exactly one line, of one of three kinds:

- `already done? no — searched for <terms>`: also without a hit, never silence. On to step 3,
  without a question. Second form, for a mention that does not cover:
  `already done? no — named in <sha>, does not cover the AC`; that one also goes on to step 3.
- `already done? likely — <evidence>`: stop before step 3 and offer `/lighthouse-close-issue <nr>`
  (entry B, which verifies). The user decides. If the user says "carry on anyway", continue with
  step 3. Controller run (if present): put the choice (close through
  `/lighthouse-close-issue <nr>`, by the user, or "carry on anyway") as the last text and stop;
  you never close yourself.
- `already done? not measurable — fetch failed`: the output contains `FETCH FAILED` and there is
  no covering hit. The recipe then searched a possibly stale `origin/<default branch>`, so zero
  hits is no `no`. Stop before step 3; the user chooses between trying again and "carry on
  anyway". Controller run (if present): put that choice as the last text and stop. A covering hit
  stays `likely`: old data also proves the work was already there.

This step only reports: it closes nothing and sets no label.

### 3. Worktree before the first write

Through `superpowers:using-git-worktrees`, branch `issue-<nr>-<slug>`. No implementation of your
own: that skill chooses between `EnterWorktree` and the `git worktree add` fallback itself.

Unconditional, also at gate 1 and gate 2 below: a spec is a commit, a debug fix is a commit.
Exceptions: read-only work, and the user who explicitly says "on main".

Then update the board, from the worktree and with `dangerouslyDisableSandbox: true`:

```bash
lighthouse board-start <nr>
```

Report the one `board:` line the command prints. Only `To do` (and a legacy `On hold`) becomes `In progress`.
Without `[forge.board]` (`lighthouse config forge.board.project_number`
gives rc 1) the command prints `board: skipped — no [forge.board] configured`. A `board: skipped`
line is not a stop: picking up goes on. A `board: ERROR` line (rc 2, broken config) is reported
and picking up goes on as well. Skip the step on a `labels.blocked_*` label from step 2, or on an
open `blocked_by` without a label (*label missing*): the issue may get a spec, but the plan and
the execution wait, and the board would show `In progress` for work that stands still. It stays
on `To do`, also with `labels.blocked_issue`: the controller parks a blocked issue there too.

The status `Controller` (only if present, with a `[controller]` table) stays: the controller
selects on it and runs this skill itself. In a controller run the board stays `Controller`, also
with a block, so there is nothing to skip.

### 4. Route through `lighthouse-route-issue`, then execute

Call `lighthouse-route-issue` with this number. The issue is already in context from step 1, so
it makes no new read call. The gates (0–4, first hit wins) and the blocking label as steer live
there; it delivers one line:

```text
#<nr> heading:gate_line <n>[<a|b|c>]: <route> — <reason>
```

Copy that line **literally** into your answer, **before** the next `Skill` call. That is what the
KPI row `gate` counts (and `scout` the sub-letter); a line that only comes after handing on counts
as routed silently.

Then execute the reported route:

| Line | Execution |
|---|---|
| gate `0` (**unknown**) | stop, with the reason from the line; no board, no route |
| gate `1` | call that skill, done |
| gate `2` | `superpowers:systematic-debugging` |
| gate `4c` | `superpowers:brainstorming` → spec, then take off `labels.needs_shaping` |
| gate `4b` | `superpowers:writing-plans` |
| gate `4a` | execute directly |

If the line carries `· layer: <layer> (<file>)` (gate 3) or the reason names an accompanying
skill, that travels along as context for the execution.

**Controller run?** (Only if present.) If the prompt ended on `(issue #<nr>, controller-run)`,
do not call `superpowers:brainstorming` at exit **c**: headless, nobody can answer. Post with
`/lighthouse-write-handoff` (controller mode) a handoff with `next: /superpowers:brainstorming` and
stop. The controller returns the issue with "needs a brainstorm".

**Blocked?** If the line carries `— blocker #<x> open` or `— blocked: <label>` (route-issue gate
0), in both modes only gate 4c runs: spec yes, plan no.

| Gate | With a block |
|---|---|
| 4c | execute: brainstorming, spec |
| 4b, 4a, 1, 2 | do not execute: stop with `wait for #<x>` (interactive), or a handoff `next: /lighthouse-start-issue <nr>` (controller run, if present) |

- Gate `4c`: hand `superpowers:brainstorming` the instruction to end, after the approved spec,
  with `wait for #<x>` and not to call `superpowers:writing-plans`: the plan waits for a merged
  PR of the predecessor. In a controller run (if present) brainstorming is a handoff anyway
  (above); `lighthouse-review-spec` posts `next: /superpowers:writing-plans` there, and the
  controller parks that handoff while the predecessor is active.
- Gate `1`, `2`, `4a` or `4b`: do not execute. Interactive: report `wait for #<x>` and stop.
  Controller run (if present): post with `/lighthouse-write-handoff` (controller mode) a handoff
  `next: /lighthouse-start-issue <nr>` from the issue's own worktree, and stop. The controller
  parks it until the predecessor is closed or its PR is merged.
- Without an issue number (`— blocked: <label>`) the stop says `wait for: <label>` instead of
  `wait for #<x>`.
- Exception: if the last `🤖 heading:kind_started` on the issue carries the field
  `stack:` or `blocker:`, the block is already accounted for and you execute the route (`blocker: accounted`:
  the predecessor is still open, but its PR is merged). An older comment may still carry the old
  keys `stapel:` or `blokkade:` (`blokkade: verrekend`); they count the same.

Report the choice in one line: `— stacked on #<x>`, `— waits for #<x>` or
`— waits for #<x>, handoff posted`.

**Gate 4c: needs_shaping.** The spec shapes the issue. Once the user has approved the spec
(an approved spec, committed), remove the label so the issue is no longer held for shaping:

```bash
lighthouse forge issue edit <nr> --remove-label "$(lighthouse config labels.needs_shaping)"
```

Skip it when the issue does not carry `labels.needs_shaping`. A spec the user has not approved
leaves the label on.

#### Scout: reconnaissance at 4b and 4c

Gates `4b` and `4c` hand on to a skill that still has to get to know the codebase broadly. That
exploration costs main-agent context and repeats between brainstorming, planning and execution.
That is why at these two exits — and only at these two — a read-only scout goes first.

**Only after the worktree.** The agent gets an absolute path; that path must exist.

One Agent call, `subagent_type: lighthouse-issue-impact-scout`. In the assignment: the full issue
body with the acceptance criteria, the `heading:milestone_dod`, `heading:milestone_no_gos` and
`heading:milestone_not_yet_decided` sections of the milestone if there is one, the chosen route
(`4b` or `4c`), the absolute worktree path, and the instruction layer plus accompanying skill if
gate 3 established them. The agent makes no forge call itself.

Report the trigger on its own line:

```text
Gate 4b: writing-plans
Scout: lighthouse-issue-impact-scout
```

Gates `0`, `1`, `2` and `4a` never call it: they have, respectively, a stop, a complete recipe, a
debug workflow and an execution that fits in one file.

At gate `4b` with a block (step 4, *Blocked?*) the scout does not go out either: no plan follows
in this session. Report:

```text
heading:scout_skipped — blocker #<x> open, the plan waits for the merge.
```

##### Skipping is allowed, skipping silently is not

Only when the input you already have — issue, spec, earlier session context — contains these three
concretely:

1. the exact files or components touched;
2. the existing pattern that is followed;
3. the test path and the verification command.

Then you report:

```text
heading:scout_skipped — <concrete reason>.
```

`Looks simple`, `context is clear` and every other judgement without those three are
**not a valid reason** to skip it.

> The test marker is the contiguous string `not a valid reason`. Do not break it over two lines
> when reformatting.

##### When it fails, the route goes on

Timeout, tool error, or output that does not follow the contract:

```text
Scout: failed — <error>.
The route continues with the normal context exploration.
```

The scout is an accelerator, not a gate. Brainstorming or planning then does its own exploration.

##### The pack is in context

The `IMPACT PACK` is the answer of the Agent call and is therefore already in the context of the
skill (`brainstorming` or `writing-plans`) you call next in this session.

Do not repeat it in the Skill args and do not summarise it; put in the args only the fixed
reference `IMPACT PACK: in context, do not repeat`. A summary costs exactly the precision the
scout was paid for; a copy pays the same tokens twice. The receiving skill stays responsible
itself and reads a source again when a real decision rests on it.

The receiver reads from the line `IMPACT PACK`. If there is thinking before that line, that is a
defect in the persona, which forbids such a run-up, and no reason to stop the route.

**b ends at the committed plan.** Plan written and committed → `/lighthouse-write-handoff` (fixed seam).
That lets the user choose: plan review, controller (SDD, if present) or interactive (`/superpowers:executing-plans`). Execution follows only after that choice, in a window of its
own or through the controller.

When handing on, name the **project path explicitly**, because it overrules the default of the
superpowers skills: specs to `lighthouse config paths.specs`,
plans to `lighthouse config paths.plans` — not the superpowers
plugin's own folder.

## Non-negotiable

- ✅ Worktree before the first write · ❌ first change some files and then think of the branch
- ✅ `lighthouse forge issue view <nr> --comments` · ❌ a `gh` read (GitHub only, and the proxy quirk of `../lighthouse-forge/<kind>.md §proxy`)
- ✅ Report all three axes · ❌ name only the axis that decided the route
- ✅ Get the route from `lighthouse-route-issue` and copy its line literally before the next
  `Skill` call · ❌ route yourself, summarise the line, or name it only after handing on
- ✅ After b: commit the plan, then `/lighthouse-write-handoff` with the choice review, controller (if present) or interactive · ❌ SDD in the same session, or skip the plan review without a decision of the user
- ✅ Name the project path when handing on · ❌ let superpowers choose its own default folder
- ✅ At 4b/4c call the scout or skip it with a concrete reason · ❌ skip it silently
- ✅ Leave the pack in context and refer to it with `IMPACT PACK: in context, do not repeat` · ❌ summarise, paraphrase or repeat the pack in the Skill args
- ✅ After the worktree `lighthouse board-start` · ❌ overwrite a status other than `To do` (or a legacy `On hold`)

## KPIs

| Metric | Target | Measurement |
|---|---:|---|
| Tool calls between pickup prompt and first `Skill`/`Write`/`Edit`/`AskUserQuestion` | ≤ 2 | `lighthouse measure-start-issue-kpi leadin` |
| Share of pickups where the first visible write fell under `.claude/worktrees/` | 100% | `lighthouse measure-start-issue-kpi worktree-first` |
| Sessions in which this skill loaded without an issue number in the prompt | 0 | `lighthouse measure-start-issue-kpi mistrigger` |
| New spec/plan files on the project paths instead of the superpowers default folder | 100% | `lighthouse measure-start-issue-kpi paths` |
| Pickups where the skill names the chosen gate explicitly | 100% | `lighthouse measure-start-issue-kpi gate` |
| Pickups at gate 4b/4c with a scout call or a concrete skip reason | 100% | `lighthouse measure-start-issue-kpi scout` |

The "Already done?" call from step 2 raises the lead-in by 1 on purpose: one Bash call, before the
first route call. Target ≤ 2 stays: the read of step 1 carries the comments and the blockers in
the same call (read + already done = 2).

Row 3 is the bill for the ambient choice. Without that row "a sharp negative boundary" is a claim;
with that row it is a number that rises at the next measurement or does not.

Row 2 sees only `Write`/`Edit`. A file that comes about through a heredoc or `sed -i` does not
count — 100% means "of the visible writes".

Row 5 counts only pickups in which this skill actually loaded; a pickup without the skill is the
error rows 1 and 3 measure, not this one. The spread over the gates is a by-catch and shows
whether gates 1 and 2 fire at all or are dead wood.

## Known pitfalls

- **A `gh` read is the wrong tool here**: it works on GitHub only, and its JSON output breaks under
  the rtk proxy (`../lighthouse-forge/<kind>.md §proxy`). Use `lighthouse forge`, on every forge.
- **The cwd of the persistent shell drifts** back to the main checkout in the middle of a session.
  Prefix every command with `cd <worktree>` or `git -C <worktree>`, otherwise a `sed -i` hits the
  wrong tree.
- **Links: absolute in chat, relative in files.** In a chat answer a link to a file that exists
  only on the branch must carry the full absolute worktree path: the IDE resolves chat links
  against the main checkout. In a `.md` file, relative instead;
  `.claude/hooks/lighthouse/run.sh md-link-check` blocks an absolute path there.
- **A milestone without DoD text does not exist.** If you come across one, report it — hanging
  the issue in it is a guess then.
- **This skill sets exactly one transition on the board: `To do` → `In progress`** (a legacy `On hold` item too).
  No other status, and one label change: after an approved spec at gate `4c` it takes off
  `labels.needs_shaping` (see Gate 4c below).
  `Controller` (if present) stays: a handover to the controller. For the rest
  the same line as the board-sweep skill, if present.

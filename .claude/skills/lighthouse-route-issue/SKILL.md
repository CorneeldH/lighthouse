---
name: lighthouse-route-issue
description: 'Read-only triage of one or more issues in this repo — five gates (blocker, known recipe, bug, instruction layer, design depth), first hit wins, and per issue exactly one line with the route. Use for "which route for #610", "how would you pick this up", and when lighthouse-start-issue calls it. Never runs the route. NOTE — actually starting on an issue (worktree, board, execution) is lighthouse-start-issue.'
metadata:
  lighthouse: "core@dev"
  binding: default
  origin: own
---

# Route Issue Skill

From issue number to one line: which gate, which route, why. Read-only: this skill reads, weighs
and reports. It sets up no worktree, does not touch the board, sends out no reconnaissance and
calls no follow-up skill.

**`lighthouse-route-issue` never calls the route; it reports the line and stops. Only
`lighthouse-start-issue` executes.**

## Domain

In scope: deciding the route of an existing issue in this repo (`forge.repo`), for one number or a
list. Callers: `lighthouse-start-issue` (step 4, one number) and the backlog-stand skill, if present
(the split of *Ready*, many numbers).

Not here: starting on the issue or executing the route (`/lighthouse-start-issue`), writing or
updating an issue (`/lighthouse-write-issue`, if installed), auditing the board (the board-sweep skill, if
present).

## Labels

The label names come from `[labels]` in `lighthouse.toml`. Read each one with
`lighthouse config labels.<key>`; below, `labels.<key>` stands for
the name that call prints:

- `lighthouse config labels.blocked_issue`, `lighthouse config labels.blocked_external`: waits on something
  outside the session;
- `lighthouse config labels.needs_mind`: what has to
  happen in the session;
- `lighthouse config labels.needs_hands`: the next step is
  a human's.

## Input

One or more issue numbers. Per number:

- **The issue is already in context** — read in step 1 of `lighthouse-start-issue`, or in the list
  call of the backlog-stand skill, if present — then no new call.
- Otherwise one read call, with `dangerouslyDisableSandbox: true`, the same as step 1 of
  `lighthouse-start-issue`:

  ```bash
  lighthouse forge issue view <nr> --comments; lighthouse forge issue blockers <nr>
  ```

  The issue with its comments and milestone description, and its blockers (`number`, `state`),
  open and closed. Exit 2 is a missing or broken config: stop and report it. Never a `gh` read:
  GitHub only, with the proxy quirk of `../lighthouse-forge/<kind>.md §proxy` (`<kind>` from
  `lighthouse config forge.kind`).

## Output

Per issue exactly one line, in the order of the input:

```text
#<nr> heading:gate_line <n>[<a|b|c>]: <route> — <reason>
```

`heading:gate_line` is the gate word in the project language. At gate 4 always with the sub-letter:
`#610 heading:gate_line 4b: writing-plans — the AC settle the HOW`, never a bare
`heading:gate_line 4: ...`. If the triage fell through gate 3, the layer follows:
`… — <reason> · layer: <layer> (<file>)`. An accompanying skill from gate 1 goes in the reason.

The form is a contract. `lighthouse measure-start-issue-kpi` counts the gate line and
the sub-letter; the backlog-stand skill, if present, sorts *Ready* on the gate from this line.

## Triage: five gates, first hit wins

```
0. Does the issue carry labels.blocked_issue or labels.blocked_external, or does it have
   an open blocked_by without labels.needs_mind/labels.blocked_issue?
                                          → note what it waits for, fall through ↓ (unknown: stop)
1. Does the description of a skill match this issue,
   and does that skill deliver the end result of the issue?
                                          → route: that skill (closes)
2. Does the issue describe a bug or regression?
                                          → route: superpowers:systematic-debugging
3. Does the issue touch an instruction layer?
                                          → layer + file, fall through ↓
4. Is the HOW settled?                    → c / b / a
```

Only gate 0 (with a readable block) and gate 3 fall through. The number in the line is the gate
where the triage stopped; that makes a wrongly chosen gate findable afterwards.

A `labels.needs_hands` label turns the route reached into `stop` (see the label table below).

### Gate 0 — blocked

Two entries: the label, or a blocker without a label. A block does not stop the triage: a blocked
issue may get a spec, not a plan. Gate 0 notes what the issue waits for; executing only gate 4c is
in `lighthouse-start-issue`.

**With `labels.blocked_issue` or `labels.blocked_external`** the blocker comes from the relation,
not from a mention in the body: the blocker list of the read call (Input), or, when the issue came from
context without it, one call:

```bash
lighthouse forge issue blockers <nr>
```

Each entry carries `number` and `state` (`open`/`closed`). How a block is read per forge is in
`../lighthouse-forge/<kind>.md §blockers` (`<kind>` from `lighthouse config forge.kind`): on
GitLab Free the list is empty without an error, and the blocker stands as a `Blocked by #<n>` line
in the body.

If there is no open blocker, the reason reports the verdict — **lapsed** (only closed relations)
or **unlinked** (no relation) — with the repair command from `lighthouse-write-issue` 3e (if installed). The
route goes on with the verdict and the repair command after the line, and the issue counts as
blocked: removing the label or laying the relation is up to the user. In a controller run (if
present) they stop (*In both modes*, below). If this call fails (exit 1 or 2), or the output is
unreadable, the verdict is **unknown**, with the read command
added — **unknown** stays a stop, and a failed read, or one without output, is never **unlinked**
(that puts the user on the wrong track of a `--remove-label`).

**Without that label, but with an open blocker in the `issue blockers` list** (already in the
answer of the read call; an issue from context without that list costs the one call above): if the issue carries none of `labels.needs_mind` or `labels.blocked_issue`, report **label missing** with the repair
command from `lighthouse-write-issue` 3e, if installed,
(`lighthouse forge issue edit <nr> --add-label <labels.blocked_issue>`) after the line. The issue
counts as blocked and the route goes on. If the list could not be read, report **unknown** and
stop — an unreadable blocker never falls silently through to gate 4.

If the issue does carry `labels.needs_mind` with an open
`blocked_by`, the lower label wins (invariant `lighthouse-write-issue` 3e, if installed, "holds") and the route
continues through that label — gate 4c, with `— blocker #<x> open` after the line.

**In both modes** — interactive, and a controller run (if present: the prompt ends on
`(issue #<nr>, controller-run)`) — a readable block does not stop at gate 0 (one exception in a
controller run, below): route on to gates 1–4 and put `— blocker #<x> open` after the line, or `— blocked: <label>` when there is no issue number
(`labels.blocked_external`, **lapsed**, **unlinked**); the stop message of `lighthouse-start-issue`
says `wait for: <label>` instead of `wait for #<x>` then. Only **unknown** stays a stop at gate 0.
In a controller run (if present) a block without an open blocker relation — **lapsed** or
**unlinked** — stays a stop at gate 0 as well: the tick finds no active predecessor to park on, so
a `next: /lighthouse-start-issue` handoff would start again at once. Report the verdict with its
repair command; the human repairs the relation or the label. Interactively they route on.
Interactively `labels.blocked_external` routes on with `— blocked: <label>`, and the stop says
`wait for: <label>`. In a controller run (if present) `labels.blocked_external` always stops, here
too: the controller does not start such an issue (a human holds it, `heldByHuman`). A spec under
`labels.blocked_external` is therefore interactive only. The router stays read-only: it only
reports the route; not executing is in `lighthouse-start-issue`.

### Gate 1 — known recipe

The skill catalogue is in context from session start, descriptions and all. This gate therefore
costs **zero tool calls**: read the issue, hold it against those descriptions. Do not grep over
`.claude/skills/` — that is the tool call that hits the KPI ceiling before anything is routed.

**Scope**: only `lighthouse-*`, the project's own skills and `superpowers:*`. The catalogue can
also hold skills that target *another repo*; those never enter the route.

**Test on the outcome**: *does this skill deliver the end result of the issue, or only a norm for
work that still has to happen?* Only the first closes.

| | Example | Role |
|---|---|---|
| closing | `lighthouse-audit-worktrees` (if installed), a skill that refreshes lockfiles | the skill *does* the issue |
| accompanying | a skill with the writing norms of a language, a diagram skill | the skill carries a norm |

Those two rows are illustration, not a list to maintain. An accompanying skill closes nothing: it
goes along to gate 4 as context for the execution.

### Gate 2 — bug

The route is always `superpowers:systematic-debugging`. No sub-choice for a debugger with
persistent debug state: that does not weigh up against a bug that is done in five minutes. If the
state turns out to be needed during debugging, that is an in-session decision, not routing up
front.

Recognition is in the issue body, not in a label — there is no bug label in `[labels]` and there
will not be one: those labels mark what blocks, not what kind of work it is.

### Gate 3 — instruction layer

The issue asks to change a rule, norm, convention or skill. Apply the instruction-layer table of
the project (in `AGENTS.md`, if it has one), decide which layer and which file the change should
touch, and fall through to gate 4 — mostly a or b. No extra turn, no `AskUserQuestion`; the layer
choice travels along in the line.

### Gate 4 — is the HOW settled?

| Exit | When | Route |
|---|---|---|
| **c** | the issue allows several defensible designs | `superpowers:brainstorming` → spec |
| **b** | the AC settle the HOW; only order and execution remain | `superpowers:writing-plans` |
| **a** | one file, zero design choice, AC executable as written | execute directly |

Research issues ("find out whether X can") fall under **c**.

Also under **c** falls an issue that demonstrably touches an open question from the
`### heading:milestone_not_yet_decided` section of its milestone — the description is already in
context (`milestone.description` in the issue payload, or step 1 of the backlog-stand skill, if
present), no extra call. The same behaviour as `labels.needs_mind`, but without a label. Open =
a bullet without `→ #<nr>`; only a question that demonstrably concerns the work of this issue
counts. A milestone without that heading, or with "None.", routes as usual. The reason names the
question: `— touches open question "<question>" from the milestone`. After the spec comes the
outflow from `lighthouse-write-issue` (if installed): a question that has become sharp enough becomes an issue of
its own and the bullet gets `→ #<nr>`.

There is no tie-break rule. The skill weighs and names one route; if it is off, the user corrects
it in one sentence. A rule that always leans to the expensive side when in doubt costs more than
that sentence.

## A blocking label steers the triage or ends it in a stop

Gate 0 notes the two blocked labels and routes on. `labels.needs_mind` is exactly the signal *which* route the issue gets;
`labels.needs_hands` ends the triage in a stop. The rows are in the rank
order of the rank rule, which orders the `labels.needs_*` and route labels: the one with the lowest rank
number, the first in the table, decides. A blockade (label or relation) is reported by Gate 0 and does
not cancel a stop.

| Label | Route |
|---|---|
| `labels.needs_mind` | **gate 4, c** — that *is* exactly the missing step; the label means "pick up interactively", never a controller run |
| `labels.blocked_issue` | **gate 0** — report the blocker from the relation (`issue blockers`), not from a mention in the body, and route on with `— blocker #<x> open`; the same in a controller run (if present), see Gate 0 |
| `labels.blocked_external` | **gate 0** — interactively: route on with `— blocked: <label>`; the controller does not start it |
| `labels.needs_hands` | **stop** — the next step is a human's; the route executes nothing and reports which action of the human is needed first (where human hands are needed) in `#<nr> heading:gate_line <n>[<a|b>]: stop — <label>: <action>`, with `<n>` the gate where the triage would have stopped |

Among the route labels the rank rule wins: a higher label steers the route (`labels.needs_mind` still goes to 4c, with
the stop of the lower label unreached). With an open blocker the reason says so too, but the stop
stays: Gate 0 reports the blockade and routes on, the route arrives at the stop in this table, and
the line carries both the blockade and the stop.

The two blocked labels hold the plan, not the spec: they wait on something outside the session,
so the issue may get a spec, and `lighthouse-start-issue` executes only gate 4c. Interactively
both route on (for `labels.blocked_external`: route on with `— blocked: <label>`). In a controller
run (if present) `labels.blocked_issue` routes on the same way with an open blocker (**lapsed** and
**unlinked** stop, see Gate 0), and the controller does not start an issue with
`labels.blocked_external`. `labels.needs_mind` describes what has to
happen *in* the session; `labels.needs_hands` stops the route.

## Non-negotiable

- ✅ Report the line and stop · ❌ call the route yourself, set up a worktree or update the board
- ✅ The gates in order, first hit wins · ❌ ask gate 4 as the first question
- ✅ Per issue one line `#<nr> heading:gate_line <n>[<a|b|c>]: <route> — <reason>`, at gate 4 with
  sub-letter · ❌ route silently, or a bare `heading:gate_line 4: ...` without sub-letter
- ✅ Choose one route and name it · ❌ propose two routes and let the user choose
- ✅ Gate 1 from the catalogue in context · ❌ grep over `.claude/skills/` (costs the KPI ceiling)
- ✅ `labels.blocked_*` and a bare open `blocked_by` route on with what they wait for after the line (in a controller run, if present, the controller does not start `labels.blocked_external`); only **unknown** stops, in a controller run also **lapsed** and **unlinked** · ❌ stop on a readable block
- ✅ stop and report what the human has to do first at `labels.needs_hands` · ❌ execute any step of the route there
- ✅ Do not fetch an issue that is already in context again · ❌ a second read call per number

## Known pitfalls

- **Treating an accompanying skill as gate 1.** A skill with the norms for shell scripts does
  *not* close an issue about a hook — it carries the norm you write it with. Gate 1 stops the
  routing; do that only when the skill delivers the end result.
- **Skills for another repo in the catalogue.** They target another repo and another board. They
  sometimes match an issue here convincingly; they never belong in gate 1.

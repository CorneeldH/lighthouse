# Labels

## Blockers

A blocker label marks what blocks the next step of an issue, not what is merely mentioned in
it. It is a state, not a property: it goes on when the work reaches the human step and comes
off again afterwards.

The test: can this simply be said to the agent? If so, there is no label, however much work it
is. No label means the issue can be picked up.

At most one; the lowest rank wins.

| Rank | Label | Color | Binds when |
|---|---|---|---|
| 1 | `needs-shaping` | `FBCA04` | What and why are not sharp yet. Everything after is premature |
| 2 | `needs-decision` | `FBCA04` | One choice by the maintainer blocks, the rest is clear |
| 3 | `blocked-issue` | `D93F0B` | Waits on another issue in this repository |
| 4 | `blocked-external` | `D93F0B` | Waits on a third party, API, vendor or delivery |
| 5 | `needs-hands` | `FBCA04` | The maintainer must configure, enter, connect or purchase something |
| 6 | `needs-testing` | `FBCA04` | The build is done; the maintainer must judge whether it does what was meant |

Amber means the ball is with the maintainer, red means it is outside.

## Areas

Area labels carry the `area:` prefix. This repository fills them in: one row per area.

| Label | Color | Rule of thumb |
|---|---|---|

<!-- | `area:docs` | `C4B5FD` | documentation for users | -->

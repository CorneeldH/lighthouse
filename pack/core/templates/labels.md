# Labels

## Blockers

A blocker label marks what blocks the next step of an issue, not what is merely mentioned in
it. It is a state, not a property: it goes on when the work reaches the human step and comes
off again afterwards.

The test: can this simply be said to the agent? If so, there is no label, however much work it
is. No label means the issue can be picked up.

Three axes. `needs-mind` and `needs-hands` stand apart: each can sit next to any other label. On
the wait axis at most one label stands, and `blocked-issue` wins only over `blocked-external`.

| Axis | Label | Color | Binds when |
|---|---|---|---|
| think | `needs-mind` | `FBCA04` | The maintainer has to think: what and why are not sharp yet, or one choice by the maintainer blocks |
| wait | `blocked-issue` | `D93F0B` | Waits on another issue in this repository |
| wait | `blocked-external` | `D93F0B` | Waits on a third party, API, vendor or delivery |
| do | `needs-hands` | `FBCA04` | The maintainer must configure, enter, connect, purchase, run or test something, or judge whether the build does what was meant |

Amber means the ball is with the maintainer, red means it is outside.

## Areas

Area labels carry the `area:` prefix. This repository fills them in: one row per area.

| Label | Color | Rule of thumb |
|---|---|---|

<!-- | `area:docs` | `C4B5FD` | documentation for users | -->

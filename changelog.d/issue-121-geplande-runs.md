---
type: internal
---
Groundwork for scheduled runs (#121): `[[schedule]]` in `lighthouse.toml` is validated, the tick plans, starts and finishes a scheduled run through its own auto-merged PR, `/api/runs` and the page list scheduled runs by schedule name, and the tick sweep removes the workspace of a finished scheduled run even when it is dirty. Job `reflect` is not registered yet, so a schedule that names it only makes the tick log `no job <job>`.

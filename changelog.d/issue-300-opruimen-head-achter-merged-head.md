---
type: fixed
---
`lighthouse tick` now cleans up a workspace whose HEAD is behind the head of its merged PR, for
example after a fix pushed from another session, instead of keeping it as `unpushed commits`.
When that PR head cannot be fetched, the workspace stays as `not measurable`.

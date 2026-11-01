---
type: added
---
`lighthouse tick` parks an executing issue that has an active blocker (a `🤖 Geparkeerd` comment with `blocked_by`, then the board to On hold), finishes a half-done park on the next tick, and puts the issue back in Controller once no blocker is active; an On hold set by a human stays untouched. The overview (`lighthouse serve`, `/api/questions`, `/api/returned`) shows only Controller issues when the repo is in `host.toml [dispatch]` with a `[forge.board]`.

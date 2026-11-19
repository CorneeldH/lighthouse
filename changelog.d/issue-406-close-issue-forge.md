---
type: changed
---
`/lighthouse-close-issue`, `lighthouse unblock-dependents` and `lighthouse worktree-audit` now talk to the forge through `lighthouse forge` instead of `gh`, so they work on GitLab as well as GitHub; without an unblock lane (GitLab, or GitHub without a bot in `[ci]`) the session releases the waiting issues itself with `lighthouse unblock-dependents`. When `worktree-audit` or `unblock-dependents` cannot reach the forge, they now write one line to stderr instead of failing silently.

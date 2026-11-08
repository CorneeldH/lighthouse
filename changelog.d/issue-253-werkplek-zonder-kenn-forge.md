---
type: added
---
`lighthouse tick` can run without kenn-forge: with `[dispatch] workplace = "local"` in `host.toml`
each run gets a git worktree and a detached `claude -p` on the host. `deploy/systemd/` has a tick
unit and timer for it.

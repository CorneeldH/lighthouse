---
type: fixed
---
The worktree audit measures a lock pid without `/proc`, so it works on macOS; a pid it cannot measure
blocks removal. `docs/host-assumptions.md` lists every assumption about the host.

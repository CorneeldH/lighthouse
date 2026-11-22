---
type: added
---
De plek van de worktrees is instelbaar met `[worktree] root` in `lighthouse.toml` (standaard `.claude/worktrees`, relatief aan de hoofdcheckout of absoluut, ook buiten de repo): hooks, worktree-audit en skills lezen die plek, en `lighthouse init` waarschuwt voor een repo of worktree-root in een sync-map.

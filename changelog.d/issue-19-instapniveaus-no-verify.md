---
type: changed
---
`git commit --no-verify` (and `-n`) is now blocked by its own base hook `no-verify-check`, on every branch and at every level: it has no stand and no override, so it stays blocked when `commit-test-gate` or `main-commit-gate` is off. The `--no-verify` rule in the instruction block is its own line, so it stays at every level.

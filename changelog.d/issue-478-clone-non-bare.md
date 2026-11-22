---
type: fixed
---
A tick reports a kenn-forge clone that is no longer bare (`core.bare` not `true`, or `core.worktree` set) with the repair, and exits 1. Cleanup removes a workspace whose worktree directory is gone when its PR merged or its branch is nowhere, instead of keeping it as `not measurable`.

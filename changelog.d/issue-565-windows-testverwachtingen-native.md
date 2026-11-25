---
type: internal
---
The tests of `gitpath`, `main-commit-gate`, `worktree-check` and `worktree-audit` now expect the native path form on Windows, and pass shell commands forward-slash paths, so they no longer fail in `windows-measure` (#565).

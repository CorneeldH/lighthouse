---
type: internal
---
The tests of `worktree-audit` and of `ask` from a subdirectory resolve their temp dir, so they also pass on macOS, where `/var` is a symlink to `/private/var`.

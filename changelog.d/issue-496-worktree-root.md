---
type: added
---
The location of the worktrees can now be set with `[worktree] root` in `lighthouse.toml` (default `.claude/worktrees`, relative to the main checkout or absolute, also outside the repository). Hooks, the worktree audit and the skills read that location, and `lighthouse init` warns about a repository or worktree root inside a sync folder.

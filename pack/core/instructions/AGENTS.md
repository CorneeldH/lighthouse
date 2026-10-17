## Lighthouse: core

- Nothing lands directly on `main`. Work on a branch in a worktree; `main-commit-guard` blocks `git commit` and `git push` to `main`.
- Escape hatch, deliberately and once: `touch .claude/allow-main` in the repo root, and remove it afterwards.

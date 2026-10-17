@AGENTS.md

# Claude Code only

Shared rules live in `AGENTS.md` (imported above); this file holds only what Claude Code alone
knows. Never duplicate, in either direction.

## Hooks

Registered in `.claude/settings.json`:

- `main-commit-guard.sh` — PreToolUse `Bash`. Blocks (exit 2) a commit or push on `main`,
  also through `git -C <path>` and a preceding `cd`. When it cannot check, it blocks too.
- `worktree-guard.sh` — PreToolUse `Edit|Write`. Warns (never blocks) at a write into the main
  checkout on `main`. When it cannot check, it says so.

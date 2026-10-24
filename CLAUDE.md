@AGENTS.md

# Claude Code only

Shared rules live in `AGENTS.md` (imported above); this file holds only what Claude Code alone
knows. Never duplicate, in either direction.

## Hooks

`lighthouse init` installs them in `.claude/hooks/lighthouse/` and registers them in
`.claude/settings.json`; never edit either by hand, change `pack/core/hooks/` instead. When a
hook cannot check, it blocks with `COULD NOT CHECK`, or says so where it never blocks.

- `main-commit-guard.sh` — PreToolUse `Bash`. Blocks (exit 2) a commit or push on `main`,
  also through `git -C <path>` and a preceding `cd`.
- `pre-commit-tests.sh` — PreToolUse `Bash`. Runs `[test_gate] command` before every
  `git commit`; a red run blocks.
- `bash-write-gates.sh` — PreToolUse and PostToolUse `Bash`. Runs the write gates on what a
  Bash call wrote.
- `pr-create-gate.sh` — PreToolUse `Bash`. Blocks a `gh pr create` whose body lacks
  `Closes #N` with evidence, `Refs #N — <reason>` or `No issue: <reason>`.
- `worktree-guard.sh` — PreToolUse `Edit|Write`. Warns (never blocks) at a write into the main
  checkout on `main`.
- `worktree-linkcheck.sh` — PostToolUse `Edit|Write`. Blocks a markdown link that is absolute
  or points nowhere.

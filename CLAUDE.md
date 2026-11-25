@AGENTS.md

# Claude Code only

Shared rules live in `AGENTS.md` (imported above); this file holds only what Claude Code alone
knows. Never duplicate, in either direction.

## Hooks

`lighthouse init` installs them in `.claude/hooks/lighthouse/` and registers them in
`.claude/settings.json`. Never edit those scripts or their registrations by hand; change
`pack/core/hooks/` instead. Other entries in `.claude/settings.json` are the project's. When a
hook cannot check, it blocks with `COULD NOT CHECK`, or says so where it never blocks.

- `main-commit-gate` (`run.sh main-commit-gate`) — PreToolUse `Bash`. Blocks (exit 2) a commit or
  push on `main`, also through `git -C <path>` and a preceding `cd`.
- `commit-test-gate` (`run.sh commit-test-gate pre|post`) — PreToolUse, PostToolUse and
  PostToolUseFailure `Bash`. `pre` tests a commit to `main` before it, and still blocks
  `--no-verify` in a repo whose `blocking` list has no `no-verify-check` yet (an older pack);
  `post` runs `[test_gate] command` on the files of the commit and reports red.
- `no-verify-check` (`run.sh no-verify-check`) — PreToolUse `Bash`. A base hook (no stand, no override):
  blocks `git commit --no-verify` and `-n` on every branch and at every level.
- `level-check` (`run.sh level-check`) — SessionStart. Never blocks; warns for each core gate
  (`main-commit-gate`, `worktree-check`, `pr-create-gate`) whose stand is not `on`.
- `bash-write-check` (`run.sh bash-write-check`) — PreToolUse, PostToolUse and PostToolUseFailure `Bash`. Runs the
  write checks on what a Bash call wrote.
- `pr-create-gate` (`run.sh pr-create-gate`) — PreToolUse `Bash`. Blocks a `gh pr create` whose body lacks
  `Closes #N` with evidence, `Refs #N — <reason>` or `No issue: <reason>`.
- `workflow-push-gate` (`run.sh workflow-push-gate`) — PreToolUse `Bash`. Only in a controller run: blocks
  a `git push` whose range (`origin/<branch>..HEAD`, or from the merge-base when the branch is not on
  origin) touches `.github/workflows/` itself (what is on `origin/<default>` already, such as a merge of main,
  does not count); the workflow pusher pushes that (`next: push-workflows`).
- `telemetry-collect-check` (`run.sh telemetry-collect-check`) — SessionStart. A base hook (no stand, no override): notes the
  lighthouse and pack versions of the session in a sidecar file in the state directory and starts
  `lighthouse telemetry collect --since-last` detached, except in a controller run or with
  `LIGHTHOUSE_TELEMETRY_COLLECT=off`. Never blocks and opens no database; an error is one line in
  `<state>/telemetry/hook.log`.
- `worktree-check` (`run.sh worktree-check`) — PreToolUse `Edit|Write`. Warns (never blocks) at a
  write into the main checkout on `main`.
- `md-link-check` (`run.sh md-link-check`) — PostToolUse `Edit|Write`. Blocks a markdown link that is absolute
  or points nowhere.
- `permission-check` (`run.sh permission-check`) — Notification `permission_prompt`. Notes the prompt in
  `<LIGHTHOUSE_STATE_DIR>/runs/<run>/permission.json`, only inside a controller run, so the dispatcher can
  tell a session waiting for a permission from a silent one. Never blocks; says `COULD NOT CHECK` when it
  cannot write.
- `usage-gate` (`run.sh usage-gate`) — UserPromptSubmit, PostToolUse, SubagentStop and Stop. Reads only the alarm
  of `lighthouse usage`: warns at a prompt, blocks once per window in `tool` and `stop`, once per
  subagent in `subagent`, never fails red. See `docs/usage-gate.md`.

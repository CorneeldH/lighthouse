---
type: changed
---
`/lighthouse-close-issue`, `lighthouse unblock-dependents` and `lighthouse worktree-audit` now read and write issues and pull requests through `lighthouse forge` instead of `gh`, so they work on GitLab as well as GitHub; checking workflow runs and the release flow still use `gh`. Without an unblock lane (GitLab, or GitHub without a bot in `[ci]`) the session releases the waiting issues itself with `lighthouse unblock-dependents`, which reports `released issues not read` with exit 1 when it cannot open the forge. When `worktree-audit` cannot read the PR status from the forge, it now writes one line to stderr instead of staying silent.

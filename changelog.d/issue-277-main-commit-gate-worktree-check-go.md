---
type: changed
---
`main-commit-gate` and `worktree-check` run in the `lighthouse` binary through `.claude/hooks/lighthouse/run.sh`. They no longer need `python3` or `jq`. Update the binary on your PATH before `lighthouse init --upgrade`: an older binary does not know these hooks, and `main-commit-gate` then blocks every Bash call.

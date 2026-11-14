---
type: changed
---
The skill scripts `board-start`, `post-artefact`, `unblock-dependents` and `worktree-audit` are now commands of the `lighthouse` binary (`lighthouse board-start`, and so on) and need no `python3` for them any more; the unblock workflow downloads the binary instead of installing `uv`. Update the binary on your PATH before `lighthouse init --upgrade`: an older binary does not know these commands.

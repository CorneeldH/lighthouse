---
type: changed
---
The skill scripts `board-start`, `post-artefact`, `unblock-dependents` and `worktree-audit` are now commands of the `lighthouse` binary (`lighthouse board-start`, and so on) and need no `python3` for them any more; the unblock workflow downloads the binary instead of installing `uv`. Update the binary on your PATH before `lighthouse init --upgrade`: an older binary does not know these commands.

A `dev` manifest outside the lighthouse repository (for example a binary built by `go install @main`) now makes the unblock-dependents workflow fail, where the Python version worked. `lighthouse init --upgrade` removes `.lighthouse/lib/lighthouse_lib/{protocol,artefacts,pr_body_gate,git_invocations,repo_identity}.py`: that matters for any project script that imports one of them.

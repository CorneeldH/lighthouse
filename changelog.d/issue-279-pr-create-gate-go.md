---
type: changed
---
`pr-create-gate` runs in the `lighthouse` binary through `.claude/hooks/lighthouse/run.sh` and no longer needs `python3` or `jq`, and the skills `write-pr` and `close-issue` check and run Evidence rows with `lighthouse evidence check|run` and check a PR body outside Claude Code with `lighthouse pr-body-gate`, in place of `evidence.py` and `pr_body_gate.py`. Update the binary on your PATH before `lighthouse init --upgrade`: an older binary does not know these commands, and `pr-create-gate` then blocks every Bash call.

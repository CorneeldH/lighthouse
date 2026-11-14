---
type: changed
---
`commit-test-gate` runs in the `lighthouse` binary through `.claude/hooks/lighthouse/run.sh`, and needs no `python3` or `jq` any more. Gate slots now also work on Windows. Update the binary on your PATH before `lighthouse init --upgrade`: an older binary does not know this hook.

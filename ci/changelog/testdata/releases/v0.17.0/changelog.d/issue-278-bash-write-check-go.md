---
type: changed
---
`bash-write-check` runs in the `lighthouse` binary through `.claude/hooks/lighthouse/run.sh`, and `go-lint-gate` and `ty-gate` find the checkouts of a turn with `lighthouse turn-roots`. Update the binary on your PATH before `lighthouse init --upgrade`: an older binary does not know these commands.

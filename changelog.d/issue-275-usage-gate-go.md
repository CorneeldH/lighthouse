---
type: added
---
`lighthouse usage alarm` prints the valid usage alarm and replaces `alarm.sh`. The hooks `usage-gate`, `permission-check` and `md-link-check` now run through `run.sh <name>` on the lighthouse binary with the same verdicts as before, and the stop gates of the `go` and `python` packs read the alarm through the binary.

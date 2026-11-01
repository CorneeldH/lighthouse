---
type: added
---
`lighthouse triage` routes the candidate issues of every repo with `[controller.triage]` through one read-only `claude -p` judgment each, writes a triage comment with the route line (and design questions at gate 4c), and moves an issue on gate 4a, 4b or 2 to `Controller` with a `🤖 Handoff` that waits for the night window. `--dry-run` judges without writing.

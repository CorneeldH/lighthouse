---
type: added
---
New command `lighthouse state`: `show <nr>` prints the state of an issue (done, active, needs human, blocked or ready), `label <nr> <key>` sets a blocking label with the rank rule (the lowest rank wins, the others go), and `return <nr> <key>` also moves the issue to `To do` on the board, leaving its Priority alone. Triage gate 0 now uses the same rules.

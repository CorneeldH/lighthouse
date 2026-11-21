---
type: fixed
---
`lighthouse tick` writes the run view after apply: a run the tick starts shows in `GET /api/runs` and *Running runs* after that same tick, and a run it closes is gone after that same tick, instead of one tick later. The tick timer in `deploy/systemd/lighthouse-tick.timer` now starts the tick every 3 minutes instead of every 5; hosts that copied the unit copy it again.

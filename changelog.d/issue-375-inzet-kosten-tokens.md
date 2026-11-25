---
type: added
---
`lighthouse telemetry collect` reads the Claude Code transcripts of a repo into `lighthouse.db`: the setup (model, effort, versions, lane) and the tokens with an estimated cost per model, from `internal/telemetry/prices.toml`. A SessionStart hook, `telemetry-collect-check`, starts it per session, and `tick` collects the runs at most hourly (#375).

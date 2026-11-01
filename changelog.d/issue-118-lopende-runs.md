---
type: added
---
The web UI has a *Running runs* card, fed by the new read-only `GET /api/runs`: the open and parked runs with their command, trigger, profile, workspace and measured stand, and the deferred starts with their reason. It reads the state the tick writes every tick, so it costs no forge calls. With `[dispatch] repos` in `host.toml`, `serve` now shows every repo listed there instead of the one `--repo`, and every question, returned issue and run carries its `repo`, shown as `owner/name#number`.

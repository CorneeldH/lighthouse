---
type: added
---
`lighthouse tick` runs as its own process next to `lighthouse serve`: it reads `[dispatch]` from `host.toml`, claims runs in the shared SQLite run state and reports every repository it skips, with the reason.

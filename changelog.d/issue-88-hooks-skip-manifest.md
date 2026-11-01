---
type: fixed
---
`bash-write-check` no longer hands a file that lighthouse itself installed (a file unit in `.lighthouse/manifest.toml`) to its gates, so `ruff format` or `gofmt` no longer rewrites it when `lighthouse init` runs in an agent's Bash call, and `lighthouse init --upgrade` afterwards says `No changes.`

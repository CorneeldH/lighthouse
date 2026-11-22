---
type: added
---
`lighthouse init` sets `"language": "dutch"` in `.claude/settings.json` when `[project] language` is `nl`, so Claude Code replies in Dutch; a `language` that is already there stays, with a warning. The core block now also puts chat replies in `[project] language`, except with `en`.

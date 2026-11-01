---
type: fixed
---
Triage now judges an issue in a temporary export of the tracked content of the default branch, so it no longer reads untracked files, ignored files or other worktrees. A free-text field that is too long, or a `redundancy_evidence` field with prose instead of references, now makes the judgment invalid.
The judgment no longer loads Claude Code settings sources: only `env` and `modelSettings` are taken from the user settings file, so auth that lives only there (for example `apiKeyHelper`) no longer reaches it.

---
type: added
---
`lighthouse lint check` checks per language pack that the linter is really wired: the version is pinned, the config keeps the blocking rules of the linter hook on, and a CI step runs the linter so that a finding turns CI red. `lighthouse init` runs it after installing and lists what is missing, without failing. See `docs/lint-check.md`.

---
type: added
---
`lighthouse uninstall` removes what `lighthouse init` installed: the units of the manifest, and what init created (files, sections, keys), after a diff and approval (`--yes`, `--force`). Seeded project files, `.lighthouse/run`, forge labels and `host.toml` stay and are reported. The record of what init created is a new file, `created.toml`, which an older binary ignores: what an `--upgrade` with an older binary creates is not recorded, so it stays on uninstall and is reported (#91).

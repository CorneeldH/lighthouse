---
type: added
---
`lighthouse init` now knows setup levels (`light`, `standard`, `strict`): `--level` chooses what gets installed, `level` in `lighthouse.toml` and `.lighthouse/components.toml` record the choice, and `[components]` switches single components on or off; the requirements of switched-off components no longer count. A first `init` in a new repository chooses `light`, and an existing repository without `level` behaves as `strict`. `init` now also fails on an invalid `lighthouse.toml`, because an existing file is read with `config.Load`.

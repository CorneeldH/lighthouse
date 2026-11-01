---
type: changed
---
The configuration page in `docs/site/config.html` reads as a reference now: first where `lighthouse.toml` and `host.toml` live, then both files in full with a comment and the default of every key, and per table one row per key with its level, default, what it does and what reads it. It replaces the key buttons, the level filter and the drawer. A test fails when the files on the page stop passing `config.py`.

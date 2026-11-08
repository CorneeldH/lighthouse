---
type: added
---
`lighthouse config`, `lighthouse headings` and `lighthouse protocol` replace the Python config readers in skills: `lighthouse config <key>`, `lighthouse config --json <key>` and `lighthouse config host-check` read `lighthouse.toml` and `host.toml` with the exit codes and output of `config.py`. Hooks can run through `.claude/hooks/lighthouse/run.sh`.

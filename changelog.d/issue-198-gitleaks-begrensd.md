---
type: internal
---
The pre-push secret scan of Entire checkpoints now scans each unique blob once, in batches, with a ledger of blobs already found clean, and runs every gitleaks invocation in a systemd scope with a memory ceiling, no swap and a CPU quota (`host.toml` keys `scan_memory_max` and `scan_cpu_quota`). `tools/hooks/checkpoint_scan.py push` scans and pushes the first set of checkpoints, holding dirty ones back in `held.txt`; see `docs/secret-scan.md`.

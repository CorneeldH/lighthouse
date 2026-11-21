---
type: changed
---
`plan-review-record` now writes the review record on every machine; the host.toml restriction and exit code 3 are gone.
The review record is now schema 6 and carries a `trajectory` (repo and issue, or `null`), taken from `--issue` or the issue branch and `forge.repo`.
The new `lighthouse review-index [--ledger <path>]... [--issue <n>]` links the findings of plan and spec review records, and the rulings of an SDD ledger, to their trajectory in `lighthouse.db`; `plan-review-record write` runs the same indexing after writing (a failure there is a warning).

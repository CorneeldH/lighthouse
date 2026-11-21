---
type: changed
---
`plan-review-record` now writes the review record on every machine; the host.toml restriction and exit code 3 are gone.
The review record is now schema 6 and carries a `trajectory` (repo and issue, or `null`), taken from `--issue` or the issue branch and `forge.repo`.

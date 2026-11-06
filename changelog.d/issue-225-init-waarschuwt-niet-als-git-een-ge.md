---
type: fixed
---
`lighthouse init` and `init --upgrade` now warn when git ignores a file they install, for example `.lighthouse/lib/` under a `lib/` rule, with the rule that ignores it. Such a file works locally but never reaches a commit.

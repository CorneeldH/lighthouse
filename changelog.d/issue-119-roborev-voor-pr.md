---
type: added
---
`/lighthouse-write-pr` runs a local roborev (Codex) review before the PR is opened when `[review] roborev = "local"` is set, processes the findings and records them in a PR comment. The dispatcher starts `/lighthouse-fix-pr` on a red PR (trigger `fixpr`), at most 3 runs per PR; after that the run returns with "PR red after 3 fix-pr runs".

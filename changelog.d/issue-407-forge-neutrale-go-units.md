---
type: changed
---
`post-artefact`, `labels` (and so `init --create-labels`) and `worktree-audit` no longer call `gh` but read and write through the forge layer, so they work on GitLab too. New forge methods `CommitKnown`, `CreateLabel`, `PullHeads` and `IssueClosingHeads`, and a new command `lighthouse forge label create`. `lighthouse init` no longer writes the GitHub workflows on a GitLab repo; the workflows keep `gh`, recorded as an exception in `lighthouse-forge`.

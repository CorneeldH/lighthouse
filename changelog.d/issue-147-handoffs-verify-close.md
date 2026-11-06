---
type: changed
---
`/lighthouse-write-pr` now always links the issue with `Refs #N`, with an Evidence table of the proven criteria, and `/lighthouse-close-issue` runs that table after the merge and closes the issue. A controller run that finishes a build pushes and hands the PR to a fresh `/lighthouse-write-pr` session, and the controller starts the close check after every merged PR it opened.

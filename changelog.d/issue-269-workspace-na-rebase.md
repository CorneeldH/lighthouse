---
type: fixed
---
A reused forge workspace follows `origin/<branch>` after the branch was rebased and force-pushed:
commits that are on origin as a patch-equal commit no longer count as own work. A workspace with real
own work or a dirty work tree no longer starts the run silently on its old `HEAD`; the dispatcher hands
the run back with the reason, the workspace and the number of own commits.

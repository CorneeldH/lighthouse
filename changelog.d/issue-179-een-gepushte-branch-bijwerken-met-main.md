---
type: changed
---
The shared instructions now say a branch that is already pushed is updated with `origin/<default branch>` by `git merge`, never by rebase plus force-push. `lighthouse-write-handoff` and `lighthouse-fix-pr` follow that rule.

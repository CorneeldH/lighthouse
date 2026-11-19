---
type: fixed
---
The close check after a merged `Refs` PR now runs on its own branch `closecheck/issue-<n>-pr-<pr>`, made from `origin/<default branch>`, so it follows the skills of the default branch instead of a stale issue branch.

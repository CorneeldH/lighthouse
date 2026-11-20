---
type: changed
---
The skills fix-pr, write-handoff, review-plan and upgrade no longer call `gh` directly but go through `lighthouse forge`, so they work on GitLab too, and `requires.toml` names `gh` only per forge. New commands: `lighthouse releases`, `lighthouse forge pr checks`, `forge pr view --comments` and the read form `forge issue status <nr>`.

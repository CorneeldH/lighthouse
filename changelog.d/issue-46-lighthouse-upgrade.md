---
type: added
---
`/lighthouse-upgrade` brings a repository to the version of the installed `lighthouse` binary: it offers `lighthouse self-update` when a newer release exists, shows the release notes in between, runs `lighthouse init --upgrade` in a worktree, checks the test gate, `check-instructions.sh` and `No changes.`, and commits only what the upgrade wrote. Push and PR follow only after your approval.

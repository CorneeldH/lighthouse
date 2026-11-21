---
type: internal
---
Go tests no longer commit their fixture repositories into the real repository when `GIT_DIR` is set in the environment (for example `go test` run from a git hook). `internal/gitenvscrub` removes the git variables that redirect a command to another repository; every test package that starts git imports it, and a guard test reports a package that forgets to.

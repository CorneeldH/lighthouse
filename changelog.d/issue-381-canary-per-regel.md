---
type: changed
---
Core hook blocks now carry the reason as `[<hook>/<id>]`. The go and shell hooks block a configuration that switches off a floor rule (the linters `errcheck`, `govet`, `ineffassign`, `staticcheck` and `unused`, and the shellcheck codes SC2164, SC2046, SC2115, SC2155 and SC2034), and name the route for existing debt: `issues.new-from-rev` for Go, a motivated `# shellcheck disable=SCxxxx <reason>` for shell.

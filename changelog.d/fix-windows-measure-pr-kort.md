---
type: internal
---
`windows-measure` splits in two jobs. `hooks` runs `ci/windows-measure.sh` on every PR that touches `cmd/`, `internal/`, `pack/core/` or `go.mod`, takes about two minutes, and turns red on a failed row. `tests` runs the Go and e2e tests on Windows only after a merge to `main` or by hand, and stays a measurement (#559). The script now inits at level `strict`, since a first init picks `light` without `commit-test-gate`, and the source checkout keeps LF so `lighthouse.exe` embeds the pack as the Linux release does.

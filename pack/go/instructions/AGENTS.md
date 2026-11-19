## Lighthouse: go

- Go code follows `/lighthouse-go-standard`.
- Two hooks check it: `gofmt-check` after every edit (formats; blocks only on an unparsable file), `go-lint-gate` at Stop (`golangci-lint`, `govet` included; blocks on a finding, not during a usage alarm).
- The linters of the floor (`errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`) stay on whatever `.golangci.yml` says; a block tags the linter. Existing debt goes behind `issues.new-from-rev`, not behind a linter that is off.

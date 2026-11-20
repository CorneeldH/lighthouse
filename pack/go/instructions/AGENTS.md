## Lighthouse: go

- Go code follows `/lighthouse-go-standard`.
- Two hooks check it: `gofmt-check` after every edit (formats; blocks only on an unparsable file), `go-lint-gate` at Stop (`golangci-lint`, `govet` included; blocks on a finding, not during a usage alarm).
- Before a commit, run `lighthouse imports check` next to `[test_gate] command`; the `imports-gate-go` hook runs it at Stop (`[imports] check = false` turns it off).

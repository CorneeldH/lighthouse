## Lighthouse: go

- Go code follows `/lighthouse-go-standard`.
- Two hooks check it: `gofmt-check` after every edit (formats; blocks only on an unparsable file), `go-lint-gate` at Stop (`golangci-lint`, `govet` included; blocks on a finding, not during a usage alarm).

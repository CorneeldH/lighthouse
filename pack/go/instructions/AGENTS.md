## Lighthouse: go

- Go code follows `/lighthouse-go-writing`.
- Two gates check it: `gofmt` after every edit (formats; blocks only on an unparsable file), `go-check` at Stop (`golangci-lint`, `govet` included; blocks on a finding, not during a usage alarm).

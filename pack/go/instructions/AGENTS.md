## Lighthouse: go

- Go code follows `/lighthouse-go-writing`.
- Two gates check it: `gofmt` after every edit (it formats, and blocks only on a file that does not parse), `go-check` when the session stops (`golangci-lint`, `govet` included; it blocks on a finding).

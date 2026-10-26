// Empty module: stops ./... (go build, go test, golangci-lint) at web/, so the .go files
// some npm packages ship in web/node_modules are never compiled.
module github.com/CorneeldH/lighthouse/web

go 1.26.4

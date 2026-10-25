// Package server builds the HTTP handler of lighthouse serve.
package server

import (
	"context"
	"io/fs"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/CorneeldH/lighthouse/internal/version"
)

type healthOutput struct {
	Body struct {
		Version string `json:"version"`
	}
}

// New returns the handler: the JSON API under /api/ and site as SPA elsewhere.
func New(site fs.FS) http.Handler {
	mux := http.NewServeMux()
	cfg := huma.DefaultConfig("lighthouse", version.String())
	cfg.OpenAPIPath = "/api/openapi"
	cfg.DocsPath = "/api/docs"
	cfg.SchemasPath = "/api/schemas"
	cfg.CreateHooks = nil // no "$schema" field in response bodies
	api := humago.New(mux, cfg)

	huma.Get(api, "/api/health", func(context.Context, *struct{}) (*healthOutput, error) {
		out := &healthOutput{}
		out.Body.Version = version.String()
		return out, nil
	}, func(op *huma.Operation) { op.OperationID = "get-health" })

	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", spa(site))
	return mux
}

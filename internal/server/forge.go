package server

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/overview"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

type questionsOutput struct {
	Body struct {
		Questions []overview.Question `json:"questions"`
	}
}

type returnedOutput struct {
	Body struct {
		Returned []overview.Returned `json:"returned"`
	}
}

type cleanupOutput struct {
	Body struct {
		Cleanup []workplace.Leftover `json:"cleanup"`
	}
}

// registerCleanup adds the endpoint that lists the workspaces cleanup left behind.
func registerCleanup(api huma.API, leftovers func(context.Context) ([]workplace.Leftover, error)) {
	huma.Get(api, "/api/cleanup", func(ctx context.Context, _ *struct{}) (*cleanupOutput, error) {
		list, err := leftovers(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if list == nil {
			list = []workplace.Leftover{}
		}
		out := &cleanupOutput{}
		out.Body.Cleanup = list
		return out, nil
	}, func(op *huma.Operation) { op.OperationID = "get-cleanup" })
}

// registerForge adds the endpoints that read the forge through src.
func registerForge(api huma.API, src overview.Source) {
	huma.Get(api, "/api/questions", func(ctx context.Context, _ *struct{}) (*questionsOutput, error) {
		o, err := overview.Build(ctx, src)
		if err != nil {
			return nil, forgeError(err)
		}
		out := &questionsOutput{}
		out.Body.Questions = o.Questions
		return out, nil
	}, func(op *huma.Operation) { op.OperationID = "get-questions" })

	huma.Get(api, "/api/returned", func(ctx context.Context, _ *struct{}) (*returnedOutput, error) {
		o, err := overview.Build(ctx, src)
		if err != nil {
			return nil, forgeError(err)
		}
		out := &returnedOutput{}
		out.Body.Returned = o.Returned
		return out, nil
	}, func(op *huma.Operation) { op.OperationID = "get-returned" })
}

// forgeError maps a failed build to 503 when the forge is not configured, 502 otherwise.
func forgeError(err error) error {
	if errors.Is(err, forge.ErrNotConfigured) {
		return huma.Error503ServiceUnavailable(err.Error())
	}
	return huma.Error502BadGateway(err.Error())
}

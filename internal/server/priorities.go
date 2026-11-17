package server

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/priorities"
)

type prioritiesOutput struct {
	Body priorities.Result
}

// registerPriorities adds the endpoint that serves the prioritisation lists and, next to
// them, the runs the view marks beside an issue. A failed build maps as forgeError does and
// touches no other endpoint; the runs come from the local tick state, so a failed read of
// those is a 500.
func registerPriorities(api huma.API, read func(context.Context) ([]priorities.Repo, error),
	runs func(context.Context) ([]dispatch.RunView, []dispatch.DeferredView, error)) {
	huma.Get(api, "/api/priorities", func(ctx context.Context, _ *struct{}) (*prioritiesOutput, error) {
		repos, err := read(ctx)
		if err != nil {
			return nil, forgeError(err)
		}
		views, _, err := runs(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		out := &prioritiesOutput{}
		out.Body.Repos = repos
		if out.Body.Repos == nil {
			out.Body.Repos = []priorities.Repo{}
		}
		out.Body.Runs = make([]priorities.Run, 0, len(views))
		for _, v := range views {
			out.Body.Runs = append(out.Body.Runs, priorities.Run{Repo: v.Repo, Issue: v.Issue, State: v.State, Phase: v.Phase})
		}
		return out, nil
	}, func(op *huma.Operation) { op.OperationID = "get-priorities" })
}

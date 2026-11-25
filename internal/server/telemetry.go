package server

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/CorneeldH/lighthouse/internal/telemetry/report"
)

// TelemetryReader serves the telemetry reports; report.Reader is the production one.
type TelemetryReader interface {
	Components(ctx context.Context, f report.Filter) (report.ComponentsReport, error)
	Timeline(ctx context.Context, f report.Filter) (report.TimelineReport, error)
	Trajectories(ctx context.Context, f report.Filter) ([]report.ListRow, error)
	// Detail is false when trajectory id does not exist.
	Detail(ctx context.Context, id int64) (report.Detail, bool, error)
	Coverage(ctx context.Context, repo string) (report.CoverageReport, error)
}

// telemetryFilter is the filter set every comparison endpoint takes.
type telemetryFilter struct {
	Repo    string `query:"repo" doc:"repository slug owner/name; empty is every repo"`
	Route   string `query:"route" doc:"triage route"`
	Size    string `query:"size" doc:"plan size class"`
	PRSize  string `query:"pr_size" doc:"PR size class"`
	Area    string `query:"area" doc:"area label"`
	Model   string `query:"model" doc:"dominant model"`
	Harness string `query:"harness" doc:"harness"`
}

func (f telemetryFilter) filter() report.Filter {
	return report.Filter{Repo: f.Repo, Route: f.Route, Size: f.Size, PRSize: f.PRSize,
		Area: f.Area, Model: f.Model, Harness: f.Harness}
}

type telemetryRepo struct {
	Repo string `query:"repo" doc:"repository slug owner/name; empty is every repo"`
}

// telemetryTrajectoriesInput repeats the fields of telemetryFilter: huma skips an
// unexported embedded struct.
type telemetryTrajectoriesInput struct {
	Repo      string `query:"repo" doc:"repository slug owner/name; empty is every repo"`
	Route     string `query:"route" doc:"triage route"`
	Size      string `query:"size" doc:"plan size class"`
	PRSize    string `query:"pr_size" doc:"PR size class"`
	Area      string `query:"area" doc:"area label"`
	Model     string `query:"model" doc:"dominant model"`
	Harness   string `query:"harness" doc:"harness"`
	Component string `query:"component" doc:"keep only trajectories in which this component ran"`
}

type telemetryDetailInput struct {
	ID int64 `path:"id" doc:"trajectory id"`
}

type telemetryComponentsOutput struct{ Body report.ComponentsReport }

type telemetryTimelineOutput struct{ Body report.TimelineReport }

type telemetryTrajectoriesOutput struct {
	Body struct {
		Trajectories []report.ListRow `json:"trajectories"`
	}
}

type telemetryDetailOutput struct{ Body report.Detail }

type telemetryCoverageOutput struct{ Body report.CoverageReport }

// orEmpty returns s, or an empty slice when s is nil, so JSON shows [] and never null.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// registerTelemetry adds the read-only telemetry endpoints. A read error is a 500; an
// unknown trajectory a 404.
func registerTelemetry(api huma.API, r TelemetryReader) {
	readErr := func(err error) error { return huma.Error500InternalServerError(err.Error()) }

	huma.Get(api, "/api/telemetry/components", func(ctx context.Context, in *telemetryFilter) (*telemetryComponentsOutput, error) {
		rep, err := r.Components(ctx, in.filter())
		if err != nil {
			return nil, readErr(err)
		}
		rep.Blocks = orEmpty(rep.Blocks)
		o := &rep.Options
		o.Repos, o.Routes, o.Sizes, o.PRSizes = orEmpty(o.Repos), orEmpty(o.Routes), orEmpty(o.Sizes), orEmpty(o.PRSizes)
		o.Areas, o.Models, o.Harnesses = orEmpty(o.Areas), orEmpty(o.Models), orEmpty(o.Harnesses)
		return &telemetryComponentsOutput{Body: rep}, nil
	}, func(op *huma.Operation) { op.OperationID = "get-telemetry-components" })

	huma.Get(api, "/api/telemetry/timeline", func(ctx context.Context, in *telemetryRepo) (*telemetryTimelineOutput, error) {
		rep, err := r.Timeline(ctx, report.Filter{Repo: in.Repo})
		if err != nil {
			return nil, readErr(err)
		}
		rep.Weeks, rep.Markers = orEmpty(rep.Weeks), orEmpty(rep.Markers)
		return &telemetryTimelineOutput{Body: rep}, nil
	}, func(op *huma.Operation) { op.OperationID = "get-telemetry-timeline" })

	huma.Get(api, "/api/telemetry/trajectories", func(ctx context.Context, in *telemetryTrajectoriesInput) (*telemetryTrajectoriesOutput, error) {
		f := report.Filter{Repo: in.Repo, Route: in.Route, Size: in.Size, PRSize: in.PRSize,
			Area: in.Area, Model: in.Model, Harness: in.Harness, Component: in.Component}
		rows, err := r.Trajectories(ctx, f)
		if err != nil {
			return nil, readErr(err)
		}
		out := &telemetryTrajectoriesOutput{}
		out.Body.Trajectories = orEmpty(rows)
		for i := range out.Body.Trajectories {
			out.Body.Trajectories[i].Components = orEmpty(out.Body.Trajectories[i].Components)
		}
		return out, nil
	}, func(op *huma.Operation) { op.OperationID = "get-telemetry-trajectories" })

	huma.Get(api, "/api/telemetry/trajectories/{id}", func(ctx context.Context, in *telemetryDetailInput) (*telemetryDetailOutput, error) {
		d, ok, err := r.Detail(ctx, in.ID)
		if err != nil {
			return nil, readErr(err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no trajectory with this id")
		}
		d.Runs, d.Sessions, d.Reviews = orEmpty(d.Runs), orEmpty(d.Sessions), orEmpty(d.Reviews)
		d.Findings, d.Recoveries, d.Events = orEmpty(d.Findings), orEmpty(d.Recoveries), orEmpty(d.Events)
		if d.ByCause == nil {
			d.ByCause = map[string]*float64{}
		}
		return &telemetryDetailOutput{Body: d}, nil
	}, func(op *huma.Operation) { op.OperationID = "get-telemetry-trajectory" })

	huma.Get(api, "/api/telemetry/coverage", func(ctx context.Context, in *telemetryRepo) (*telemetryCoverageOutput, error) {
		rep, err := r.Coverage(ctx, in.Repo)
		if err != nil {
			return nil, readErr(err)
		}
		rep.Lines = orEmpty(rep.Lines)
		return &telemetryCoverageOutput{Body: rep}, nil
	}, func(op *huma.Operation) { op.OperationID = "get-telemetry-coverage" })
}

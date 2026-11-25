package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/telemetry/report"
)

// TestSeedFeedsTheTab checks that the seeded database gives what the e2e tests
// assert: a closed trajectory in the "with" group of plan_review under the
// correlation label, with a priced cost.
func TestSeedFeedsTheTab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l.db")
	var stderr bytes.Buffer
	if code := run([]string{path}, &stderr); code != 0 {
		t.Fatalf("run = %d, stderr %q", code, stderr.String())
	}

	ctx := context.Background()
	s, err := store.OpenReadOnly(ctx, path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	d, err := report.Load(ctx, s.DB, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(d.Trajectories) != 1 || !d.Trajectories[0].Closed {
		t.Fatalf("trajectories = %+v, want one closed", d.Trajectories)
	}

	r := report.Components(d, report.Filter{})
	var plan *report.Block
	for i := range r.Blocks {
		if r.Blocks[i].Name == "plan_review" {
			plan = &r.Blocks[i]
		}
	}
	if plan == nil {
		t.Fatal("no plan_review block")
	}
	if plan.Label != report.LabelCorrelation || plan.Reason != report.ReasonReviewCosts {
		t.Errorf("plan_review label %q reason %q", plan.Label, plan.Reason)
	}
	with := plan.Groups[0]
	if with.Name != report.PlaceWith || with.Reviews != 1 || with.Found.Value == nil || *with.Found.Value != 1 {
		t.Errorf("with group = %+v, want one review that found something", with)
	}
	if with.Cost.Value == nil || *with.Cost.Value != 1.25 || with.CostKind != "estimate" {
		t.Errorf("with cost = %+v %q, want 1.25 estimate", with.Cost, with.CostKind)
	}
}

func TestRunNeedsOneArgument(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(nil, &stderr); code != 2 {
		t.Errorf("run() = %d, want 2", code)
	}
}

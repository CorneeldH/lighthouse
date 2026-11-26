package measurerecovery_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/scheduled/measurerecovery"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/telemetry/recovery/recoverytest"
)

// Classification: config (params days) and io (git, SQLite, fake forge). These tests fail if the
// job accepts days 0 or an unknown key, ignores days or its default of 30, gates a slot away,
// stops running the recovery measurement on the served repo, or runs without a forge.

func bind(t *testing.T, params map[string]any) (scheduled.Direct, error) {
	t.Helper()
	reg, ok := scheduled.Lookup(measurerecovery.Name)
	if !ok {
		t.Fatal("measure-recovery is not registered")
	}
	j, err := scheduled.Bind(reg, repoconfig.Schedule{Name: "recovery", Job: measurerecovery.Name, Params: params})
	if err != nil {
		return nil, err
	}
	d, ok := j.(scheduled.Direct)
	if !ok {
		t.Fatalf("bound job %T is no scheduled.Direct", j)
	}
	return d, nil
}

func TestMeasureRecoveryParams(t *testing.T) {
	for name, tt := range map[string]struct {
		params map[string]any
		want   string // "" valid, else part of the error
	}{
		"none":     {nil, ""},
		"days 45":  {map[string]any{"days": int64(45)}, ""},
		"days 0":   {map[string]any{"days": int64(0)}, "params days: must be between 1 and 3650"},
		"negative": {map[string]any{"days": int64(-3)}, "params days: must be between 1 and 3650"},
		"too big":  {map[string]any{"days": int64(3651)}, "params days: must be between 1 and 3650"},
		"string":   {map[string]any{"days": "30"}, "params days: must be a whole number"},
		"unknown":  {map[string]any{"dayz": int64(3)}, `params: unknown key "dayz"`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := bind(t, tt.params)
			switch {
			case tt.want == "" && err != nil:
				t.Fatalf("Bind = %v, want valid", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Fatalf("Bind = %v, want an error with %q", err, tt.want)
			}
		})
	}
}

func TestMeasureRecoveryGateNeverSkips(t *testing.T) {
	j, err := bind(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	v, err := j.Gate(context.Background(), scheduled.Env{})
	if err != nil || v != (scheduled.Verdict{}) {
		t.Errorf("Gate = %+v, %v; want an empty verdict", v, err)
	}
}

// runOn runs the job bound with params on the rig's clone and returns the rig.
func runOn(t *testing.T, params map[string]any) (*recoverytest.Rig, error) {
	t.Helper()
	r := recoverytest.NewRig(t)
	j, err := bind(t, params)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := repoconfig.Load(r.Clone)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.OpenShared(context.Background(), r.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	return r, j.Run(context.Background(), st.DB, scheduled.Env{Repo: repo, Now: recoverytest.After, Forge: r.Fake})
}

func TestMeasureRecoveryRun(t *testing.T) {
	r, err := runOn(t, map[string]any{"days": int64(45)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The rows the CLI writes for the rig: #7 recovered by its fix, on the trajectory of issue 3.
	if got := r.Text(`SELECT t.issue || ' ' || r.pr || ' ' || r.status FROM recoveries r JOIN trajectories t ON t.id = r.trajectory_id`); got != "3 7 recovered" {
		t.Errorf("recovery row = %q, want 3 7 recovered", got)
	}
	if n := r.Count(`SELECT count(*) FROM recoveries`); n != 1 {
		t.Errorf("recoveries = %d, want 1", n)
	}
	if got := r.Text(`SELECT route FROM trajectories WHERE issue = 3`); got != "2" {
		t.Errorf("route = %q, want 2: the trait pass ran", got)
	}
	if want := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC); !r.Fake.Since.Equal(want) {
		t.Errorf("merged PRs since %v, want %v: days 45", r.Fake.Since, want)
	}
}

func TestMeasureRecoveryRunDefaultDays(t *testing.T) {
	r, err := runOn(t, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC); !r.Fake.Since.Equal(want) {
		t.Errorf("merged PRs since %v, want %v: the default of 30 days", r.Fake.Since, want)
	}
}

func TestMeasureRecoveryRunWithoutForgeFails(t *testing.T) {
	j, err := bind(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Run(context.Background(), nil, scheduled.Env{Repo: repoconfig.Repo{Slug: "o/r"}}); err == nil ||
		!strings.Contains(err.Error(), "no forge") {
		t.Errorf("Run without a forge = %v, want a no-forge error", err)
	}
}

func TestMeasureRecoveryUnboundRunFails(t *testing.T) {
	reg, _ := scheduled.Lookup(measurerecovery.Name)
	d, ok := reg.(scheduled.Direct)
	if !ok {
		t.Fatalf("registered job %T is no scheduled.Direct", reg)
	}
	if err := d.Run(context.Background(), nil, scheduled.Env{}); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("unbound Run = %v, want a not-configured error", err)
	}
}

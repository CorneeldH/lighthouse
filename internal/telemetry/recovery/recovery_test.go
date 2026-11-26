package recovery_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/telemetry/recovery"
	"github.com/CorneeldH/lighthouse/internal/telemetry/recovery/recoverytest"
)

// Classification: io (git, SQLite) with a fake forge. These tests fail if Measure stops fetching,
// measures a closed row again, loses the issue of an open row, writes anything after a git or
// forge error, or records route, area and size of a trajectory wrongly.

type rig struct {
	*recoverytest.Rig
	now time.Time
}

func newRig(t *testing.T) *rig {
	t.Helper()
	return &rig{Rig: recoverytest.NewRig(t), now: recoverytest.After}
}

// measure runs Measure over the clone of the rig with days and returns its output.
func (r *rig) measure(t *testing.T, days int) (string, error) {
	t.Helper()
	repo, err := repoconfig.Load(r.Clone)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.OpenShared(context.Background(), r.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	var out bytes.Buffer
	err = recovery.Measure(context.Background(), st.DB, r.Fake, repo, days, r.now, &out)
	return out.String(), err
}

// mustMeasure is measure that must succeed.
func (r *rig) mustMeasure(t *testing.T, days int) string {
	t.Helper()
	out, err := r.measure(t, days)
	if err != nil {
		t.Fatalf("Measure: %v (out %q)", err, out)
	}
	return out
}

func TestMeasureFetchesMeasuresAndIsIdempotent(t *testing.T) {
	r := newRig(t)
	r.Fake.Merged = []forge.PRSummary{
		{Number: 7, MergedAt: recoverytest.Merge7}, {Number: 8, MergedAt: recoverytest.Merge8},
		{Number: 9, MergedAt: recoverytest.Merge8}, {Number: 10, MergedAt: recoverytest.Merge8},
	}
	r.Fake.Heads = []forge.PullHead{
		{Number: 7, HeadRef: "issue-3-eerste"}, {Number: 8, HeadRef: "issue-4-tweede"},
		{Number: 9, HeadRef: "fix-los"}, {Number: 10, HeadRef: "issue-5-zonder-commit"},
	}
	before := recoverytest.Git(t, r.Clone, nil, "rev-parse", "origin/main")

	out := r.mustMeasure(t, 14)
	for _, want := range []string{
		"#7 recovered 1 items, 0 fix-pr rounds\n",
		"#8 open 0 items, 0 fix-pr rounds\n",
		"#9 skipped: no_issue_branch\n",
		"#10 skipped: no_merge_commit\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if recoverytest.Git(t, r.Clone, nil, "rev-parse", "origin/main") == before {
		t.Error("origin/main did not move: the run did not fetch")
	}
	if r.Fake.Searches != 1 {
		t.Errorf("SearchIssues called %d times, want 1 per run", r.Fake.Searches)
	}
	if want := "label:bug created:>=2026-11-10"; r.Fake.Query != want {
		t.Errorf("query = %q, want %q", r.Fake.Query, want)
	}
	if want := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC); !r.Fake.Since.Equal(want) {
		t.Errorf("merged PRs since %v, want %v: the last 14 days", r.Fake.Since, want)
	}

	r.mustMeasure(t, 14)
	if n := r.Count(`SELECT count(*) FROM recoveries`); n != 2 {
		t.Errorf("recoveries = %d after two runs, want 2", n)
	}
	if n := r.Count(`SELECT count(*) FROM recovery_items`); n != 1 {
		t.Errorf("recovery_items = %d after two runs, want 1", n)
	}
}

func TestMeasureBugIssueCounts(t *testing.T) {
	r := newRig(t)
	r.Fake.Issues = []forge.Issue{{Number: 30, Title: "stuk door #7", Labels: []string{"bug"}, CreatedAt: recoverytest.Merge7.Add(48 * time.Hour)}}
	if out := r.mustMeasure(t, 14); !strings.Contains(out, "#7 recovered 2 items") {
		t.Fatalf("out %q; want the fix and the bug issue", out)
	}
}

func TestMeasureWithoutFixInWindowIsOpen(t *testing.T) {
	r := newRig(t)
	r.now = recoverytest.Merge7.Add(2 * time.Hour) // the fix of the rig is a day later than this clock
	if out := r.mustMeasure(t, 14); !strings.Contains(out, "#7 open 1 items") {
		t.Fatalf("out %q; want open", out)
	}
}

func TestMeasureCountsFixPRRounds(t *testing.T) {
	r := newRig(t)
	r.Exec(`INSERT INTO runs (repo, issue, session_id, mode, trigger, trigger_key, attempt, started_at)
		VALUES ('o/r', 3, 's', 'start', 'fixpr', 'fixpr:7:abc', 1, ?)`,
		recoverytest.Merge7.Add(-time.Hour).Format(time.RFC3339))
	if out := r.mustMeasure(t, 14); !strings.Contains(out, "#7 recovered 1 items, 1 fix-pr rounds") {
		t.Fatalf("out %q", out)
	}
}

func TestMeasureClosedRowCallsNoForgeSearch(t *testing.T) {
	r := newRig(t)
	r.mustMeasure(t, 14)
	*r.Fake = recoverytest.Forge{Merged: r.Fake.Merged, Heads: r.Fake.Heads, IssueComments: r.Fake.IssueComments}
	if out := r.mustMeasure(t, 14); out != "" {
		t.Fatalf("second run: out %q; want silence", out)
	}
	if r.Fake.Searches != 0 || r.Fake.HeadList != 0 {
		t.Errorf("closed row: %d searches, %d head lists, want none", r.Fake.Searches, r.Fake.HeadList)
	}
}

func TestMeasureClosesOpenRowOutsideDays(t *testing.T) {
	r := newRig(t)
	r.now = recoverytest.Merge7.Add(2 * time.Hour)
	if out := r.mustMeasure(t, 14); !strings.Contains(out, "#7 open") {
		t.Fatalf("first run: out %q", out)
	}
	r.now = recoverytest.After
	r.Fake.Merged = nil // #7 is no longer in the last day
	if out := r.mustMeasure(t, 1); !strings.Contains(out, "#7 recovered 1 items") {
		t.Fatalf("out %q; want the open row closed", out)
	}
	if n := r.Count(`SELECT count(*) FROM recoveries WHERE status = 'recovered'`); n != 1 {
		t.Errorf("recovered rows = %d, want 1", n)
	}
	if r.Fake.HeadList != 1 { // only the first run needed heads
		t.Errorf("PullHeads called %d times in total, want 1: the open row carries its issue", r.Fake.HeadList)
	}
}

func TestMeasureErrorsWriteNothing(t *testing.T) {
	cases := map[string]func(r *rig){
		"missing origin":  func(r *rig) { recoverytest.Git(t, r.Clone, nil, "remote", "remove", "origin") },
		"merged PRs fail": func(r *rig) { r.Fake.MergedErr = errors.New("boom") },
		"search fails":    func(r *rig) { r.Fake.IssErr = errors.New("rate limited") },
		"gitlab":          func(r *rig) { r.WriteConfig("[forge]\nrepo = \"o/r\"\nkind = \"gitlab\"\n") },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.Fake.Merged = []forge.PRSummary{{Number: 7, MergedAt: recoverytest.Merge7}, {Number: 8, MergedAt: recoverytest.Merge8}}
			r.Fake.Heads = append(r.Fake.Heads, forge.PullHead{Number: 8, HeadRef: "issue-4-tweede"})
			setup(r)
			if _, err := r.measure(t, 14); err == nil {
				t.Fatal("Measure = nil, want an error")
			}
			if n := r.Count(`SELECT count(*) FROM recoveries`); n != 0 {
				t.Errorf("recoveries = %d, want nothing written", n)
			}
		})
	}
}

func TestMeasureRefusesDaysBelowOne(t *testing.T) {
	r := newRig(t)
	if _, err := r.measure(t, 0); err == nil || !strings.Contains(err.Error(), "days 0 is below 1") {
		t.Fatalf("Measure(days 0) = %v, want the days error", err)
	}
	if r.Fake.Since != (time.Time{}) {
		t.Error("days 0 reached the forge")
	}
}

func TestMeasureOpenRowKeepsItsIssueWhenHeadIsGone(t *testing.T) {
	for name, heads := range map[string][]forge.PullHead{
		"head missing":   nil,
		"other issue":    {{Number: 7, HeadRef: "issue-99-ander"}},
		"no issue named": {{Number: 7, HeadRef: "fix-los"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.now = recoverytest.Merge7.Add(2 * time.Hour)
			if out := r.mustMeasure(t, 14); !strings.Contains(out, "#7 open") {
				t.Fatalf("first run: out %q", out)
			}
			r.now = recoverytest.After
			r.Fake.Heads = heads
			out := r.mustMeasure(t, 14)
			if !strings.Contains(out, "#7 recovered 1 items") || strings.Contains(out, "skipped") {
				t.Fatalf("out %q; want #7 re-measured, not skipped", out)
			}
			if n := r.Count(`SELECT count(*) FROM trajectories`); n != 1 {
				t.Errorf("trajectories = %d, want 1: no orphan", n)
			}
			if n := r.Count(`SELECT count(*) FROM recoveries r JOIN trajectories t ON t.id = r.trajectory_id WHERE t.issue = 3`); n != 1 {
				t.Errorf("recoveries on issue 3 = %d, want 1", n)
			}
		})
	}
}

func TestTraitPassBackfillsClosedRow(t *testing.T) {
	r := newRig(t)
	r.WithAreaPrefix()
	r.mustMeasure(t, 14)
	r.Exec(`UPDATE trajectories SET route = NULL, area = NULL`)
	r.Exec(`UPDATE recoveries SET code_lines = NULL`)
	r.Fake.IssueLabels = []string{"bug", "2-area:web"}
	r.Fake.IssueComments = recoverytest.Triage("invariant")
	if out := r.mustMeasure(t, 14); out != "" {
		t.Fatalf("out %q; want silence", out)
	}
	if got := r.Text(`SELECT route FROM trajectories WHERE issue = 3`); got != "0" {
		t.Errorf("route = %q, want 0 (invariant)", got)
	}
	if got := r.Text(`SELECT area FROM trajectories WHERE issue = 3`); got != "web" {
		t.Errorf("area = %q, want web", got)
	}
	if n := r.Count(`SELECT count(*) FROM recoveries WHERE pr = 7 AND code_lines IS NOT NULL`); n != 1 {
		t.Errorf("code_lines of #7 not filled")
	}
	if got := r.Text(`SELECT status FROM recoveries WHERE pr = 7`); got != "recovered" {
		t.Errorf("status = %q, want recovered: a trait pass never reopens", got)
	}
}

func TestTraitPassForgeErrorKeepsOldValue(t *testing.T) {
	r := newRig(t)
	r.Fake.Merged = []forge.PRSummary{{Number: 7, MergedAt: recoverytest.Merge7}, {Number: 8, MergedAt: recoverytest.Merge8}}
	r.Fake.Heads = append(r.Fake.Heads, forge.PullHead{Number: 8, HeadRef: "issue-4-tweede"})
	r.mustMeasure(t, 14)
	r.Exec(`UPDATE trajectories SET route = NULL WHERE issue = 4`)
	r.Exec(`UPDATE trajectories SET route = 'old' WHERE issue = 3`)
	r.Fake.IssueErr = map[int]error{3: errors.New("rate limited")}
	out := r.mustMeasure(t, 14)
	if got := r.Text(`SELECT route FROM trajectories WHERE issue = 3`); got != "old" {
		t.Errorf("route of #3 = %q, want old kept", got)
	}
	if got := r.Text(`SELECT route FROM trajectories WHERE issue = 4`); got != "2" {
		t.Errorf("route of #4 = %q, want 2: the second trajectory is still done", got)
	}
	if n := strings.Count(out, "traits #3: "); n != 1 || strings.Contains(out, "traits #4") {
		t.Errorf("output %q; want exactly one traits line for #3", out)
	}
}

func TestTraitPassRereadsInsideWindow(t *testing.T) {
	r := newRig(t)
	r.WithAreaPrefix()
	r.Fake.IssueLabels = []string{"2-area:web"}
	r.mustMeasure(t, 14)
	if got := r.Text(`SELECT area FROM trajectories WHERE issue = 3`); got != "web" {
		t.Fatalf("area = %q, want web", got)
	}
	r.Fake.IssueLabels = []string{"2-area:api"}
	r.mustMeasure(t, 14)
	if got := r.Text(`SELECT area FROM trajectories WHERE issue = 3`); got != "api" {
		t.Errorf("area = %q, want api: the PR is inside the window", got)
	}
}

func TestTraitPassSkipsSettledTrajectory(t *testing.T) {
	r := newRig(t)
	r.mustMeasure(t, 14)
	r.Exec(`UPDATE trajectories SET route = '2', area = 'web'`)
	r.Exec(`UPDATE recoveries SET code_lines = 1 WHERE code_lines IS NULL`)
	r.Fake.IssueCalls, r.Fake.CommCalls = 0, 0
	r.Fake.Merged = nil
	if out := r.mustMeasure(t, 1); out != "" {
		t.Fatalf("out %q", out)
	}
	if r.Fake.IssueCalls != 0 || r.Fake.CommCalls != 0 {
		t.Errorf("forge reads: %d issue, %d comments, want none for a settled trajectory", r.Fake.IssueCalls, r.Fake.CommCalls)
	}
}

func TestTraitPassWithoutAreaPrefixSettlesWithNullArea(t *testing.T) {
	r := newRig(t)
	r.mustMeasure(t, 14)
	// No area prefix: the area stays NULL, and that must not keep the trajectory a target.
	r.Exec(`UPDATE trajectories SET route = '2', area = NULL`)
	r.Exec(`UPDATE recoveries SET code_lines = 1 WHERE code_lines IS NULL`)
	r.Fake.IssueCalls, r.Fake.CommCalls = 0, 0
	r.Fake.Merged = nil
	if out := r.mustMeasure(t, 1); out != "" {
		t.Fatalf("out %q", out)
	}
	if r.Fake.IssueCalls != 0 || r.Fake.CommCalls != 0 {
		t.Errorf("forge reads: %d issue, %d comments, want none without an area prefix", r.Fake.IssueCalls, r.Fake.CommCalls)
	}
}

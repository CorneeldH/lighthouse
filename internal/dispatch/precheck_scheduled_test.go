package dispatch

import (
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
)

// Classification: pure logic. Fails if the latest slot of a schedule stops being a boundary, so
// that a repo gathered before a slot is not gathered again after it until the gather goes stale.
func TestScheduleBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 31, 4, 1, 0, 0, time.UTC)
	got := ScheduleBoundaries([]repoconfig.Schedule{
		{Name: "daily", Cron: "0 4 * * *"},
		{Name: "monthly", Cron: "0 4 1 * *"},
		{Name: "broken", Cron: "not cron"},
	}, now)
	want := []time.Time{
		time.Date(2026, 10, 31, 4, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC),
	}
	if len(got) != len(want) {
		t.Fatalf("boundaries = %v, want %v", got, want)
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Errorf("boundary %d = %v, want %v", i, got[i], want[i])
		}
	}
	prev := TickState{Fingerprint: "fp", FullAt: now.Add(-11 * time.Minute)}
	if due, reason := Due(prev, "fp", now, got); !due || reason != "boundary" {
		t.Errorf("Due = %v, %q; want a boundary gather after the 04:00 slot", due, reason)
	}
}

// (e) Classification: pure logic. Fails if the precheck wakes an open scheduled run at the host's
// max_runtime instead of its record's, or does not wake it at pr_at + pr_timeout while its PR waits
// for CI without a board change: the run would then stay open until the gather goes stale.
func TestBoundariesOfAScheduledRun(t *testing.T) {
	started := time.Date(2026, 10, 31, 4, 0, 0, 0, time.UTC)
	d := hostconfig.Dispatch{MaxRuntime: 120 * time.Minute, MaxWait: 5 * time.Hour, Night: "x"}
	fast := scheduled.Record{Verdict: scheduled.Verdict{Mode: "fast"}, MaxRuntime: 45 * time.Minute}.Encode()
	run := runs.OpenRun{ID: 1, Schedule: "codebase-map", StartedAt: started, Verdict: fast, WorkspaceID: "ws"}

	at := func(open runs.OpenRun, fullAt, now time.Time) (bool, string) {
		b := Boundaries(d, []runs.OpenRun{open}, nil, nil, now)
		return Due(TickState{Fingerprint: "fp", FullAt: fullAt}, "fp", now, b)
	}
	if due, reason := at(run, started.Add(44*time.Minute), started.Add(46*time.Minute)); !due || reason != "boundary" {
		t.Errorf("fast run at 46m: Due = %v, %q; want a boundary at its 45m", due, reason)
	}
	if due, _ := at(run, started.Add(110*time.Minute), started.Add(125*time.Minute)); due {
		t.Error("fast run: a boundary at the host's 120m, want only the record's 45m")
	}

	withPR := run
	withPR.PRAt = started.Add(50 * time.Minute)
	if due, reason := at(withPR, withPR.PRAt.Add(59*time.Minute), withPR.PRAt.Add(61*time.Minute)); !due || reason != "boundary" {
		t.Errorf("PR pending at pr_at+61m: Due = %v, %q; want a boundary at pr_at+60m", due, reason)
	}
	if due, _ := at(withPR, started.Add(40*time.Minute), started.Add(60*time.Minute)); due {
		t.Error("PR phase: a boundary at the record's 45m, want none (max_runtime no longer counts)")
	}

	// An issue run keeps the host's max_runtime.
	issue := runs.OpenRun{ID: 2, Issue: 7, StartedAt: started, WorkspaceID: "ws"}
	if due, reason := at(issue, started.Add(119*time.Minute), started.Add(121*time.Minute)); !due || reason != "boundary" {
		t.Errorf("issue run at 121m: Due = %v, %q; want the host's boundary", due, reason)
	}
}

package dispatch

import (
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
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

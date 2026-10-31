package dispatch

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
)

func TestDue(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	prev := func(full time.Duration) TickState { return TickState{Fingerprint: "a", FullAt: now.Add(-full)} }
	tests := []struct {
		name       string
		prev       TickState
		fp         string
		boundaries []time.Time
		due        bool
		reason     string
	}{
		{"unchanged", prev(10 * time.Minute), "a", nil, false, "unchanged"},
		{"just before stale", prev(29*time.Minute + 59*time.Second), "a", nil, false, "unchanged"},
		{"stale", prev(30 * time.Minute), "a", nil, true, "stale"},
		{"boundary passed", prev(10 * time.Minute), "a", []time.Time{now.Add(-5 * time.Minute)}, true, "boundary"},
		{"boundary before last full", prev(10 * time.Minute), "a", []time.Time{now.Add(-11 * time.Minute)}, false, "unchanged"},
		{"boundary in future", prev(10 * time.Minute), "a", []time.Time{now.Add(time.Minute)}, false, "unchanged"},
		{"changed", prev(10 * time.Minute), "b", nil, true, "changed"},
		{"first", TickState{}, "a", nil, true, "first"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			due, reason := Due(tc.prev, tc.fp, now, tc.boundaries)
			if due != tc.due || reason != tc.reason {
				t.Errorf("Due = %v, %q; want %v, %q", due, reason, tc.due, tc.reason)
			}
		})
	}
}

func TestBoundaries(t *testing.T) {
	d := hostconfig.Dispatch{Night: "21:00-08:00", MaxRuntime: 8 * time.Hour, MaxWait: time.Hour}
	started := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	open := []runs.OpenRun{{ID: 1, Issue: 1, StartedAt: started}}

	t.Run("utc", func(t *testing.T) {
		now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
		got := Boundaries(d, open, now)
		for _, w := range []time.Time{
			time.Date(2026, 10, 31, 18, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 31, 11, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 31, 21, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 30, 21, 0, 0, 0, time.UTC),
		} {
			if !containsTime(got, w) {
				t.Errorf("missing %v in %v", w, got)
			}
		}
	})
	t.Run("night follows the zone of now", func(t *testing.T) {
		cet := time.FixedZone("CET", 3600)
		now := time.Date(2026, 10, 31, 12, 0, 0, 0, cet)
		got := Boundaries(d, nil, now)
		for _, w := range []time.Time{
			time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 30, 20, 0, 0, 0, time.UTC),
		} {
			if !containsTime(got, w) {
				t.Errorf("missing %v in %v", w, got)
			}
		}
		if containsTime(got, time.Date(2026, 10, 31, 21, 0, 0, 0, time.UTC)) {
			t.Errorf("night boundary built in UTC: %v", got)
		}
	})
}

func containsTime(ts []time.Time, w time.Time) bool {
	for _, x := range ts {
		if x.Equal(w) {
			return true
		}
	}
	return false
}

func TestTickStateRoundTrip(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenShared(ctx, filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatalf("OpenShared: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	got, err := LoadTickState(ctx, st.DB, "o/r")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Fingerprint != "" || !got.FullAt.IsZero() {
		t.Fatalf("empty Load = %+v, want zero", got)
	}

	cet := time.FixedZone("CET", 3600)
	full := time.Date(2026, 10, 31, 12, 0, 0, 0, cet)
	want := TickState{Fingerprint: "abc", FullAt: full}
	if err := SaveTickState(ctx, st.DB, "o/r", want, full); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err = LoadTickState(ctx, st.DB, "o/r")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Fingerprint != "abc" || !got.FullAt.Equal(full) {
		t.Fatalf("Load = %+v, want %+v", got, want)
	}
	var raw string
	if err := st.DB.QueryRowContext(ctx, `SELECT value FROM tick_state WHERE repo='o/r' AND kind='fingerprint' AND key='full_at'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw[len(raw)-1] != 'Z' {
		t.Errorf("raw full_at %q does not end in Z", raw)
	}

	want2 := TickState{Fingerprint: "def", FullAt: full.Add(time.Hour)}
	if err := SaveTickState(ctx, st.DB, "o/r", want2, full); err != nil {
		t.Fatal(err)
	}
	got, _ = LoadTickState(ctx, st.DB, "o/r")
	if got.Fingerprint != "def" || !got.FullAt.Equal(want2.FullAt) {
		t.Fatalf("overwrite Load = %+v, want %+v", got, want2)
	}
}

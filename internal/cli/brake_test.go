package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

const (
	markerCreated = "2026-10-31T10:00:00Z"
	markerReset   = "2026-10-31T15:00:00Z"
)

// writeMarker writes the marker of session in the marker directory of env.
func writeMarker(t *testing.T, env tickEnv, session string, controller bool) park.Marker {
	t.Helper()
	m := park.Marker{SessionID: session, Cwd: "/work", Window: "session", ResetAt: markerReset,
		CreatedAt: markerCreated, Source: "guard", Controller: controller}
	dir, err := park.MarkerDir(env.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, session+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return m
}

// claim opens a run of session on repo#issue; closeAs, when not empty, closes it.
func claim(t *testing.T, db *sql.DB, repo string, issue int, session, profile, trigger, key string, closeAs runs.Outcome) {
	t.Helper()
	ctx := context.Background()
	r, err := runs.Take(ctx, db, runs.Claim{Repo: repo, Issue: issue, SessionID: session, Mode: "start", Trigger: trigger,
		TriggerKey: key, Command: "/lighthouse-start-issue", Prompt: "p", Profile: profile, Branch: "b", Now: t0.Add(-3 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if closeAs != "" {
		if err := runs.Release(ctx, db, r.ID, closeAs, "", t0.Add(-2*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
}

func facts(t *testing.T, env tickEnv, db *sql.DB) hostFacts {
	t.Helper()
	f, err := readHostFacts(context.Background(), env, db, hostconfig.DefaultDispatch())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Open runs of every repo count, and so does a parked run: a controller marker whose last run
// closed as parked. A marker of a plain session counts for nothing.
func TestReadHostFactsCounts(t *testing.T) {
	env, dbPath := tickFixture(t)
	db := openStore(t, dbPath)
	claim(t, db, "o/a", 1, "sess-a", "software", "handoff", "start:1", "")
	claim(t, db, "o/b", 2, "sess-b", "infra", "handoff", "start:2", "")
	claim(t, db, "o/a", 3, "sess-p", "software", "handoff", "start:3", runs.Parked)
	claim(t, db, "o/a", 4, "sess-q", "software", "handoff", "start:4", runs.Parked)
	m := writeMarker(t, env, "sess-p", true)
	writeMarker(t, env, "sess-q", false)

	f := facts(t, env, db)
	if f.Brake.InUse != 3 || f.Brake.SoftwareInUse != 2 {
		t.Errorf("InUse %d, SoftwareInUse %d; want 3 and 2 (two open runs and one parked)", f.Brake.InUse, f.Brake.SoftwareInUse)
	}
	if got := f.Markers["o/a"]; len(got) != 1 || got[0] != m || len(f.Markers) != 1 {
		t.Errorf("Markers = %+v, want only the controller marker under o/a", f.Markers)
	}
}

// A run that is open and has a fresh marker (the guard parked it, the tick has not closed it yet)
// is counted once.
func TestReadHostFactsOpenRunNotDoubled(t *testing.T) {
	env, dbPath := tickFixture(t)
	db := openStore(t, dbPath)
	claim(t, db, "o/a", 1, "sess-a", "software", "handoff", "start:1", "")
	writeMarker(t, env, "sess-a", true)

	f := facts(t, env, db)
	if f.Brake.InUse != 1 || f.Brake.SoftwareInUse != 1 {
		t.Errorf("InUse %d, SoftwareInUse %d; want 1 and 1", f.Brake.InUse, f.Brake.SoftwareInUse)
	}
	if len(f.Markers["o/a"]) != 1 {
		t.Errorf("Markers = %+v, want the marker of the open run, for settling it as parked", f.Markers)
	}
}

// A marker whose session has no run is a plain session or a foreign one: not counted, not given
// to any repo.
func TestHostLoadIgnoresMarkerWithoutRun(t *testing.T) {
	env, dbPath := tickFixture(t)
	db := openStore(t, dbPath)
	writeMarker(t, env, "sess-unknown", true)

	f := facts(t, env, db)
	if f.Brake.InUse != 0 || f.Brake.SoftwareInUse != 0 || len(f.Markers) != 0 {
		t.Errorf("InUse %d, SoftwareInUse %d, Markers %+v; want a marker without a run to count for nothing",
			f.Brake.InUse, f.Brake.SoftwareInUse, f.Markers)
	}
}

// A marker that will not resume is not parked, but its repo still gets it, so Decide drops it.
func TestReadHostFactsNotResumable(t *testing.T) {
	m := park.Marker{SessionID: "sess-p", CreatedAt: markerCreated, ResetAt: markerReset}
	for name, tc := range map[string]struct {
		setup      func(t *testing.T, db *sql.DB)
		wantInUse  int
		wantMarker bool
	}{
		"closed as handoff": {func(t *testing.T, db *sql.DB) {
			claim(t, db, "o/a", 1, "sess-p", "software", "handoff", "start:1", runs.Handoff)
		}, 0, true},
		"usage key already resumed": {func(t *testing.T, db *sql.DB) {
			claim(t, db, "o/a", 1, "sess-p", "software", "handoff", "start:1", runs.Parked)
			claim(t, db, "o/a", 1, "sess-p", "software", "usage", m.Key(), runs.Handoff)
		}, 0, true},
		"usage resume failed to start once": {func(t *testing.T, db *sql.DB) {
			claim(t, db, "o/a", 1, "sess-p", "software", "handoff", "start:1", runs.Parked)
			claim(t, db, "o/a", 1, "sess-p", "software", "usage", m.Key(), runs.StartFailed)
		}, 1, true},
	} {
		t.Run(name, func(t *testing.T) {
			env, dbPath := tickFixture(t)
			db := openStore(t, dbPath)
			tc.setup(t, db)
			writeMarker(t, env, "sess-p", true)

			f := facts(t, env, db)
			if f.Brake.InUse != tc.wantInUse {
				t.Errorf("InUse = %d, want %d", f.Brake.InUse, tc.wantInUse)
			}
			if got := len(f.Markers["o/a"]); (got == 1) != tc.wantMarker {
				t.Errorf("Markers = %+v, want the marker under o/a: %v", f.Markers, tc.wantMarker)
			}
		})
	}
}

// Without a readable meminfo the memory brake cannot check, but the tick can: nil, no error.
func TestReadHostFactsMemInfoMissing(t *testing.T) {
	env, dbPath := tickFixture(t)
	env.MemInfo = filepath.Join(t.TempDir(), "nothing")
	f := facts(t, env, openStore(t, dbPath))
	if f.Brake.MemAvailableGiB != nil {
		t.Errorf("MemAvailableGiB = %v, want nil", *f.Brake.MemAvailableGiB)
	}
}

func TestReadHostFactsBrake(t *testing.T) {
	env, dbPath := tickFixture(t)
	db := openStore(t, dbPath)
	setUsage(t, env, 90, 20)
	pause, err := park.PauseFile(env.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	until := t0.Add(3 * time.Hour)
	if err := park.WritePause(pause, until); err != nil {
		t.Fatal(err)
	}
	env.Now = func() time.Time { return t0 }

	f := facts(t, env, db)
	b := f.Brake
	if !b.Paused || !f.PauseUntil.Equal(until) {
		t.Errorf("Paused %v, PauseUntil %v; want a pause until %v", b.Paused, f.PauseUntil, until)
	}
	if !b.UsageMeasured || b.SessionPct != 90 || b.WeekPct != 20 || b.HaltPctSession != 85 || b.HaltPctWeek != 95 {
		t.Errorf("usage = %+v, want measured 90/20 against 85/95", b)
	}
	if b.MemAvailableGiB == nil || *b.MemAvailableGiB != 32 || b.Slots != 4 || b.MaxSoftware != 2 ||
		b.MinMemAvailableGB != 8 || b.ReservePerRunGB != 3 || b.Manual {
		t.Errorf("brake = %+v, want 32 GiB free, the default limits and not manual", b)
	}
}

// An unmeasurable usage is a closed brake, not a tick error.
func TestReadHostFactsUsageNotMeasured(t *testing.T) {
	env, dbPath := tickFixture(t)
	if err := os.Remove(env.Meter.Paths.Cache()); err != nil {
		t.Fatal(err)
	}
	env.Meter.RunUsage = func(context.Context, string) ([]byte, error) { return nil, errors.New("claude is down") }
	if f := facts(t, env, openStore(t, dbPath)); f.Brake.UsageMeasured {
		t.Error("UsageMeasured = true without a cache")
	}
}

package dispatch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

const runSession = "62b1fdd2-7894-4e1e-a0a1-fc81a5d6ef39"

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadPermission(t *testing.T) {
	note := func(dir string) string { return filepath.Join(dir, "runs", runSession, "permission.json") }

	t.Run("the note of the hook", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, note(dir), `{"version": 1, "at": "2026-10-24T09:05:01Z", "tool": "Write", "path": "/tmp/x", `+
			`"thread": "agent-a", "message": "Claude needs your permission to use Write"}`+"\n")
		got, err := ReadPermission(dir, runSession)
		if err != nil {
			t.Fatal(err)
		}
		want := Permission{At: time.Date(2026, 10, 24, 9, 5, 1, 0, time.UTC), Tool: "Write", Path: "/tmp/x", Thread: "agent-a"}
		if got == nil || *got != want {
			t.Errorf("ReadPermission = %+v, want %+v", got, want)
		}
	})
	t.Run("no note", func(t *testing.T) {
		got, err := ReadPermission(t.TempDir(), runSession)
		if got != nil || err != nil {
			t.Errorf("ReadPermission = %+v, %v; want nil, nil", got, err)
		}
	})
	for name, body := range map[string]string{
		"broken json":     `{"version": 1, "at": `,
		"unknown version": `{"version": 2, "at": "2026-10-24T09:05:01Z", "tool": "Write"}`,
		"no at":           `{"version": 1, "tool": "Write"}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, note(dir), body)
			if got, err := ReadPermission(dir, runSession); err == nil {
				t.Errorf("ReadPermission = %+v, nil; want an error", got)
			}
		})
	}
	t.Run("a session id that is no file name", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "runs", "permission.json"), `{"version": 1, "at": "2026-10-24T09:05:01Z"}`)
		for _, id := range []string{"", "..", "a/b"} {
			if got, err := ReadPermission(dir, id); err == nil {
				t.Errorf("ReadPermission(%q) = %+v, nil; want an error", id, got)
			}
		}
	})
}

func TestParked(t *testing.T) {
	marker := func(dir string) string { return filepath.Join(dir, runSession+".json") }

	t.Run("a marker file", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, marker(dir), `{"session_id": "`+runSession+`"}`)
		if got, err := Parked(dir, runSession); !got || err != nil {
			t.Errorf("Parked = %v, %v; want true, nil", got, err)
		}
	})
	t.Run("no marker", func(t *testing.T) {
		if got, err := Parked(t.TempDir(), runSession); got || err != nil {
			t.Errorf("Parked = %v, %v; want false, nil", got, err)
		}
	})
	t.Run("no resume dir", func(t *testing.T) {
		if got, err := Parked(filepath.Join(t.TempDir(), "missing"), runSession); got || err != nil {
			t.Errorf("Parked = %v, %v; want false, nil", got, err)
		}
	})
	t.Run("a symlink", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(t.TempDir(), "elsewhere.json")
		writeFile(t, target, "{}")
		if err := os.Symlink(target, marker(dir)); err != nil {
			t.Fatal(err)
		}
		if got, err := Parked(dir, runSession); got || err != nil {
			t.Errorf("Parked = %v, %v; want false, nil", got, err)
		}
	})
	t.Run("a directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(marker(dir), 0o700); err != nil {
			t.Fatal(err)
		}
		if got, err := Parked(dir, runSession); got || err != nil {
			t.Errorf("Parked = %v, %v; want false, nil", got, err)
		}
	})
	t.Run("a session id that is no file name", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".json"), "{}")
		if got, err := Parked(dir, ""); err == nil {
			t.Errorf("Parked(\"\") = %v, nil; want an error", got)
		}
	})
}

func TestDurationText(t *testing.T) {
	var in struct {
		MaxWait Duration `json:"max_wait"`
	}
	if err := json.Unmarshal([]byte(`{"max_wait": "1h"}`), &in); err != nil {
		t.Fatal(err)
	}
	if in.MaxWait != Duration(time.Hour) {
		t.Errorf("max_wait = %v, want 1h", time.Duration(in.MaxWait))
	}
	out, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"max_wait":"1h0m0s"}` {
		t.Errorf("marshal = %s", out)
	}
	var back struct {
		MaxWait Duration `json:"max_wait"`
	}
	if err := json.Unmarshal(out, &back); err != nil || back.MaxWait != Duration(time.Hour) {
		t.Errorf("round trip = %v, %v; want 1h", time.Duration(back.MaxWait), err)
	}
	if err := json.Unmarshal([]byte(`{"max_wait": "soon"}`), &back); err == nil {
		t.Error(`"soon" decoded without an error`)
	}
}

func TestBoundariesActivity(t *testing.T) {
	d := hostconfig.Dispatch{MaxRuntime: 8 * time.Hour, MaxWait: time.Hour}
	started := time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	open := []runs.OpenRun{
		{ID: 1, Issue: 1, StartedAt: started},
		{ID: 2, Issue: 2, StartedAt: started},
		{ID: 3, Issue: 3, StartedAt: started},
	}
	states := map[int64]RunState{
		1: {
			SessionAt:  time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC),
			Transcript: &workplace.Transcript{LastEvent: time.Date(2026, 10, 31, 11, 10, 0, 0, time.UTC)},
			Permission: &Permission{At: time.Date(2026, 10, 31, 11, 20, 0, 0, time.UTC)},
		},
		2: {SessionAt: time.Date(2026, 10, 31, 11, 30, 0, 0, time.UTC)},
		3: {}, // not measured: only the run's own limits
	}
	got := Boundaries(d, open, states, now)
	for _, w := range []time.Time{
		time.Date(2026, 10, 31, 16, 0, 0, 0, time.UTC),  // started + max_runtime
		time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC),   // started + max_wait
		time.Date(2026, 10, 31, 12, 10, 0, 0, time.UTC), // run 1: last event + max_wait
		time.Date(2026, 10, 31, 12, 20, 0, 0, time.UTC), // run 1: permission + max_wait
		time.Date(2026, 10, 31, 12, 30, 0, 0, time.UTC), // run 2: session_at + max_wait, no transcript
	} {
		if !containsTime(got, w) {
			t.Errorf("missing %v in %v", w, got)
		}
	}
	// With a transcript the forge time does not count: it lags behind a subagent.
	if containsTime(got, time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("run 1 has a transcript, but session_at + max_wait is in %v", got)
	}
	if n := len(got); n != 9 {
		t.Errorf("boundaries = %d (%v), want 9: 2 per run, 3 measured, no night window", n, got)
	}
}

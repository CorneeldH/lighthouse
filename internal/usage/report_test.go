package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var reportNow = time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)

func intp(v int) *int { return &v }

// writeCache writes a Claude cache generated age before reportNow.
func writeCache(t *testing.T, age time.Duration, session, week int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "usage-window.json")
	reset := time.Date(2026, 9, 6, 12, 30, 0, 0, time.UTC)
	w := Windows{Session: Window{Pct: session, Reset: reset}, Week: Window{Pct: week, Reset: reset.Add(96 * time.Hour)}}
	if err := WriteState(path, NewState(w, reportNow.Add(-age), time.UTC)); err != nil {
		t.Fatal(err)
	}
	return path
}

func noCodex(context.Context) (CodexState, error) { return CodexState{}, errors.New("unused") }

func TestReportClaude(t *testing.T) {
	cases := []struct {
		name    string
		age     time.Duration
		state   string
		wantAge int
	}{
		{"fresh", 60 * time.Second, StateOK, 60},
		{"boundary 900 is ok", 900 * time.Second, StateOK, 900},
		{"901 is stale", 901 * time.Second, StateStale, 901},
		{"future cache", -120 * time.Second, StateOK, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &Reporter{Cache: writeCache(t, c.age, 99, 48), ReadCodex: noCodex, Now: func() time.Time { return reportNow }}
			got := r.Report(context.Background()).Claude
			if got.Session.State != c.state || got.Week.State != c.state {
				t.Errorf("state = %q/%q, want %q", got.Session.State, got.Week.State, c.state)
			}
			if got.Session.Pct == nil || *got.Session.Pct != 99 || got.Week.Pct == nil || *got.Week.Pct != 48 {
				t.Errorf("pct = %v/%v, want 99/48", got.Session.Pct, got.Week.Pct)
			}
			if got.Session.AgeS == nil || *got.Session.AgeS != c.wantAge {
				t.Errorf("age_s = %v, want %d", got.Session.AgeS, c.wantAge)
			}
			if got.Session.ResetAt != "2026-09-06T12:30:00Z" {
				t.Errorf("reset_at = %q", got.Session.ResetAt)
			}
			if got.Session.LowerBound {
				t.Error("claude is never a lower bound")
			}
		})
	}
}

func TestReportClaudeNotMeasurable(t *testing.T) {
	broken := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(broken, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, cache := range map[string]string{
		"missing": filepath.Join(t.TempDir(), "absent.json"),
		"broken":  broken,
		"empty":   "",
	} {
		t.Run(name, func(t *testing.T) {
			r := &Reporter{Cache: cache, ReadCodex: noCodex, Now: func() time.Time { return reportNow }}
			got := r.Report(context.Background()).Claude
			for _, w := range []WindowReport{got.Session, got.Week} {
				if w.State != StateNotMeasurable || w.Pct != nil || w.AgeS != nil || w.Reason == "" {
					t.Errorf("window = %+v, want not_measurable with nil pct/age and a reason", w)
				}
			}
			b, err := json.Marshal(got.Session)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(b, []byte(`"pct":null`)) || !bytes.Contains(b, []byte(`"age_s":null`)) {
				t.Errorf("json = %s, want null pct and age_s", b)
			}
		})
	}

	r := &Reporter{Cache: writeCache(t, time.Minute, 0, 0), ReadCodex: noCodex, Now: func() time.Time { return reportNow }}
	b, err := json.Marshal(r.Report(context.Background()).Claude.Session)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"pct":0`)) || !bytes.Contains(b, []byte(`"state":"ok"`)) {
		t.Errorf("json = %s, want pct 0 and state ok", b)
	}
}

func TestReportCodex(t *testing.T) {
	gen := reportNow.Format(timeLayout)
	win := CodexWindow{Pct: 54, ResetAt: "2026-09-06T12:00:00Z"}
	cases := []struct {
		name      string
		st        CodexState
		err       error
		state     string
		age       *int
		lower     bool
		reason    string
		wantPct   *int
		wantReset string
	}{
		{"app-server", CodexState{Source: "app-server", GeneratedAt: gen, Session: win, Week: win}, nil, StateOK, intp(0), false, "", intp(54), "2026-09-06T12:00:00Z"},
		{"rollout stale", CodexState{Source: "rollout-log", GeneratedAt: gen, AgeS: 2100, LowerBound: true, Session: win, Week: win}, nil, StateStale, intp(2100), true, "", intp(54), "2026-09-06T12:00:00Z"},
		{"error", CodexState{}, errors.New("codex not found"), StateNotMeasurable, nil, false, "codex not found", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &Reporter{ReadCodex: func(context.Context) (CodexState, error) { return c.st, c.err }, Now: func() time.Time { return reportNow }}
			rep := r.Report(context.Background()).Codex
			for _, w := range []WindowReport{rep.Session, rep.Week} {
				if w.State != c.state || w.LowerBound != c.lower || w.Reason != c.reason || w.ResetAt != c.wantReset {
					t.Errorf("window = %+v", w)
				}
				if (w.AgeS == nil) != (c.age == nil) || (w.AgeS != nil && *w.AgeS != *c.age) {
					t.Errorf("age_s = %v, want %v", w.AgeS, c.age)
				}
				if (w.Pct == nil) != (c.wantPct == nil) || (w.Pct != nil && *w.Pct != *c.wantPct) {
					t.Errorf("pct = %v, want %v", w.Pct, c.wantPct)
				}
			}
		})
	}
}

func TestReporterRemembersCodex(t *testing.T) {
	cases := []struct {
		name      string
		fail      bool
		wait      time.Duration
		wantCalls int32
	}{
		{"success 300 s", false, 300 * time.Second, 1},
		{"success 301 s", false, 301 * time.Second, 2},
		{"failure 60 s", true, 60 * time.Second, 1},
		{"failure 61 s", true, 61 * time.Second, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			now := reportNow
			var calls atomic.Int32
			r := &Reporter{
				Now: func() time.Time { return now },
				ReadCodex: func(context.Context) (CodexState, error) {
					calls.Add(1)
					if c.fail {
						return CodexState{}, errors.New("boom")
					}
					return CodexState{Source: "app-server", GeneratedAt: now.Format(timeLayout)}, nil
				},
			}
			r.Report(context.Background())
			now = now.Add(c.wait)
			r.Report(context.Background())
			if got := calls.Load(); got != c.wantCalls {
				t.Errorf("calls = %d, want %d", got, c.wantCalls)
			}
		})
	}

	t.Run("remembered age grows", func(t *testing.T) {
		now := reportNow
		r := &Reporter{
			Now: func() time.Time { return now },
			ReadCodex: func(context.Context) (CodexState, error) {
				return CodexState{Source: "app-server", GeneratedAt: now.Format(timeLayout)}, nil
			},
		}
		r.Report(context.Background())
		now = now.Add(120 * time.Second)
		got := r.Report(context.Background()).Codex.Session
		if got.AgeS == nil || *got.AgeS != 120 {
			t.Errorf("age_s = %v, want 120", got.AgeS)
		}
	})
}

func TestReporterOneCodexAtATime(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 8)
	var calls atomic.Int32
	r := &Reporter{
		Now: func() time.Time { return reportNow },
		ReadCodex: func(context.Context) (CodexState, error) {
			calls.Add(1)
			started <- struct{}{}
			<-release
			return CodexState{Source: "app-server", GeneratedAt: reportNow.Format(timeLayout)}, nil
		},
	}
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Report(context.Background())
		}()
	}
	<-started
	time.Sleep(50 * time.Millisecond) // let the other four queue on the mutex
	close(release)
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Errorf("ReadCodex calls = %d, want 1", got)
	}
}

func TestReporterWritesNothing(t *testing.T) {
	cache := writeCache(t, time.Minute, 10, 20)
	dir := filepath.Dir(cache)
	alarm := filepath.Join(dir, "usage-window.alarm")
	if err := os.WriteFile(alarm, []byte("alarm\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for _, p := range []string{cache, alarm} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		before[p] = b
	}
	r := &Reporter{Cache: cache, Now: func() time.Time { return reportNow }, ReadCodex: func(context.Context) (CodexState, error) {
		return CodexState{Source: "app-server", GeneratedAt: reportNow.Format(timeLayout)}, nil
	}}
	r.Report(context.Background())
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("dir has %d entries, want 2", len(entries))
	}
	for p, want := range before {
		got, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s changed (err %v)", p, err)
		}
	}
}

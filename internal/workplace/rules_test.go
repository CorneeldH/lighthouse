package workplace

import (
	"testing"
	"time"
)

var now0 = time.Date(2026, 10, 31, 15, 0, 0, 0, time.UTC)

func sess(key, state, sid string, updated time.Time) Session {
	return Session{Key: key, State: state, SessionID: sid, UpdatedAt: updated}
}

func TestBusyForeignWorking(t *testing.T) {
	t.Parallel()
	got, ok := Busy([]Session{sess("a", "working", "x", now0.Add(-5*time.Hour))}, nil, nil, nil, now0, time.Hour)
	if !ok || got.Key != "a" {
		t.Fatalf("got %v %v, want busy a", got, ok)
	}
}

func TestBusyForeignIdleRecent(t *testing.T) {
	t.Parallel()
	_, ok := Busy([]Session{sess("a", "idle", "x", now0.Add(-10*time.Minute))}, nil, nil, nil, now0, time.Hour)
	if !ok {
		t.Fatal("want busy")
	}
}

func TestBusyForeignIdleStale(t *testing.T) {
	t.Parallel()
	_, ok := Busy([]Session{sess("a", "idle", "x", now0.Add(-2*time.Hour))}, nil, nil, nil, now0, time.Hour)
	if ok {
		t.Fatal("want not busy")
	}
}

func TestBusyForeignUnparsedUpdatedAt(t *testing.T) {
	t.Parallel()
	_, ok := Busy([]Session{sess("a", "idle", "x", time.Time{})}, nil, nil, nil, now0, time.Hour)
	if !ok {
		t.Fatal("want busy")
	}
}

func TestBusyForeignDone(t *testing.T) {
	t.Parallel()
	_, ok := Busy([]Session{sess("a", "done", "x", now0)}, nil, nil, nil, now0, time.Hour)
	if ok {
		t.Fatal("want not busy")
	}
}

func TestBusyOpenRunWaiting(t *testing.T) {
	t.Parallel()
	s := sess("a", "idle", "mine", now0.Add(-2*time.Hour))
	_, ok := Busy([]Session{s}, map[string]bool{"mine": true}, map[string]bool{"mine": true}, nil, now0, time.Hour)
	if !ok {
		t.Fatal("want busy")
	}
}

func TestBusyOpenRunDone(t *testing.T) {
	t.Parallel()
	s := sess("a", "done", "mine", now0)
	_, ok := Busy([]Session{s}, map[string]bool{"mine": true}, map[string]bool{"mine": true}, nil, now0, time.Hour)
	if ok {
		t.Fatal("want not busy")
	}
}

func TestBusyClosedOwnIgnored(t *testing.T) {
	t.Parallel()
	s := sess("a", "working", "mine", now0)
	_, ok := Busy([]Session{s}, map[string]bool{"mine": true}, map[string]bool{}, nil, now0, time.Hour)
	if ok {
		t.Fatal("want not busy")
	}
}

func TestBusyActivityOverridesUpdatedAt(t *testing.T) {
	t.Parallel()
	s := sess("a", "idle", "x", now0.Add(-2*time.Hour))
	_, ok := Busy([]Session{s}, nil, nil, map[string]time.Time{"a": now0.Add(-5 * time.Minute)}, now0, time.Hour)
	if !ok {
		t.Fatal("want busy")
	}
}

func TestBusyActivityStale(t *testing.T) {
	t.Parallel()
	s := sess("a", "idle", "x", now0.Add(-5*time.Minute))
	_, ok := Busy([]Session{s}, nil, nil, map[string]time.Time{"a": now0.Add(-2 * time.Hour)}, now0, time.Hour)
	if ok {
		t.Fatal("want not busy")
	}
}

func TestBusyFirstInOrder(t *testing.T) {
	t.Parallel()
	got, ok := Busy([]Session{
		sess("done", "done", "x", now0),
		sess("first", "working", "y", now0),
		sess("second", "working", "z", now0),
	}, nil, nil, nil, now0, time.Hour)
	if !ok || got.Key != "first" {
		t.Fatalf("got %v %v, want first", got, ok)
	}
}

// Not parallel: mutates the global time.Local.
func TestBusyReason(t *testing.T) {
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })

	got := BusyReason(sess("k1", "working", "x", time.Date(2026, 10, 31, 15, 4, 0, 0, time.UTC)))
	if want := "workplace busy: session k1, working since 15:04"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	got = BusyReason(sess("k2", "idle", "x", time.Time{}))
	if want := "workplace busy: session k2, idle since unknown"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestForeignLiveStaleIdle(t *testing.T) {
	t.Parallel()
	s := sess("a", "idle", "x", now0.Add(-48*time.Hour))
	if _, ok := ForeignLive([]Session{s}, nil); !ok {
		t.Fatal("want live")
	}
}

func TestForeignLiveIgnoresOwnAndDone(t *testing.T) {
	t.Parallel()
	own := map[string]bool{"mine": true}
	ss := []Session{sess("a", "working", "mine", now0), sess("b", "done", "x", now0)}
	if got, ok := ForeignLive(ss, own); ok {
		t.Fatalf("got %v, want none", got)
	}
}

func TestBusyOpenRunWorking(t *testing.T) {
	t.Parallel()
	s := sess("a", "working", "mine", now0.Add(-5*time.Hour))
	_, ok := Busy([]Session{s}, map[string]bool{"mine": true}, map[string]bool{"mine": true}, nil, now0, time.Hour)
	if !ok {
		t.Fatal("want busy")
	}
}

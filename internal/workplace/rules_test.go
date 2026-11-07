package workplace

import (
	"reflect"
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
	_, ok := Busy([]Session{s}, Own{"a": "mine"}, map[string]bool{"mine": true}, nil, now0, time.Hour)
	if !ok {
		t.Fatal("want busy")
	}
}

func TestBusyOpenRunDone(t *testing.T) {
	t.Parallel()
	s := sess("a", "done", "mine", now0)
	_, ok := Busy([]Session{s}, Own{"a": "mine"}, map[string]bool{"mine": true}, nil, now0, time.Hour)
	if ok {
		t.Fatal("want not busy")
	}
}

func TestBusyClosedOwnIgnored(t *testing.T) {
	t.Parallel()
	s := sess("a", "working", "mine", now0)
	_, ok := Busy([]Session{s}, Own{"a": "mine"}, map[string]bool{}, nil, now0, time.Hour)
	if ok {
		t.Fatal("want not busy")
	}
}

// A resume row shares the session_id of the earlier row; its late agent has a
// key no row recorded, so it is foreign even though its session_id is known.
func TestBusyUnrecordedKeyWithOwnSessionIDIsForeign(t *testing.T) {
	t.Parallel()
	s := sess("k2", "working", "mine", now0)
	got, ok := Busy([]Session{s}, Own{"k1": "mine"}, map[string]bool{}, nil, now0, time.Hour)
	if !ok || got.Key != "k2" {
		t.Fatalf("got %v %v, want busy k2", got, ok)
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
	got = BusyReason(sess("k3", "", "x", time.Time{}))
	if want := "workplace busy: session k3, unknown since unknown"; got != want {
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
	own := Own{"a": "mine"}
	ss := []Session{sess("a", "working", "mine", now0), sess("b", "done", "x", now0)}
	if got, ok := ForeignLive(ss, own); ok {
		t.Fatalf("got %v, want none", got)
	}
}

func TestForeignLiveUnrecordedKeyWithOwnSessionID(t *testing.T) {
	t.Parallel()
	s := sess("k2", "idle", "mine", now0)
	got, ok := ForeignLive([]Session{s}, Own{"k1": "mine"})
	if !ok || got.Key != "k2" {
		t.Fatalf("got %v %v, want live k2", got, ok)
	}
}

func TestBusyOpenRunWorking(t *testing.T) {
	t.Parallel()
	s := sess("a", "working", "mine", now0.Add(-5*time.Hour))
	_, ok := Busy([]Session{s}, nil, map[string]bool{"mine": true}, nil, now0, time.Hour)
	if !ok {
		t.Fatal("want busy")
	}
}

func TestOwnHas(t *testing.T) {
	t.Parallel()
	own := Own{"k1": "s1"}
	cases := []struct {
		s    Session
		want bool
	}{
		{Session{Key: "k1", SessionID: "s1"}, true},
		{Session{Key: "k1", SessionID: ""}, true},
		{Session{Key: "k1", SessionID: "x"}, false},
		{Session{Key: "k2", SessionID: "s1"}, false},
		{Session{Key: "k2", SessionID: ""}, false},
	}
	for _, c := range cases {
		if got := own.Has(c.s); got != c.want {
			t.Errorf("Has(%+v) = %v, want %v", c.s, got, c.want)
		}
	}
}

// A key adopted with an empty session_id can turn out to be foreign once forge
// fills in another id: then it is foreign, never own.
func TestBusyRecordedKeyWithOtherSessionIDIsForeign(t *testing.T) {
	t.Parallel()
	s := sess("kf", "working", "x", now0)
	got, ok := Busy([]Session{s}, Own{"kf": "mine"}, map[string]bool{}, nil, now0, time.Hour)
	if !ok || got.Key != "kf" {
		t.Fatalf("got %v %v, want busy kf", got, ok)
	}
}

func TestForeignLiveRecordedKeyWithOtherSessionID(t *testing.T) {
	t.Parallel()
	s := sess("kf", "idle", "x", now0)
	if got, ok := ForeignLive([]Session{s}, Own{"kf": "mine"}); !ok || got.Key != "kf" {
		t.Fatalf("got %v %v, want live kf", got, ok)
	}
}

// A claude outlives its run (#206), also in forge state done, and while one lives forge refuses a
// new start in the worktree (#263): every own session of a closed run counts, in any state.
func TestEarlier(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		sessions []Session
		own      Own
		open     map[string]bool
		want     []string
	}{
		{"returned run waiting on a permission", []Session{sess("k1", "approval", "s1", now0)}, Own{"k1": "s1"}, nil, []string{"s1"}},
		{"closed run in forge state done", []Session{sess("k1", "done", "s1", now0)}, Own{"k1": "s1"}, nil, []string{"s1"}},
		{"forge sent no session id", []Session{sess("k1", "idle", "", now0)}, Own{"k1": "s1"}, nil, []string{"s1"}},
		{"open run of the issue", []Session{sess("k1", "working", "s1", now0)}, Own{"k1": "s1"}, map[string]bool{"s1": true}, nil},
		{"open run, forge sent no session id", []Session{sess("k1", "working", "", now0)}, Own{"k1": "s1"}, map[string]bool{"s1": true}, nil},
		{"foreign session", []Session{sess("x", "working", "f1", now0)}, Own{"k1": "s1"}, nil, nil},
		{"recorded key with another session id", []Session{sess("k1", "working", "f1", now0)}, Own{"k1": "s1"}, nil, nil},
		{"resume shares the session id", []Session{sess("k1", "done", "s1", now0), sess("k2", "approval", "s1", now0)}, Own{"k1": "s1", "k2": "s1"}, nil, []string{"s1"}},
		{"list order", []Session{sess("k2", "idle", "s2", now0), sess("x", "idle", "f1", now0), sess("k1", "done", "s1", now0)}, Own{"k1": "s1", "k2": "s2"}, nil, []string{"s2", "s1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Earlier(tc.sessions, tc.own, tc.open); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Earlier = %q, want %q", got, tc.want)
			}
		})
	}
}

package collect

import (
	"testing"
	"time"
)

func at(min int) time.Time {
	return time.Date(2026, 11, 24, 10, min, 0, 0, time.UTC)
}

func ptr(t time.Time) *time.Time { return &t }

func TestWindowsTwoRunsSameSession(t *testing.T) {
	runs := []RunRow{
		{ID: 2, Started: at(30), Ended: ptr(at(40))},
		{ID: 1, Started: at(10), Ended: ptr(at(20))},
	}
	ws := Windows(runs, at(59))
	if len(ws) != 2 || ws[0].RunID != 1 || ws[1].RunID != 2 {
		t.Fatalf("windows not sorted by start: %+v", ws)
	}
	for _, c := range []struct {
		name string
		line time.Time
		want int64
	}{
		{"before first start", at(5), 1},
		{"at first start", at(10), 1},
		{"inside first", at(15), 1},
		{"at end of first (exclusive)", at(20), 1},
		{"between runs, closest before", at(25), 1},
		{"at second start", at(30), 2},
		{"inside second", at(35), 2},
		{"after second end", at(50), 2},
	} {
		if got := RunFor(ws, c.line); got != c.want {
			t.Errorf("%s: run %d, want %d", c.name, got, c.want)
		}
	}
	if ws[0].To != at(20) || ws[1].To != at(40) {
		t.Errorf("To = %v, %v", ws[0].To, ws[1].To)
	}
}

func TestWindowsOpenRun(t *testing.T) {
	ws := Windows([]RunRow{{ID: 7, Started: at(10)}}, at(50))
	if len(ws) != 1 || !ws[0].To.IsZero() || ws[0].From != at(10) {
		t.Fatalf("open run window = %+v", ws)
	}
	if got := RunFor(ws, at(55)); got != 7 {
		t.Errorf("run %d", got)
	}
}

func TestWindowsEmpty(t *testing.T) {
	if ws := Windows(nil, at(1)); len(ws) != 0 {
		t.Fatalf("%+v", ws)
	}
	if got := RunFor(nil, at(1)); got != 0 {
		t.Fatalf("%d", got)
	}
}

func TestPickObservationFirstInWindow(t *testing.T) {
	obs := []Observation{
		{At: at(5), LighthouseVersion: "0"},
		{At: at(10), LighthouseVersion: "1"},
		{At: at(15), LighthouseVersion: "2"},
	}
	got := PickObservation(obs, RunWindow{RunID: 1, From: at(10), To: at(20)})
	if got == nil || got.LighthouseVersion != "1" {
		t.Fatalf("got %+v", got)
	}
	open := PickObservation(obs, RunWindow{RunID: 1, From: at(12)})
	if open == nil || open.LighthouseVersion != "2" {
		t.Fatalf("open window got %+v", open)
	}
}

func TestPickObservationNoneInWindow(t *testing.T) {
	obs := []Observation{{At: at(5)}, {At: at(20)}}
	if got := PickObservation(obs, RunWindow{From: at(10), To: at(20)}); got != nil {
		t.Fatalf("got %+v", got)
	}
}

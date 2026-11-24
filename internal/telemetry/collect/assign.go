// Package collect assigns transcript lines to runs and keeps the per-session sidecar of
// lighthouse and pack versions that the SessionStart hook writes and the collector reads.
package collect

import (
	"sort"
	"time"
)

// RunRow is the part of a runs row that decides which transcript lines belong to the run.
type RunRow struct {
	ID      int64
	Started time.Time
	Ended   *time.Time
}

// RunWindow is the stretch of a transcript that belongs to one run: From inclusive, To
// exclusive. A zero To is open.
type RunWindow struct {
	RunID    int64
	From, To time.Time
}

// Windows returns one window per run that started at or before readingEnd, ordered by start.
// A run that has ended closes at its end; one that has not closes at the start of the next
// run, or stays open when there is none.
func Windows(runs []RunRow, readingEnd time.Time) []RunWindow {
	sorted := make([]RunRow, 0, len(runs))
	for _, r := range runs {
		if !r.Started.After(readingEnd) {
			sorted = append(sorted, r)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Started.Before(sorted[j].Started) })
	ws := make([]RunWindow, len(sorted))
	for i, r := range sorted {
		w := RunWindow{RunID: r.ID, From: r.Started}
		switch {
		case r.Ended != nil:
			w.To = *r.Ended
		case i+1 < len(sorted):
			w.To = sorted[i+1].Started
		}
		ws[i] = w
	}
	return ws
}

// RunFor returns the run a line at t belongs to: the window that holds it, else the run that
// started closest before it, else (t before every start) the first run. It is 0 without windows.
// ws must come from Windows.
func RunFor(ws []RunWindow, t time.Time) int64 {
	if len(ws) == 0 {
		return 0
	}
	var before int64
	found := false
	for _, w := range ws {
		if t.Before(w.From) {
			break
		}
		if w.To.IsZero() || t.Before(w.To) {
			return w.RunID
		}
		before, found = w.RunID, true
	}
	if found {
		return before
	}
	return ws[0].RunID
}

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

package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// rolloutWindow is a window of a rate_limits line; a pointer field tells a
// missing value from a zero.
type rolloutWindow struct {
	UsedPercent *float64 `json:"used_percent"`
	ResetsAt    *float64 `json:"resets_at"`
}

type rolloutLine struct {
	Timestamp string `json:"timestamp"`
	Payload   struct {
		RateLimits struct {
			LimitID   *string        `json:"limit_id"`
			Primary   *rolloutWindow `json:"primary"`
			Secondary *rolloutWindow `json:"secondary"`
		} `json:"rate_limits"`
	} `json:"payload"`
}

// readRollout returns the newest usable rate_limits line in the session logs
// of all homes. The log is as old as the last `codex exec` in that home, so
// the reading is a lower bound.
func readRollout(homes []string, now time.Time) (CodexState, error) {
	var best CodexState
	var bestTime time.Time
	for _, home := range homes {
		root := filepath.Join(home, "sessions")
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil // a missing or unreadable directory is empty
			}
			if ok, _ := filepath.Match("rollout-*.jsonl", d.Name()); !ok {
				return nil
			}
			scanRollout(path, func(at time.Time, session, week CodexWindow) {
				if bestTime.IsZero() || at.After(bestTime) {
					bestTime = at
					best = CodexState{SourceFile: path, Session: session, Week: week}
				}
			})
			return nil
		})
	}
	if bestTime.IsZero() {
		return CodexState{}, errors.New("no usable rate_limits line in the session logs")
	}
	best.Source = "rollout-log"
	best.GeneratedAt = now.UTC().Format(timeLayout)
	best.AgeS = int(now.Sub(bestTime).Seconds())
	best.LowerBound = true
	return best, nil
}

// scanRollout calls found for every usable rate_limits line of one file.
// An unreadable file is skipped; what was read before the error stays usable.
// ReadBytes has no line limit: a huge tool output must not hide later lines.
func scanRollout(path string, found func(at time.Time, session, week CodexWindow)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	br := bufio.NewReader(f)
	for {
		line, err := br.ReadBytes('\n')
		if bytes.Contains(line, []byte("rate_limits")) {
			if at, session, week, ok := parseRolloutLine(line); ok {
				found(at, session, week)
			}
		}
		if err != nil {
			return // io.EOF or a read error
		}
	}
}

// parseRolloutLine accepts limit_id "codex" or none, a timestamp with a time
// zone, and primary and secondary as objects with both numbers.
func parseRolloutLine(line []byte) (at time.Time, session, week CodexWindow, ok bool) {
	var rec rolloutLine
	if json.Unmarshal(bytes.TrimSpace(line), &rec) != nil {
		return
	}
	limits := rec.Payload.RateLimits
	if limits.LimitID != nil && *limits.LimitID != "codex" {
		return
	}
	at, err := time.Parse(time.RFC3339Nano, rec.Timestamp) // fails without a zone
	if err != nil {
		return
	}
	session, sok := rolloutWindowState(limits.Primary)
	week, wok := rolloutWindowState(limits.Secondary)
	return at, session, week, sok && wok
}

func rolloutWindowState(w *rolloutWindow) (CodexWindow, bool) {
	if w == nil || w.UsedPercent == nil || w.ResetsAt == nil {
		return CodexWindow{}, false
	}
	return CodexWindow{
		Pct:     int(*w.UsedPercent),
		ResetAt: time.Unix(int64(*w.ResetsAt), 0).UTC().Format(timeLayout),
	}, true
}

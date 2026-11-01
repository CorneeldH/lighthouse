package workplace

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ErrNotMeasurable says the host holds no transcript to measure from.
var ErrNotMeasurable = errors.New("workplace: not measurable")

// Transcripts is an optional interface next to Workplace. A workplace without
// a transcript on the host returns ErrNotMeasurable.
type Transcripts interface {
	TranscriptDir(ctx context.Context, workspaceID string) (string, error)
}

// Transcript is what the dispatcher reads from the transcript of one session.
type Transcript struct {
	// LastEvent is the newest event over all threads.
	LastEvent time.Time `json:"last_event"`
	// Threads is the newest event per thread: "" is the main transcript, the
	// file name without ".jsonl" a subagent.
	Threads map[string]time.Time `json:"threads,omitempty"`
	// UnknownCommand is the command the session failed to start on, "" if none.
	UnknownCommand string `json:"unknown_command,omitempty"`
	// AssistantTurn is true once the main transcript holds an assistant line.
	AssistantTurn bool `json:"assistant_turn"`
}

// ProjectDir is the directory Claude Code keeps the transcripts of cwd in:
// every character outside [A-Za-z0-9] becomes a '-'.
func ProjectDir(home, cwd string) string {
	b := []byte(cwd)
	for i, c := range b {
		alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !alnum {
			b[i] = '-'
		}
	}
	return filepath.Join(home, ".claude", "projects", string(b))
}

const tailWindow = 64 << 10

// ReadTranscript reads <dir>/<sessionID>.jsonl and the subagent transcripts
// under <dir>/<sessionID>/subagents. The error wraps fs.ErrNotExist when the
// main transcript is missing, and is ErrNotMeasurable when no thread has a
// line with a timestamp.
func ReadTranscript(dir, sessionID string) (Transcript, error) {
	main := filepath.Join(dir, sessionID+".jsonl")
	t := Transcript{Threads: map[string]time.Time{}}
	last, ok, err := lastTimestamp(main)
	if err != nil {
		return Transcript{}, err
	}
	if ok {
		t.Threads[""] = last
	}
	subs, err := filepath.Glob(filepath.Join(dir, sessionID, "subagents", "*.jsonl"))
	if err != nil {
		return Transcript{}, err
	}
	for _, p := range subs {
		last, ok, err := lastTimestamp(p)
		if err != nil {
			return Transcript{}, err
		}
		if ok {
			t.Threads[strings.TrimSuffix(filepath.Base(p), ".jsonl")] = last
		}
	}
	if len(t.Threads) == 0 {
		return Transcript{}, fmt.Errorf("%s: %w", main, ErrNotMeasurable)
	}
	for _, ts := range t.Threads {
		if ts.After(t.LastEvent) {
			t.LastEvent = ts
		}
	}
	if t.AssistantTurn, t.UnknownCommand, err = scanStart(main); err != nil {
		return Transcript{}, err
	}
	return t, nil
}

// lastTimestamp is the newest timestamp in the tail of the file. A window that
// holds none doubles until the start of the file is reached.
func lastTimestamp(path string) (time.Time, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return time.Time{}, false, err
	}
	size := st.Size()
	for window := int64(tailWindow); ; window *= 2 {
		start := max(size-window, 0)
		buf := make([]byte, size-start)
		if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
			return time.Time{}, false, err
		}
		if start > 0 { // the first line of the window may be cut
			if i := bytes.IndexByte(buf, '\n'); i >= 0 {
				buf = buf[i+1:]
			} else {
				buf = nil
			}
		}
		var newest time.Time
		for _, line := range bytes.Split(buf, []byte{'\n'}) {
			if ts, ok := lineTimestamp(line); ok && ts.After(newest) {
				newest = ts
			}
		}
		if !newest.IsZero() {
			return newest, true, nil
		}
		if start == 0 {
			return time.Time{}, false, nil
		}
	}
}

// lineTimestamp is the timestamp of one line; a line that does not parse, or
// has none, has none.
func lineTimestamp(line []byte) (time.Time, bool) {
	var v struct {
		Timestamp string `json:"timestamp"`
	}
	if json.Unmarshal(line, &v) != nil || v.Timestamp == "" {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, v.Timestamp)
	return ts, err == nil
}

var unknownCommand = regexp.MustCompile(`Unknown (?:command|skill): (\S+)`)

// unknownIn is the command named by a start-failure message, "" if content is
// not one. Only a string content counts.
func unknownIn(content any) string {
	text, ok := content.(string)
	if !ok {
		return ""
	}
	if m := unknownCommand.FindStringSubmatch(text); m != nil {
		return strings.TrimRight(m[1], ".?,")
	}
	return ""
}

// scanStart streams the main transcript from the top to the first assistant
// line. No line has a size limit.
func scanStart(path string) (assistant bool, unknown string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return false, "", err
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReader(f)
	for {
		line, rerr := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var v struct {
				Type    string `json:"type"`
				Content any    `json:"content"`
			}
			if json.Unmarshal(line, &v) == nil {
				if v.Type == "assistant" {
					return true, unknown, nil
				}
				if unknown == "" && v.Type == "system" {
					unknown = unknownIn(v.Content)
				}
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return false, unknown, nil
			}
			return false, "", rerr
		}
	}
}

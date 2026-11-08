package workplace

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ErrNotMeasurable says the host holds no transcript to measure from.
var ErrNotMeasurable = errors.New("workplace: not measurable")

// Transcripts is an optional interface next to Workplace. Transcript reads the
// transcript of one session as ReadTranscript does: an error that wraps
// fs.ErrNotExist or is ErrNotMeasurable says there is nothing to measure.
type Transcripts interface {
	Transcript(ctx context.Context, workspaceID, sessionID string) (Transcript, error)
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
	// Background counts the background tasks the main transcript launched, a Bash task with
	// run_in_background or an async subagent, that have not ended: no task-notification and no
	// TaskStop names them after their last start. A SendMessage that resumes a subagent starts
	// it again (#299). Claude ends its turn while they run, and a Stop hook can then make the
	// forge report the session done (#270).
	Background int `json:"background,omitempty"`
	// TurnOpen is true when a user or assistant line follows the last stop_hook_summary of the
	// main transcript: claude took a turn after its Stop, on the notification of a background
	// task, and the forge stand from that Stop is stale. Without a stop_hook_summary (no Stop
	// hooks) it stays false.
	TurnOpen bool `json:"turn_open,omitempty"`
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

// HostTranscriptDir is the directory Claude Code keeps the transcripts of worktree in, under
// home ("": os.UserHomeDir). A directory that is not on this host is ErrNotMeasurable.
func HostTranscriptDir(home, workspaceID, worktree string) (string, error) {
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return "", fmt.Errorf("transcripts of %s: %w", workspaceID, err)
		}
	}
	dir := ProjectDir(home, worktree)
	st, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("transcripts of %s: %s is not on this host: %w", workspaceID, dir, ErrNotMeasurable)
	case err != nil:
		return "", fmt.Errorf("transcripts of %s: %w", workspaceID, err)
	case !st.IsDir():
		return "", fmt.Errorf("transcripts of %s: %s is no directory: %w", workspaceID, dir, ErrNotMeasurable)
	}
	return dir, nil
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
	if t.Background, t.TurnOpen, err = scanWork(main); err != nil {
		return Transcript{}, err
	}
	return t, nil
}

var taskID = regexp.MustCompile(`<task-id>([^<\s]+)</task-id>`)

// workMarks are the strings a line needs to matter to scanWork; a line without any of them is
// not decoded.
var workMarks = [][]byte{
	[]byte(`"backgroundTaskId"`), []byte(`"async_launched"`), []byte(`"resumedAgentId"`),
	[]byte(`<task-id>`), []byte(`"task_id"`),
	[]byte(`"stop_hook_summary"`), []byte(`"type":"user"`), []byte(`"type":"assistant"`),
}

// work is what scanWork gathers from the lines of the main transcript.
type work struct {
	running  map[string]bool // per task: started and not ended since, in transcript order
	summary  bool            // a stop_hook_summary was seen
	turnOpen bool            // a user or assistant line follows the last stop_hook_summary
}

// scanWork streams the main transcript for the work that still runs after a Stop: the
// background tasks whose last start has no end after it, and whether a turn opened after the
// last stop_hook_summary. A start is the tool result of a Bash call with a backgroundTaskId,
// of an Agent call with status async_launched, or of a SendMessage that resumed a subagent
// (resumedAgentId), which runs again after its earlier task-notification (#299). An end is a
// <task-id> in any line but an assistant line (the task-notification), or the result of a
// TaskStop. A subagent's own launches stay out: the async agent is already a task of the main
// transcript.
func scanWork(path string) (background int, turnOpen bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = f.Close() }()
	w := work{running: map[string]bool{}}
	r := bufio.NewReader(f)
	for {
		line, rerr := r.ReadBytes('\n')
		if hasAny(line, workMarks) {
			w.read(line)
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) {
				return 0, false, rerr
			}
			break
		}
	}
	for _, running := range w.running {
		if running {
			background++
		}
	}
	return background, w.turnOpen, nil
}

// read takes in one line: a turn boundary, the task it starts, or the tasks it ends.
func (w *work) read(line []byte) {
	var v struct {
		Type          string          `json:"type"`
		Subtype       string          `json:"subtype"`
		ToolUseResult json.RawMessage `json:"toolUseResult"`
	}
	if json.Unmarshal(line, &v) != nil {
		return
	}
	switch v.Type {
	case "system":
		if v.Subtype == "stop_hook_summary" {
			w.summary, w.turnOpen = true, false
		}
		return
	case "assistant":
		w.turnOpen = w.summary
		return
	case "user":
		w.turnOpen = w.summary
	}
	for _, m := range taskID.FindAllSubmatch(line, -1) {
		w.running[string(m[1])] = false
	}
	var res struct {
		BackgroundTaskID string `json:"backgroundTaskId"`
		Status           string `json:"status"`
		AgentID          string `json:"agentId"`
		ResumedAgentID   string `json:"resumedAgentId"`
		TaskID           string `json:"task_id"`
		Message          string `json:"message"`
	}
	if len(v.ToolUseResult) == 0 || json.Unmarshal(v.ToolUseResult, &res) != nil {
		return // no result, or a plain string such as an error
	}
	switch {
	case res.BackgroundTaskID != "":
		w.running[res.BackgroundTaskID] = true
	case res.Status == "async_launched" && res.AgentID != "":
		w.running[res.AgentID] = true
	case res.ResumedAgentID != "":
		w.running[res.ResumedAgentID] = true
	case res.TaskID != "" && res.Message != "":
		w.running[res.TaskID] = false
	}
}

func hasAny(line []byte, marks [][]byte) bool {
	for _, m := range marks {
		if bytes.Contains(line, m) {
			return true
		}
	}
	return false
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

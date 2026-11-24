// Package history reads Claude Code transcripts and measures how a repo is worked in.
package history

import (
	"bufio"
	"encoding/json"
	"os"
	"time"
)

// Record is the metadata of one transcript line. It never holds prompt text, tool output
// or file contents.
type Record struct {
	File                                                   string
	Line                                                   int
	Type, Timestamp, GitBranch, Cwd, Entrypoint, SessionID string
	Commands                                               []string // input.command of Bash tool_use
	WrittenPaths                                           []string // input.file_path of Write and Edit tool_use
	PRNumber                                               int      // pr-link
	HookName, HookCommand                                  string   // hook_blocking_error attachment
}

type rawLine struct {
	Type       string          `json:"type"`
	Timestamp  string          `json:"timestamp"`
	GitBranch  string          `json:"gitBranch"`
	Cwd        string          `json:"cwd"`
	Entrypoint string          `json:"entrypoint"`
	SessionID  string          `json:"sessionId"`
	PRNumber   int             `json:"prNumber"`
	Message    json.RawMessage `json:"message"`
	Attachment json.RawMessage `json:"attachment"`
}

type rawMessage struct {
	Content json.RawMessage `json:"content"`
}

type rawBlock struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Input struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
	} `json:"input"`
}

type rawAttachment struct {
	Type          string          `json:"type"`
	HookName      string          `json:"hookName"`
	BlockingError json.RawMessage `json:"blockingError"`
}

const maxLine = 64 << 20

// ReadFile reads one transcript line by line and calls fn per record. A line that is not
// JSON counts as skipped. A record whose timestamp is not later than since is dropped; a
// record without a usable timestamp is kept.
func ReadFile(path string, since time.Time, fn func(Record)) (skipped int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	n := 0
	for sc.Scan() {
		n++
		b := sc.Bytes()
		if len(b) == 0 {
			continue
		}
		var raw rawLine
		if json.Unmarshal(b, &raw) != nil {
			skipped++
			continue
		}
		if !since.IsZero() {
			if ts, ok := parseTime(raw.Timestamp); ok && !ts.After(since) {
				continue
			}
		}
		r := Record{File: path, Line: n, Type: raw.Type, Timestamp: raw.Timestamp, GitBranch: raw.GitBranch,
			Cwd: raw.Cwd, Entrypoint: raw.Entrypoint, SessionID: raw.SessionID, PRNumber: raw.PRNumber}
		switch raw.Type {
		case "assistant":
			fillTools(&r, raw.Message)
		case "attachment":
			fillHook(&r, raw.Attachment)
		}
		fn(r)
	}
	return skipped, sc.Err()
}

func fillTools(r *Record, msg json.RawMessage) {
	var m rawMessage
	if json.Unmarshal(msg, &m) != nil {
		return
	}
	var blocks []rawBlock
	if json.Unmarshal(m.Content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		switch b.Name {
		case "Bash":
			if b.Input.Command != "" {
				r.Commands = append(r.Commands, b.Input.Command)
			}
		case "Write", "Edit":
			if b.Input.FilePath != "" {
				r.WrittenPaths = append(r.WrittenPaths, b.Input.FilePath)
			}
		}
	}
}

func fillHook(r *Record, att json.RawMessage) {
	var a rawAttachment
	if json.Unmarshal(att, &a) != nil || a.Type != "hook_blocking_error" {
		return
	}
	r.HookName = a.HookName
	var be struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(a.BlockingError, &be) == nil {
		r.HookCommand = be.Command
	}
}

func parseTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

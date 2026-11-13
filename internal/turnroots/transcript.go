// Package turnroots is the Go port of pack/core/lib/turn_roots.py (#54, #80): in which checkouts
// of this project did the turn write, and did it touch a path that matches a pattern there?
//
// The Stop gates check a checkout. The `cwd` of the Stop payload is the shell's directory at that
// moment, not where the turn changed files, so the turn is read from `transcript_path` instead:
// from the last prompt on, plus the subagent entries since that moment. See turn_roots.py for the
// measurement behind it.
package turnroots

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

// Undetermined says the checkout of the turn cannot be determined; Msg says why.
type Undetermined struct{ Msg string }

func (e *Undetermined) Error() string { return e.Msg }

// entries are the JSON objects of a transcript; a line that is no object is skipped.
func entries(path string) ([]map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []map[string]any
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var v any
			if json.Unmarshal(line, &v) == nil {
				if entry, ok := v.(map[string]any); ok {
					out = append(out, entry)
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// strerror is the Python `OSError.strerror` of err: the errno text with a capital, as
// turn_roots.py printed it.
func strerror(err error) string {
	var errno syscall.Errno
	msg := err.Error()
	if errors.As(err, &errno) {
		msg = errno.Error()
	}
	r, size := utf8.DecodeRuneInString(msg)
	return string(unicode.ToUpper(r)) + msg[size:]
}

// truthy is Python truthiness for a decoded JSON value.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// content is `(entry.get("message") or {}).get("content")`.
func content(entry map[string]any) any {
	message, _ := entry["message"].(map[string]any)
	return message["content"]
}

// isPrompt says whether entry starts a turn: a `user` entry that is no `isMeta` and no
// `isCompactSummary`, with a string content or text blocks without a `tool_result`. An
// auto-compaction summary is a string-content user entry without isMeta; taking it for a prompt
// would restart the turn in the middle of it.
func isPrompt(entry map[string]any) bool {
	if entry["type"] != "user" || truthy(entry["isMeta"]) || truthy(entry["isCompactSummary"]) {
		return false
	}
	switch c := content(entry).(type) {
	case string:
		return true
	case []any:
		text, result := false, false
		for _, block := range c {
			if b, ok := block.(map[string]any); ok {
				text = text || b["type"] == "text"
				result = result || b["type"] == "tool_result"
			}
		}
		return text && !result
	}
	return false
}

// moment is the `timestamp` of entry; false when it is missing or unreadable. A `Z` and an offset
// both work.
func moment(entry map[string]any) (time.Time, bool) {
	stamp, ok := entry["timestamp"].(string)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, stamp)
	return t, err == nil
}

// toolUse is one tool call of the turn; cwd is the `cwd` of its entry, "" when it is no string.
type toolUse struct {
	name  string
	input map[string]any
	cwd   string
	hasWd bool
}

// toolUses are the tool calls in entries.
func toolUses(turn []map[string]any) []toolUse {
	var uses []toolUse
	for _, entry := range turn {
		if entry["type"] != "assistant" {
			continue
		}
		blocks, ok := content(entry).([]any)
		if !ok {
			continue
		}
		cwd, hasWd := entry["cwd"].(string)
		for _, block := range blocks {
			b, ok := block.(map[string]any)
			if !ok || b["type"] != "tool_use" {
				continue
			}
			if input, ok := b["input"].(map[string]any); ok {
				name, _ := b["name"].(string)
				uses = append(uses, toolUse{name: name, input: input, cwd: cwd, hasWd: hasWd})
			}
		}
	}
	return uses
}

// withoutSuffix is pathlib's `with_suffix("")`: the last suffix of the final component dropped.
func withoutSuffix(path string) string {
	name := filepath.Base(path)
	if i := strings.LastIndex(name, "."); i > 0 && i < len(name)-1 {
		return path[:len(path)-(len(name)-i)]
	}
	return path
}

// turnEntries are the entries of the current turn and the moment of its prompt (ok false without a
// readable `timestamp`): the main transcript from the last prompt, plus the subagent entries from
// that moment on.
func turnEntries(transcript string) (turn []map[string]any, start time.Time, ok bool, err error) {
	all, err := entries(transcript)
	if err != nil {
		return nil, time.Time{}, false, &Undetermined{"the transcript " + transcript + " cannot be read (" + strerror(err) + ")."}
	}
	var prompt map[string]any
	for _, entry := range all {
		if isPrompt(entry) {
			turn, prompt = []map[string]any{}, entry
		} else if prompt != nil {
			turn = append(turn, entry)
		}
	}
	if prompt == nil {
		return nil, time.Time{}, false, &Undetermined{"no prompt in the transcript " + transcript + "."}
	}

	start, ok = moment(prompt)
	subagents := filepath.Join(withoutSuffix(transcript), "subagents")
	if info, statErr := os.Stat(subagents); !ok || statErr != nil || !info.IsDir() {
		return turn, start, ok, nil
	}
	// `glob("*.jsonl")` without filepath.Glob: the transcript directory may hold a `[`.
	dir, _ := os.ReadDir(subagents)
	var paths []string
	for _, e := range dir {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			paths = append(paths, filepath.Join(subagents, e.Name()))
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		sub, err := entries(path)
		if err != nil {
			return nil, time.Time{}, false, &Undetermined{"the subagent transcript " + path + " cannot be read (" + strerror(err) + ")."}
		}
		for _, entry := range sub {
			if m, has := moment(entry); has && !m.Before(start) {
				turn = append(turn, entry)
			}
		}
	}
	return turn, start, ok, nil
}

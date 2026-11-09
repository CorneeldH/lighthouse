package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/hook"
)

func init() { hook.Register("permission-check", permissionCheck) }

const (
	permPrefix   = "permission-check: COULD NOT CHECK — "
	commandChars = 80
)

var (
	permissionKinds = map[string]bool{"permission_prompt": true, "worker_permission_prompt": true}
	pathKeys        = []string{"file_path", "path", "notebook_path"}
	toUse           = regexp.MustCompile(`to use (\S+)`)
)

// permissionCheck notes a permission prompt of a controller run in
// <LIGHTHOUSE_STATE_DIR>/runs/<run>/permission.json, so the dispatcher can tell a session
// waiting for a permission from a silent one. Notification hook: always code 0, never output;
// a note that cannot be written says COULD NOT CHECK on stderr.
func permissionCheck(_ context.Context, _ string, _ string, p hook.Payload) hook.Result {
	run := os.Getenv("LIGHTHOUSE_CONTROLLER_RUN")
	if !uuidShape.MatchString(run) {
		return hook.Result{}
	}
	state := os.Getenv("LIGHTHOUSE_STATE_DIR")
	if state == "" {
		return hook.Result{Stderr: permPrefix + "LIGHTHOUSE_STATE_DIR is empty\n"}
	}
	if err := notePermission(filepath.Join(state, "runs", run, "permission.json"), p.Raw, time.Now()); err != nil {
		return hook.Result{Stderr: permPrefix + strings.ReplaceAll(err.Error(), "\n", " ") + "\n"}
	}
	return hook.Result{}
}

// permissionNote is the file the dispatcher reads; the field order is the contract's.
type permissionNote struct {
	Version int    `json:"version"`
	At      string `json:"at"`
	Tool    string `json:"tool"`
	Path    string `json:"path"`
	Thread  string `json:"thread"`
	Message string `json:"message"`
}

// notePermission writes the note for a permission prompt; any other payload writes nothing.
func notePermission(target string, raw []byte, now time.Time) error {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	data, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	if kind, _ := data["notification_type"].(string); !permissionKinds[kind] {
		return nil
	}
	message, _ := data["message"].(string)
	var found *toolUseAt
	if transcript, _ := data["transcript_path"].(string); transcript != "" {
		found = newestToolUse(transcript)
	}
	if found == nil {
		found = &toolUseAt{}
		if m := toUse.FindStringSubmatch(message); m != nil {
			found.tool = m[1]
		}
	}
	return writeNote(target, permissionNote{
		Version: 1,
		// Milliseconds, as transcript lines have them: the tool_use behind the prompt is
		// usually written in the same second.
		At:      now.UTC().Format("2006-01-02T15:04:05.000Z"),
		Tool:    found.tool,
		Path:    found.path,
		Thread:  found.thread,
		Message: message,
	})
}

type toolUseAt struct {
	at                 time.Time
	tool, path, thread string
}

// newestToolUse is the newest tool_use (by timestamp) in transcript and in
// <transcript without its suffix>/subagents/*.jsonl; nil without one. On a tie the later one
// read wins, the subagent files in name order after the main transcript.
func newestToolUse(transcript string) *toolUseAt {
	best := newestIn(transcript, "", nil)
	base := filepath.Base(transcript)
	stem := transcript
	if ext := filepath.Ext(base); ext != base {
		stem = strings.TrimSuffix(transcript, ext)
	}
	files, _ := filepath.Glob(filepath.Join(stem, "subagents", "*.jsonl"))
	for _, f := range files {
		best = newestIn(f, strings.TrimSuffix(filepath.Base(f), ".jsonl"), best)
	}
	return best
}

// newestIn is the newest tool_use of file, if not older than best; an unreadable file or a bad
// line is skipped.
func newestIn(file, thread string, best *toolUseAt) *toolUseAt {
	b, err := os.ReadFile(file)
	if err != nil {
		return best
	}
	text := strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", "\n"), "\r", "\n")
	for _, line := range strings.Split(text, "\n") {
		var entry struct {
			Type      string `json:"type"`
			Timestamp any    `json:"timestamp"`
			Message   struct {
				Content []any `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &entry) != nil || entry.Type != "assistant" {
			continue
		}
		at := moment(entry.Timestamp)
		for _, c := range entry.Message.Content {
			block, _ := c.(map[string]any)
			if block["type"] != "tool_use" {
				continue
			}
			name, ok := block["name"].(string)
			input, isMap := block["input"].(map[string]any)
			if !ok || !isMap {
				continue
			}
			if best == nil || !at.Before(best.at) {
				best = &toolUseAt{at: at, tool: name, path: toolPath(name, input), thread: thread}
			}
		}
	}
	return best
}

// moment is the timestamp of a transcript line; the zero time, older than all, when it has none.
// A time without an offset is UTC.
func moment(v any) time.Time {
	s, ok := v.(string)
	if !ok {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// toolPath is the first 80 characters of a Bash command, else the first path argument.
func toolPath(name string, input map[string]any) string {
	if name == "Bash" {
		command, _ := input["command"].(string)
		if r := []rune(command); len(r) > commandChars {
			return string(r[:commandChars])
		}
		return command
	}
	for _, key := range pathKeys {
		if s, ok := input[key].(string); ok {
			return s
		}
	}
	return ""
}

// writeNote writes note atomically with mode 0600: a tmp file in the same dir, then rename.
func writeNote(target string, note permissionNote) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(note); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".permission-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(buf.Bytes())
	cerr := tmp.Close()
	err = errors.Join(werr, cerr)
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o600)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), target)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

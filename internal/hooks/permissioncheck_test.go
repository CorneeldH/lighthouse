package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Classification: this is a hook/gate change; the likely regression surface is the
// permission.json a controller run gets (fields, mode, place) and the COULD NOT CHECK line.

const permMessage = "Claude needs your permission to use Write"

func toolUse(tool string, input map[string]any, stamp string) map[string]any {
	return map[string]any{
		"type":      "assistant",
		"timestamp": stamp,
		"message": map[string]any{
			"role":    "assistant",
			"content": []any{map[string]any{"type": "tool_use", "id": "t1", "name": tool, "input": input}},
		},
	}
}

func transcript(t *testing.T, path string, entries ...map[string]any) string {
	t.Helper()
	var b strings.Builder
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteString("\n")
	}
	return writeFile(t, path, b.String())
}

func permPayload(transcript, kind string) map[string]any {
	return map[string]any{
		"hook_event_name":   "Notification",
		"session_id":        "s1",
		"transcript_path":   transcript,
		"notification_type": kind,
		"message":           permMessage,
	}
}

// permEnv sets a controller run and a state dir; it returns the state dir.
func permEnv(t *testing.T) string {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", gateRun)
	t.Setenv("LIGHTHOUSE_STATE_DIR", state)
	return state
}

func readNote(t *testing.T, state string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(state, "runs", gateRun, "permission.json"))
	if err != nil {
		t.Fatalf("permission.json: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("permission.json %q: %v", b, err)
	}
	return m
}

func noteFields(m map[string]any) [3]any { return [3]any{m["tool"], m["path"], m["thread"]} }

func TestPermissionOutsideARunWritesNothing(t *testing.T) {
	for _, run := range []string{"", "../x", "ABC"} {
		state := permEnv(t)
		t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", run)
		tr := transcript(t, filepath.Join(t.TempDir(), "t.jsonl"))
		res := call(t, permissionCheck, "", permPayload(tr, "permission_prompt"))
		if res.Code != 0 || res.Output != nil || res.Stderr != "" {
			t.Errorf("run %q: %+v", run, res)
		}
		if _, err := os.Stat(filepath.Join(state, "runs")); !os.IsNotExist(err) {
			t.Errorf("run %q: runs dir exists (%v)", run, err)
		}
	}
}

func TestPermissionWritesToolAndPath(t *testing.T) {
	state := permEnv(t)
	tr := transcript(t, filepath.Join(t.TempDir(), "t.jsonl"),
		toolUse("Read", map[string]any{"file_path": "/tmp/old"}, "2026-11-01T10:00:00Z"),
		toolUse("Write", map[string]any{"file_path": "/tmp/x"}, "2026-11-01T10:00:05Z"),
	)
	res := call(t, permissionCheck, "", permPayload(tr, "permission_prompt"))
	if res.Code != 0 || res.Output != nil || res.Stderr != "" {
		t.Fatalf("%+v", res)
	}
	m := readNote(t, state)
	if m["version"] != float64(1) || m["message"] != permMessage || noteFields(m) != [3]any{"Write", "/tmp/x", ""} {
		t.Errorf("note %v", m)
	}
	path := filepath.Join(state, "runs", gateRun, "permission.json")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("run dir has %d entries, want only permission.json", len(entries))
	}
}

func TestPermissionNewestToolUse(t *testing.T) {
	tests := []struct {
		name       string
		main, sub  map[string]any
		wantFields [3]any
	}{
		{"subagent newer", toolUse("Read", map[string]any{"file_path": "/tmp/main"}, "2026-11-01T10:00:00Z"),
			toolUse("Edit", map[string]any{"file_path": "/tmp/sub"}, "2026-11-01T10:00:09Z"),
			[3]any{"Edit", "/tmp/sub", "agent-a"}},
		{"main newer", toolUse("Write", map[string]any{"file_path": "/tmp/main"}, "2026-11-01T10:00:20Z"),
			toolUse("Edit", map[string]any{"file_path": "/tmp/sub"}, "2026-11-01T10:00:09Z"),
			[3]any{"Write", "/tmp/main", ""}},
		{"tie goes to the later file", toolUse("Write", map[string]any{"file_path": "/tmp/main"}, "2026-11-01T10:00:09Z"),
			toolUse("Edit", map[string]any{"path": "/tmp/sub"}, "2026-11-01T10:00:09Z"),
			[3]any{"Edit", "/tmp/sub", "agent-a"}},
		{"offset is compared as a moment", toolUse("Write", map[string]any{"file_path": "/tmp/main"}, "2026-11-01T11:00:00+02:00"),
			toolUse("NotebookEdit", map[string]any{"notebook_path": "/tmp/nb"}, "2026-11-01T10:00:00Z"),
			[3]any{"NotebookEdit", "/tmp/nb", "agent-a"}},
		{"no timestamp is oldest", toolUse("Write", map[string]any{"file_path": "/tmp/main"}, "2026-11-01T10:00:00Z"),
			toolUse("Edit", map[string]any{"file_path": "/tmp/sub"}, "garbage"),
			[3]any{"Write", "/tmp/main", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := permEnv(t)
			dir := t.TempDir()
			tr := transcript(t, filepath.Join(dir, "t.jsonl"), tt.main)
			transcript(t, filepath.Join(dir, "t", "subagents", "agent-a.jsonl"), tt.sub)
			call(t, permissionCheck, "", permPayload(tr, "permission_prompt"))
			if got := noteFields(readNote(t, state)); got != tt.wantFields {
				t.Errorf("got %v, want %v", got, tt.wantFields)
			}
		})
	}
}

func TestPermissionBashPathIsFirst80Chars(t *testing.T) {
	state := permEnv(t)
	command := "echo é" + strings.Repeat("x", 200)
	tr := transcript(t, filepath.Join(t.TempDir(), "t.jsonl"),
		toolUse("Bash", map[string]any{"command": command}, "2026-11-01T10:00:00Z"))
	call(t, permissionCheck, "", permPayload(tr, "permission_prompt"))
	want := "echo é" + strings.Repeat("x", 74)
	if got := readNote(t, state)["path"]; got != want {
		t.Errorf("path %q, want %q", got, want)
	}
}

func TestPermissionKinds(t *testing.T) {
	tests := []struct {
		kind  string
		wants bool
	}{
		{"permission_prompt", true},
		{"worker_permission_prompt", true},
		{"idle_prompt", false},
	}
	for _, tt := range tests {
		state := permEnv(t)
		tr := transcript(t, filepath.Join(t.TempDir(), "t.jsonl"),
			toolUse("Write", map[string]any{"file_path": "/tmp/x"}, "2026-11-01T10:00:00Z"))
		res := call(t, permissionCheck, "", permPayload(tr, tt.kind))
		_, err := os.Stat(filepath.Join(state, "runs"))
		if got := err == nil; got != tt.wants || res.Stderr != "" {
			t.Errorf("%s: written %v stderr %q, want written %v", tt.kind, got, res.Stderr, tt.wants)
		}
	}
}

func TestPermissionFallsBackToMessage(t *testing.T) {
	tests := []struct {
		name, message string
		want          [3]any
	}{
		{"tool named", permMessage, [3]any{"Write", "", ""}},
		{"no tool", "Claude needs your permission", [3]any{"", "", ""}},
	}
	for _, tt := range tests {
		state := permEnv(t)
		p := permPayload(filepath.Join(t.TempDir(), "missing.jsonl"), "permission_prompt")
		p["message"] = tt.message
		call(t, permissionCheck, "", p)
		if got := noteFields(readNote(t, state)); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestPermissionSecondPromptOverwrites(t *testing.T) {
	state := permEnv(t)
	tr := filepath.Join(t.TempDir(), "t.jsonl")
	transcript(t, tr, toolUse("Write", map[string]any{"file_path": "/tmp/a"}, "2026-11-01T10:00:00Z"))
	call(t, permissionCheck, "", permPayload(tr, "permission_prompt"))
	transcript(t, tr, toolUse("Edit", map[string]any{"file_path": "/tmp/b"}, "2026-11-01T10:01:00Z"))
	call(t, permissionCheck, "", permPayload(tr, "permission_prompt"))
	if got := readNote(t, state)["path"]; got != "/tmp/b" {
		t.Errorf("path %v, want /tmp/b", got)
	}
}

func TestPermissionCouldNotCheck(t *testing.T) {
	t.Run("no state dir", func(t *testing.T) {
		permEnv(t)
		t.Setenv("LIGHTHOUSE_STATE_DIR", "")
		res := call(t, permissionCheck, "", permPayload("", "permission_prompt"))
		if res.Code != 0 || res.Output != nil || res.Stderr != "permission-check: COULD NOT CHECK — LIGHTHOUSE_STATE_DIR is empty\n" {
			t.Errorf("%+v", res)
		}
	})
	t.Run("payload not JSON", func(t *testing.T) {
		state := permEnv(t)
		res := call(t, permissionCheck, "", "{nope")
		if res.Code != 0 || !strings.HasPrefix(res.Stderr, "permission-check: COULD NOT CHECK — ") {
			t.Errorf("%+v", res)
		}
		if _, err := os.Stat(state); !os.IsNotExist(err) {
			t.Errorf("state dir written: %v", err)
		}
	})
	t.Run("state dir is a file", func(t *testing.T) {
		permEnv(t)
		blocker := writeFile(t, filepath.Join(t.TempDir(), "f"), "")
		t.Setenv("LIGHTHOUSE_STATE_DIR", blocker)
		res := call(t, permissionCheck, "", permPayload("", "permission_prompt"))
		if res.Code != 0 || !strings.HasPrefix(res.Stderr, "permission-check: COULD NOT CHECK — ") ||
			strings.Count(res.Stderr, "\n") != 1 {
			t.Errorf("%+v", res)
		}
	})
}

func TestPermissionAtHasMilliseconds(t *testing.T) {
	state := permEnv(t)
	before := time.Now().UTC().Truncate(time.Millisecond)
	call(t, permissionCheck, "", permPayload("", "permission_prompt"))
	after := time.Now().UTC()
	at, _ := readNote(t, state)["at"].(string)
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`).MatchString(at) {
		t.Fatalf("at %q", at)
	}
	moment, err := time.Parse(time.RFC3339Nano, at)
	if err != nil || moment.Before(before) || moment.After(after) {
		t.Errorf("at %s not within [%s, %s] (%v)", at, before, after, err)
	}
}

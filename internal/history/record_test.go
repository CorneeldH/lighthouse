package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeLines(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func collect(t *testing.T, p string, since time.Time) ([]Record, int) {
	t.Helper()
	var recs []Record
	skipped, err := ReadFile(p, since, func(r Record) { recs = append(recs, r) })
	if err != nil {
		t.Fatal(err)
	}
	return recs, skipped
}

func TestReadFile_LongLine(t *testing.T) {
	big := strings.Repeat("x", 5<<20)
	line, _ := json.Marshal(map[string]any{
		"type": "assistant", "timestamp": "2026-10-01T10:00:00Z",
		"message": map[string]any{"content": []any{
			map[string]any{"type": "tool_use", "name": "Write", "input": map[string]any{"file_path": "/a/b.go", "content": big}},
		}},
	})
	recs, skipped := collect(t, writeLines(t, string(line)), time.Time{})
	if skipped != 0 || len(recs) != 1 {
		t.Fatalf("skipped=%d recs=%d", skipped, len(recs))
	}
	if !reflect.DeepEqual(recs[0].WrittenPaths, []string{"/a/b.go"}) {
		t.Fatalf("paths %v", recs[0].WrittenPaths)
	}
}

func TestReadFile_BadLineAndSince(t *testing.T) {
	p := writeLines(t,
		`{"type":"user","timestamp":"2026-10-01T10:00:00Z"}`,
		`{not json`,
		`{"type":"user","timestamp":"2026-10-02T10:00:00Z"}`,
		`{"type":"user","timestamp":"2026-10-03T10:00:00Z"}`,
		`{"type":"pr-link","prNumber":7}`,
	)
	recs, skipped := collect(t, p, time.Time{})
	if skipped != 1 || len(recs) != 4 {
		t.Fatalf("skipped=%d recs=%d", skipped, len(recs))
	}
	if recs[3].PRNumber != 7 || recs[3].Line != 5 || recs[3].File != p {
		t.Fatalf("%+v", recs[3])
	}
	// Boundary: a timestamp equal to since is skipped; no timestamp is kept.
	since := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	recs, _ = collect(t, p, since)
	if len(recs) != 2 || recs[0].Timestamp != "2026-10-03T10:00:00Z" || recs[1].Type != "pr-link" {
		t.Fatalf("%+v", recs)
	}
}

func TestReadFile_Fields(t *testing.T) {
	p := writeLines(t,
		`{"type":"assistant","gitBranch":"b","cwd":"/c","entrypoint":"cli","sessionId":"s1","message":{"content":[{"type":"text","text":"hi"},{"type":"tool_use","name":"Bash","input":{"command":"git push"}},{"type":"tool_use","name":"Edit","input":{"file_path":"/x"}}]}}`,
		`{"type":"attachment","attachment":{"type":"hook_blocking_error","hookName":"PreToolUse:Bash","blockingError":{"blockingError":"SECRET-OUT","command":"run.sh gate"}}}`,
	)
	recs, _ := collect(t, p, time.Time{})
	a, h := recs[0], recs[1]
	if a.GitBranch != "b" || a.Cwd != "/c" || a.Entrypoint != "cli" || a.SessionID != "s1" ||
		!reflect.DeepEqual(a.Commands, []string{"git push"}) || !reflect.DeepEqual(a.WrittenPaths, []string{"/x"}) {
		t.Fatalf("%+v", a)
	}
	if h.HookName != "PreToolUse:Bash" || h.HookCommand != "run.sh gate" {
		t.Fatalf("%+v", h)
	}
}

func TestReadFile_NoContentKept(t *testing.T) {
	p := writeLines(t,
		`{"type":"user","timestamp":"2026-10-01T10:00:00Z","gitBranch":"main","message":{"role":"user","content":"SECRET-PROMPT-1"}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"SECRET-PROMPT-1"}]}}`,
		`{"type":"attachment","attachment":{"type":"hook_blocking_error","hookName":"h","blockingError":{"blockingError":"SECRET-PROMPT-1","command":"c"}}}`,
	)
	recs, _ := collect(t, p, time.Time{})
	if len(recs) != 3 {
		t.Fatalf("recs %d", len(recs))
	}
	if s := strings.Join(strings.Fields(strings.ReplaceAll(strings.TrimSpace(fmtAll(recs)), "\n", " ")), " "); strings.Contains(s, "SECRET-PROMPT-1") {
		t.Fatalf("content kept: %s", s)
	}
}

func fmtAll(recs []Record) string {
	b, _ := json.Marshal(recs)
	return string(b)
}

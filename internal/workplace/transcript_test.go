package workplace

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fixtures = "testdata/transcript"

func at(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func read(t *testing.T, dir, session string) Transcript {
	t.Helper()
	tr, err := ReadTranscript(filepath.Join(fixtures, dir), session)
	if err != nil {
		t.Fatalf("ReadTranscript(%s): %v", dir, err)
	}
	return tr
}

func TestProjectDir(t *testing.T) {
	cases := []struct{ cwd, want string }{
		{
			"/home/corneel/.kenn/forge/worktrees/github/github.com/corneeldh/personal_ai_agent/work-issue-558-heavy-claude-foundry-98090489",
			"/h/.claude/projects/-home-corneel--kenn-forge-worktrees-github-github-com-corneeldh-personal-ai-agent-work-issue-558-heavy-claude-foundry-98090489",
		},
		{"/a/b.c", "/h/.claude/projects/-a-b-c"},
	}
	for _, c := range cases {
		if got := ProjectDir("/h", c.cwd); got != c.want {
			t.Errorf("ProjectDir(%q) = %q, want %q", c.cwd, got, c.want)
		}
	}
}

func TestReadTranscriptSubagentCounts(t *testing.T) {
	tr := read(t, "main-ended-subagent-working", "s")
	if want := at(t, "2026-10-24T10:40:00Z"); !tr.LastEvent.Equal(want) {
		t.Errorf("LastEvent = %v, want %v", tr.LastEvent, want)
	}
	if want := at(t, "2026-10-24T10:00:00Z"); !tr.Threads[""].Equal(want) {
		t.Errorf("main thread = %v, want %v", tr.Threads[""], want)
	}
}

// TestReadTranscriptBackground counts the background tasks of the main transcript that did not
// end (#270). In background-pending a Bash task ends by a task-notification, one by TaskStop,
// an assistant line quoting the agent's task id ends nothing, and a launch in a subagent
// transcript does not count: the async agent itself is the main transcript's task.
func TestReadTranscriptBackground(t *testing.T) {
	cases := []struct {
		dir  string
		want int
	}{
		{"background-pending", 1},
		{"background-bash-running", 1},
		{"background-ended", 0},
		{"main-ended-subagent-working", 0},
	}
	for _, c := range cases {
		if got := read(t, c.dir, "s").Background; got != c.want {
			t.Errorf("%s: Background = %d, want %d", c.dir, got, c.want)
		}
	}
}

func TestReadTranscriptUnknownCommand(t *testing.T) {
	tr := read(t, "unknown-command", "s")
	if tr.UnknownCommand != "/lighthouse-bestaat-niet" {
		t.Errorf("UnknownCommand = %q", tr.UnknownCommand)
	}
	if tr.AssistantTurn {
		t.Error("AssistantTurn = true, want false")
	}
}

func TestReadTranscriptNoAssistant(t *testing.T) {
	tr := read(t, "no-assistant", "s")
	if tr.AssistantTurn || tr.UnknownCommand != "" {
		t.Errorf("got AssistantTurn=%v UnknownCommand=%q", tr.AssistantTurn, tr.UnknownCommand)
	}
}

func TestUnknownCommandInAssistantDoesNotCount(t *testing.T) {
	tr := read(t, "assistant-mentions-unknown", "s")
	if tr.UnknownCommand != "" || !tr.AssistantTurn {
		t.Errorf("got AssistantTurn=%v UnknownCommand=%q", tr.AssistantTurn, tr.UnknownCommand)
	}
}

func TestReadTranscriptTornLine(t *testing.T) {
	tr := read(t, "torn-line", "s")
	if want := at(t, "2026-10-24T10:00:00Z"); !tr.LastEvent.Equal(want) {
		t.Errorf("LastEvent = %v, want %v", tr.LastEvent, want)
	}
}

func TestReadTranscript558(t *testing.T) {
	tr := read(t, "case-558", "62b1fdd2-7894-4e1e-a0a1-fc81a5d6ef39")
	if want := at(t, "2026-10-24T09:59:08.249Z"); !tr.LastEvent.Equal(want) {
		t.Errorf("LastEvent = %v, want %v", tr.LastEvent, want)
	}
}

func TestReadTranscript818(t *testing.T) {
	tr := read(t, "case-818", "73270a65-d89d-4763-9fef-3b363058df04")
	want := map[string]time.Time{
		"":                        at(t, "2026-10-18T10:51:30.716Z"),
		"agent-a1370a2b6f1c5edda": at(t, "2026-10-18T10:46:52.563Z"),
		"agent-a82f84926776ea6ae": at(t, "2026-10-18T10:50:35.405Z"),
	}
	if len(tr.Threads) != len(want) {
		t.Errorf("Threads = %v", tr.Threads)
	}
	for k, w := range want {
		if !tr.Threads[k].Equal(w) {
			t.Errorf("thread %q = %v, want %v", k, tr.Threads[k], w)
		}
	}
	if !tr.LastEvent.Equal(want[""]) {
		t.Errorf("LastEvent = %v, want %v", tr.LastEvent, want[""])
	}
}

func writeSession(t *testing.T, lines ...string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReadTranscriptLongPrelude(t *testing.T) {
	big := `{"type":"attachment","timestamp":"2026-10-24T10:00:00.000Z","pad":"` + strings.Repeat("x", 90<<10) + `"}`
	small := `{"type":"attachment","timestamp":"2026-10-24T10:00:00.000Z","pad":"` + strings.Repeat("y", 1000) + `"}`
	lines := []string{big}
	for len(strings.Join(lines, "\n")) < 500<<10 {
		lines = append(lines, small)
	}
	lines = append(lines, `{"type":"assistant","timestamp":"2026-10-24T10:05:00.000Z"}`)
	tr, err := ReadTranscript(writeSession(t, lines...), "s")
	if err != nil {
		t.Fatal(err)
	}
	if !tr.AssistantTurn {
		t.Error("AssistantTurn = false, want true")
	}
}

func TestReadTranscriptHugeLastLine(t *testing.T) {
	huge := `{"type":"attachment","pad":"` + strings.Repeat("z", 100<<10) + `"}`
	dir := writeSession(t, `{"type":"user","timestamp":"2026-10-24T10:00:00.000Z"}`, huge)
	tr, err := ReadTranscript(dir, "s")
	if err != nil {
		t.Fatal(err)
	}
	if want := at(t, "2026-10-24T10:00:00Z"); !tr.LastEvent.Equal(want) {
		t.Errorf("LastEvent = %v, want %v", tr.LastEvent, want)
	}
}

func TestReadTranscriptNoTimestamp(t *testing.T) {
	dir := writeSession(t, `{"type":"cost-state"}`, `{"type":"bridge-session"}`)
	if _, err := ReadTranscript(dir, "s"); !errors.Is(err, ErrNotMeasurable) {
		t.Errorf("err = %v, want ErrNotMeasurable", err)
	}
}

func TestReadTranscriptUnknownCommandSuggestion(t *testing.T) {
	dir := writeSession(t, `{"type":"system","subtype":"informational","timestamp":"2026-10-24T10:00:00.000Z","content":"Unknown command: /x. Did you mean /y?"}`)
	tr, err := ReadTranscript(dir, "s")
	if err != nil {
		t.Fatal(err)
	}
	if tr.UnknownCommand != "/x" {
		t.Errorf("UnknownCommand = %q, want /x", tr.UnknownCommand)
	}
}

func TestTranscriptDirMissing(t *testing.T) {
	_, err := ReadTranscript(filepath.Join(t.TempDir(), "absent"), "s")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestUnknownCommandQuotedByUserOrAttachmentDoesNotCount(t *testing.T) {
	dir := writeSession(t,
		`{"type":"attachment","timestamp":"2026-10-24T10:00:00.000Z","attachment":{"content":"Unknown command: /x"}}`,
		`{"type":"user","timestamp":"2026-10-24T10:00:01.000Z","message":{"role":"user","content":"Unknown command: /x"}}`,
		`{"type":"assistant","timestamp":"2026-10-24T10:00:02.000Z"}`)
	tr, err := ReadTranscript(dir, "s")
	if err != nil {
		t.Fatal(err)
	}
	if tr.UnknownCommand != "" {
		t.Errorf("UnknownCommand = %q, want empty", tr.UnknownCommand)
	}
}

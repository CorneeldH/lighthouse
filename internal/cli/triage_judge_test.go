package cli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tri "github.com/CorneeldH/lighthouse/internal/triage"
)

// fakeClaude puts a claude on the PATH that writes its arguments (NUL-separated), its stdin and
// its cwd into dir, then runs body.
func fakeClaude(t *testing.T, body string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	bin := t.TempDir()
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do printf '%s\\0' \"$a\"; done > " + filepath.Join(dir, "args") + "\n" +
		"cat > " + filepath.Join(dir, "stdin") + "\n" +
		"pwd > " + filepath.Join(dir, "cwd") + "\n" +
		body + "\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestExecJudgeFlags(t *testing.T) {
	rec := fakeClaude(t, `printf '{"is_error":false}'`)
	work := t.TempDir()

	out, err := execJudge(context.Background(), work, "the prompt\n")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"is_error":false}` {
		t.Errorf("stdout = %q", out)
	}
	args := strings.Split(strings.TrimSuffix(readFile(t, filepath.Join(rec, "args")), "\x00"), "\x00")
	want := []string{"-p", "--model", "opus", "--tools", "Read,Grep,Glob", "--strict-mcp-config",
		"--setting-sources", "user", "--settings", `{"disableAllHooks": true}`, "--output-format", "json",
		"--json-schema", tri.Schema}
	if !slices.Equal(args, want) {
		t.Errorf("args:\n got %q\nwant %q", args, want)
	}
	if got := readFile(t, filepath.Join(rec, "stdin")); got != "the prompt\n" {
		t.Errorf("stdin = %q", got)
	}
	wantDir, err := filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(rec, "cwd"))); got != wantDir {
		t.Errorf("cwd = %q, want %q", got, wantDir)
	}
}

func TestExecJudgeNonZero(t *testing.T) {
	fakeClaude(t, "printf 'not logged in%0300d' 0 >&2\nexit 1")
	_, err := execJudge(context.Background(), t.TempDir(), "p")
	if err == nil {
		t.Fatal("err = nil, want the failed exit")
	}
	msg := err.Error()
	if !strings.Contains(msg, "exit status 1") || !strings.Contains(msg, "not logged in") {
		t.Errorf("err = %q, want the exit status and the start of stderr", msg)
	}
	if strings.Contains(msg, strings.Repeat("0", 200)) {
		t.Errorf("err carries more than 200 bytes of stderr: %q", msg)
	}
}

func TestExecJudgeTimeout(t *testing.T) {
	fakeClaude(t, "exec sleep 5")
	old := judgeTimeout
	judgeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { judgeTimeout = old })

	start := time.Now()
	_, err := execJudge(context.Background(), t.TempDir(), "p")
	if err == nil {
		t.Fatal("err = nil, want a timeout")
	}
	if !strings.Contains(err.Error(), "deadline exceeded") {
		t.Errorf("err = %v, want the deadline", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("took %v, want under 2s", took)
	}
}

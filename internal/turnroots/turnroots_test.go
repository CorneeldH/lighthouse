package turnroots

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Classification: pure-logic. TestFnmatchCase fails when `*` stops matching `/` (path.Match),
// when `?` or a set stops working, or when matching is no longer anchored or case-sensitive.
func TestFnmatchCase(t *testing.T) {
	tests := []struct {
		name, pattern string
		want          bool
	}{
		{"a/b.go", "*.go", true},
		{"go.mod", "*.go", false},
		{"go.mod", "go.mod", true},
		{"Go.mod", "go.mod", false},
		{"a.go", "[ab].go", true},
		{"b.go", "[ab].go", true},
		{"c.go", "[ab].go", false},
		{"c.go", "[!ab].go", true},
		{"a.go", "[!ab].go", false},
		{"m.go", "[a-z].go", true},
		{"M.go", "[a-z].go", false},
		{"x.go", "?.go", true},
		{"xy.go", "?.go", false},
		{"/.go", "?.go", true},
		{"src/pkg/a.py", "src/*.py", true},
		{"tests/test_a.py", "src/*.py", false},
		{"a.gox", "*.go", false},
		{"[.go", "[.go", true},
		{"a]", "[]]", false},
		{"]", "[]]", true},
		{"a.go", "a.go*", true},
		{"a+b", "a+b", true},
		{"aab", "a+b", false},
		{"-", "[a-]", true},
		{"b", "[c-a]", false},
		{".golangci.yml", ".golangci*", true},
	}
	for _, tt := range tests {
		if got := fnmatchCase(tt.name, tt.pattern); got != tt.want {
			t.Errorf("fnmatchCase(%q, %q) = %v, want %v", tt.name, tt.pattern, got, tt.want)
		}
	}
}

// Classification: pure-logic. TestIsPrompt fails when a meta entry, a compaction summary or a
// tool result starts a new turn, or when a text prompt no longer does.
func TestIsPrompt(t *testing.T) {
	tests := []struct {
		name  string
		entry string
		want  bool
	}{
		{"string content", `{"type":"user","message":{"content":"hi"}}`, true},
		{"text block", `{"type":"user","message":{"content":[{"type":"text","text":"hi"}]}}`, true},
		{"text and image", `{"type":"user","message":{"content":[{"type":"text"},{"type":"image"}]}}`, true},
		{"tool result", `{"type":"user","message":{"content":[{"type":"tool_result"}]}}`, false},
		{"text and tool result", `{"type":"user","message":{"content":[{"type":"text"},{"type":"tool_result"}]}}`, false},
		{"image only", `{"type":"user","message":{"content":[{"type":"image"}]}}`, false},
		{"empty list", `{"type":"user","message":{"content":[]}}`, false},
		{"meta", `{"type":"user","isMeta":true,"message":{"content":"hi"}}`, false},
		{"meta false", `{"type":"user","isMeta":false,"message":{"content":"hi"}}`, true},
		{"meta truthy string", `{"type":"user","isMeta":"yes","message":{"content":"hi"}}`, false},
		{"compact summary", `{"type":"user","isCompactSummary":true,"message":{"content":"This session is being continued"}}`, false},
		{"assistant", `{"type":"assistant","message":{"content":"hi"}}`, false},
		{"no message", `{"type":"user"}`, false},
		{"null message", `{"type":"user","message":null}`, false},
		{"content no list", `{"type":"user","message":{"content":5}}`, false},
	}
	for _, tt := range tests {
		var entry map[string]any
		if err := json.Unmarshal([]byte(tt.entry), &entry); err != nil {
			t.Fatal(err)
		}
		if got := isPrompt(entry); got != tt.want {
			t.Errorf("%s: isPrompt = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// Classification: cli-output. TestPayloadErrors fails when a payload that is no valid JSON or no
// object gets another reason than turn_roots.py gave.
func TestPayloadErrors(t *testing.T) {
	tests := []struct{ raw, want string }{
		{"{not json", "the payload is not valid JSON."},
		{"", "the payload is not valid JSON."},
		{"[1]", "the payload is no JSON object."},
		{"null", "the payload is no JSON object."},
		{`{"cwd":"/x"}`, "the payload has no transcript_path."},
		{`{"cwd":"/x","transcript_path":""}`, "the payload has no transcript_path."},
	}
	for _, tt := range tests {
		_, err := Resolve([]byte(tt.raw), "")
		var u *Undetermined
		if !errors.As(err, &u) || u.Msg != tt.want {
			t.Errorf("Resolve(%q) error %v, want Undetermined %q", tt.raw, err, tt.want)
		}
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// committedRepo is a repo with one commit, made before the turn starts.
func committedRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "a.go")
	git(t, repo, "commit", "-qm", "a")
	return repo
}

// turnPayload writes a transcript with one prompt at now and one `ls`, and returns the Stop payload.
func turnPayload(t *testing.T, repo string, now time.Time) []byte {
	t.Helper()
	prompt := map[string]any{
		"type": "user", "timestamp": now.UTC().Format(time.RFC3339Nano), "cwd": repo,
		"message": map[string]any{"role": "user", "content": "go"},
	}
	bash := map[string]any{
		"type": "assistant", "timestamp": now.UTC().Format(time.RFC3339Nano), "cwd": repo,
		"message": map[string]any{"content": []any{map[string]any{
			"type": "tool_use", "name": "Bash", "input": map[string]any{"command": "ls"},
		}}},
	}
	var lines []string
	for _, e := range []map[string]any{prompt, bash} {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(b))
	}
	transcript := filepath.Join(t.TempDir(), "s1.jsonl")
	if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{"cwd": repo, "transcript_path": transcript})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

// Classification: cli-output. The Python version turned every ValueError into "the payload is not
// valid JSON.", also one raised by parse_reflog on git output. This fails when a reflog line git
// cannot give us is reported as a JSON error, or no longer counts as a git failure (touched).
func TestAGitParseErrorIsNotInvalidJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake git is a shell script")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	repo := committedRepo(t)
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
	payload := turnPayload(t, repo, time.Now())

	// Without the fake the turn touched nothing: the commit lies before the prompt.
	got, err := Classify(payload, repo, []string{"*.go"})
	if err != nil || len(got) != 1 || got[0] != (Verdict{Root: repo, Touched: false}) {
		t.Fatalf("real git: %v, %v; want untouched %s", got, err, repo)
	}

	bin := t.TempDir()
	script := "#!/bin/sh\nfor a in \"$@\"; do [ \"$a\" = reflog ] && { printf 'no-separators-here\\n'; exit 0; }; done\nexec '" + realGit + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	got, err = Classify(payload, repo, []string{"*.go"})
	if err != nil {
		if strings.Contains(err.Error(), "not valid JSON") {
			t.Fatalf("a reflog parse error is reported as invalid JSON: %v", err)
		}
		var u *Undetermined
		if !errors.As(err, &u) {
			t.Fatalf("error %T %v, want *Undetermined", err, err)
		}
		return
	}
	if len(got) != 1 || got[0] != (Verdict{Root: repo, Touched: true}) {
		t.Fatalf("fake reflog: %v, want touched %s", got, repo)
	}
}

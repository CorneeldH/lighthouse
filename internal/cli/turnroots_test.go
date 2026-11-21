package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Classification: cli-output. Fails when an undetermined turn no longer exits 1 with its reason on
// stdout, the contract the Stop gates read (the cases with a repo are in internal/turnroots).
func TestTurnRootsCommandUndetermined(t *testing.T) {
	tests := []struct{ in, wantOut string }{
		{"{not json", "the payload is not valid JSON.\n"},
		{"[]", "the payload is no JSON object.\n"},
		{`{"cwd":"/x"}`, "the payload has no transcript_path.\n"},
	}
	for _, tt := range tests {
		var out, errOut bytes.Buffer
		root := NewRoot(strings.NewReader(tt.in), &out, &errOut)
		root.SetArgs([]string{"turn-roots", "--match", "*.go"})
		code := execute(root, &errOut)
		if code != 1 || out.String() != tt.wantOut || errOut.String() != "" {
			t.Errorf("%q: code %d out %q err %q, want 1 %q \"\"", tt.in, code, out.String(), errOut.String(), tt.wantOut)
		}
	}
}

// Classification: cli-output. Fails when the roots are no longer printed one per line, or the
// verdicts of --match no longer read `touched<TAB>root` / `untouched<TAB>root`, the lines the Stop
// gates split on.
func TestTurnRootsCommandPrintsRootsAndVerdicts(t *testing.T) {
	for _, name := range []string{"LIGHTHOUSE_CONTROLLER_RUN", "LIGHTHOUSE_REPO_ROOT", "LIGHTHOUSE_TOML"} {
		t.Setenv(name, "")
	}
	for _, name := range []string{"GIT_DIR", "GIT_INDEX_FILE", "GIT_WORK_TREE"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	initCmd := exec.Command("git", "-C", repo, "-c", "commit.gpgsign=false", "init", "-q", "-b", "main")
	initCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	entries := []map[string]any{
		{"type": "user", "timestamp": "2026-10-25T10:00:00.000Z", "cwd": repo, "message": map[string]any{"role": "user", "content": "go"}},
		{"type": "assistant", "timestamp": "2026-10-25T10:00:01.000Z", "cwd": repo, "message": map[string]any{"content": []any{
			map[string]any{"type": "tool_use", "name": "Edit", "input": map[string]any{"file_path": filepath.Join(repo, "a.go")}},
		}}},
	}
	var lines []string
	for _, e := range entries {
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
	t.Setenv("CLAUDE_PROJECT_DIR", repo)

	tests := []struct {
		args    []string
		wantOut string
	}{
		{[]string{"turn-roots"}, repo + "\n"},
		{[]string{"turn-roots", "--match", "*.go"}, "touched\t" + repo + "\n"},
		{[]string{"turn-roots", "--match", "*.md"}, "untouched\t" + repo + "\n"},
	}
	for _, tt := range tests {
		var out, errOut bytes.Buffer
		root := NewRoot(bytes.NewReader(payload), &out, &errOut)
		root.SetArgs(tt.args)
		code := execute(root, &errOut)
		if code != 0 || out.String() != tt.wantOut || errOut.String() != "" {
			t.Errorf("%v: code %d out %q err %q, want 0 %q \"\"", tt.args, code, out.String(), errOut.String(), tt.wantOut)
		}
	}
}

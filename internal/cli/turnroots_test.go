package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// turnRootsRepo is a repo with one commit and a Stop payload whose turn Edits the given files.
func turnRootsRepo(t *testing.T, rels ...string) (string, []byte) {
	t.Helper()
	for _, name := range []string{"LIGHTHOUSE_CONTROLLER_RUN", "LIGHTHOUSE_REPO_ROOT", "LIGHTHOUSE_TOML"} {
		t.Setenv(name, "")
	}
	for _, name := range []string{"GIT_DIR", "GIT_INDEX_FILE", "GIT_WORK_TREE"} {
		t.Setenv(name, "") // registers the restore; the Unsetenv below removes the variable itself
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "a"}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var uses []any
	for _, rel := range rels {
		uses = append(uses, map[string]any{"type": "tool_use", "name": "Edit", "input": map[string]any{"file_path": filepath.Join(repo, rel)}})
	}
	var lines []string
	for _, e := range []map[string]any{
		{"type": "user", "timestamp": now, "cwd": repo, "message": map[string]any{"role": "user", "content": "go"}},
		{"type": "assistant", "timestamp": now, "cwd": repo, "message": map[string]any{"content": uses}},
	} {
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
	return repo, payload
}

func runTurnRoots(t *testing.T, payload []byte, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRoot(bytes.NewReader(payload), &out, &errOut)
	root.SetArgs(append([]string{"turn-roots"}, args...))
	if code := execute(root, &errOut); code != 0 {
		t.Fatalf("exit %d, out %q err %q", code, out.String(), errOut.String())
	}
	return out.String()
}

// Classification: cli-output. Fails when the roots are no longer printed one per line, or the
// verdicts of --match no longer read `touched<TAB>root` / `untouched<TAB>root`, the lines the Stop
// gates split on.
func TestTurnRootsCommandPrintsRootsAndVerdicts(t *testing.T) {
	repo, payload := turnRootsRepo(t, "a.go")
	t.Setenv("CLAUDE_PROJECT_DIR", repo)
	tests := []struct {
		args    []string
		wantOut string
	}{
		{nil, repo + "\n"},
		{[]string{"--match", "*.go"}, "touched\t" + repo + "\n"},
		{[]string{"--match", "*.md"}, "untouched\t" + repo + "\n"},
	}
	for _, tt := range tests {
		if got := runTurnRoots(t, payload, tt.args...); got != tt.wantOut {
			t.Errorf("%v: output %q, want %q", tt.args, got, tt.wantOut)
		}
	}
}

// Classification: cli-output. The hooks parse `file\t<root>\t<path>` after the verdict lines.
// Fails when the line form changes, a path with a space is cut or quoted, or --files stops
// adding the lines.
func TestTurnRootsFilesFlag(t *testing.T) {
	repo, payload := turnRootsRepo(t, "b.go", "my dir/c d.go", "x.txt")
	got := runTurnRoots(t, payload, "--match", "*.go", "--files")
	want := "touched\t" + repo + "\nfile\t" + repo + "\tb.go\nfile\t" + repo + "\tmy dir/c d.go\n"
	if got != want {
		t.Errorf("output %q, want %q", got, want)
	}
}

// Classification: cli-output. Fails when a verdict in doubt (no files) prints a file line, or an
// untouched root prints one.
func TestTurnRootsFilesFlagUntouchedHasNoFileLines(t *testing.T) {
	repo, payload := turnRootsRepo(t, "x.txt")
	got := runTurnRoots(t, payload, "--match", "*.go", "--files")
	if want := "untouched\t" + repo + "\n"; got != want {
		t.Errorf("output %q, want %q", got, want)
	}
}

// Classification: cli-output. ty-gate.sh reads this output unchanged. Fails when the verdict
// lines change or file lines appear without --files.
func TestTurnRootsWithoutFilesFlagUnchanged(t *testing.T) {
	repo, payload := turnRootsRepo(t, "b.go")
	got := runTurnRoots(t, payload, "--match", "*.go")
	if want := "touched\t" + repo + "\n"; got != want {
		t.Errorf("output %q, want %q", got, want)
	}
}

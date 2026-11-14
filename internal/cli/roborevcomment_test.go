package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func runRoborevComment(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.AddCommand(newRoborevCommentCommand())
	root.SetArgs(append([]string{"roborev-comment"}, args...))
	code = execute(root, &errOut)
	return out.String(), errOut.String(), code
}

// commentFiles writes state (as JSON) and a raw "ruw" and returns the render arguments.
func commentFiles(t *testing.T, state map[string]any) []string {
	t.Helper()
	dir := t.TempDir()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	statePath, rawPath := filepath.Join(dir, "s.json"), filepath.Join(dir, "r.md")
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rawPath, []byte("ruw"), 0o644); err != nil {
		t.Fatal(err)
	}
	return []string{"render", "--pr", "7", "--bron", "ci", "--state", statePath, "--raw", rawPath}
}

func doneState(findings ...map[string]any) map[string]any {
	return map[string]any{"range": "main..abc1234", "uitkomst": "done", "sha": "abc1234", "bevindingen": findings}
}

func TestRoborevCommentRendersADoneStateToStdout(t *testing.T) {
	f := map[string]any{"path": "src/a.py", "line": 12, "severity": "high", "uitkomst": "fixed", "commit": "abc1234", "reden": nil}
	out, errOut, code := runRoborevComment(t, commentFiles(t, doneState(f))...)
	if code != 0 || errOut != "" || !strings.HasPrefix(out, "<!-- roborev-verwerkt: abc1234 -->") || !strings.Contains(out, `"bron":"ci"`) {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	if !strings.HasSuffix(out, "</details>\n") {
		t.Errorf("stdout must end in a newline: %q", out[len(out)-20:])
	}
}

func TestRoborevCommentExitsTwoOnASkipState(t *testing.T) {
	out, errOut, code := runRoborevComment(t, commentFiles(t, map[string]any{"range": "a..b", "uitkomst": "skip", "reden": "geen .roborev.toml"})...)
	if code != 2 || out != "" || !strings.HasPrefix(errOut, "roborev_comment: ") {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestRoborevCommentExitsTwoWhenTheCommentCannotFit(t *testing.T) {
	var findings []map[string]any
	for i := 0; i < 1000; i++ {
		findings = append(findings, map[string]any{"path": "f.py", "line": i, "severity": "high", "uitkomst": "left", "commit": nil, "reden": strings.Repeat("r", 100)})
	}
	out, errOut, code := runRoborevComment(t, commentFiles(t, doneState(findings...))...)
	if code != 2 || out != "" || !strings.Contains(errOut, "65536") || !strings.HasPrefix(errOut, "roborev_comment: ") {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestRoborevCommentUsageErrorsExitTwo(t *testing.T) {
	for name, args := range map[string][]string{
		"no subcommand": {},
		"unknown":       {"nope"},
		"no flags":      {"render"},
		"bad bron":      {"render", "--pr", "7", "--bron", "x", "--state", "a", "--raw", "b"},
		"bad pr":        {"render", "--pr", "x", "--bron", "ci", "--state", "a", "--raw", "b"},
		"extra arg":     {"render", "--pr", "7", "--bron", "ci", "--state", "a", "--raw", "b", "more"},
	} {
		out, errOut, code := runRoborevComment(t, args...)
		if code != 2 || out != "" || errOut == "" {
			t.Errorf("%s: code %d, stdout %q, stderr %q", name, code, out, errOut)
		}
	}
}

func TestRoborevCommentUnreadableStateExitsTwo(t *testing.T) {
	_, errOut, code := runRoborevComment(t, "render", "--pr", "7", "--bron", "ci", "--state", "/nonexistent/s.json", "--raw", "/nonexistent/r")
	if code != 2 || !strings.HasPrefix(errOut, "roborev_comment: ") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

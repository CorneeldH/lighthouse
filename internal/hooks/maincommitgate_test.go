package hooks

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/hook"
)

// repo makes a git repository on branch with one commit, and points the config at a
// lighthouse.toml without a default branch.
func repo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	toml := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(toml, []byte("[project]\nlanguage = \"en\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", toml)
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	for _, args := range [][]string{
		{"init", "-q", "-b", branch},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func gate(t *testing.T, command, cwd string) hook.Result {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"tool_input": map[string]any{"command": command}, "cwd": cwd})
	if err != nil {
		t.Fatal(err)
	}
	return MainCommitGate(context.Background(), "", "", hook.Payload{Raw: raw})
}

func TestMainCommitGateBlocksACommitOnMain(t *testing.T) {
	dir := repo(t, "main")
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	res := gate(t, "git commit -m x", dir)

	want := "Main-commit gate: blocked — '" + root + "' is on branch 'main' (commit).\n"
	if res.Code != 2 || !strings.HasPrefix(res.Stderr, want) {
		t.Fatalf("got code %d, stderr %q; want 2 and a stderr starting with %q", res.Code, res.Stderr, want)
	}
	if res.Output != nil {
		t.Errorf("output %v; a block writes nothing to stdout", res.Output)
	}
}

func TestMainCommitGateAllowsACommitOnAFeatureBranch(t *testing.T) {
	dir := repo(t, "issue-1-x")

	res := gate(t, "git commit -m x", dir)

	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("got code %d, stderr %q; want a silent pass", res.Code, res.Stderr)
	}
}

func TestMainCommitGateUnparseableBlocksOnlyWhenACommitMayBeInIt(t *testing.T) {
	dir := repo(t, "issue-1-x")
	cases := []struct {
		command string
		code    int
		stderr  string
	}{
		{"echo 'open && git commit -m x", 2, gateUnparseable},
		{"echo 'open && git push", 2, gateUnparseable},
		{"echo 'we push this", 0, ""},
		{"git status && echo 'open", 0, ""},
	}
	for _, c := range cases {
		t.Run(c.command, func(t *testing.T) {
			res := gate(t, c.command, dir)
			if res.Code != c.code || res.Stderr != c.stderr {
				t.Errorf("got code %d, stderr %q; want %d, %q", res.Code, res.Stderr, c.code, c.stderr)
			}
		})
	}
}

func TestMainCommitGateWithoutGitCouldNotCheck(t *testing.T) {
	dir := repo(t, "main")
	t.Setenv("PATH", t.TempDir())

	res := gate(t, "git commit -m x", dir)

	want := "Main-commit gate: COULD NOT CHECK — missing tooling: git\n" +
		"\n" +
		"This hook makes sure nothing lands directly on the default branch.\n" +
		"Without that tooling nothing was checked; that is not a green light.\n"
	if res.Code != 2 || res.Stderr != want {
		t.Fatalf("got code %d, stderr %q; want 2, %q", res.Code, res.Stderr, want)
	}
}

func TestReadGatePayload(t *testing.T) {
	cases := []struct {
		raw, command, cwd string
		ok                bool
	}{
		{`{"tool_input": {"command": "git commit"}, "cwd": "/r"}`, "git commit", "/r", true},
		{`{"tool_input": {"command": 5}}`, "5", "", true},
		{`{"tool_input": {"command": false}, "cwd": null}`, "", "", true},
		{`{"tool_input": null}`, "", "", true},
		{`null`, "", "", true},
		{` `, "", "", true},
		{`[]`, "", "", false},
		{`{"tool_input": "git commit"}`, "", "", false},
		{`not json`, "", "", false},
		// jq indexes by exact key; a key in another case is another field.
		{`{"tool_input": {"command": "git commit -m x"}, "Tool_Input": null}`, "git commit -m x", "", true},
		{`{"tool_input": {"command": "git commit -m x", "Command": null}}`, "git commit -m x", "", true},
		{`{"cwd": "/r", "CWD": "/other"}`, "", "/r", true},
		// A duplicate key: the last one wins, as in jq.
		{`{"tool_input": {"command": "ls", "command": "git push"}}`, "git push", "", true},
		{`{"tool_input": false}`, "", "", false},
	}
	for _, c := range cases {
		command, cwd, ok := readGatePayload([]byte(c.raw))
		if command != c.command || cwd != c.cwd || ok != c.ok {
			t.Errorf("readGatePayload(%s) = %q, %q, %v; want %q, %q, %v", c.raw, command, cwd, ok, c.command, c.cwd, c.ok)
		}
	}
}

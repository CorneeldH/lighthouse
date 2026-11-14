package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/hook"
)

const gateMinimalTOML = "[project]\nlanguage = \"en\"\n\n[forge]\nkind = \"github\"\nrepo = \"Acme/widgets\"\n"

// toolDir is a PATH directory with links to the named real tools only.
func toolDir(t *testing.T, tools ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, tool := range tools {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("no %s", tool)
		}
		if err := os.Symlink(real, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Besluit 7: without git or bash nothing can be tested, and that blocks.
func TestMissingToolingBlocks(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tools   []string
		missing string
	}{
		{"neither", nil, " git bash"},
		{"no bash", []string{"git"}, " bash"},
		{"no git", []string{"bash"}, " git"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PATH", toolDir(t, tc.tools...))
			res := call(t, CommitTestGate, "pre", map[string]any{"tool_input": map[string]any{"command": "git commit -m x"}})
			want := "commit-test-gate: COULD NOT CHECK — missing tooling:" + tc.missing + "\n" +
				"\n" +
				"This hook runs the test command of every commit.\n" +
				"Without that tooling nothing was tested; that is not a green light.\n"
			if res.Code != 2 || res.Output != nil || res.Stderr != want {
				t.Fatalf("got %d %v\n%s\nwant:\n%s", res.Code, res.Output, res.Stderr, want)
			}
		})
	}
}

func TestUnknownModeBlocks(t *testing.T) {
	for _, stand := range []string{"", "bogus"} {
		res := call(t, CommitTestGate, stand, map[string]any{"tool_input": map[string]any{"command": "git commit -m x"}})
		want := "commit-test-gate: COULD NOT CHECK — unknown mode, expected pre or post\n" +
			"\n" +
			"The registration in .claude/settings.json names no known mode; nothing was tested.\n"
		if res.Code != 2 || res.Output != nil || res.Stderr != want {
			t.Fatalf("stand %q: got %d %v\n%s", stand, res.Code, res.Output, res.Stderr)
		}
	}
}

func TestInvalidPayloadBlocks(t *testing.T) {
	res := call(t, CommitTestGate, "pre", "{not json")
	want := "commit-test-gate: COULD NOT CHECK — the payload is not valid JSON.\n" +
		"\n" +
		"Without the command nothing was tested; that is not a green light.\n"
	if res.Code != 2 || res.Output != nil || res.Stderr != want {
		t.Fatalf("got %d %v\n%s", res.Code, res.Output, res.Stderr)
	}
}

// Twin of test_post_without_record_reads_no_config: no marker means nothing to test, so a broken
// lighthouse.toml does not block and no slot is taken.
func TestPostWithoutMarkerIsSilentAndReadsNoConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LIGHTHOUSE_TOML", writeFile(t, filepath.Join(dir, "lighthouse.toml"), "[test_gate\n"))
	t.Setenv("LIGHTHOUSE_REPO_ROOT", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	slots := filepath.Join(dir, "slots")
	t.Setenv("LIGHTHOUSE_GATE_SLOT_DIR", slots)
	res := call(t, CommitTestGate, "post", map[string]any{
		"session_id": "s1", "tool_use_id": "t1", "cwd": dir,
		"tool_input": map[string]any{"command": "git commit -m x"},
	})
	if res.Code != 0 || res.Output != nil || res.Stderr != "" {
		t.Fatalf("got %d %v\n%s", res.Code, res.Output, res.Stderr)
	}
	if _, err := os.Stat(slots); !os.IsNotExist(err) {
		t.Fatalf("slot dir made: %v", err)
	}
}

// Without LIGHTHOUSE_REPO_ROOT the configuration comes from the root the hook gets, and the test
// command does not see LIGHTHOUSE_REPO_ROOT.
func TestPreReadsTheConfigOfItsRoot(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "lighthouse.toml"), gateMinimalTOML+
		"\n[test_gate]\ncommand = 'echo \"root=${LIGHTHOUSE_REPO_ROOT:-none}\"'\nslot_wait_seconds = 0\n")
	repo := realDir(t, t.TempDir())
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "a"}} {
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	t.Setenv("LIGHTHOUSE_REPO_ROOT", "")
	t.Setenv("LIGHTHOUSE_TOML", "")
	t.Setenv("LIGHTHOUSE_HOST_TOML", filepath.Join(root, "absent.toml"))
	t.Setenv("LIGHTHOUSE_GATE_SLOT_DIR", filepath.Join(root, "slots"))
	t.Setenv("CLAUDE_PROJECT_DIR", repo)

	raw := `{"session_id":"s1","cwd":"` + repo + `","tool_input":{"command":"git commit -m x"}}`
	res := CommitTestGate(t.Context(), root, "pre", hook.Payload{Raw: []byte(raw)})
	spec, _ := res.Output["hookSpecificOutput"].(map[string]any)
	got, _ := spec["additionalContext"].(string)
	want := "commit-test-gate: no gate memory threshold in host.toml, host memory not checked\n" +
		"commit-test-gate: no gate_slice in host.toml, tests run without memory brake\n" +
		"commit-test-gate: " + repo + " — root=none"
	if res.Code != 0 || res.Stderr != "" || got != want || spec["hookEventName"] != "PreToolUse" {
		t.Fatalf("got %d %q\n%s\nwant context:\n%s", res.Code, res.Stderr, got, want)
	}
}

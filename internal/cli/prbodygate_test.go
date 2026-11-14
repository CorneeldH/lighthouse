package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Classification: cli-output. These tests fail when `lighthouse pr-body-gate` no longer passes
// its argument as the cwd and stdin as the command to prbody.Main, or no longer returns its exit
// code and streams. Every expected value is what pr_body_gate.py printed on the same input; the
// verdicts themselves are tested in internal/prbody.

// gateProject is a git repository with forge.repo Acme/widgets, set as the project of the gate.
func gateProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	toml := "[project]\nlanguage = \"en\"\n\n[forge]\nkind = \"github\"\nrepo = \"Acme/widgets\"\n"
	if err := os.WriteFile(filepath.Join(dir, "lighthouse.toml"), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	headings, err := os.ReadFile(filepath.Join("..", "..", "pack", "core", "headings.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".lighthouse"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".lighthouse", "headings.toml"), headings, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_PROJECT_DIR", dir)
	t.Setenv("LIGHTHOUSE_REPO_ROOT", dir)
	t.Setenv("LIGHTHOUSE_TOML", "")
	t.Setenv("GH_REPO", "")
	return dir
}

// runCommand runs the root command on args with stdin, and returns the exit code and streams.
func runCommand(stdin string, args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	root := NewRoot(strings.NewReader(stdin), &out, &errOut)
	root.SetArgs(args)
	code = execute(root, &errOut)
	return code, out.String(), errOut.String()
}

const blockedTail = "\n\nWrite the body with /lighthouse-write-pr <nr> and pass it with --body-file <path>.\n" +
	"Forms: `Closes #N` + `### Evidence #N` (one row per AC), `Refs #N — <reason>`, `No issue: <reason>`.\n"

func TestPrBodyGateCommand(t *testing.T) {
	project := gateProject(t)
	tests := []struct {
		name, stdin, cwd string
		wantCode         int
		wantOut, wantErr string
	}{
		{"pass line on stdout", `gh pr create --repo Acme/widgets --body "No issue: alleen een kleine opschoning"`, project,
			0, "pr-create-gate: body checked — No issue\n", ""},
		{"other repo skipped", `gh pr create --repo other/repo --body "geen vorm"`, project,
			0, "pr-create-gate: skipped — other repo other/repo\n", ""},
		{"no gh pr create", "git status", project, 0, "", ""},
		{"blocker on stderr", `gh pr create --repo Acme/widgets --body "geen vorm"`, project,
			2, "", "pr-create-gate: BLOCKED — no valid form in the PR body: `Closes #N` with `### Evidence #N`, `Refs #N — <reason>` or `No issue: <reason>`." + blockedTail},
		{"empty cwd is unknown", `gh pr create --body "geen vorm"`, "",
			2, "", "pr-create-gate: BLOCKED — working directory of `gh pr create` cannot be resolved" + blockedTail},
		{"cwd outside the repo", `gh pr create --body "geen vorm"`, string(filepath.Separator),
			0, "pr-create-gate: skipped — outside this repo\n", ""},
		{"open quote", `gh pr create --body "open`, project, 3, "", ""},
		{"a flag is a cwd", `gh pr create --body "geen vorm"`, "--help",
			2, "", "pr-create-gate: BLOCKED — working directory --help cannot be resolved" + blockedTail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errOut := runCommand(tt.stdin, "pr-body-gate", tt.cwd)
			if code != tt.wantCode || out != tt.wantOut || errOut != tt.wantErr {
				t.Errorf("code %d out %q err %q, want %d %q %q", code, out, errOut, tt.wantCode, tt.wantOut, tt.wantErr)
			}
		})
	}
}

func TestPrBodyGateCommandConfigError(t *testing.T) {
	project := gateProject(t)
	missing := filepath.Join(project, "missing.toml")
	t.Setenv("LIGHTHOUSE_TOML", missing)
	code, out, errOut := runCommand(`gh pr create --repo Acme/widgets --body "geen vorm"`, "pr-body-gate", project)
	want := "pr-create-gate: " + missing + ": file is missing\n"
	if code != 1 || out != "" || errOut != want {
		t.Errorf("code %d out %q err %q, want 1 \"\" %q", code, out, errOut, want)
	}
}

func TestPrBodyGateCommandUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"a", "b"}} {
		code, out, errOut := runCommand("", append([]string{"pr-body-gate"}, args...)...)
		want := "usage: lighthouse pr-body-gate <cwd>  (command on stdin)\n"
		if code != 64 || out != "" || errOut != want {
			t.Errorf("%q: code %d out %q err %q, want 64 \"\" %q", args, code, out, errOut, want)
		}
	}
}

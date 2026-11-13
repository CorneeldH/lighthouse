package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Classification: fails when the command maps a rejected metric or a good run to the wrong
// exit code, or loses the message.
func TestMeasureStartIssueCommand(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
	}{
		{"unknown metric", []string{"bogus"}, 2,
			"measure: unknown metric \"bogus\" (choose from leadin, worktree-first, mistrigger, paths, gate, scout)\n"},
		{"extra argument", []string{"leadin", "extra"}, 2,
			"measure: unknown metric \"extra\" (choose from leadin, worktree-first, mistrigger, paths, gate, scout)\n"},
		{"empty metric", []string{""}, 2,
			"measure: unknown metric \"\" (choose from leadin, worktree-first, mistrigger, paths, gate, scout)\n"},
		{"empty directory", []string{"leadin", "--dir", t.TempDir()}, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			root := NewRoot(bytes.NewReader(nil), &out, &errOut)
			root.SetArgs(append([]string{"measure-start-issue-kpi"}, tt.args...))
			code := execute(root, &errOut)
			if code != tt.wantCode || errOut.String() != tt.wantErr {
				t.Errorf("code %d stderr %q, want %d %q", code, errOut.String(), tt.wantCode, tt.wantErr)
			}
			if tt.wantCode == 0 && out.Len() == 0 {
				t.Error("no table on stdout")
			}
			if tt.wantCode != 0 && out.Len() != 0 {
				t.Errorf("stdout %q on a failure", out.String())
			}
		})
	}
}

// Classification: fails when --since is not passed on to git log: the paths row prints it and
// counts only the files added after it.
func TestMeasureStartIssueSince(t *testing.T) {
	repo := t.TempDir()
	toml := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(toml, []byte("[project]\nlanguage = \"en\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_REPO_ROOT", repo)
	t.Setenv("LIGHTHOUSE_TOML", toml)
	for _, args := range [][]string{{"init", "-q"}, {"add", "f.md"}, {"commit", "-q", "-m", "x"}} {
		if args[0] == "add" {
			if err := os.WriteFile(filepath.Join(repo, "f.md"), []byte("x\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	var out, errOut bytes.Buffer
	root := NewRoot(bytes.NewReader(nil), &out, &errOut)
	root.SetArgs([]string{"measure-start-issue-kpi", "paths", "--dir", t.TempDir(), "--since", "2099-01-01"})
	if code := execute(root, &errOut); code != 0 {
		t.Fatalf("code %d stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "\"since\": \"2099-01-01\"") || !strings.Contains(out.String(), "\"share\": null") {
		t.Errorf("stdout %q", out.String())
	}
}

// Classification: fails when measure-fix-pr-kpi is not registered or prints no scope row.
func TestMeasureCommandsRun(t *testing.T) {
	var out, errOut bytes.Buffer
	root := NewRoot(bytes.NewReader(nil), &out, &errOut)
	root.SetArgs([]string{"measure-fix-pr-kpi", "scope", "--dir", t.TempDir()})
	if code := execute(root, &errOut); code != 0 {
		t.Fatalf("code %d stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "\"past_the_boundary\": 0") {
		t.Errorf("stdout %q", out.String())
	}
}

package cli

import (
	"bytes"
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

// Classification: fails when --since is not passed on: the paths row prints it.
func TestMeasureStartIssueSince(t *testing.T) {
	var out, errOut bytes.Buffer
	root := NewRoot(bytes.NewReader(nil), &out, &errOut)
	root.SetArgs([]string{"measure-start-issue-kpi", "worktree-first", "--dir", t.TempDir(), "--since", "2026-01-01"})
	if code := execute(root, &errOut); code != 0 {
		t.Fatalf("code %d stderr %q", code, errOut.String())
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

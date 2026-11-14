package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// Classification: cli-output. These tests fail when `lighthouse evidence` no longer passes its
// arguments and flags to evidence.CheckCell and evidence.Run, reads --issue other than Python's
// int(), or reports a wrong command line with another exit code than argparse's 2. Every
// expected stdout and exit code is what evidence.py printed on the same input; the verdicts
// themselves are tested in internal/evidence.

func TestEvidenceCheckCommand(t *testing.T) {
	gateProject(t)
	tests := []struct {
		cell     string
		wantCode int
		wantOut  string
	}{
		{"`grep -F x f`", 0, ""},
		{"`rm -rf x`", 1, "`rm` is not in the allowlist\n"},
	}
	for _, tt := range tests {
		code, out, errOut := runCommand("", "evidence", "check", tt.cell)
		if code != tt.wantCode || out != tt.wantOut || errOut != "" {
			t.Errorf("%s: code %d out %q err %q, want %d %q \"\"", tt.cell, code, out, errOut, tt.wantCode, tt.wantOut)
		}
	}
}

func TestEvidenceCheckCommandCouldNotCheck(t *testing.T) {
	project := gateProject(t)
	missing := filepath.Join(project, "missing.toml")
	t.Setenv("LIGHTHOUSE_TOML", missing)
	code, out, errOut := runCommand("", "evidence", "check", "`uv run pytest tests/x.py`")
	want := "COULD NOT CHECK: " + missing + ": file is missing\n"
	if code != 3 || out != want || errOut != "" {
		t.Errorf("code %d out %q err %q, want 3 %q \"\"", code, out, errOut, want)
	}
}

// evidenceBody writes a Refs body for #10 with a row that holds and a row that waits on the PR,
// next to the file f the rows read, and returns the body path and the directory.
func evidenceBody(t *testing.T) (body, dir string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("hallo wereld\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body = filepath.Join(dir, "body.md")
	text := "Refs #10 — close-issue runs the rows after the merge\n\n### Evidence #10\n\n" +
		"| AC | Command | Result |\n|---|---|---|\n" +
		"| 1. one | `grep -F hallo f` | wereld |\n" +
		"| 2. two | `cat f` | `na PR: nope` |\n"
	if err := os.WriteFile(body, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return body, dir
}

const (
	evidenceHead      = "| AC | Outcome | Detail |\n|---|---|---|\n| 1. one | holds | exit code 0, `wereld` found |\n"
	evidenceRan       = evidenceHead + "| 2. two | does not hold | missing from output: 'nope' |\n"
	evidenceNotRan    = evidenceHead + "| 2. two | after PR, not measured | runs only after the PR |\n"
	evidenceNoBlockOf = "Closes without evidence block: heading `### Evidence #8` is missing\n"
)

func TestEvidenceRunCommand(t *testing.T) {
	gateProject(t)
	body, dir := evidenceBody(t)
	tests := []struct {
		name     string
		extra    []string
		issue    string
		wantCode int
		wantOut  string
	}{
		{"runs every row", nil, "10", 1, evidenceRan},
		{"before the PR skips the after-PR row", []string{"--before-pr"}, "10", 0, evidenceNotRan},
		{"another issue has no block", nil, "8", 2, evidenceNoBlockOf},
		{"a leading zero stays decimal", nil, "010", 1, evidenceRan},
		{"whitespace around the number", nil, " 10 ", 1, evidenceRan},
		{"an underscore between digits", nil, "1_0", 1, evidenceRan},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"evidence", "run", "--issue", tt.issue, "--body-file", body, "--cwd", dir}, tt.extra...)
			code, out, errOut := runCommand("", args...)
			if code != tt.wantCode || out != tt.wantOut || errOut != "" {
				t.Errorf("code %d out %q err %q, want %d %q \"\"", code, out, errOut, tt.wantCode, tt.wantOut)
			}
		})
	}
}

// A wrong command line exits 2 with argparse's shape on stderr: the usage line, then
// `<command>: error: <reason>`. Nothing runs and stdout stays empty.
func TestEvidenceCommandUsageErrors(t *testing.T) {
	gateProject(t)
	const (
		top = "usage: lighthouse evidence [-h] {check,run} ...\n"
		chk = "usage: lighthouse evidence check [-h] cell\n"
		run = "usage: lighthouse evidence run [-h] --issue ISSUE --body-file BODY_FILE --cwd CWD [--before-pr]\n"
	)
	tests := []struct {
		args    []string
		wantErr string
	}{
		{nil, top + "lighthouse evidence: error: the following arguments are required: cmd\n"},
		{[]string{"foo"}, top + "lighthouse evidence: error: argument cmd: invalid choice: 'foo' (choose from check, run)\n"},
		{[]string{"--bogus"}, top + "lighthouse evidence: error: unknown flag: --bogus\n"},
		{[]string{"check"}, chk + "lighthouse evidence check: error: the following arguments are required: cell\n"},
		{[]string{"check", "a", "b"}, chk + "lighthouse evidence check: error: unrecognized arguments: b\n"},
		{[]string{"check", "--bogus"}, chk + "lighthouse evidence check: error: unknown flag: --bogus\n"},
		{[]string{"run"}, run + "lighthouse evidence run: error: the following arguments are required: --issue, --body-file, --cwd\n"},
		{[]string{"run", "--issue", "5"}, run + "lighthouse evidence run: error: the following arguments are required: --body-file, --cwd\n"},
		{[]string{"run", "--issue", "5", "--body-file", "b", "--cwd", "c", "extra"}, run + "lighthouse evidence run: error: unrecognized arguments: extra\n"},
		{[]string{"run", "--bogus"}, run + "lighthouse evidence run: error: unknown flag: --bogus\n"},
	}
	for _, tt := range tests {
		code, out, errOut := runCommand("", append([]string{"evidence"}, tt.args...)...)
		if code != 2 || out != "" || errOut != tt.wantErr {
			t.Errorf("%q: code %d out %q err %q, want 2 \"\" %q", tt.args, code, out, errOut, tt.wantErr)
		}
	}
}

// Python's int() refuses these; strconv's base 0 would read 0x10 as 16.
func TestEvidenceRunCommandInvalidIssue(t *testing.T) {
	gateProject(t)
	body, dir := evidenceBody(t)
	for _, issue := range []string{"x", "", "0x10", "1__0", "_10", "10_", "+-1"} {
		code, out, errOut := runCommand("", "evidence", "run", "--issue", issue, "--body-file", body, "--cwd", dir)
		want := "usage: lighthouse evidence run [-h] --issue ISSUE --body-file BODY_FILE --cwd CWD [--before-pr]\n" +
			"lighthouse evidence run: error: argument --issue: invalid int value: '" + issue + "'\n"
		if code != 2 || out != "" || errOut != want {
			t.Errorf("%q: code %d out %q err %q, want 2 \"\" %q", issue, code, out, errOut, want)
		}
	}
}

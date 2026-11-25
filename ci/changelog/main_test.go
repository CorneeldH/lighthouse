package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRunNotesPrintsSection(t *testing.T) {
	root := releaseRoot(t, nil, preamble+oldRelease, true)
	code, stdout, stderr := runCLI("notes", "v0.1.6", "--root", root)
	if code != 0 || stdout != "### Fixed\n\n- Old.\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestRunNotesTagAfterRoot(t *testing.T) {
	root := releaseRoot(t, nil, preamble+oldRelease, true)
	c1, out1, _ := runCLI("notes", "v0.1.6", "--root", root)
	c2, out2, _ := runCLI("notes", "--root", root, "v0.1.6")
	if c1 != 0 || c2 != 0 || out1 != out2 || out1 == "" {
		t.Fatalf("tag first: %d %q; tag last: %d %q", c1, out1, c2, out2)
	}
}

func TestRunNotesMissingSection(t *testing.T) {
	root := releaseRoot(t, nil, preamble+oldRelease, true)
	code, stdout, stderr := runCLI("notes", "v0.2.0", "--root", root)
	want := "no section for v0.2.0 in " + filepath.Join(root, "CHANGELOG.md") + "\n"
	if code != 1 || stdout != "" || stderr != want {
		t.Fatalf("code=%d stdout=%q stderr=%q, want stderr %q", code, stdout, stderr, want)
	}
}

func TestRunNotesUnreadable(t *testing.T) {
	root := releaseRoot(t, nil, "", false)
	code, _, stderr := runCLI("notes", "v0.1.6", "--root", root)
	if code != 1 || !strings.HasPrefix(stderr, filepath.Join(root, "CHANGELOG.md")+": cannot read: ") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

// taggedRepo is a release root that is a git repo with tag v0.1.6.
func taggedRepo(t *testing.T, fragments map[string]string) string {
	t.Helper()
	root := releaseRoot(t, fragments, preamble+oldRelease, true)
	git(t, root, "init", "-q", "-b", "main")
	commit(t, root, "one")
	git(t, root, "tag", "v0.1.6")
	return root
}

func TestRunReleasePrintsTag(t *testing.T) {
	root := taggedRepo(t, map[string]string{"a.md": "fixed"})
	code, stdout, stderr := runCLI("release", "--version", "v0.1.7", "--date", "2026-10-27", "--root", root)
	if code != 0 || stdout != "v0.1.7\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	text, _ := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if !strings.Contains(string(text), "## [v0.1.7] - 2026-10-27\n") {
		t.Errorf("--date ignored: %q", text)
	}
}

func TestRunReleaseDefaultDateIsToday(t *testing.T) {
	root := taggedRepo(t, map[string]string{"a.md": "fixed"})
	if code, _, stderr := runCLI("release", "--version", "v0.1.7", "--root", root); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	text, _ := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if !strings.Contains(string(text), "## [v0.1.7] - 20") {
		t.Errorf("no date in %q", text)
	}
}

func TestRunReleaseGitFails(t *testing.T) {
	code, _, stderr := runCLI("release", "--version", "v0.1.7", "--root", filepath.Join(t.TempDir(), "nope"))
	if code != 2 || !strings.HasPrefix(stderr, "COULD NOT CHECK: git tag ") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunReleaseError(t *testing.T) {
	root := taggedRepo(t, nil)
	code, _, stderr := runCLI("release", "--version", "v0.2.0", "--root", root)
	want := "nothing to release\n"
	if code != 1 || stderr != want {
		t.Fatalf("code=%d stderr=%q, want 1 and %q", code, stderr, want)
	}
}

func TestRunReleaseFragmentError(t *testing.T) {
	root := taggedRepo(t, map[string]string{"a.md": "bugfix"})
	code, stdout, stderr := runCLI("release", "--version", "v0.1.7", "--root", root)
	want := filepath.Join(root, "changelog.d", "a.md") + ": unknown type 'bugfix', expected one of added, changed, fixed, removed, internal\n"
	if code != 1 || stdout != "" || stderr != want {
		t.Fatalf("code=%d stdout=%q stderr=%q, want 1, empty and %q", code, stdout, stderr, want)
	}
}

func TestRunUsageErrors(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		args []string
		want string
	}{
		{nil, "a command is required"},
		{[]string{"frobnicate"}, "unknown command 'frobnicate'"},
		{[]string{"check", "--root", root, "--head", "x"}, "--base and --head are required"},
		{[]string{"check", "--root", root, "--base", "x"}, "--base and --head are required"},
		{[]string{"check", "--root", root, "--base", "x", "--head", "y", "extra"}, "unexpected argument 'extra'"},
		{[]string{"release", "--root", root}, "--version is required"},
		{[]string{"release", "--root", root, "--version", "0.30"}, "invalid --version '0.30'"},
		{[]string{"release", "--root", root, "--version", "v0.30.0-rc.1"}, "invalid --version 'v0.30.0-rc.1'"},
		{[]string{"notes", "--root", root}, "exactly one <tag> is required"},
		{[]string{"notes", "--root", root, "a", "b"}, "exactly one <tag> is required"},
		{[]string{"check", "--root", root, "--nope"}, "flag provided but not defined: -nope"},
	}
	for _, tt := range tests {
		code, stdout, stderr := runCLI(tt.args...)
		if code != 2 || stdout != "" {
			t.Errorf("run(%v) = %d, stdout %q; want 2, empty", tt.args, code, stdout)
		}
		if !strings.Contains(stderr, tt.want) {
			t.Errorf("run(%v) stderr %q lacks %q", tt.args, stderr, tt.want)
		}
		if !strings.Contains(stderr, "Usage of changelog") || !strings.Contains(stderr, "-root") {
			t.Errorf("run(%v) stderr %q lacks the usage text", tt.args, stderr)
		}
	}
}

func TestRunReleaseRefusesExistingTag(t *testing.T) {
	root := taggedRepo(t, map[string]string{"a.md": "fixed"})
	code, _, stderr := runCLI("release", "--version", "v0.1.6", "--root", root)
	if code != 1 || stderr != "v0.1.6 already has a tag\n" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

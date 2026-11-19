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
	code, stdout, stderr := runCLI("release", "--event", "push", "--date", "2026-10-27", "--root", root)
	if code != 0 || stdout != "v0.1.7\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	text, _ := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if !strings.Contains(string(text), "## [v0.1.7] - 2026-10-27\n") {
		t.Errorf("--date ignored: %q", text)
	}
}

func TestRunReleaseNothingPrintsEmptyLine(t *testing.T) {
	root := taggedRepo(t, map[string]string{"a.md": "added"})
	code, stdout, _ := runCLI("release", "--event", "push", "--root", root)
	if code != 0 || stdout != "\n" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
}

func TestRunReleaseDefaultDateIsToday(t *testing.T) {
	root := taggedRepo(t, map[string]string{"a.md": "fixed"})
	if code, _, stderr := runCLI("release", "--event", "push", "--root", root); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	text, _ := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if !strings.Contains(string(text), "## [v0.1.7] - 20") {
		t.Errorf("no date in %q", text)
	}
}

func TestRunReleaseWithoutTags(t *testing.T) {
	root := releaseRoot(t, map[string]string{"a.md": "fixed"}, preamble+oldRelease, true)
	git(t, root, "init", "-q", "-b", "main")
	code, stdout, stderr := runCLI("release", "--event", "push", "--root", root)
	want := "COULD NOT CHECK: no vX.Y.Z release tag found (a checkout without tags?)\n"
	if code != 2 || stdout != "" || stderr != want {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, "changelog.d", "a.md")); err != nil {
		t.Errorf("fragment removed: %v", err)
	}
}

func TestRunReleaseGitFails(t *testing.T) {
	code, _, stderr := runCLI("release", "--event", "push", "--root", filepath.Join(t.TempDir(), "nope"))
	if code != 2 || !strings.HasPrefix(stderr, "COULD NOT CHECK: git tag failed: ") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunReleaseError(t *testing.T) {
	root := taggedRepo(t, nil)
	code, _, stderr := runCLI("release", "--event", "workflow_dispatch", "--bump", "minor", "--root", root)
	want := "nothing to release: no releasable fragment for the forced bump\n"
	if code != 1 || stderr != want {
		t.Fatalf("code=%d stderr=%q, want 1 and %q", code, stderr, want)
	}
}

func TestRunReleaseFragmentError(t *testing.T) {
	root := taggedRepo(t, map[string]string{"a.md": "bugfix"})
	code, stdout, stderr := runCLI("release", "--event", "push", "--root", root)
	want := filepath.Join(root, "changelog.d", "a.md") + ": unknown type 'bugfix', expected one of added, changed, fixed, removed, internal\n"
	if code != 1 || stdout != "" || stderr != want {
		t.Fatalf("code=%d stdout=%q stderr=%q, want 1, empty and %q", code, stdout, stderr, want)
	}
}

func TestRunUsageErrors(t *testing.T) {
	tests := [][]string{
		nil,
		{"frobnicate"},
		{"check", "--head", "x"},
		{"check", "--base", "x"},
		{"release"},
		{"release", "--event", "tuesday"},
		{"release", "--event", "push", "--bump", "huge"},
		{"notes"},
		{"notes", "a", "b"},
		{"check", "--nope"},
	}
	for _, args := range tests {
		code, stdout, stderr := runCLI(args...)
		if code != 2 || stdout != "" || stderr == "" {
			t.Errorf("run(%v) = %d, stdout %q, stderr %q; want 2, empty, usage", args, code, stdout, stderr)
		}
	}
}

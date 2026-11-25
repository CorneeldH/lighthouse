package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func addFragment(t *testing.T, root, name, typ string) {
	t.Helper()
	write(t, filepath.Join(root, "changelog.d", name), "---\ntype: "+typ+"\n---\n"+name+" text.\n")
	commit(t, root, "add "+name)
}

func TestSplitFirstTime(t *testing.T) {
	root := taggedRepo(t, map[string]string{"a.md": "added"})
	code, stdout, stderr := runCLI("split", "--kind", "minor", "--root", root)
	if code != 0 || stdout != "split release/v0.2 v0.2.0-rc.1\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, _ = runCLI("split", "--kind", "major", "--root", root)
	if code != 0 || stdout != "split release/v1.0 v1.0.0-rc.1\n" {
		t.Fatalf("major: code=%d stdout=%q", code, stdout)
	}
}

func TestSplitSkipsWithoutReleasableFragment(t *testing.T) {
	root := taggedRepo(t, map[string]string{"a.md": "internal"})
	code, stdout, _ := runCLI("split", "--kind", "minor", "--root", root)
	if code != 0 || !strings.HasPrefix(stdout, "skip ") {
		t.Fatalf("code=%d stdout=%q, want skip", code, stdout)
	}
}

func TestSplitSkipsFragmentAlreadyReleased(t *testing.T) {
	root := taggedRepo(t, map[string]string{"a.md": "added"})
	git(t, root, "checkout", "-q", "-b", "release/v0.2")
	releaseCommit(t, root, "v0.2.0", []string{"a.md"}, true)
	git(t, root, "checkout", "-q", "main")
	code, stdout, _ := runCLI("split", "--kind", "minor", "--root", root)
	if code != 0 || !strings.HasPrefix(stdout, "skip ") {
		t.Fatalf("code=%d stdout=%q, want skip", code, stdout)
	}
}

// openRCRepo has release/v0.2 split off with a.md and tagged rc.1, main still open.
func openRCRepo(t *testing.T) string {
	t.Helper()
	root := taggedRepo(t, map[string]string{"a.md": "added"})
	git(t, root, "branch", "release/v0.2")
	git(t, root, "tag", "v0.2.0-rc.1", "release/v0.2")
	return root
}

func TestSplitWhilePreviousRCOpenMerges(t *testing.T) {
	root := openRCRepo(t)
	addFragment(t, root, "b.md", "fixed")
	code, stdout, stderr := runCLI("split", "--kind", "minor", "--root", root)
	if code != 0 || stdout != "merge release/v0.2 v0.2.0-rc.2\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestSplitWhilePreviousRCOpenWithoutNewFragmentsSkips(t *testing.T) {
	root := openRCRepo(t)
	code, stdout, _ := runCLI("split", "--kind", "minor", "--root", root)
	if code != 0 || !strings.HasPrefix(stdout, "skip ") {
		t.Fatalf("code=%d stdout=%q, want skip", code, stdout)
	}
}

func TestSplitBadFragmentIsRed(t *testing.T) {
	root := taggedRepo(t, map[string]string{"a.md": "bugfix"})
	if code, _, _ := runCLI("split", "--kind", "minor", "--root", root); code != 1 {
		t.Fatalf("code=%d, want 1", code)
	}
}

func TestMilestoneReleaseMarker(t *testing.T) {
	tests := []struct{ desc, want string }{
		{"Product.\n\nRelease: ja\n", "yes"},
		{"release: JA", "yes"},
		{"Intro\r\n  Release: ja  \r\nMore", "yes"},
		{"Release: nee", "no"},
		{"", "no"},
		{"Release: ja, misschien", "no"},
		{"We say Release: ja in the text", "no"},
	}
	for _, tc := range tests {
		code, stdout, _ := runCLI("milestone-release", "--description", tc.desc)
		if code != 0 || stdout != tc.want+"\n" {
			t.Errorf("%q: code=%d stdout=%q, want %q", tc.desc, code, stdout, tc.want)
		}
	}
}

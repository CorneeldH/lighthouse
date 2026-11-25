package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fragRepo is a git repo on main whose changelog.d holds the given fragments (name to type),
// with CHANGELOG.md, in one commit.
func fragRepo(t *testing.T, fragments map[string]string) string {
	t.Helper()
	root := releaseRoot(t, fragments, preamble+oldRelease, true)
	git(t, root, "init", "-q", "-b", "main")
	commit(t, root, "base")
	return root
}

// releaseCommit removes the named fragments in a commit with the release subject, and tags it
// when tag is not empty.
func releaseCommit(t *testing.T, root, version string, names []string, tag bool) {
	t.Helper()
	for _, n := range names {
		if err := os.Remove(filepath.Join(root, "changelog.d", n)); err != nil {
			t.Fatal(err)
		}
	}
	commit(t, root, "chore(release): "+version)
	if tag {
		git(t, root, "tag", version)
	}
}

func setOf(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

func TestReleasedFragmentsFromReleaseCommit(t *testing.T) {
	root := fragRepo(t, map[string]string{"a.md": "fixed", "b.md": "added"})
	releaseCommit(t, root, "v0.28.0", []string{"a.md"}, true)
	got, err := releasedFragments(root)
	if err != nil || !reflect.DeepEqual(got, setOf("a.md")) {
		t.Fatalf("releasedFragments = %v, %v; want only a.md", got, err)
	}
}

func TestReleasedFragmentsIgnoresUntaggedReleaseCommit(t *testing.T) {
	root := fragRepo(t, map[string]string{"a.md": "fixed"})
	releaseCommit(t, root, "v0.28.0", []string{"a.md"}, false)
	got, err := releasedFragments(root)
	if err != nil || len(got) != 0 {
		t.Fatalf("releasedFragments = %v, %v; want empty", got, err)
	}
}

func TestReleasedFragmentsSquashSuffix(t *testing.T) {
	root := fragRepo(t, map[string]string{"a.md": "fixed"})
	if err := os.Remove(filepath.Join(root, "changelog.d", "a.md")); err != nil {
		t.Fatal(err)
	}
	commit(t, root, "chore(release): v0.28.0 (#12)")
	git(t, root, "tag", "v0.28.0")
	got, err := releasedFragments(root)
	if err != nil || !got["a.md"] {
		t.Fatalf("releasedFragments = %v, %v; want a.md", got, err)
	}
}

// twoBranchRepo releases a on release/v0.28 and b on release/v0.29, in the given order, and
// leaves main with both fragments still open.
func twoBranchRepo(t *testing.T, aFirst bool) string {
	root := fragRepo(t, map[string]string{"a.md": "fixed", "b.md": "added"})
	steps := []struct{ branch, version, name string }{
		{"release/v0.28", "v0.28.0", "a.md"}, {"release/v0.29", "v0.29.0", "b.md"},
	}
	if !aFirst {
		steps[0], steps[1] = steps[1], steps[0]
	}
	for _, s := range steps {
		git(t, root, "checkout", "-q", "-b", s.branch, "main")
		releaseCommit(t, root, s.version, []string{s.name}, true)
		git(t, root, "checkout", "-q", "main")
	}
	return root
}

func TestReleasedFragmentsOrderIndependent(t *testing.T) {
	first, err1 := releasedFragments(twoBranchRepo(t, true))
	second, err2 := releasedFragments(twoBranchRepo(t, false))
	if err1 != nil || err2 != nil || !reflect.DeepEqual(first, second) || !reflect.DeepEqual(first, setOf("a.md", "b.md")) {
		t.Fatalf("first = %v (%v), second = %v (%v); want both {a.md b.md}", first, err1, second, err2)
	}
}

func TestReleasedFragmentIsNotRereleased(t *testing.T) {
	root := fragRepo(t, map[string]string{"a.md": "fixed"})
	git(t, root, "checkout", "-q", "-b", "release/v0.28", "main")
	releaseCommit(t, root, "v0.28.0", []string{"a.md"}, true)
	git(t, root, "checkout", "-q", "main")
	write(t, filepath.Join(root, "changelog.d", "c.md"), "---\ntype: added\n---\nc text.\n")
	released, err := releasedFragments(root)
	if err != nil || !released["a.md"] {
		t.Fatalf("releasedFragments = %v, %v", released, err)
	}
	var notes strings.Builder
	if _, err := release(root, "v0.29.0", "2026-10-27", []string{"v0.28.0"}, released, &notes); err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if strings.Contains(string(text), "a.md text.") || !strings.Contains(string(text), "c text.") {
		t.Errorf("section wrong: %q", text)
	}
	if _, err := os.Stat(filepath.Join(root, "changelog.d", "a.md")); !os.IsNotExist(err) {
		t.Errorf("a.md still open: %v", err)
	}
	if !strings.Contains(notes.String(), "a.md") {
		t.Errorf("skip not reported: %q", notes.String())
	}
}

func TestRunReleasedPrintsSorted(t *testing.T) {
	root := twoBranchRepo(t, true)
	code, stdout, stderr := runCLI("released", "--root", root)
	if code != 0 || stdout != "a.md\nb.md\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

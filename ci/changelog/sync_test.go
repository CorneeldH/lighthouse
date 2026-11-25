package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// releasedSource is a git checkout whose tag version released the fragments of fragments.
func releasedSource(t *testing.T, version string, fragments map[string]string) string {
	t.Helper()
	src := fragRepo(t, fragments)
	if _, err := release(src, version, "2026-11-20", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	commit(t, src, "chore(release): "+version)
	git(t, src, "tag", version)
	return src
}

func changelogOf(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSyncAddsSectionAndRemovesFragments(t *testing.T) {
	src := releasedSource(t, "v0.2.0", map[string]string{"a.md": "added"})
	main := releaseRoot(t, map[string]string{"a.md": "added"}, preamble+oldRelease, true)
	code, _, stderr := runCLI("sync", "--tag", "v0.2.0", "--source", src, "--root", main)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	want := preamble + "## [v0.2.0] - 2026-11-20\n\n### Added\n\n- a.md text.\n\n" + oldRelease
	if got := changelogOf(t, main); got != want {
		t.Fatalf("CHANGELOG.md =\n%q\nwant\n%q", got, want)
	}
	if _, err := os.Stat(filepath.Join(main, "changelog.d", "a.md")); !os.IsNotExist(err) {
		t.Errorf("a.md still open: %v", err)
	}
}

func TestSyncIsIdempotent(t *testing.T) {
	src := releasedSource(t, "v0.2.0", map[string]string{"a.md": "added"})
	main := releaseRoot(t, map[string]string{"a.md": "added"}, preamble+oldRelease, true)
	if code, _, stderr := runCLI("sync", "--tag", "v0.2.0", "--source", src, "--root", main); code != 0 {
		t.Fatalf("first: code=%d stderr=%q", code, stderr)
	}
	first := changelogOf(t, main)
	if code, _, stderr := runCLI("sync", "--tag", "v0.2.0", "--source", src, "--root", main); code != 0 {
		t.Fatalf("second: code=%d stderr=%q", code, stderr)
	}
	if got := changelogOf(t, main); got != first {
		t.Fatalf("second run changed CHANGELOG.md:\n%q\n%q", got, first)
	}
}

func TestSyncKeepsFragmentsForNextMilestone(t *testing.T) {
	src := releasedSource(t, "v0.2.0", map[string]string{"a.md": "added"})
	main := releaseRoot(t, map[string]string{"a.md": "added", "later.md": "fixed"}, preamble+oldRelease, true)
	if code, _, stderr := runCLI("sync", "--tag", "v0.2.0", "--source", src, "--root", main); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(main, "changelog.d", "later.md")); err != nil {
		t.Errorf("later.md removed: %v", err)
	}
}

func TestSyncPatchSectionOrder(t *testing.T) {
	src := releasedSource(t, "v0.1.7", map[string]string{"p.md": "fixed"})
	main := releaseRoot(t, nil, preamble+"## [v0.2.0] - 2026-11-19\n\n### Added\n\n- New.\n\n"+oldRelease, true)
	if code, _, stderr := runCLI("sync", "--tag", "v0.1.7", "--source", src, "--root", main); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	got := changelogOf(t, main)
	i20, i17, i16 := strings.Index(got, "## [v0.2.0]"), strings.Index(got, "## [v0.1.7]"), strings.Index(got, "## [v0.1.6]")
	if i20 < 0 || i17 < 0 || i16 < 0 || !(i20 < i17 && i17 < i16) {
		t.Fatalf("order wrong (v0.2.0 %d, v0.1.7 %d, v0.1.6 %d):\n%s", i20, i17, i16, got)
	}
}

func TestSyncUnknownTag(t *testing.T) {
	src := releasedSource(t, "v0.2.0", map[string]string{"a.md": "added"})
	main := releaseRoot(t, nil, preamble+oldRelease, true)
	code, _, stderr := runCLI("sync", "--tag", "v0.9.0", "--source", src, "--root", main)
	if code != 1 || !strings.Contains(stderr, "no section for v0.9.0") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

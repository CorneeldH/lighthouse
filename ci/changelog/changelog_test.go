package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, root, msg string) string {
	t.Helper()
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", msg)
	return git(t, root, "rev-parse", "HEAD")
}

func TestParseFragment(t *testing.T) {
	tests := []struct {
		name, body string
		typ, text  string // a want of typ "" means an error containing text
	}{
		{"plain", "---\ntype: fixed\n---\nNo more rewrites.\n", "fixed", "No more rewrites."},
		{"bomAndCRLF", "\xef\xbb\xbf---\r\ntype: added\r\n---\r\nNew skill.\r\n", "added", "New skill."},
		{"noOpeningLine", "type: fixed\n---\nx\n", "", "first line must be exactly `---`"},
		{"notClosed", "---\ntype: fixed\nx\n", "", "front matter is not closed by a `---` line"},
		{"emptyText", "---\ntype: fixed\n---\n   \n", "", ": empty text"},
		{"unknownType", "---\ntype: bugfix\n---\nx\n", "", "unknown type 'bugfix', expected one of added, changed, fixed, removed, internal"},
		{"twoTypes", "---\ntype: fixed\ntype: added\n---\nx\n", "", "must hold exactly one line `type: <type>`, found 2"},
		{"extraKey", "---\ntype: fixed\nscope: cli\n---\nx\n", "", "found 2"},
		{"otherKey", "---\nscope: cli\n---\nx\n", "", "unknown front matter line 'scope: cli', only `type:` is allowed"},
		{"emptyFrontMatter", "---\n---\nx\n", "", "found 0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "x.md")
			write(t, p, tc.body)
			got, err := parseFragment(p)
			if tc.typ != "" {
				want := fragment{path: p, typ: tc.typ, text: tc.text}
				if err != nil || got != want {
					t.Fatalf("parseFragment = %+v, %v; want %+v", got, err, want)
				}
				return
			}
			var fe *fragmentError
			if !errors.As(err, &fe) {
				t.Fatalf("err = %v, want *fragmentError", err)
			}
			if !strings.HasPrefix(fe.msg, p+": ") || !strings.Contains(fe.msg, tc.text) {
				t.Fatalf("msg = %q, want prefix %q and %q", fe.msg, p+": ", tc.text)
			}
		})
	}
}

func TestParseFragmentUnreadable(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.md")
	_, err := parseFragment(missing)
	var fe *fragmentError
	if !errors.As(err, &fe) || !strings.HasPrefix(fe.msg, missing+": cannot read: ") {
		t.Fatalf("err = %v, want fragmentError starting %q", err, missing+": cannot read: ")
	}
	bad := filepath.Join(dir, "bad.md")
	write(t, bad, "---\ntype: fixed\n---\n\xff\n")
	if _, err := parseFragment(bad); !errors.As(err, &fe) || !strings.Contains(fe.msg, "cannot read") {
		t.Fatalf("invalid UTF-8: err = %v, want cannot read", err)
	}
}

func names(frags []fragment) []string {
	var out []string
	for _, f := range frags {
		out = append(out, filepath.Base(f.path))
	}
	return out
}

func TestReadFragmentsOrder(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"x-10.md", "x-2.md", "y.md", "x.md", "x-3.md", "b.md", "a.md"} {
		write(t, filepath.Join(dir, n), "---\ntype: fixed\n---\nx\n")
	}
	write(t, filepath.Join(dir, ".lighthouse"), "not a fragment\n")
	frags, err := readFragments(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.md", "b.md", "x.md", "x-2.md", "x-3.md", "x-10.md", "y.md"}
	if got := names(frags); !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestReadFragmentsMissingDir(t *testing.T) {
	frags, err := readFragments(filepath.Join(t.TempDir(), "nope"))
	if frags != nil || err != nil {
		t.Fatalf("readFragments = %v, %v; want nil, nil", frags, err)
	}
}

func TestReadFragmentsRefusesOtherFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "notes.txt"), "x\n")
	_, err := readFragments(dir)
	want := filepath.Join(dir, "notes.txt") + ": not a `*.md` fragment"
	var fe *fragmentError
	if !errors.As(err, &fe) || fe.msg != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

const goodFragment = "---\ntype: added\n---\nNew skill.\n"

// checkRepo is a repo whose commit 1 holds changelog.d/.lighthouse and changelog.d/old.md.
func checkRepo(t *testing.T) (root, c1 string) {
	t.Helper()
	root = t.TempDir()
	write(t, filepath.Join(root, "changelog.d", ".lighthouse"), "marker\n")
	write(t, filepath.Join(root, "changelog.d", "old.md"), "---\ntype: fixed\n---\nOld.\n")
	write(t, filepath.Join(root, "code.go"), "package a\n")
	git(t, root, "init", "-q", "-b", "main")
	return root, commit(t, root, "one")
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, root string)
		code   int
		stderr string
	}{
		{"noFragmentAdded", func(t *testing.T, root string) {
			write(t, filepath.Join(root, "code.go"), "package b\n")
		}, 1, "no new fragment in changelog.d/: add one (see docs/releasing.md), type `internal` if the change needs no changelog entry\n"},
		{"modifiedFragmentIsNotNew", func(t *testing.T, root string) {
			write(t, filepath.Join(root, "changelog.d", "old.md"), "---\ntype: added\n---\nEdited.\n")
		}, 1, "no new fragment"},
		{"renamedFragmentCounts", func(t *testing.T, root string) {
			git(t, root, "mv", "changelog.d/old.md", "changelog.d/new.md")
		}, 0, ""},
		{"brokenFragmentAnywhere", func(t *testing.T, root string) {
			write(t, filepath.Join(root, "changelog.d", "old.md"), "garbage\n")
			write(t, filepath.Join(root, "changelog.d", "new.md"), goodFragment)
		}, 1, "{root}/changelog.d/old.md: first line must be exactly `---`"},
		{"validNewFragment", func(t *testing.T, root string) {
			write(t, filepath.Join(root, "changelog.d", "new.md"), goodFragment)
		}, 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root, c1 := checkRepo(t)
			tc.change(t, root)
			c2 := commit(t, root, "two")
			var stdout, stderr bytes.Buffer
			code := run([]string{"check", "--base", c1, "--head", c2, "--root", root}, &stdout, &stderr)
			if code != tc.code {
				t.Fatalf("exit = %d, want %d (stderr %q)", code, tc.code, stderr.String())
			}
			if tc.stderr == "" {
				if stderr.Len() != 0 {
					t.Fatalf("stderr = %q, want empty", stderr.String())
				}
			} else if want := strings.ReplaceAll(tc.stderr, "{root}", root); !strings.HasPrefix(stderr.String(), want) {
				t.Fatalf("stderr = %q, want prefix %q", stderr.String(), want)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
		})
	}
	t.Run("markerAloneIsNotAFragment", func(t *testing.T) {
		root := t.TempDir()
		write(t, filepath.Join(root, "code.go"), "package a\n")
		git(t, root, "init", "-q", "-b", "main")
		c1 := commit(t, root, "one")
		write(t, filepath.Join(root, "changelog.d", ".lighthouse"), "marker\n")
		c2 := commit(t, root, "two")
		var stderr bytes.Buffer
		code := run([]string{"check", "--base", c1, "--head", c2, "--root", root}, new(bytes.Buffer), &stderr)
		if code != 1 || !strings.HasPrefix(stderr.String(), "no new fragment in changelog.d/") {
			t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
		}
	})
	t.Run("badBaseCannotCheck", func(t *testing.T) {
		root, c1 := checkRepo(t)
		var stderr bytes.Buffer
		code := run([]string{"check", "--base", "deadbeef", "--head", c1, "--root", root}, new(bytes.Buffer), &stderr)
		if code != 2 || !strings.HasPrefix(stderr.String(), "COULD NOT CHECK: git diff deadbeef..."+c1+" failed: ") {
			t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
		}
	})
	t.Run("nonUTF8Name", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("NTFS names are UTF-16, so Go writes no non-UTF-8 name there; see docs/host-assumptions.md")
		}
		root, c1 := checkRepo(t)
		bad := filepath.Join(root, "changelog.d") + "/\xff.md"
		if err := os.WriteFile(bad, []byte(goodFragment), 0o600); err != nil {
			// APFS refuses a name that is not valid UTF-8 with EILSEQ: there the case cannot occur.
			if errors.Is(err, syscall.EILSEQ) {
				t.Skipf("the filesystem refuses a non-UTF-8 name: %v", err)
			}
			t.Fatal(err)
		}
		c2 := commit(t, root, "two")
		var stderr bytes.Buffer
		code := run([]string{"check", "--base", c1, "--head", c2, "--root", root}, new(bytes.Buffer), &stderr)
		if code != 2 || !strings.HasPrefix(stderr.String(), "COULD NOT CHECK: ") {
			t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
		}
	})
}

func frag(typ string) fragment { return fragment{path: "f.md", typ: typ, text: "Entry."} }

func fragsOf(types ...string) []fragment {
	var out []fragment
	for _, typ := range types {
		out = append(out, frag(typ))
	}
	return out
}

func TestDecideBump(t *testing.T) {
	tests := []struct {
		types         []string
		event, forced string
		want          string
	}{
		{nil, "push", "auto", ""},
		{[]string{"internal"}, "push", "auto", ""},
		{[]string{"fixed"}, "push", "auto", "patch"},
		{[]string{"fixed", "internal"}, "push", "auto", "patch"},
		{[]string{"added"}, "push", "auto", ""},
		{[]string{"fixed", "added"}, "push", "auto", "minor"},
		{[]string{"added"}, "schedule", "auto", "minor"},
		{[]string{"fixed"}, "schedule", "auto", "patch"},
		{[]string{"internal"}, "schedule", "auto", ""},
		{[]string{"changed"}, "workflow_dispatch", "auto", "minor"},
		{[]string{"fixed"}, "workflow_dispatch", "minor", "minor"},
		{[]string{"added"}, "workflow_dispatch", "major", "major"},
	}
	for _, tc := range tests {
		got, err := decideBump(fragsOf(tc.types...), tc.event, tc.forced)
		if err != nil || got != tc.want {
			t.Errorf("decideBump(%v, %s, %s) = %q, %v; want %q", tc.types, tc.event, tc.forced, got, err, tc.want)
		}
	}
}

func TestDecideBumpRefuses(t *testing.T) {
	tests := []struct {
		types  []string
		forced string
		msg    string
	}{
		{[]string{"added"}, "patch", "forced bump patch is lower than the minor the fragments need"},
		{nil, "major", "nothing to release: no releasable fragment for the forced bump"},
		{[]string{"internal"}, "minor", "nothing to release: no releasable fragment for the forced bump"},
	}
	for _, tc := range tests {
		_, err := decideBump(fragsOf(tc.types...), "workflow_dispatch", tc.forced)
		var re *releaseError
		if !errors.As(err, &re) || re.msg != tc.msg {
			t.Errorf("decideBump(%v, %s) err = %v, want releaseError %q", tc.types, tc.forced, err, tc.msg)
		}
	}
}

func TestLatestTag(t *testing.T) {
	got, err := latestTag([]string{"v0.1.6", "v0.1.10", "v0.1.9", "v0.2.0-rc.1", "vfoo"})
	if err != nil || got != "v0.1.10" {
		t.Fatalf("latestTag = %q, %v; want v0.1.10", got, err)
	}
	for _, tags := range [][]string{nil, {"v0.2.0-rc.1"}} {
		_, err := latestTag(tags)
		var te *tagError
		if !errors.As(err, &te) || te.msg != "no vX.Y.Z release tag found (a checkout without tags?)" {
			t.Errorf("latestTag(%v) err = %v, want tagError", tags, err)
		}
	}
}

func TestNextVersion(t *testing.T) {
	for bump, want := range map[string]string{"patch": "v0.1.7", "minor": "v0.2.0", "major": "v1.0.0"} {
		if got, err := nextVersion("v0.1.6", bump); err != nil || got != want {
			t.Errorf("nextVersion(%s) = %q, %v; want %q", bump, got, err, want)
		}
	}
	if _, err := nextVersion("v0.1.6", "huge"); err == nil || err.Error() != "cannot bump 'v0.1.6' by 'huge'" {
		t.Errorf("err = %v", err)
	}
}

func TestRenderSection(t *testing.T) {
	frags := []fragment{
		{typ: "removed", text: "Gone."}, {typ: "internal", text: "Secret."},
		{typ: "added", text: "New."}, {typ: "fixed", text: "Fix\nsecond line."},
	}
	want := "## [v0.2.0] - 2026-10-27\n\n### Added\n\n- New.\n\n### Fixed\n\n- Fix\n  second line.\n\n### Removed\n\n- Gone.\n"
	if got := renderSection("v0.2.0", "2026-10-27", frags); got != want {
		t.Fatalf("renderSection =\n%q\nwant\n%q", got, want)
	}
}

const preamble = "# Changelog\n\nIntro.\n\n"
const oldRelease = "## [v0.1.6] - 2026-01-01\n\n### Fixed\n\n- Old.\n"

func TestInsertSection(t *testing.T) {
	section := "## [v0.2.0] - d\n\n- x\n"
	if got, want := insertSection(preamble+oldRelease, section), preamble+section+"\n"+oldRelease; got != want {
		t.Errorf("above latest: got %q, want %q", got, want)
	}
}

func TestInsertSectionWithoutRelease(t *testing.T) {
	section := "## [v0.2.0] - d\n\n- x\n"
	for in, want := range map[string]string{
		"# Changelog\n": "# Changelog\n\n" + section,
		"# Changelog":   "# Changelog\n\n" + section,
		"":              section,
	} {
		if got := insertSection(in, section); got != want {
			t.Errorf("insertSection(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractSection(t *testing.T) {
	in := preamble + "## [v0.2.0] - d\n\n### Added\n\n- New.\n\n" + oldRelease
	if got, ok := extractSection(in, "v0.2.0"); !ok || got != "### Added\n\n- New." {
		t.Errorf("v0.2.0 = %q, %v", got, ok)
	}
	if got, ok := extractSection(in, "v0.1.6"); !ok || got != "### Fixed\n\n- Old." {
		t.Errorf("v0.1.6 = %q, %v", got, ok)
	}
	if got, ok := extractSection(in, "v9.9.9"); ok || got != "" {
		t.Errorf("missing = %q, %v", got, ok)
	}
}

func TestExtractSectionCRLF(t *testing.T) {
	got, ok := extractSection("## [v1] - d\r\nbody\r\n## [v0] - d\r\n", "v1")
	if !ok || got != "body" {
		t.Fatalf("got %q, %v; want body, true", got, ok)
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		in   string
		keep bool
		want []string
	}{
		{"", false, nil},
		{"a\nb\n", false, []string{"a", "b"}},
		{"a\r\nb\rc\nd", false, []string{"a", "b", "c", "d"}},
		{"a\r\nb\rc\n\n", true, []string{"a\r\n", "b\r", "c\n", "\n"}},
	}
	for _, tc := range tests {
		if got := splitLines(tc.in, tc.keep); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitLines(%q, %v) = %q, want %q", tc.in, tc.keep, got, tc.want)
		}
	}
}

func releaseRoot(t *testing.T, fragments map[string]string, changelog string, withChangelog bool) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, "changelog.d", ".lighthouse"), "marker\n")
	for name, typ := range fragments {
		write(t, filepath.Join(root, "changelog.d", name), "---\ntype: "+typ+"\n---\n"+name+" text.\n")
	}
	if withChangelog {
		write(t, filepath.Join(root, "CHANGELOG.md"), changelog)
	}
	return root
}

func TestReleaseWritesChangelogAndRemovesFragments(t *testing.T) {
	root := releaseRoot(t, map[string]string{"a.md": "fixed", "b.md": "added", "c.md": "internal"}, preamble+oldRelease, true)
	tag, err := release(root, "push", "auto", "2026-10-27", []string{"v0.1.6"})
	if err != nil || tag != "v0.2.0" {
		t.Fatalf("release = %q, %v; want v0.2.0", tag, err)
	}
	text, _ := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	want := preamble + "## [v0.2.0] - 2026-10-27\n\n### Added\n\n- b.md text.\n\n### Fixed\n\n- a.md text.\n\n" + oldRelease
	if string(text) != want {
		t.Fatalf("CHANGELOG.md =\n%q\nwant\n%q", text, want)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "changelog.d"))
	if len(entries) != 1 || entries[0].Name() != ".lighthouse" {
		t.Fatalf("changelog.d holds %v, want only the marker", entries)
	}
}

func TestReleaseNothingLeavesTreeUntouched(t *testing.T) {
	root := releaseRoot(t, map[string]string{"a.md": "added"}, preamble+oldRelease, true)
	tag, err := release(root, "push", "auto", "2026-10-27", []string{"v0.1.6"})
	if err != nil || tag != "" {
		t.Fatalf("release = %q, %v; want empty, nil", tag, err)
	}
	text, _ := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if string(text) != preamble+oldRelease {
		t.Errorf("CHANGELOG.md changed: %q", text)
	}
	if _, err := os.Stat(filepath.Join(root, "changelog.d", "a.md")); err != nil {
		t.Errorf("fragment removed: %v", err)
	}
}

func TestReleaseWithoutChangelogRefuses(t *testing.T) {
	root := releaseRoot(t, map[string]string{"a.md": "fixed"}, "", false)
	_, err := release(root, "push", "auto", "2026-10-27", []string{"v0.1.6"})
	var re *releaseError
	if !errors.As(err, &re) || !strings.HasPrefix(re.msg, filepath.Join(root, "CHANGELOG.md")+": cannot read: ") {
		t.Fatalf("err = %v, want releaseError", err)
	}
	if _, err := os.Stat(filepath.Join(root, "changelog.d", "a.md")); err != nil {
		t.Errorf("fragment removed: %v", err)
	}
}

func TestReleaseWithoutTagsComesFirst(t *testing.T) {
	root := releaseRoot(t, map[string]string{"a.md": "fixed"}, "", false)
	_, err := release(root, "push", "auto", "2026-10-27", nil)
	var te *tagError
	if !errors.As(err, &te) {
		t.Fatalf("err = %v, want tagError", err)
	}
}

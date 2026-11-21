package checkinstr

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Run on a root or a config that cannot be measured: exit 2 with COULD NOT CHECK on stderr and
// nothing on stdout.
// Class: cli-output. At risk: an unmeasurable repo passing as green.
func TestRunCouldNotCheck(t *testing.T) {
	t.Run("a root that is no directory", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "bestaat-niet")
		var out, errOut bytes.Buffer
		code := Run(missing, t.TempDir(), &out, &errOut)
		want := "check-instructions: COULD NOT CHECK — repository root '" + missing + "' is not a directory\n"
		if code != 2 || errOut.String() != want || out.Len() != 0 {
			t.Errorf("code %d stderr %q stdout %q, want 2 %q \"\"", code, errOut.String(), out.String(), want)
		}
	})
	t.Run("a broken lighthouse.toml", func(t *testing.T) {
		// The text after `config: ` is the TOML parser's, not ours: only the prefix is pinned.
		r := check(t, emptySkillRoots(), runOptions{toml: "[project\n"})
		if r.code != 2 || r.stdout != "" ||
			!strings.HasPrefix(r.stderr, "check-instructions: COULD NOT CHECK — config: ") {
			t.Errorf("code %d stdout %q stderr %q, want 2 \"\" and the prefix of a config failure", r.code, r.stdout, r.stderr)
		}
	})
}

// The checks run on this very repository, so a skill pair that drifts or a description that
// overlaps fails the commit that does it. The fix for an overlap is a bounding clause in that
// description, not a higher threshold.
// Class: cli-output. At risk: the instruction tree of lighthouse itself going red unnoticed.
func TestRunIsGreenOnThisRepo(t *testing.T) {
	base := t.TempDir()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("../.. is not the repo root: %v", err)
	}
	// Check 5 reads the personal directory: the one of the machine would block every commit.
	r := runIn(t, root, base, filepath.Join(base, "personal", "skills"), runOptions{})
	if r.code != 0 {
		t.Errorf("exit %d on this repo, want 0\nstdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
	}
}

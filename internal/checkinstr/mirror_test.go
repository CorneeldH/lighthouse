package checkinstr

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureDir holds the check-11 fixtures that the former check-instructions.sh script recorded before the port:
// per case a claude/ and an agents/ skill tree, the stdout of --print-mirror-exceptions and the
// check-11 block of a full run. The root path without its leading slash is {root}; the hint of
// the recorded block names `lighthouse check-instructions` instead of the script. They were
// recorded with the git config pinned below and the cwd outside any repo.
const fixtureDir = "../../tests/pack/fixtures/check_instructions"

// copyTree copies the files under src to dst with mode 0644: git prints the mode of a binary pair.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func readFixture(t *testing.T, path, root string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "{root}", strings.TrimPrefix(root, "/"))
}

// Red when the Go output of check 11 differs by one byte from what the bash script recorded:
// a changed line, a missing final newline, a binary file, a file on one side, a marked skill.
func TestMirrorFixtures(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.abbrev")
	t.Setenv("GIT_CONFIG_VALUE_0", "12")
	for _, name := range []string{"changed-line", "missing-newline", "binary", "one-side", "marked"} {
		t.Run(name, func(t *testing.T) {
			fixture, err := filepath.Abs(filepath.Join(fixtureDir, name))
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			copyTree(t, filepath.Join(fixture, "claude"), root+"/.claude/skills")
			copyTree(t, filepath.Join(fixture, "agents"), root+"/.agents/skills")
			if err := os.WriteFile(root+"/lighthouse.toml", nil, 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("LIGHTHOUSE_TOML", root+"/lighthouse.toml")
			t.Chdir(root)

			var out, errOut bytes.Buffer
			if code := PrintMirrorExceptions(root, &out, &errOut); code != 0 {
				t.Fatalf("--print-mirror-exceptions exit %d, stderr:\n%s", code, errOut.String())
			}
			if want := readFixture(t, filepath.Join(fixture, "print-mirror-exceptions.txt"), root); out.String() != want {
				t.Errorf("--print-mirror-exceptions:\n got: %q\nwant: %q", out.String(), want)
			}

			rp, code := open(root, root+"/personal", &errOut)
			if code != 0 {
				t.Fatalf("open: exit %d, stderr:\n%s", code, errOut.String())
			}
			out.Reset()
			checkMirrorContent(rp, &report{out: &out}, &errOut)
			if want := readFixture(t, filepath.Join(fixture, "check-11.txt"), root); out.String() != want {
				t.Errorf("check 11:\n got: %q\nwant: %q", out.String(), want)
			}
			if errOut.Len() != 0 {
				t.Errorf("stderr: %q, want nothing", errOut.String())
			}
		})
	}
}

// Red when the FAIL display keeps the function name git puts after a hunk header: `diff -u`, which
// the script used, prints none.
func TestDisplayHasNoFunctionContext(t *testing.T) {
	recorded := "=== demo/SKILL.md\nalpha\n1\n2\n3\n4\n5\n< old"
	now := "=== demo/SKILL.md\nalpha\n1\n2\n3\n4\n5\n< new"
	want := "@@ -5,4 +5,4 @@\n 3\n 4\n 5\n-< old\n+< new\n"
	if got := display(recorded, now); got != want {
		t.Errorf("display:\n got: %q\nwant: %q", got, want)
	}
}

// Red when the FAIL display honours a user's external diff: `true` as diff.external prints
// nothing, and the FAIL body would be empty.
func TestDisplayIgnoresExternalDiff(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "diff.external")
	t.Setenv("GIT_CONFIG_VALUE_0", "true")
	want := "@@ -1 +1 @@\n-< old\n+< new\n"
	if got := display("< old", "< new"); got != want {
		t.Errorf("display:\n got: %q\nwant: %q", got, want)
	}
}

//go:build e2e

package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// literalGrep runs ci/literal-grep.sh from the repo root on dirs, with extra env entries
// (LITERAL_* from the test session are dropped first), and returns the exit code and stdout.
func literalGrep(t *testing.T, env []string, dirs ...string) (int, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("ci/literal-grep.sh is a bash script")
	}
	root := repoRoot(t)
	var base []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "LITERAL_") {
			base = append(base, kv)
		}
	}
	cmd := exec.Command(filepath.Join(root, "ci", "literal-grep.sh"), dirs...)
	cmd.Dir = root
	cmd.Env = append(base, env...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), stdout.String()
	}
	if err != nil {
		t.Fatalf("literal-grep did not run: %v", err)
	}
	return 0, stdout.String()
}

func writeAt(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// allowlist writes an allow list with the given lines and returns the env entry for it.
func allowlist(t *testing.T, lines ...string) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "allow.txt")
	writeAt(t, path, "# exceptions\n\n"+strings.Join(lines, "\n")+"\n")
	return []string{"LITERAL_ALLOWLIST=" + path}
}

const markers = `MARKERS = ("<!-- paia:",)` + "\n"

// A production change that fails this test: a script that matches case-sensitively or drops
// one of the denied literals (a repo name, host name, host IP, label or board ID), so such a
// literal gets into pack/ unnoticed.
func TestLiteralGrepHitFailsCaseInsensitively(t *testing.T) {
	for _, hit := range []string{"PAIA_ROOT", "Beelink", "/home/x", "4-lane:stalled", "PVT_kwHOAGmU3s4BfI-i"} {
		t.Run(hit, func(t *testing.T) {
			dir := t.TempDir()
			writeAt(t, filepath.Join(dir, "a.md"), "x "+hit+" y\n")
			code, out := literalGrep(t, nil, dir)
			if code != 1 {
				t.Errorf("rc = %d, want 1; stdout: %s", code, out)
			}
			if !strings.Contains(out, "a.md:1:") {
				t.Errorf("stdout lacks a.md:1: %s", out)
			}
		})
	}
}

// A production change that fails this test: a deny list that lost its patterns turns the
// check green (rc 0) instead of unusable (rc 2).
func TestLiteralGrepEmptyDenylistIsRC2(t *testing.T) {
	dir := t.TempDir()
	deny := filepath.Join(t.TempDir(), "deny.txt")
	writeAt(t, deny, "# only a comment\n\n")
	writeAt(t, filepath.Join(dir, "a.txt"), "PAIA\n")

	code, out := literalGrep(t, []string{"LITERAL_DENYLIST=" + deny}, dir)

	if code != 2 {
		t.Errorf("rc = %d, want 2; stdout: %s", code, out)
	}
}

// A production change that fails this test: the script stops skipping __pycache__ and *.pyc
// (the installer skips them and they are never committed), so a built cache fails the scan.
func TestLiteralGrepPythonCachesAreSkipped(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, filepath.Join(dir, "lib", "__pycache__", "x.cpython-314.pyc"), "paia")
	writeAt(t, filepath.Join(dir, "lib", "y.pyc"), "paia")

	code, out := literalGrep(t, nil, dir)

	if code != 0 {
		t.Errorf("rc = %d, want 0; stdout: %s", code, out)
	}
}

// A production change that fails this test: an allow list entry (path:text) that no longer
// lets its exact line through, so a deliberate exception fails the scan.
func TestLiteralGrepAllowlistedLinePasses(t *testing.T) {
	pack := filepath.Join(t.TempDir(), "pack")
	writeAt(t, filepath.Join(pack, "a.py"), markers)
	env := allowlist(t, pack+"/a.py:"+strings.TrimSuffix(markers, "\n"))

	code, out := literalGrep(t, env, pack)

	if code != 0 {
		t.Errorf("rc = %d, want 0; stdout: %s", code, out)
	}
}

// A production change that fails this test: an allow list that matches the text only, so the
// same literal in another file passes too.
func TestLiteralGrepSameTextInOtherFileFails(t *testing.T) {
	pack := filepath.Join(t.TempDir(), "pack")
	writeAt(t, filepath.Join(pack, "a.py"), markers)
	writeAt(t, filepath.Join(pack, "b.py"), markers)
	env := allowlist(t, pack+"/a.py:"+strings.TrimSuffix(markers, "\n"))

	code, out := literalGrep(t, env, pack)

	if code != 1 {
		t.Errorf("rc = %d, want 1; stdout: %s", code, out)
	}
	if !strings.Contains(out, pack+"/b.py:1:") {
		t.Errorf("stdout lacks the b.py hit: %s", out)
	}
	if strings.Contains(out, pack+"/a.py") {
		t.Errorf("stdout names the allowlisted a.py: %s", out)
	}
}

// A production change that fails this test: an allow list that matches the path only, so
// every other line of an allowlisted file passes too.
func TestLiteralGrepOtherLineInAllowlistedFileFails(t *testing.T) {
	pack := filepath.Join(t.TempDir(), "pack")
	writeAt(t, filepath.Join(pack, "a.py"), markers+"PAIA_ROOT = 1\n")
	env := allowlist(t, pack+"/a.py:"+strings.TrimSuffix(markers, "\n"))

	code, out := literalGrep(t, env, pack)

	if code != 1 {
		t.Errorf("rc = %d, want 1; stdout: %s", code, out)
	}
	if !strings.Contains(out, pack+"/a.py:2:") {
		t.Errorf("stdout lacks the line 2 hit: %s", out)
	}
	if strings.Contains(out, pack+"/a.py:1:") {
		t.Errorf("stdout names the allowlisted line 1: %s", out)
	}
}

// A production change that fails this test: the script stops skipping <dir>/SOURCES.toml
// (which names source paths and is never installed), so a pack scan fails on it.
func TestLiteralGrepSourcesTomlIsSkipped(t *testing.T) {
	pack := filepath.Join(t.TempDir(), "pack")
	line := `source_path = ".claude/skills/paia-start-issue/SKILL.md"` + "\n"
	writeAt(t, filepath.Join(pack, "SOURCES.toml"), line)
	if code, out := literalGrep(t, nil, pack); code != 0 {
		t.Fatalf("SOURCES.toml alone: rc = %d, want 0; stdout: %s", code, out)
	}
	writeAt(t, filepath.Join(pack, "core", "x.md"), line)

	code, out := literalGrep(t, nil, pack)

	if code != 1 {
		t.Errorf("rc = %d, want 1; stdout: %s", code, out)
	}
	if strings.Contains(out, "SOURCES.toml") {
		t.Errorf("stdout names SOURCES.toml: %s", out)
	}
}

// A production change that fails this test: an allow list that cannot be read stops being an
// error (rc 2) and the scan goes on without exceptions or turns green.
func TestLiteralGrepUnreadableAllowlistIsRC2(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, filepath.Join(dir, "a.txt"), "clean\n")
	env := []string{"LITERAL_ALLOWLIST=" + filepath.Join(t.TempDir(), "missing.txt")}

	code, out := literalGrep(t, env, dir)

	if code != 2 {
		t.Errorf("rc = %d, want 2; stdout: %s", code, out)
	}
}

// A production change that fails this test: LITERAL_SKIP_TESTS=1 stops skipping *_test.go
// and testdata/ (the Go scan fails on test literals), or the skip becomes the default (a
// pack scan misses those files).
func TestLiteralGrepGoTestsAreSkippedOnlyOnRequest(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, filepath.Join(dir, "a_test.go"), "// beelink\n")
	writeAt(t, filepath.Join(dir, "testdata", "x.json"), `{"host": "beelink"}`+"\n")

	if code, out := literalGrep(t, nil, dir); code != 1 {
		t.Errorf("without the skip: rc = %d, want 1; stdout: %s", code, out)
	}
	if code, out := literalGrep(t, []string{"LITERAL_SKIP_TESTS=1"}, dir); code != 0 {
		t.Errorf("with the skip: rc = %d, want 0; stdout: %s", code, out)
	}
}

// A production change that fails this test: a comment or blank line of the deny list is
// taken as a pattern; a blank pattern matches every line.
func TestLiteralGrepCommentAndBlankDenylistLinesMatchNothing(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, filepath.Join(dir, "a.txt"), "plain text\n")

	code, out := literalGrep(t, nil, dir)

	if code != 0 {
		t.Errorf("rc = %d, want 0; stdout: %s", code, out)
	}
}

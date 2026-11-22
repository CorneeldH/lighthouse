//go:build e2e

package e2e

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runNoPython runs ci/no-python.sh with a freshly built lighthouse on an empty work directory,
// with extra env entries, and returns the exit code, stderr and the work directory.
func runNoPython(t *testing.T, env ...string) (int, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("ci/no-python.sh is a bash script")
	}
	bin := buildLighthouse(t)
	work := scratchDir(t)
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "ci", "no-python.sh"), bin, work)
	cmd.Env = append(cleanEnv(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("no-python.sh did not run: %v", err)
		}
		code = exit.ExitCode()
	}
	return code, stderr.String(), work
}

// copyTree copies the directory src to dst.
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

// A production change that fails this test: ci/no-python.sh stops failing on a registered hook without a fixture.
func TestNoPythonMissingFixtureFails(t *testing.T) {
	fixtures := filepath.Join(t.TempDir(), "fixtures")
	copyTree(t, filepath.Join(repoRoot(t), "tests", "pack", "fixtures", "hooks"), fixtures)
	if err := os.Remove(filepath.Join(fixtures, "Stop", "go-lint-gate.json")); err != nil {
		t.Fatal(err)
	}

	code, stderr, _ := runNoPython(t, "NO_PYTHON_FIXTURES="+fixtures)

	if code != 1 {
		t.Errorf("exit code = %d, want 1; stderr:\n%s", code, stderr)
	}
	want := "no fixture " + fixtures + "/Stop/go-lint-gate.json"
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr lacks %q:\n%s", want, stderr)
	}
}

// A production change that fails this test: ci/no-python.sh stops failing on a file of the initialised repo that names python3.
func TestNoPythonPython3HitFails(t *testing.T) {
	extra := filepath.Join(t.TempDir(), "extra.txt")
	if err := os.WriteFile(extra, []byte("one\nexec python3 -c pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stderr, work := runNoPython(t, "NO_PYTHON_EXTRA="+extra)

	if code != 1 {
		t.Errorf("exit code = %d, want 1; stderr:\n%s", code, stderr)
	}
	want := "python3 in " + work + "/repo/extra.txt:2"
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr lacks %q:\n%s", want, stderr)
	}
}

// scratchDir is an empty directory the script may fill with a HOME: a Go module cache in it is
// read-only, so the cleanup makes it writable first, which t.TempDir does not.
func scratchDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lighthouse-nopython-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(path, 0o755)
			}
			return nil
		})
		_ = os.RemoveAll(dir)
	})
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

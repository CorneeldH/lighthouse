//go:build unix

package checkpointscan

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// passThrough drops everything up to the first `--` and runs the rest, like systemd-run and
// choom do with their own options.
const passThrough = "#!/bin/sh\nwhile [ \"$1\" != -- ]; do shift; done\nshift\nexec \"$@\"\n"

const systemctlOK = "#!/bin/sh\nexit 0\n"

// boundedStubs are the stubs that make wrap build a scope without a user bus.
func boundedStubs() map[string]string {
	return map[string]string{"systemctl": systemctlOK, "systemd-run": passThrough, "choom": passThrough}
}

// stubPath puts stubs (name -> script) first on PATH and forgets what a probe learned before.
func stubPath(t *testing.T, stubs map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	resetBounding()
	t.Cleanup(resetBounding)
	return dir
}

// onlyPath is PATH with just the stubs and the tools the chain needs, so a tool the real host
// has can be left out.
func onlyPath(t *testing.T, stubs map[string]string, tools ...string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range tools {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("no %s on this host", tool)
		}
		if err := os.Symlink(real, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	resetBounding()
	t.Cleanup(resetBounding)
}

func joined(argv []string) string { return " " + strings.Join(argv, " ") + " " }

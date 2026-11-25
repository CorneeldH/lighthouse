//go:build unix

package checkpointscan

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/stubbin"
)

// passThrough drops everything up to the first `--` and runs the rest, like systemd-run and
// choom do with their own options.
const passThrough = "#!/bin/sh\nwhile [ \"$1\" != -- ]; do shift; done\nshift\nexec \"$@\"\n"

const systemctlOK = "#!/bin/sh\nexit 0\n"

// boundedStubs are the stubs that make wrap build a scope without a user bus.
func boundedStubs() map[string]string {
	return map[string]string{"systemctl": systemctlOK, "systemd-run": passThrough, "choom": passThrough}
}

// writeStubs writes the sh scripts (name -> script, with or without the #!/bin/sh line) into a fresh
// directory and returns it. The scripts shift arguments, exec and test exit codes: POSIX behaviour.
func writeStubs(t *testing.T, stubs map[string]string) string {
	t.Helper()
	stubbin.RequirePOSIX(t, "shift loop, exec and exit codes of the wrap chain stubs")
	dir := t.TempDir()
	for name, body := range stubs {
		stubbin.MakeScript(t, dir, name, strings.TrimPrefix(body, "#!/bin/sh\n"))
	}
	return dir
}

// stubPath puts stubs (name -> script) first on PATH and forgets what a probe learned before.
func stubPath(t *testing.T, stubs map[string]string) string {
	t.Helper()
	dir := writeStubs(t, stubs)
	stubbin.Prepend(t, dir)
	resetBounding()
	t.Cleanup(resetBounding)
	return dir
}

// onlyPath is PATH with just the stubs and the tools the chain needs, so a tool the real host
// has can be left out.
func onlyPath(t *testing.T, stubs map[string]string, tools ...string) {
	t.Helper()
	dir := writeStubs(t, stubs)
	for _, tool := range tools {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("no %s on this host", tool)
		}
		if err := os.Symlink(real, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	stubbin.Alone(t, dir) // the stubs and the linked tools only: that isolation is the point
	resetBounding()
	t.Cleanup(resetBounding)
}

func joined(argv []string) string { return " " + strings.Join(argv, " ") + " " }

// Package testpath holds helpers for tests that need an absolute path or a
// home directory that behaves the same on every platform. It imports no
// production code, so any _test.go file may import it without creating a cycle.
package testpath

import (
	"os"
	"path/filepath"
	"testing"
)

// Abs joins the platform root and elem into an absolute, cleaned path: "/" on
// Linux and macOS, `C:\` (the volume of the temp dir) on Windows. It touches no
// file system and needs no *testing.T, so it works in table literals.
func Abs(elem ...string) string {
	root := filepath.VolumeName(os.TempDir()) + string(filepath.Separator)
	return filepath.Join(append([]string{root}, elem...)...)
}

// SetHome points both HOME and USERPROFILE at dir for the duration of the
// test, because os.UserHomeDir reads HOME on Unix and USERPROFILE on Windows.
func SetHome(t testing.TB, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

// SkipOnWindowsPOSIXAbs skips the test on Windows, where a POSIX absolute path
// such as /abs is not absolute.
func SkipOnWindowsPOSIXAbs(t testing.TB) {
	t.Helper()
	if filepath.Separator == '\\' {
		t.Skip("a POSIX path such as /abs is not absolute on Windows")
	}
}

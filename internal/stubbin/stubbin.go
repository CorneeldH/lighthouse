// Package stubbin writes fake executables (a stub git, codex, ...) that tests put on the PATH.
// A stub made by Make runs on every platform: a shell script on POSIX, a .cmd file on Windows.
//
// It is for tests only: it takes a *testing.T and fails it on any error.
package stubbin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Spec is what a simple stub does.
type Spec struct {
	Stdout string
	Stderr string
	Exit   int
	// LogFile, when set, gets one line per call: the arguments separated by one space.
	LogFile string
}

// MakeScript writes the POSIX script sh (without shebang) as executable name in dir and returns its
// path. On Windows it skips the test: such a script imitates POSIX behaviour.
func MakeScript(t *testing.T, dir, name, sh string) string {
	t.Helper()
	RequirePOSIX(t, "script "+name)
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+sh), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// RequirePOSIX skips the test on Windows.
func RequirePOSIX(t *testing.T, reason string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub imitates POSIX behaviour: " + reason)
	}
}

// Prepend puts dir at the front of the PATH for the test.
func Prepend(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", PathValue(t, dir))
}

// Only sets the PATH to dir plus the directory of each tool in keep.
func Only(t *testing.T, dir string, keep ...string) {
	t.Helper()
	t.Setenv("PATH", PathValue(t, dir, keep...))
}

// PathValue returns the PATH value itself, without setting it, for tests that pass the PATH through
// cmd.Env. Without keep it is dir in front of the current PATH; with keep it is the form of Only.
func PathValue(t *testing.T, dir string, keep ...string) string {
	t.Helper()
	sep := string(os.PathListSeparator)
	if len(keep) == 0 {
		return dir + sep + os.Getenv("PATH")
	}
	dirs, err := lookDirs(keep)
	if err != nil {
		t.Fatal(err)
	}
	return joinPath(sep, append([]string{dir}, dirs...)...)
}

// ReadLog reads a LogFile as lines, with CRLF normalised to LF.
func ReadLog(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// lookDirs returns the directory of each tool, or an error for the first one LookPath cannot find.
func lookDirs(keep []string) ([]string, error) {
	var dirs []string
	for _, tool := range keep {
		p, err := exec.LookPath(tool)
		if err != nil {
			return nil, fmt.Errorf("stubbin: tool %q to keep not found: %w", tool, err)
		}
		dirs = append(dirs, filepath.Dir(p))
	}
	return dirs, nil
}

// joinPath joins dirs with sep, leaving out duplicates.
func joinPath(sep string, dirs ...string) string {
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		if seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return strings.Join(out, sep)
}

// cmdText is the Windows .cmd text of a stub. base is the path of the stub without extension: the
// output comes byte-exact from base.stdout and base.stderr, so nothing needs escaping.
func cmdText(s Spec, base string) string {
	var b strings.Builder
	b.WriteString("@echo off\r\n")
	b.WriteString(`type "` + base + ".stdout\"\r\n")
	if s.Stderr != "" {
		b.WriteString(`type "` + base + ".stderr\" 1>&2\r\n")
	}
	if s.LogFile != "" {
		b.WriteString(`echo(%*>>"` + s.LogFile + "\"\r\n")
	}
	b.WriteString("exit /b " + strconv.Itoa(s.Exit) + "\r\n")
	return b.String()
}

// shText is the POSIX script text of a stub.
func shText(s Spec) string {
	q := func(v string) string { return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'" }
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	if s.LogFile != "" {
		b.WriteString(`echo "$*" >> ` + q(s.LogFile) + "\n")
	}
	b.WriteString("printf '%s' " + q(s.Stdout) + "\n")
	if s.Stderr != "" {
		b.WriteString("printf '%s' " + q(s.Stderr) + " >&2\n")
	}
	b.WriteString("exit " + strconv.Itoa(s.Exit) + "\n")
	return b.String()
}

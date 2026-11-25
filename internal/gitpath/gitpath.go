// Package gitpath normalises path output of git commands to the native
// form of the operating system. Git prints forward slashes on every platform,
// and its lines end in a newline (or CRLF on Windows).
package gitpath

import (
	"path/filepath"
	"strings"
)

// Native turns one path from git output into the native OS form. Surrounding
// whitespace (including a trailing newline or CRLF) is removed, and the result
// is cleaned. An empty or whitespace-only input yields "".
func Native(out string) string {
	s := strings.TrimSpace(out)
	if s == "" {
		return ""
	}
	return filepath.Clean(filepath.FromSlash(s))
}

// NativeLines splits git output into lines and applies Native to each one.
// Empty lines are skipped.
func NativeLines(out string) []string {
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if p := Native(line); p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

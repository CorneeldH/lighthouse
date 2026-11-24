// Package automemory recognises the paths of Claude Code's auto-memory.
package automemory

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"
)

var memoryRe = regexp.MustCompile(`(^|/)\.claude/projects/[^/]+/memory(/|$)`)

// ConfigDir returns CLAUDE_CONFIG_DIR when set, else <home>/.claude.
func ConfigDir(getenv func(string) string, home string) string {
	if d := getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(home, ".claude")
}

// Normalize turns backslashes into slashes, expands a leading ~, $HOME or
// ${HOME} to home, and lowercases the result.
func Normalize(p, home string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	home = strings.ReplaceAll(home, `\`, "/")
	switch {
	case p == "~":
		p = home
	case strings.HasPrefix(p, "~/"):
		p = home + p[1:]
	case strings.HasPrefix(p, "$HOME/"):
		p = home + p[len("$HOME"):]
	case strings.HasPrefix(p, "${HOME}/"):
		p = home + p[len("${HOME}"):]
	}
	return strings.ToLower(p)
}

// IsMemoryPath reports whether p lies in an auto-memory directory, under
// any .claude directory or under configDir.
func IsMemoryPath(p, home, configDir string) bool {
	n := Normalize(p, home)
	if memoryRe.MatchString(n) {
		return true
	}
	if configDir == "" {
		return false
	}
	prefix := strings.TrimRight(Normalize(configDir, home), "/") + "/projects/"
	rest, ok := strings.CutPrefix(n, prefix)
	if !ok {
		return false
	}
	seg, tail, ok := strings.Cut(rest, "/")
	if !ok || seg == "" {
		return false
	}
	return tail == "memory" || strings.HasPrefix(tail, "memory/")
}

// EncodeProject replaces every UTF-16 code unit that is not an ASCII letter
// or digit with '-', like JavaScript's replace(/[^a-zA-Z0-9]/g, "-"): an
// astral character becomes two dashes.
func EncodeProject(path string) string {
	units := utf16.Encode([]rune(path))
	var b strings.Builder
	for _, u := range units {
		if u >= 'a' && u <= 'z' || u >= 'A' && u <= 'Z' || u >= '0' && u <= '9' {
			b.WriteByte(byte(u))
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

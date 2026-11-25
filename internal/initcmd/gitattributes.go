package initcmd

import (
	"bytes"
	"strings"
)

const gitattributesPath = ".gitattributes"

// missingGitattributes returns the lighthouse rules ensureGitattributes would add to old.
func missingGitattributes(old []byte) []string {
	var missing []string
	if !hasAttrPattern(old, "*.sh", "") {
		missing = append(missing, "*.sh eol=lf")
	}
	if !hasAttrPattern(old, ".claude/hooks/lighthouse/**", "eol=lf") {
		missing = append(missing, ".claude/hooks/lighthouse/** eol=lf")
	}
	return missing
}

// ensureGitattributes returns old with the rules that keep the hook scripts LF at the end:
// `*.sh eol=lf` unless a line already has exactly that pattern, and
// `.claude/hooks/lighthouse/** eol=lf` unless a line with that pattern already sets eol=lf.
// It only appends, in the line ending of the document; old itself is returned when
// nothing is missing.
func ensureGitattributes(old []byte) []byte {
	eol := "\n"
	if bytes.Contains(old, []byte("\r\n")) {
		eol = "\r\n"
	}
	missing := missingGitattributes(old)
	if len(missing) == 0 {
		return old
	}
	out := append([]byte(nil), old...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, eol...)
	}
	for _, l := range missing {
		out = append(out, l+eol...)
	}
	return out
}

// hasAttrPattern reports whether a line has pattern as its first field and, when attr
// is not empty, that attribute among the others.
func hasAttrPattern(b []byte, pattern, attr string) bool {
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) == 0 || f[0] != pattern {
			continue
		}
		if attr == "" {
			return true
		}
		for _, a := range f[1:] {
			if a == attr {
				return true
			}
		}
	}
	return false
}

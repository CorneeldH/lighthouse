package codelang

import (
	"strings"
	"unicode"
)

// isPySpace reports whether r is whitespace in the sense of Python's str.isspace, which is
// wider than unicode.IsSpace: it also holds the separators U+001C to U+001F.
func isPySpace(r rune) bool {
	switch {
	case r >= '\t' && r <= '\r', r >= 0x1c && r <= 0x1f, r == 0x85:
		return true
	}
	return unicode.Is(unicode.Z, r)
}

// trimSpace is Python's str.strip().
func trimSpace(s string) string {
	return strings.TrimFunc(s, isPySpace)
}

// splitLines has the semantics of Python's str.splitlines(): it splits on \n, \r, \r\n, \v, \f,
// U+001C to U+001E, U+0085, U+2028 and U+2029, and a trailing separator starts no empty line.
func splitLines(s string) []string {
	lines := []string{}
	runes := []rune(s)
	start := 0
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			lines = append(lines, string(runes[start:i]))
			if runes[i] == '\r' && i+1 < len(runes) && runes[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(runes) {
		lines = append(lines, string(runes[start:]))
	}
	return lines
}

// Package pytext holds the few text rules of Python that the ported skill scripts depend on and
// that Go's standard library words differently: `str.strip`, `str.split`, `str.splitlines`,
// `html.escape`, `json.dumps` of a string and the universal newlines of `subprocess.run(text=True)`.
//
// Each function is the Python function, not an improvement on it: the artefact comments are
// compared byte for byte with what the Python script wrote.
package pytext

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// IsSpace is `str.isspace` for one character. It is unicode.IsSpace plus the four information
// separators U+001C to U+001F, which Python counts as whitespace and Go does not.
func IsSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// Strip is `s.strip()`.
func Strip(s string) string { return strings.TrimFunc(s, IsSpace) }

// Fields is `s.split()`.
func Fields(s string) []string { return strings.FieldsFunc(s, IsSpace) }

// isLineBreak is every character at which `str.splitlines` cuts, besides the `\r\n` pair.
func isLineBreak(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

// SplitLines is `s.splitlines(keepends)`: it cuts at \n, \r, \r\n, \v, \f, U+001C to U+001E,
// U+0085, U+2028 and U+2029, not only at \n. Empty input gives no lines, and a final terminator
// does not start an empty line.
func SplitLines(s string, keepends bool) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isLineBreak(r) {
			i += size
			continue
		}
		end := i + size
		if r == '\r' && end < len(s) && s[end] == '\n' {
			end++
		}
		if keepends {
			lines = append(lines, s[start:end])
		} else {
			lines = append(lines, s[start:i])
		}
		start, i = end, end
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// HTMLEscape is `html.escape(s)` with quote=True: `&#x27;` for the apostrophe and `&quot;` for the
// double quote, where Go's html.EscapeString writes `&#39;` and `&#34;`.
func HTMLEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#x27;").Replace(s)
}

// UniversalNewlines is what `subprocess.run(text=True)` does to captured output: `\r\n` and a
// lone `\r` both become `\n`.
func UniversalNewlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// JSONString is the JSON text of s as `json.dumps` writes it. With ensureASCII every character
// above U+007F becomes `\uXXXX` (a surrogate pair above U+FFFF); without it the character stays.
// Unlike encoding/json, `<`, `>`, `&`, U+2028 and U+2029 are never escaped, and DEL stays.
func JSONString(s string, ensureASCII bool) string {
	const hex = "0123456789abcdef"
	var b strings.Builder
	b.WriteByte('"')
	u := func(c rune) {
		b.WriteString(`\u`)
		for shift := 12; shift >= 0; shift -= 4 {
			b.WriteByte(hex[(c>>uint(shift))&0xf])
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r < 0x20:
			u(r)
		case r > 0x7e && ensureASCII:
			if r > 0xffff {
				r -= 0x10000
				u(0xd800 + (r >> 10))
				u(0xdc00 + (r & 0x3ff))
			} else {
				u(r)
			}
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Package mdlinks extracts markdown links the way the former Python link parser of pack/core did:
// a link in a file is relative and points to an existing target, an absolute link is always a
// problem.
//
// Python's \s, splitlines and unquote are wider or more lenient than Go's \s, bufio.Scanner and
// url.PathUnescape; this package spells those out so both sides give one verdict.
package mdlinks

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ws is Python's str \s: ASCII whitespace plus \x1c-\x1f and the Unicode spaces.
const ws = ` \t\n\v\f\r\x1c-\x1f\x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}`

var (
	fenceRe = regexp.MustCompile("^[" + ws + "]*(```|~~~)")
	inline  = regexp.MustCompile("`[^`\n]*`")
	linkRe  = regexp.MustCompile(
		`\[[^\]]*\]\(` +
			`(?:<(?P<angle>[^<>\n]*)>|(?P<bare>[^)<` + ws + `][^)` + ws + `]*))` +
			`(?:[` + ws + `]+(?:"[^"]*"|'[^']*'|\([^)]*\)))?` +
			`[` + ws + `]*\)`)
	schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
	angleIdx = linkRe.SubexpIndex("angle")
	bareIdx  = linkRe.SubexpIndex("bare")
)

// Problem is one finding: Kind is "dead" or "absolute".
type Problem struct{ Kind, Target string }

// splitLines splits like Python's str.splitlines: on \n, \r\n, \r, \v, \f, \x1c-\x1e, \x85,
// U+2028 and U+2029, without the terminators and without a trailing empty line.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch r {
		case '\n', '\v', '\f', '\x1c', '\x1d', '\x1e', '\u0085', ' ', ' ':
		case '\r':
			lines = append(lines, s[start:i])
			if i+1 < len(s) && s[i+1] == '\n' {
				size++
			}
			i += size
			start = i
			continue
		default:
			i += size
			continue
		}
		lines = append(lines, s[start:i])
		i += size
		start = i
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// StripCode removes fenced blocks (three backticks or tildes, toggling per line) and inline code.
func StripCode(text string) string {
	var out []string
	inside := false
	for _, line := range splitLines(text) {
		if fenceRe.MatchString(line) {
			inside = !inside
			continue
		}
		if !inside {
			out = append(out, inline.ReplaceAllString(line, ""))
		}
	}
	return strings.Join(out, "\n")
}

func targets(text string) []string {
	stripped := StripCode(text)
	var out []string
	for _, m := range linkRe.FindAllStringSubmatchIndex(stripped, -1) {
		// An empty angle destination (<>) matches with start == end, a bare one has start -1.
		if m[2*angleIdx] >= 0 {
			out = append(out, stripped[m[2*angleIdx]:m[2*angleIdx+1]])
		} else {
			out = append(out, stripped[m[2*bareIdx]:m[2*bareIdx+1]])
		}
	}
	return out
}

// RelativeLinks returns the targets of relative links outside code, anchor cut off,
// deduplicated in order. Anchors, absolute targets and scheme targets are left out.
func RelativeLinks(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range targets(text) {
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "/") || schemeRe.MatchString(t) {
			continue
		}
		t, _, _ = strings.Cut(t, "#")
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// AbsoluteLinks returns the targets that begin with "/", outside code, duplicates kept.
func AbsoluteLinks(text string) []string {
	var out []string
	for _, t := range targets(text) {
		if strings.HasPrefix(t, "/") {
			out = append(out, t)
		}
	}
	return out
}

// unquote decodes %XX like Python's urllib.parse.unquote: a malformed % stays as it is and
// bytes that are not UTF-8 become U+FFFD.
func unquote(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	var pending []byte
	flush := func() {
		for len(pending) > 0 {
			r, size := utf8.DecodeRune(pending)
			b.WriteRune(r) // RuneError for an invalid byte, written as U+FFFD
			pending = pending[size:]
		}
	}
	for i := 0; i < len(s); {
		if s[i] == '%' && i+2 < len(s) && unhex(s[i+1]) >= 0 && unhex(s[i+2]) >= 0 {
			pending = append(pending, byte(unhex(s[i+1])<<4|unhex(s[i+2])))
			i += 3
			continue
		}
		flush()
		b.WriteByte(s[i])
		i++
	}
	flush()
	return b.String()
}

func unhex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// FindProblems returns the dead relative links (in order) and then every absolute link of
// mdPath. A relative target that resolves outside root is skipped. A read error goes back to
// the caller.
func FindProblems(mdPath, root string) ([]Problem, error) {
	raw, err := os.ReadFile(mdPath)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	absDir, err := filepath.Abs(filepath.Dir(mdPath))
	if err != nil {
		return nil, err
	}
	var problems []Problem
	for _, t := range RelativeLinks(text) {
		dec := unquote(t)
		resolved := filepath.Clean(filepath.Join(absDir, dec))
		if filepath.IsAbs(dec) {
			resolved = filepath.Clean(dec)
		}
		rel, err := filepath.Rel(absRoot, resolved)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		if _, err := os.Stat(resolved); err != nil {
			problems = append(problems, Problem{"dead", t})
		}
	}
	for _, t := range AbsoluteLinks(text) {
		problems = append(problems, Problem{"absolute", t})
	}
	return problems, nil
}

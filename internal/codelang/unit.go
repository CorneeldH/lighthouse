package codelang

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// marker on a line exempts that line from the check.
const marker = "lighthouse: allow-language"

// ident is an identifier: an ASCII start, then Unicode letters, digits and "_".
const (
	ident     = `[A-Za-z_][\p{L}\p{N}_]*`
	identList = ident + `(?:[ \t]*,[ \t]*` + ident + `)*`
	// pySpace and pyNotSpace are Python's \s and \S: RE2's are ASCII only.
	pySpace = `\p{Z}\t\n\v\f\r\x1c-\x1f\x85`
)

var identRE = regexp.MustCompile(ident)

// Unit is one piece of text or one name the scanners hand to the judge. Kind is "comment",
// "docstring", "prose", "name" or "directory". Text is the name or the first line, Body the
// whole unit for JudgeText and layer 2 (empty for a name). Name selects JudgeName.
type Unit struct {
	Line       int
	Kind, Text string
	Body       string
	Name       bool
	LineBreaks bool
}

// Finding is a unit that layer 1 did not find clean. Body is what a later layer gets to see
// (empty for a name; the caller falls back to Text).
type Finding struct {
	Line                    int
	Kind, Text, Body, Layer string
	Doubt                   bool
}

// numbered is one line of text with its 1-based line number.
type numbered struct {
	line int
	text string
}

// lineOf is the 1-based line of a byte offset; only "\n" counts.
func lineOf(text string, offset int) int {
	return strings.Count(text[:offset], "\n") + 1
}

var wordRuneRE = regexp.MustCompile(`^[\p{L}\p{N}_]$`)

// isWordRune is Python's \w.
func isWordRune(r rune) bool { return wordRuneRE.MatchString(string(r)) }

// commentUnits makes one unit per comment block; every line is a sentence boundary there.
// Marked lines stay out of the body, but the first line names the block.
func commentUnits(blocks [][]numbered) []Unit {
	var units []Unit
	for _, block := range blocks {
		var kept []string
		for _, l := range block {
			if !strings.Contains(l.text, marker) {
				kept = append(kept, l.text)
			}
		}
		units = append(units, Unit{
			Line: block[0].line, Kind: "comment", Text: block[0].text,
			Body: strings.Join(kept, "\n"), LineBreaks: true,
		})
	}
	return units
}

// nameUnits makes one unit per name per line, none on a marked line.
func nameUnits(names []numbered, source string) []Unit {
	lines := splitLines(source)
	seen := map[numbered]bool{}
	var units []Unit
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		if n.line > 0 && n.line <= len(lines) && strings.Contains(lines[n.line-1], marker) {
			continue
		}
		units = append(units, Unit{Line: n.line, Kind: "name", Text: n.text, Name: true})
	}
	return units
}

// judge turns units into findings and doubts, stable by line.
func judge(s *Scorer, units []Unit) []Finding {
	found := []Finding{}
	for _, u := range units {
		var verdict Verdict
		if u.Name {
			verdict = s.JudgeName(u.Text)
		} else {
			verdict = s.JudgeText(u.Body, u.LineBreaks)
		}
		if verdict == VerdictClean {
			continue
		}
		found = append(found, Finding{
			Line: u.Line, Kind: u.Kind, Text: u.Text, Body: u.Body,
			Layer: "layer 1", Doubt: verdict == VerdictDoubt,
		})
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Line < found[j].Line })
	return found
}

// Scan judges the comments, prose and names of src; language is "python", "shell", "go" or
// "markdown".
func Scan(s *Scorer, language, src string) ([]Finding, error) {
	switch language {
	case "shell":
		return judge(s, scanShell(src)), nil
	case "go":
		return judge(s, scanGo(src)), nil
	case "markdown":
		return judge(s, scanMarkdown(src)), nil
	case "python":
		return nil, fmt.Errorf("python scanner not yet implemented")
	}
	return nil, fmt.Errorf("unknown language %q", language)
}

// runeAt is the rune that starts at byte offset i of s; false at the end.
func runeAt(s string, i int) (rune, bool) {
	if i >= len(s) {
		return 0, false
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return r, true
}

// runeBefore is the rune that ends at byte offset i of s; false at the start.
func runeBefore(s string, i int) (rune, bool) {
	if i <= 0 {
		return 0, false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return r, true
}

// findAllChecked is FindAllStringSubmatchIndex for a pattern that had a lookbehind or a \b:
// ok sees the text and the match, and a refused match restarts the search one rune later, as
// the regex engine would. The pattern must not use ^.
func findAllChecked(re *regexp.Regexp, s string, ok func(s string, m []int) bool) [][]int {
	var found [][]int
	for pos := 0; pos <= len(s); {
		m := re.FindStringSubmatchIndex(s[pos:])
		if m == nil {
			break
		}
		for i := range m {
			if m[i] >= 0 {
				m[i] += pos
			}
		}
		if !ok(s, m) {
			_, width := utf8.DecodeRuneInString(s[m[0]:])
			pos = m[0] + max(width, 1)
			continue
		}
		found = append(found, m)
		pos = m[1]
		if m[1] == m[0] {
			pos++
		}
	}
	return found
}

// notAfterWordRune is the check of a leading \b on a pattern that starts with a letter.
func notAfterWordRune(s string, m []int) bool {
	r, ok := runeBefore(s, m[0])
	return !ok || !isWordRune(r)
}

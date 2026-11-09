// Package gitinvocations finds `git commit` and `git push` inside an arbitrary shell command.
//
// It is the Go twin of pack/core/lib/git_invocations.py, which stays frozen. The two must say
// the same thing: tests/pack/fixtures/git_invocations/cases.json is read by both.
package gitinvocations

import (
	"errors"
	"strings"
	"unicode"
)

// Unresolved stands for a directory that cannot be traced.
const Unresolved = "<unknown>"

// NewDir stands for a directory that does not exist yet.
const NewDir = "<new>"

// ErrUnparseable means an open quote: no conclusion can be drawn about this command.
var ErrUnparseable = errors.New("unparseable: open quote")

type segKind int

const (
	segCommand segKind = iota
	segSubEnter
	segSubLeave
	segAndSep
)

// word is a word of a segment with how it came to be: literal means no expansion can change it,
// tilde means it began with an unquoted `~`.
type word struct {
	text    string
	literal bool
	tilde   bool
}

// segment is a command segment, or one of the markers around sub-shells and `&&`.
type segment struct {
	kind  segKind
	words []word
}

// pyIsSpace is Python's str.isspace for the characters that matter here.
func pyIsSpace(c rune) bool {
	return unicode.IsSpace(c) || (c >= 0x1c && c <= 0x1f)
}

func hasPrefixAt(text []rune, i int, prefix string) bool {
	p := []rune(prefix)
	if i < 0 || i+len(p) > len(text) {
		return false
	}
	for k, r := range p {
		if text[i+k] != r {
			return false
		}
	}
	return true
}

func indexRune(text []rune, from int, r rune) int {
	for k := from; k < len(text); k++ {
		if text[k] == r {
			return k
		}
	}
	return -1
}

// readDelimiter reads the heredoc delimiter from position j.
func readDelimiter(line []rune, j int) (string, int) {
	n := len(line)
	var out []rune
	for j < n {
		c := line[j]
		if c == '\\' {
			j++
			if j < n {
				out = append(out, line[j])
				j++
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote := c
			j++
			for j < n && line[j] != quote {
				out = append(out, line[j])
				j++
			}
			if j < n {
				j++
			}
			continue
		}
		if pyIsSpace(c) || strings.ContainsRune(";|&<>()", c) {
			break
		}
		out = append(out, c)
		j++
	}
	return string(out), j
}

// skipAnsiC reads a `$'...'` string from j, the position after the opening quote. In ANSI-C
// quoting a backslash escapes the next character, an apostrophe too.
func skipAnsiC(text []rune, j int) (body []rune, next int, closed bool) {
	n := len(text)
	for j < n {
		if text[j] == '\\' && j+1 < n {
			body = append(body, text[j+1])
			j += 2
			continue
		}
		if text[j] == '\'' {
			return body, j + 1, true
		}
		body = append(body, text[j])
		j++
	}
	return body, j, false
}

// quotedStep is one step inside `'...'` or `"..."`.
func quotedStep(line []rune, i int, sq, dq bool) (int, bool, bool) {
	c := line[i]
	if sq {
		if c == '\'' {
			sq = false
		}
		return i + 1, sq, dq
	}
	if c == '\\' && i+1 < len(line) {
		return i + 2, sq, dq
	}
	if c == '"' {
		dq = false
	}
	return i + 1, sq, dq
}

// heredocOperator reads a `<<` from i. The delimiter is "" when there is none.
func heredocOperator(line []rune, i, arith int) (next int, wordStart bool, delimiter string) {
	if hasPrefixAt(line, i, "<<<") {
		return i + 3, true, ""
	}
	if arith > 0 {
		// `$((1 << 2))` is a bit shift, not a heredoc.
		return i + 2, false, ""
	}
	n := len(line)
	j := i + 2
	if j < n && line[j] == '-' {
		j++
	}
	for j < n && (line[j] == ' ' || line[j] == '\t') {
		j++
	}
	delimiter, j = readDelimiter(line, j)
	return j, true, delimiter
}

// unquotedStep is one step outside quotes.
func unquotedStep(line []rune, i, arith int) (next, newArith int, sq, dq, wordStart bool) {
	c := line[i]
	switch {
	case c == '\\':
		return i + 2, arith, false, false, false
	case c == '\'':
		return i + 1, arith, true, false, false
	case c == '"':
		return i + 1, arith, false, true, false
	case c == '$' && hasPrefixAt(line, i+1, "(("):
		return i + 3, arith + 1, false, false, false
	case c == '$' && hasPrefixAt(line, i+1, "'"):
		// ANSI-C quoting: in `$'...'` a backslash-escaped apostrophe does not close the string.
		_, end, _ := skipAnsiC(line, i+2)
		return end, arith, false, false, false
	case arith > 0 && hasPrefixAt(line, i, "))"):
		return i + 2, arith - 1, false, false, false
	}
	return i + 1, arith, false, false, strings.ContainsRune(" \t;&|()<>", c)
}

// scanLine finds the heredoc operators in redirection position and the start of a `#` comment.
// The cut point is the index where a comment begins, or len(line). Bash starts a comment only at
// a `#` at the start of a word and outside quotes.
func scanLine(line []rune, sq, dq bool) (delimiters []string, newSq, newDq bool, cut int) {
	arith := 0
	wordStart := !sq && !dq
	i, n := 0, len(line)
	for i < n {
		if sq || dq {
			i, sq, dq = quotedStep(line, i, sq, dq)
			continue
		}
		if line[i] == '#' && wordStart {
			return delimiters, sq, dq, i
		}
		if hasPrefixAt(line, i, "<<") {
			var delimiter string
			i, wordStart, delimiter = heredocOperator(line, i, arith)
			if delimiter != "" {
				delimiters = append(delimiters, delimiter)
			}
			continue
		}
		i, arith, sq, dq, wordStart = unquotedStep(line, i, arith)
	}
	return delimiters, sq, dq, n
}

// stripHeredocs removes heredoc bodies and `#` comments: an example command inside a heredoc is
// no command.
func stripHeredocs(command string) string {
	var kept []string
	var queue []string
	sq, dq := false, false
	for _, text := range strings.Split(command, "\n") {
		if len(queue) > 0 {
			if strings.TrimFunc(text, pyIsSpace) == queue[0] {
				queue = queue[1:]
			}
			continue
		}
		line := []rune(text)
		var delimiters []string
		var cut int
		delimiters, sq, dq, cut = scanLine(line, sq, dq)
		kept = append(kept, string(line[:cut]))
		queue = delimiters
	}
	return strings.Join(kept, "\n")
}

type splitter struct {
	text     []rune
	n        int
	segments []segment
	outer    []outerState
	stack    []string
	current  []word
	buf      []rune
	dirty    bool
	started  bool
	tilde    bool
}

type outerState struct {
	current []word
	buf     []rune
	dirty   bool
	started bool
	tilde   bool
}

func (s *splitter) flushWord() {
	if s.started || len(s.buf) > 0 {
		s.current = append(s.current, word{string(s.buf), !s.dirty, s.tilde})
	}
	s.buf = nil
	s.dirty = false
	s.started = false
	s.tilde = false
}

func (s *splitter) flushSegment() {
	s.flushWord()
	s.segments = append(s.segments, segment{kind: segCommand, words: s.current})
	s.current = nil
}

func (s *splitter) marker(kind segKind) {
	s.segments = append(s.segments, segment{kind: kind})
}

func (s *splitter) enterSub(kind string) {
	s.outer = append(s.outer, outerState{s.current, s.buf, s.dirty, s.started, s.tilde})
	s.current = nil
	s.buf = nil
	s.dirty = false
	s.started = false
	s.tilde = false
	s.stack = append(s.stack, kind)
	s.marker(segSubEnter)
}

func (s *splitter) leaveSub(substitution bool) {
	s.flushSegment()
	s.marker(segSubLeave)
	o := s.outer[len(s.outer)-1]
	s.outer = s.outer[:len(s.outer)-1]
	s.current = o.current
	if substitution {
		// The outcome of a substitution cannot be traced.
		s.buf = append(o.buf, '$')
		s.dirty = true
		s.started = true
		s.tilde = false
	} else {
		// `( )` is a group, not a word: the surrounding word stays whole.
		s.buf = o.buf
		s.dirty = o.dirty
		s.started = o.started
		s.tilde = o.tilde
	}
	s.stack = s.stack[:len(s.stack)-1]
}

func (s *splitter) pop() { s.stack = s.stack[:len(s.stack)-1] }

// dqChar handles one character inside `"..."`.
func (s *splitter) dqChar(i int) int {
	text, n, c := s.text, s.n, s.text[i]
	if c == '\\' && i+1 < n && strings.ContainsRune("$`\"\\\n", text[i+1]) {
		if text[i+1] != '\n' {
			s.buf = append(s.buf, text[i+1])
			s.started = true
		}
		return i + 2
	}
	if c == '"' {
		s.pop()
		return i + 1
	}
	if c == '$' && i+1 < n && text[i+1] == '(' {
		s.enterSub("sub")
		return i + 2
	}
	if c == '`' {
		s.enterSub("bq")
		return i + 1
	}
	if c == '$' {
		s.dirty = true
	}
	s.buf = append(s.buf, c)
	s.started = true
	return i + 1
}

// quoteChar handles `$'`, `'`, `"` or `\` at i. ok is false when i holds none of them.
func (s *splitter) quoteChar(i int) (next int, ok bool, err error) {
	text, n, c := s.text, s.n, s.text[i]
	switch {
	case c == '$' && i+1 < n && text[i+1] == '\'':
		body, end, closed := skipAnsiC(text, i+2)
		if !closed {
			return 0, true, ErrUnparseable
		}
		// The escapes are not expanded here (`\n` stays `n`), so the word counts as non-literal.
		s.buf = append(s.buf, body...)
		s.dirty = true
		s.started = true
		return end, true, nil
	case c == '\'':
		end := indexRune(text, i+1, '\'')
		if end < 0 {
			return 0, true, ErrUnparseable
		}
		s.buf = append(s.buf, text[i+1:end]...)
		s.started = true
		return end + 1, true, nil
	case c == '"':
		s.stack = append(s.stack, "dq")
		s.started = true
		return i + 1, true, nil
	case c == '\\':
		if i+1 < n && text[i+1] == '\n' {
			// Line continuation: the two lines are one command.
			return i + 2, true, nil
		}
		if i+1 < n {
			s.buf = append(s.buf, text[i+1])
			s.started = true
			return i + 2, true, nil
		}
		return i + 1, true, nil
	}
	return 0, false, nil
}

// subshellChar handles `$(`, `)`, a backtick or `(` at i.
func (s *splitter) subshellChar(i int, top string) (int, bool) {
	text, n, c := s.text, s.n, s.text[i]
	switch {
	case c == '$' && i+1 < n && text[i+1] == '(':
		s.enterSub("sub")
		return i + 2, true
	case c == ')' && (top == "sub" || top == "paren"):
		s.leaveSub(top == "sub")
		return i + 1, true
	case c == '`':
		if top == "bq" {
			s.leaveSub(true)
		} else {
			s.enterSub("bq")
		}
		return i + 1, true
	case c == '(':
		s.flushSegment()
		s.enterSub("paren")
		return i + 1, true
	}
	return 0, false
}

// separatorChar handles whitespace, `&&`, a segment boundary or `<>` at i.
func (s *splitter) separatorChar(i int) (int, bool) {
	text, n, c := s.text, s.n, s.text[i]
	switch {
	case c == ' ' || c == '\t' || c == '\r':
		s.flushWord()
		return i + 1, true
	case c == '&' && i+1 < n && text[i+1] == '&':
		s.flushSegment()
		s.marker(segAndSep)
		return i + 2, true
	case c == '\n' || strings.ContainsRune(";&|)", c):
		s.flushSegment()
		return i + 1, true
	case c == '<' || c == '>':
		s.flushWord()
		start := i
		for i < n && (text[i] == '<' || text[i] == '>') {
			i++
		}
		s.current = append(s.current, word{string(text[start:i]), true, false})
		return i, true
	}
	return 0, false
}

// wordChar handles an ordinary word character at i.
func (s *splitter) wordChar(i int) int {
	c := s.text[i]
	if c == '$' || c == '*' || c == '?' {
		s.dirty = true
	} else if c == '~' && !s.started && len(s.buf) == 0 {
		s.tilde = true
	}
	s.buf = append(s.buf, c)
	s.started = true
	return i + 1
}

func (s *splitter) run() ([]segment, error) {
	i := 0
	for i < s.n {
		top := ""
		if len(s.stack) > 0 {
			top = s.stack[len(s.stack)-1]
		}
		if top == "dq" {
			i = s.dqChar(i)
			continue
		}
		next, ok, err := s.quoteChar(i)
		if err != nil {
			return nil, err
		}
		if !ok {
			next, ok = s.subshellChar(i, top)
		}
		if !ok {
			next, ok = s.separatorChar(i)
		}
		if !ok {
			next = s.wordChar(i)
		}
		i = next
	}
	for len(s.stack) > 0 {
		top := s.stack[len(s.stack)-1]
		if top == "dq" {
			return nil, ErrUnparseable
		}
		s.leaveSub(top != "paren")
	}
	s.flushSegment()
	return s.segments, nil
}

// splitSegments splits into command segments. A new segment begins at a newline, `;`, `&`, `|`,
// a sub-shell `(`, a command substitution `$(` or a backtick. Redirections only split the word.
// The content of a substitution is a segment of its own, and the surrounding word is marked
// non-literal. Markers go around every sub-shell, so that its `cd` does not linger.
func splitSegments(text string) ([]segment, error) {
	r := []rune(text)
	return (&splitter{text: r, n: len(r)}).run()
}

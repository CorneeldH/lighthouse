package codelang

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// pyKind is the kind of a Python token. Keywords are names.
type pyKind int

const (
	pyName pyKind = iota
	pyOp
	pyStr  // a string literal without an f prefix, prefix and quotes included
	pyFstr // the start of an f-string; its replacement fields follow as tokens, braces as ops
	pyComment
	pyNewline // the end of a logical line
	pyNL      // a line break inside a logical line, or after a blank or comment line
	pyIndent
	pyDedent
	pyNumber
	pyEOF
)

// pyToken is one token. depth is the number of brackets open before it; a closer has the
// depth of its opener. The braces of an f-string replacement field count as brackets.
type pyToken struct {
	kind          pyKind
	text          string
	line, endLine int
	depth         int
}

// pyOps are the operators, longest first per start so the first match is the longest.
var pyOps = []string{
	"**=", "//=", ">>=", "<<=", "...",
	"**", "//", ">>", "<<", "<=", ">=", "==", "!=", "->", ":=",
	"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "@=",
}

// fFrame is one level of f-string nesting: the literal text of an f-string, a replacement
// field (code), or the format spec after its ":".
type fFrame struct {
	kind  fFrameKind
	quote string // fLiteral: the closing quote
	raw   bool   // fLiteral: an r prefix
	level int    // fField: the bracket depth with its own brace open
}

type fFrameKind int

const (
	fLiteral fFrameKind = iota
	fField
	fSpec
)

type pyLexer struct {
	src         string
	pos, line   int
	tokens      []pyToken
	brackets    []byte // '(', '[', '{', or 'f' for the brace of a replacement field
	frames      []fFrame
	indents     []int
	atLineStart bool
	lineHasCode bool // a token other than a comment since the last NEWLINE
}

// pyLex tokenizes Python 3.12 source as far as the scanner needs it. A source the tokenizer
// of Python refuses is an error; a grammar error is not (there is no parser).
func pyLex(src string) ([]pyToken, error) {
	if strings.IndexByte(src, 0) >= 0 {
		return nil, errors.New("source code cannot contain null bytes")
	}
	l := &pyLexer{src: src, line: 1, indents: []int{0}, atLineStart: true}
	for {
		if n := len(l.frames); n > 0 && l.frames[n-1].kind != fField {
			if err := l.fstringText(); err != nil {
				return nil, err
			}
			continue
		}
		if l.pos >= len(l.src) {
			break
		}
		if err := l.next(); err != nil {
			return nil, err
		}
	}
	if len(l.brackets) > 0 {
		return nil, errors.New("unexpected EOF in multi-line statement")
	}
	if l.lineHasCode {
		l.emit(pyNewline, "", l.line)
	}
	for range l.indents[1:] {
		l.emit(pyDedent, "", l.line)
	}
	l.emit(pyEOF, "", l.line)
	return l.tokens, nil
}

func (l *pyLexer) emit(kind pyKind, text string, line int) {
	l.tokens = append(l.tokens, pyToken{kind: kind, text: text, line: line, endLine: l.line, depth: len(l.brackets)})
	if kind != pyComment && kind != pyNL && kind != pyNewline {
		l.lineHasCode = true
	}
}

// inField reports whether the lexer reads the top level of a replacement field.
func (l *pyLexer) inField() bool {
	n := len(l.frames)
	return n > 0 && l.frames[n-1].kind == fField && l.frames[n-1].level == len(l.brackets)
}

// next reads one token, or the indentation at the start of a line, in code.
func (l *pyLexer) next() error {
	if l.atLineStart {
		l.atLineStart = false
		if len(l.brackets) == 0 {
			if err := l.indentation(); err != nil {
				return err
			}
			if l.pos >= len(l.src) {
				return nil
			}
		}
	}
	c := l.src[l.pos]
	switch {
	case c == ' ' || c == '\t' || c == '\f' || c == '\r':
		l.pos++
	case c == '\n':
		l.newline()
	case c == '#':
		end := strings.IndexByte(l.src[l.pos:], '\n')
		if end < 0 {
			end = len(l.src) - l.pos
		}
		l.emit(pyComment, strings.TrimSuffix(l.src[l.pos:l.pos+end], "\r"), l.line)
		l.pos += end
	case c == '\\':
		return l.continuation()
	case c == '\'' || c == '"':
		return l.str("")
	case c >= '0' && c <= '9' || c == '.' && l.pos+1 < len(l.src) && l.src[l.pos+1] >= '0' && l.src[l.pos+1] <= '9':
		l.number()
	default:
		r, width := utf8.DecodeRuneInString(l.src[l.pos:])
		if isPyIdentStart(r) {
			return l.name()
		}
		if l.inField() {
			if done := l.fieldEnd(); done {
				return nil
			}
		}
		if strings.ContainsRune("([{", r) {
			l.emit(pyOp, string(r), l.line)
			l.brackets = append(l.brackets, byte(r))
			l.pos++
			return nil
		}
		if strings.ContainsRune(")]}", r) {
			return l.closer(byte(r))
		}
		for _, op := range pyOps {
			if strings.HasPrefix(l.src[l.pos:], op) {
				l.emit(pyOp, op, l.line)
				l.pos += len(op)
				return nil
			}
		}
		l.emit(pyOp, string(r), l.line)
		l.pos += width
	}
	return nil
}

// newline ends a physical line: NEWLINE after code outside brackets, NL otherwise.
func (l *pyLexer) newline() {
	if len(l.brackets) == 0 && l.lineHasCode {
		l.emit(pyNewline, "\n", l.line)
		l.lineHasCode = false
	} else {
		l.emit(pyNL, "\n", l.line)
	}
	l.pos++
	l.line++
	l.atLineStart = true
}

// continuation joins the next line to this one after a backslash.
func (l *pyLexer) continuation() error {
	rest := l.src[l.pos+1:]
	switch {
	case strings.HasPrefix(rest, "\n"):
		l.pos += 2
	case strings.HasPrefix(rest, "\r\n"):
		l.pos += 3
	case rest == "":
		return errors.New("unexpected EOF while parsing")
	default:
		return errors.New("unexpected character after line continuation character")
	}
	l.line++
	if l.pos >= len(l.src) {
		return errors.New("unexpected EOF while parsing")
	}
	return nil
}

// indentation measures the indentation of a line outside brackets and emits INDENT or
// DEDENT. A blank or comment-only line changes nothing. Columns follow Python: a tab moves to
// the next multiple of 8, a form feed resets to 0.
func (l *pyLexer) indentation() error {
	col := 0
	for ; l.pos < len(l.src); l.pos++ {
		switch l.src[l.pos] {
		case ' ':
			col++
		case '\t':
			col = (col/8 + 1) * 8
		case '\f':
			col = 0
		default:
			goto measured
		}
	}
measured:
	if l.pos >= len(l.src) {
		return nil
	}
	if c := l.src[l.pos]; c == '#' || c == '\n' || c == '\r' || c == '\\' {
		return nil
	}
	top := l.indents[len(l.indents)-1]
	switch {
	case col > top:
		l.indents = append(l.indents, col)
		l.emit(pyIndent, "", l.line)
	case col < top:
		for col < l.indents[len(l.indents)-1] {
			l.indents = l.indents[:len(l.indents)-1]
			l.emit(pyDedent, "", l.line)
		}
		if col != l.indents[len(l.indents)-1] {
			return errors.New("unindent does not match any outer indentation level")
		}
	}
	return nil
}

// isPyIdentStart is an approximation of XID_Start plus "_".
func isPyIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.Is(unicode.Nl, r)
}

// isPyIdentPart is an approximation of XID_Continue.
func isPyIdentPart(r rune) bool {
	return isPyIdentStart(r) || unicode.IsDigit(r) || unicode.In(r, unicode.Mn, unicode.Mc, unicode.Pc, unicode.Nd)
}

// name reads an identifier, or a string when the identifier is a string prefix.
func (l *pyLexer) name() error {
	start := l.pos
	for l.pos < len(l.src) {
		r, width := utf8.DecodeRuneInString(l.src[l.pos:])
		if !isPyIdentPart(r) {
			break
		}
		l.pos += width
	}
	word := l.src[start:l.pos]
	if l.pos < len(l.src) && (l.src[l.pos] == '\'' || l.src[l.pos] == '"') && isStringPrefix(word) {
		return l.str(word)
	}
	l.emit(pyName, word, l.line)
	return nil
}

func isStringPrefix(word string) bool {
	switch strings.ToLower(word) {
	case "r", "u", "b", "f", "rb", "br", "fr", "rf":
		return true
	}
	return false
}

// number reads a number literal. Like CPython's tokenizer it stops before letters that are
// no part of it, so "1for" is a number and the keyword "for".
func (l *pyLexer) number() {
	start := l.pos
	digits := func(set string) {
		for l.pos < len(l.src) && strings.IndexByte(set, l.src[l.pos]) >= 0 {
			l.pos++
		}
	}
	const decimal = "0123456789_"
	if rest := l.src[l.pos:]; len(rest) > 1 && rest[0] == '0' && strings.IndexByte("xXoObB", rest[1]) >= 0 {
		l.pos += 2
		digits(decimal + "abcdefABCDEF") // the hex set; the octal and binary digits are in it
		l.emit(pyNumber, l.src[start:l.pos], l.line)
		return
	}
	digits(decimal)
	if l.pos < len(l.src) && l.src[l.pos] == '.' {
		l.pos++
		digits(decimal)
	}
	if rest := l.src[l.pos:]; len(rest) > 1 && (rest[0] == 'e' || rest[0] == 'E') {
		exponent := rest[1:]
		if exponent[0] == '+' || exponent[0] == '-' {
			exponent = exponent[1:]
		}
		if exponent != "" && exponent[0] >= '0' && exponent[0] <= '9' {
			l.pos += len(rest) - len(exponent)
			digits(decimal)
		}
	}
	if l.pos < len(l.src) && (l.src[l.pos] == 'j' || l.src[l.pos] == 'J') {
		l.pos++
	}
	l.emit(pyNumber, l.src[start:l.pos], l.line)
}

// closer pops a bracket; at the top level of a replacement field "}" is handled before.
func (l *pyLexer) closer(c byte) error {
	if len(l.brackets) == 0 {
		return fmt.Errorf("unmatched '%c'", c)
	}
	open := l.brackets[len(l.brackets)-1]
	if open == 'f' {
		open = '{'
	}
	if pairs := map[byte]byte{')': '(', ']': '[', '}': '{'}; pairs[c] != open {
		return fmt.Errorf("closing parenthesis '%c' does not match opening parenthesis '%c'", c, open)
	}
	l.brackets = l.brackets[:len(l.brackets)-1]
	l.emit(pyOp, string(c), l.line)
	l.pos++
	return nil
}

// fieldEnd handles "}", ":" and "!" at the top level of a replacement field; it reports
// whether it consumed something.
func (l *pyLexer) fieldEnd() bool {
	switch c := l.src[l.pos]; {
	case c == '}':
		l.brackets = l.brackets[:len(l.brackets)-1]
		l.frames = l.frames[:len(l.frames)-1]
		l.emit(pyOp, "}", l.line)
		l.pos++
		return true
	case c == ':':
		l.frames = append(l.frames, fFrame{kind: fSpec})
		l.pos++
		return true
	case c == '!' && !strings.HasPrefix(l.src[l.pos:], "!="):
		l.pos++
		for l.pos < len(l.src) {
			r, width := utf8.DecodeRuneInString(l.src[l.pos:])
			if !isPyIdentPart(r) {
				break
			}
			l.pos += width
		}
		return true
	}
	return false
}

// str reads a string literal that starts at l.pos with its quote; prefix is already read.
// An f-string only opens here: its text is read by fstringText.
func (l *pyLexer) str(prefix string) error {
	start, startLine := l.pos-len(prefix), l.line
	quote := l.src[l.pos : l.pos+1]
	if strings.HasPrefix(l.src[l.pos:], strings.Repeat(quote, 3)) {
		quote = strings.Repeat(quote, 3)
	}
	lower := strings.ToLower(prefix)
	raw := strings.Contains(lower, "r")
	l.pos += len(quote)
	if strings.Contains(lower, "f") {
		l.emit(pyFstr, l.src[start:l.pos], startLine)
		l.frames = append(l.frames, fFrame{kind: fLiteral, quote: quote, raw: raw})
		return nil
	}
	for {
		if l.pos >= len(l.src) {
			return unterminated(quote, "string", startLine)
		}
		switch c := l.src[l.pos]; {
		case c == '\\':
			l.skipEscape()
		case c == '\n':
			if len(quote) == 1 {
				return unterminated(quote, "string", startLine)
			}
			l.line++
			l.pos++
		case strings.HasPrefix(l.src[l.pos:], quote):
			l.pos += len(quote)
			l.tokens = append(l.tokens, pyToken{
				kind: pyStr, text: l.src[start:l.pos], line: startLine, endLine: l.line, depth: len(l.brackets),
			})
			l.lineHasCode = true
			return nil
		default:
			l.pos++
		}
	}
}

// skipEscape steps over a backslash and the character after it; a line break counts.
func (l *pyLexer) skipEscape() {
	l.pos++
	switch {
	case strings.HasPrefix(l.src[l.pos:], "\r\n"):
		l.pos += 2
		l.line++
	case strings.HasPrefix(l.src[l.pos:], "\n"):
		l.pos++
		l.line++
	case l.pos < len(l.src):
		_, width := utf8.DecodeRuneInString(l.src[l.pos:])
		l.pos += width
	}
}

func unterminated(quote, what string, line int) error {
	if len(quote) == 3 {
		return fmt.Errorf("unterminated triple-quoted %s literal (detected at line %d)", what, line)
	}
	return fmt.Errorf("unterminated %s literal (detected at line %d)", what, line)
}

// fstringText reads the literal text of an f-string, or a format spec, up to the next
// replacement field or the end of the string or spec.
func (l *pyLexer) fstringText() error {
	top := len(l.frames) - 1
	// a spec sits above its field, which may sit in another spec: the quote and the r prefix
	// are those of the nearest f-string below
	literal := l.frames[top]
	for i := top; literal.kind != fLiteral; i-- {
		literal = l.frames[i-1]
	}
	for {
		if l.pos >= len(l.src) {
			return unterminated(literal.quote, "f-string", l.line)
		}
		rest := l.src[l.pos:]
		switch c := rest[0]; {
		case strings.HasPrefix(rest, literal.quote):
			if l.frames[top].kind == fSpec {
				return unterminated(literal.quote, "f-string", l.line)
			}
			l.pos += len(literal.quote)
			l.frames = l.frames[:top]
			return nil
		case c == '\\':
			switch {
			case !literal.raw && strings.HasPrefix(rest, `\N{`):
				end := strings.IndexByte(rest, '}')
				if end < 0 {
					end = len(rest) - 1
				}
				l.pos += end + 1
			case strings.HasPrefix(rest, `\{`) || strings.HasPrefix(rest, `\}`):
				l.pos++
			default:
				l.skipEscape()
			}
		case c == '\n':
			if len(literal.quote) == 1 {
				return unterminated(literal.quote, "f-string", l.line)
			}
			l.line++
			l.pos++
		case c == '{' && strings.HasPrefix(rest, "{{") && l.frames[top].kind == fLiteral:
			l.pos += 2
		case c == '{':
			l.emit(pyOp, "{", l.line)
			l.brackets = append(l.brackets, 'f')
			l.frames = append(l.frames, fFrame{kind: fField, level: len(l.brackets)})
			l.pos++
			return nil
		case c == '}' && l.frames[top].kind == fSpec:
			l.frames = l.frames[:top-1]
			l.brackets = l.brackets[:len(l.brackets)-1]
			l.emit(pyOp, "}", l.line)
			l.pos++
			return nil
		case c == '}' && strings.HasPrefix(rest, "}}"):
			l.pos += 2
		default:
			l.pos++
		}
	}
}

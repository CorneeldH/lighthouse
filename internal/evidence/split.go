// Package evidence is the Go twin of pack/core/lib/evidence.py: the allowlist of an Evidence
// row (the Command cell of `### Evidence #N`), the runner with its output cap and deadline, and
// `run` over a whole PR body. A PR body is untrusted text: what falls outside the allowlist never
// runs, and nothing runs through a shell.
package evidence

import (
	"strings"
	"unicode"
)

// punctuation are the characters shlex groups into operator tokens (punctuation_chars=True).
const punctuation = "();<>|&"

// shlexWhitespace is shlex's whitespace: not Python's str.isspace.
const shlexWhitespace = " \t\r\n"

// shlexError is a ValueError of shlex; its text is Python's, capital letter included.
type shlexError string

func (e shlexError) Error() string { return string(e) }

// The two errors of shlex.
const (
	errNoClosingQuote = shlexError("No closing quotation")
	errNoEscapedChar  = shlexError("No escaped character")
)

// pyIsSpace is Python's str.isspace for one character.
func pyIsSpace(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) }

// pyStrip is Python's str.strip() without arguments.
func pyStrip(s string) string { return strings.TrimFunc(s, pyIsSpace) }

// The lexer states of shlex; a quote or the escape character is a state of its own.
const (
	stateNone   = 0
	stateSpace  = ' '
	stateWord   = 'a'
	statePunct  = 'c'
	stateEscape = '\\'
)

// lexer is shlex.shlex(s, posix=True, punctuation_chars=True) with whitespace_split=True and
// commenters="". The state carries over from one token to the next, as in shlex.
type lexer struct {
	runes []rune
	pos   int
	state rune
}

// next is the next character, or false at the end of the input.
func (l *lexer) next() (rune, bool) {
	if l.pos >= len(l.runes) {
		return 0, false
	}
	l.pos++
	return l.runes[l.pos-1], true
}

func isQuote(r rune) bool { return r == '\'' || r == '"' }

func isPunct(r rune) bool { return strings.ContainsRune(punctuation, r) }

func isShlexSpace(r rune) bool { return strings.ContainsRune(shlexWhitespace, r) }

// token is shlex's read_token: (token, false) at the end of the input. A quoted empty word
// (`”`) is a token; an empty unquoted one is not.
func (l *lexer) token() (string, bool, error) {
	quoted := false
	escaped := rune(stateSpace)
	var tok []rune
	emit := func() bool { return len(tok) > 0 || quoted }
loop:
	for {
		c, ok := l.next()
		switch l.state {
		case stateNone:
			tok = tok[:0]
			break loop
		case stateSpace:
			switch {
			case !ok:
				l.state = stateNone
				break loop
			case isShlexSpace(c):
				if emit() {
					break loop
				}
			case c == '\\':
				escaped, l.state = stateWord, stateEscape
			case isPunct(c):
				tok, l.state = []rune{c}, statePunct
			case isQuote(c):
				l.state = c
			default:
				tok, l.state = []rune{c}, stateWord
			}
		case '\'', '"':
			quoted = true
			switch {
			case !ok:
				return "", false, errNoClosingQuote
			case c == l.state:
				l.state = stateWord
			case c == '\\' && l.state == '"':
				escaped, l.state = l.state, stateEscape
			default:
				tok = append(tok, c)
			}
		case stateEscape:
			if !ok {
				return "", false, errNoEscapedChar
			}
			// In posix shells, only the quote itself or the escape character may be escaped
			// within quotes.
			if isQuote(escaped) && c != '\\' && c != escaped {
				tok = append(tok, '\\')
			}
			tok = append(tok, c)
			l.state = escaped
		default: // stateWord, statePunct
			switch {
			case !ok:
				l.state = stateNone
				break loop
			case isShlexSpace(c):
				l.state = stateSpace
				if emit() {
					break loop
				}
			case l.state == statePunct:
				if isPunct(c) {
					tok = append(tok, c)
					continue
				}
				l.pos-- // read again in the space state
				l.state = stateSpace
				break loop
			case isQuote(c):
				l.state = c
			case c == '\\':
				escaped, l.state = stateWord, stateEscape
			case !isPunct(c):
				tok = append(tok, c)
			default:
				l.pos--
				l.state = stateSpace
				if emit() {
					break loop
				}
			}
		}
	}
	if !quoted && len(tok) == 0 {
		return "", false, nil
	}
	return string(tok), true, nil
}

// splitPosix is list(shlex) over s, with the settings of lexer.
func splitPosix(s string) ([]string, error) {
	l := &lexer{runes: []rune(s), state: stateSpace}
	var out []string
	for {
		tok, ok, err := l.token()
		if err != nil {
			return nil, err
		}
		if !ok {
			return out, nil
		}
		out = append(out, tok)
	}
}

// onlyPunctuation is a non-empty token of operator characters only.
func onlyPunctuation(tok string) bool {
	return tok != "" && strings.Trim(tok, punctuation) == ""
}

// stripBackticks drops one pair of enclosing backticks after the surrounding whitespace.
func stripBackticks(cell string) string {
	s := pyStrip(cell)
	if len(s) >= 2 && strings.HasPrefix(s, "`") && strings.HasSuffix(s, "`") {
		s = s[1 : len(s)-1]
	}
	return s
}

// SplitCommand is the Command cell as argv, or ("", reason) is not; the reason says why it is
// not a bare argv.
func SplitCommand(cell string) ([]string, string) {
	s := strings.ReplaceAll(stripBackticks(cell), `\|`, "|")
	if strings.Contains(s, "$(") {
		return nil, "`$(` (command substitution) is not allowed"
	}
	if strings.Contains(s, "`") {
		return nil, "a backtick in the command is not allowed"
	}
	argv, err := splitPosix(s)
	if err != nil {
		return nil, "the command cannot be split (" + err.Error() + ")"
	}
	for _, tok := range argv {
		if onlyPunctuation(tok) {
			return nil, "shell operator `" + tok + "` is not allowed"
		}
	}
	if len(argv) == 0 {
		return nil, "empty command"
	}
	return argv, ""
}

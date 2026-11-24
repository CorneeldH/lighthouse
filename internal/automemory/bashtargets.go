package automemory

import (
	"path"
	"regexp"
	"strings"
)

type tokenKind int

const (
	tokWord tokenKind = iota
	tokSep
	tokRedirect
)

type token struct {
	kind tokenKind
	text string
}

var assignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// tokenize splits a shell command into words, separators and redirect
// operators. Quotes are removed; their content stays one word.
func tokenize(s string) []token {
	var (
		toks   []token
		buf    strings.Builder
		have   bool // buf holds a word, possibly empty because of quotes
		quoted bool // part of the current word was quoted
	)
	flush := func() {
		if have {
			toks = append(toks, token{tokWord, buf.String()})
		}
		buf.Reset()
		have, quoted = false, false
	}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch c {
		case '\'', '"':
			have, quoted = true, true
			j := i + 1
			for j < len(rs) && rs[j] != c {
				buf.WriteRune(rs[j])
				j++
			}
			i = j
		case ' ', '\t', '\r':
			flush()
		case '\n', ';', '|':
			flush()
			if c == '|' && i+1 < len(rs) && rs[i+1] == '|' {
				i++
			}
			toks = append(toks, token{tokSep, string(c)})
		case '&':
			if !have && i+1 < len(rs) && rs[i+1] == '>' {
				op := "&>"
				i++
				if i+1 < len(rs) && rs[i+1] == '>' {
					op = "&>>"
					i++
				}
				toks = append(toks, token{tokRedirect, op})
				continue
			}
			flush()
			if i+1 < len(rs) && rs[i+1] == '&' {
				i++
			}
			toks = append(toks, token{tokSep, "&"})
		case '>':
			op := ">"
			if have && !quoted && buf.String() == "2" {
				op = "2>"
				buf.Reset()
				have = false
			} else {
				flush()
			}
			if i+1 < len(rs) && rs[i+1] == '>' {
				op += ">"
				i++
			}
			toks = append(toks, token{tokRedirect, op})
			// "&1" after an operator is a descriptor, not a background marker.
			if i+1 < len(rs) && rs[i+1] == '&' {
				i++
				buf.WriteRune('&')
				have = true
			}
		default:
			have = true
			buf.WriteRune(c)
		}
	}
	flush()
	return toks
}

// WriteTargets returns the words of command that it writes to: redirect
// targets and the file arguments of tee, cp, mv, ln, install, rm, touch,
// mkdir and sed -i. The words have their quotes removed and are not
// normalized (no ~ or $HOME expansion).
func WriteTargets(command string) []string {
	var targets []string
	var args []string // words of the current segment, redirects removed
	redirect := false
	endSegment := func() {
		targets = append(targets, commandTargets(args)...)
		args = nil
		redirect = false
	}
	for _, t := range tokenize(command) {
		switch t.kind {
		case tokSep:
			endSegment()
		case tokRedirect:
			redirect = true
		case tokWord:
			if redirect {
				redirect = false
				if t.text != "/dev/null" && !strings.HasPrefix(t.text, "&") {
					targets = append(targets, t.text)
				}
				continue
			}
			args = append(args, t.text)
		}
	}
	endSegment()
	return targets
}

// commandTargets returns the file targets of one segment's words.
func commandTargets(words []string) []string {
	i := 0
	for i < len(words) {
		w := words[i]
		if assignRe.MatchString(w) || w == "sudo" || w == "command" {
			i++
			continue
		}
		break
	}
	if i >= len(words) {
		return nil
	}
	name := path.Base(words[i])
	rest := words[i+1:]
	switch name {
	case "tee", "rm", "touch", "mkdir":
		return nonOptions(rest)
	case "cp", "mv", "ln", "install":
		files := nonOptions(rest)
		if len(files) == 0 {
			return nil
		}
		return files[len(files)-1:]
	case "sed":
		inPlace := false
		for _, a := range rest {
			if strings.HasPrefix(a, "-i") {
				inPlace = true
			}
		}
		if !inPlace {
			return nil
		}
		files := nonOptions(rest)
		if len(files) < 2 {
			return nil
		}
		return files[1:]
	}
	return nil
}

func nonOptions(words []string) []string {
	var out []string
	for _, w := range words {
		if !strings.HasPrefix(w, "-") {
			out = append(out, w)
		}
	}
	return out
}

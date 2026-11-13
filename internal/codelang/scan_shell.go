package codelang

import (
	"regexp"
	"strings"
)

const pyNotSpaceClass = `[^` + pySpace + `]`

var (
	heredocRE         = regexp.MustCompile(`^<<(-?)[ \t]*(?:'([^']*)'|"([^"]*)"|(` + ident + `))`)
	shellFunctionRE   = regexp.MustCompile(`(?m)^[ \t]*function[ \t]+(` + ident + `)`)
	shellParensRE     = regexp.MustCompile(`(?m)^[ \t]*(` + ident + `)[ \t]*\([ \t]*\)`)
	shellAssignRE     = regexp.MustCompile(`(?m)(?:^|[;&|(])[ \t]*(` + ident + `)\+?=`)
	shellDeclareRE    = regexp.MustCompile(`(?m)^[ \t]*(?:local|export|readonly|declare|typeset)`)
	shellDeclaredRE   = regexp.MustCompile(`^[ \t]+(?:(-` + pyNotSpaceClass + `+)|(` + ident + `))(?:=` + pyNotSpaceClass + `*)?`)
	shellDeclaredName = 2
)

// heredocState is a heredoc whose body is being read, or waiting for its line. A delimiter
// that is empty (<<”) never closes, like the None the Python original compares with.
type heredocState struct {
	delimiter string
	never     bool
	dashed    bool
}

// scanShellText returns the code lines with strings, comments and heredoc bodies blanked, and
// the comment blocks.
func scanShellText(source string) ([]string, [][]numbered) {
	var codeLines []string
	var blocks [][]numbered
	lastComment := -1
	var quote rune
	var heredoc *heredocState
	var pending []heredocState
	for n, rawLine := range splitLines(source) {
		number := n + 1
		if heredoc != nil {
			body := strings.TrimSuffix(rawLine, "\r")
			if heredoc.dashed {
				body = strings.TrimLeft(body, "\t")
			}
			if !heredoc.never && body == heredoc.delimiter {
				heredoc = nil
				if len(pending) > 0 {
					next := pending[0]
					pending = pending[1:]
					heredoc = &next
				}
			}
			codeLines = append(codeLines, "")
			continue
		}
		line := []rune(rawLine)
		var out []rune
		for i := 0; i < len(line); i++ {
			char := line[i]
			switch {
			case quote != 0:
				if char == '\\' && quote == '"' {
					i++
				} else if char == quote {
					quote = 0
					out = append(out, char)
				}
			case char == '\\':
				i++
			case char == '\'' || char == '"':
				quote = char
				out = append(out, char)
			case char == '#' && (i == 0 || strings.ContainsRune(" \t;&|(", line[i-1])):
				if number != 1 || !strings.HasPrefix(rawLine, "#!") {
					text := trimSpace(string(line[i+1:]))
					if lastComment == number-1 && len(blocks) > 0 {
						blocks[len(blocks)-1] = append(blocks[len(blocks)-1], numbered{number, text})
					} else {
						blocks = append(blocks, []numbered{{number, text}})
					}
					lastComment = number
				}
				i = len(line)
			case char == '<' && i+1 < len(line) && line[i+1] == '<' && !strings.Contains(string(line[:i]), "(("):
				rest := string(line[i:])
				match := heredocRE.FindStringSubmatch(rest)
				if match != nil && !strings.HasPrefix(rest, "<<<") {
					state := heredocState{dashed: match[1] != ""}
					switch {
					case match[2] != "":
						state.delimiter = match[2]
					case match[3] != "":
						state.delimiter = match[3]
					case match[4] != "":
						state.delimiter = match[4]
					default:
						state.never = true
					}
					pending = append(pending, state)
					i += len([]rune(match[0])) - 1
				}
				out = append(out, char)
			default:
				out = append(out, char)
			}
		}
		codeLines = append(codeLines, string(out))
		if len(pending) > 0 && quote == 0 {
			next := pending[0]
			pending = pending[1:]
			heredoc = &next
		}
	}
	return codeLines, blocks
}

// scanShell returns the units of comments and names in shell source. It is a heuristic
// reader, not a parser: it skips quoted strings and heredoc bodies and looks for function
// names and assigned variables.
func scanShell(source string) []Unit {
	codeLines, blocks := scanShellText(source)
	code := strings.Join(codeLines, "\n")
	var names []numbered
	for _, re := range []*regexp.Regexp{shellFunctionRE, shellParensRE, shellAssignRE} {
		for _, m := range re.FindAllStringSubmatchIndex(code, -1) {
			names = append(names, numbered{lineOf(code, m[2]), code[m[2]:m[3]]})
		}
	}
	for _, d := range shellDeclareRE.FindAllStringIndex(code, -1) {
		position := d[1]
		if r, ok := runeAt(code, position); ok && isWordRune(r) {
			continue // \b after the keyword
		}
		for {
			m := shellDeclaredRE.FindStringSubmatchIndex(code[position:])
			if m == nil {
				break
			}
			if m[2*shellDeclaredName] >= 0 {
				// A name counts only when a blank, "=", a newline or the end follows it.
				if r, ok := runeAt(code, position+m[2*shellDeclaredName+1]); ok && !strings.ContainsRune(" \t=\n", r) {
					break
				}
				names = append(names, numbered{
					lineOf(code, position+m[2*shellDeclaredName]),
					code[position+m[2*shellDeclaredName] : position+m[2*shellDeclaredName+1]],
				})
			}
			position += m[1]
		}
	}
	return append(commentUnits(blocks), nameUnits(names, source)...)
}

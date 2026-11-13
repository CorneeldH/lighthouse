package codelang

import (
	"regexp"
	"strings"
)

var (
	goPackageRE     = regexp.MustCompile(`(?m)^[ \t]*package[ \t]+(` + ident + `)`)
	goFuncNamedRE   = regexp.MustCompile(`(?m)^[ \t]*func[ \t]+(?:\(([^)]*)\)[ \t]*)?(` + ident + `)[ \t]*(?:\[[^\]]*\][ \t]*)?\(([^)]*)\)`)
	goFuncParamsRE  = regexp.MustCompile(`func[ \t]*\(([^)]*)\)`)
	goDeclarationRE = regexp.MustCompile(`(?m)^[ \t]*(?:var|const|type)[ \t]+(` + identList + `)`)
	goGroupRE       = regexp.MustCompile(`(?m)^[ \t]*(?:var|const|type)[ \t]*\(`)
	goDefineRE      = regexp.MustCompile(`(` + identList + `)[ \t]*:=`)
	goStructRE      = regexp.MustCompile(`struct[ \t]*\{`)
	goFieldRE       = regexp.MustCompile(`^[ \t]*(` + identList + `)[ \t]+` + pyNotSpaceClass)
	goGroupEntryRE  = regexp.MustCompile(`(?m)^[ \t]*(` + identList + `)`)
	identFullRE     = regexp.MustCompile(`^` + ident + `$`)
	notComma        = regexp.MustCompile(`[^,]+`)
	fieldRE         = regexp.MustCompile(`[^\n;]+`)
)

// scanGoText returns the source with comments, strings and runes blanked (same byte offsets),
// and the comment blocks.
func scanGoText(source string) (string, [][]numbered) {
	out := []byte(source)
	var blocks [][]numbered
	lastLineComment := -1 // line of the last // comment, when it ended the last block
	line := 1
	size := len(source)
	blank := func(start, end int) {
		for k := start; k < end; k++ {
			if out[k] != '\n' {
				out[k] = ' '
			}
		}
	}
	for i := 0; i < size; {
		char := source[i]
		switch {
		case char == '\n':
			line++
		case strings.HasPrefix(source[i:], "//"):
			end := strings.IndexByte(source[i:], '\n')
			if end < 0 {
				end = size
			} else {
				end += i
			}
			text := trimSpace(source[i+2 : end])
			if lastLineComment == line-1 && len(blocks) > 0 {
				blocks[len(blocks)-1] = append(blocks[len(blocks)-1], numbered{line, text})
			} else {
				blocks = append(blocks, []numbered{{line, text}})
			}
			lastLineComment = line
			blank(i, end)
			i = end
			continue
		case strings.HasPrefix(source[i:], "/*"):
			var body string
			end := strings.Index(source[i+2:], "*/")
			if end < 0 {
				// Unterminated: Python still drops the last two characters.
				end = size
				runes := []rune(source[i+2:])
				body = string(runes[:max(len(runes)-2, 0)])
			} else {
				end += i + 4
				body = source[i+2 : end-2]
			}
			var block []numbered
			for n, part := range strings.Split(body, "\n") {
				block = append(block, numbered{line + n, trimSpace(strings.TrimLeft(trimSpace(part), "*"))})
			}
			blocks = append(blocks, block)
			lastLineComment = -1
			line += strings.Count(source[i:end], "\n")
			blank(i, end)
			i = end
			continue
		case char == '"' || char == '\'' || char == '`':
			end := i + 1
			for end < size && source[end] != char {
				if source[end] == '\n' && char != '`' {
					break
				}
				if source[end] == '\\' && char != '`' {
					end += 2
				} else {
					end++
				}
			}
			end = min(end+1, size)
			blank(i+1, end-1)
			line += strings.Count(source[i:end], "\n")
			i = end
			continue
		}
		i++
	}
	return string(out), blocks
}

// goParamNames returns the parameter names in the text of one parameter list that begins at
// byte offset start of code.
func goParamNames(group string, start int, code string) []numbered {
	type piece struct {
		offset int
		tokens []string
	}
	var pieces []piece
	named := false
	for _, m := range notComma.FindAllStringIndex(group, -1) {
		tokens := strings.FieldsFunc(group[m[0]:m[1]], isPySpace)
		pieces = append(pieces, piece{m[0], tokens})
		named = named || len(tokens) > 1
	}
	if !named {
		return nil // only types, no names
	}
	var names []numbered
	for _, p := range pieces {
		if len(p.tokens) == 0 || !identFullRE.MatchString(p.tokens[0]) {
			continue
		}
		at := start + p.offset + strings.Index(group[p.offset:], p.tokens[0])
		names = append(names, numbered{lineOf(code, at), p.tokens[0]})
	}
	return names
}

// addIdents adds every identifier of code[from:to], the text of an identifier list.
func addIdents(names []numbered, code string, from, to int) []numbered {
	for _, m := range identRE.FindAllStringIndex(code[from:to], -1) {
		names = append(names, numbered{lineOf(code, from+m[0]), code[from+m[0] : from+m[1]]})
	}
	return names
}

// closing is the offset after the bracket that closes the one open at from.
func closing(code string, from int, open, shut byte) int {
	depth, end := 1, from
	for end < len(code) && depth > 0 {
		switch code[end] {
		case open:
			depth++
		case shut:
			depth--
		}
		end++
	}
	return end
}

func goNames(code string) []numbered {
	var names []numbered
	for _, m := range goPackageRE.FindAllStringSubmatchIndex(code, -1) {
		names = addIdents(names, code, m[2], m[3])
	}
	for _, m := range goFuncNamedRE.FindAllStringSubmatchIndex(code, -1) {
		names = append(names, numbered{lineOf(code, m[4]), code[m[4]:m[5]]})
		if m[2] >= 0 {
			names = append(names, goParamNames(code[m[2]:m[3]], m[2], code)...)
		}
		names = append(names, goParamNames(code[m[6]:m[7]], m[6], code)...)
	}
	for _, m := range findAllChecked(goFuncParamsRE, code, notAfterWordRune) {
		names = append(names, goParamNames(code[m[2]:m[3]], m[2], code)...)
	}
	for _, m := range goDeclarationRE.FindAllStringSubmatchIndex(code, -1) {
		names = addIdents(names, code, m[2], m[3])
	}
	notQualified := func(s string, m []int) bool { // (?<![\w.])
		r, ok := runeBefore(s, m[0])
		return !ok || !isWordRune(r) && r != '.'
	}
	for _, m := range findAllChecked(goDefineRE, code, notQualified) {
		names = addIdents(names, code, m[2], m[3])
	}
	for _, group := range goGroupRE.FindAllStringIndex(code, -1) {
		end := closing(code, group[1], '(', ')')
		inner := code[group[1]:end]
		for _, entry := range goGroupEntryRE.FindAllStringSubmatchIndex(inner, -1) {
			names = addIdents(names, code, group[1]+entry[2], group[1]+entry[3])
		}
	}
	for _, m := range findAllChecked(goStructRE, code, notAfterWordRune) {
		end := closing(code, m[1], '{', '}')
		if end-1 < m[1] {
			continue
		}
		body := code[m[1] : end-1]
		for _, field := range fieldRE.FindAllStringIndex(body, -1) {
			match := goFieldRE.FindStringSubmatchIndex(body[field[0]:field[1]])
			if match != nil {
				base := m[1] + field[0]
				names = addIdents(names, code, base+match[2], base+match[3])
			}
		}
	}
	return names
}

// scanGo returns the units of comments and names in Go source. It is a heuristic reader, not
// a parser: it blanks comments, strings, raw strings and runes first, then looks for the
// declaration forms of Go.
func scanGo(source string) []Unit {
	code, blocks := scanGoText(source)
	return append(commentUnits(blocks), nameUnits(goNames(code), source)...)
}

package codelang

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// scanPython returns the comment blocks, docstrings and new names of Python src, in the
// order code_language.py found them: comments, docstrings, names. There is no parser: the
// names come from reading the tokens statement by statement, so a grammar error that
// tokenizes fine is scanned, where ast.parse would refuse it.
func scanPython(src string) ([]Unit, error) {
	tokens, err := pyLex(src)
	if err != nil {
		return nil, &CannotCheckError{Reason: "cannot parse Python: " + err.Error()}
	}
	var blocks [][]numbered
	for _, t := range tokens {
		if t.kind != pyComment {
			continue
		}
		text := trimSpace(strings.TrimLeft(t.text, "#"))
		if n := len(blocks); n > 0 && blocks[n-1][len(blocks[n-1])-1].line == t.line-1 {
			blocks[n-1] = append(blocks[n-1], numbered{t.line, text})
		} else {
			blocks = append(blocks, []numbered{{t.line, text}})
		}
	}
	r := &pyReader{docPending: true}
	for _, line := range logicalLines(tokens) {
		r.read(line)
	}
	units := commentUnits(blocks)
	units = append(units, r.docs...)
	return append(units, nameUnits(r.names, src)...), nil
}

// lineToken is a token of one logical line, with its index in the line (pos) and the
// estimated AST depth the brackets around it add (nest).
type lineToken struct {
	pyToken
	pos, nest int
}

// placedName is a name with the estimated AST depth of the node that stores it, relative to
// its statement, and the index of its token. ast.walk visits a line by depth, so the names of
// one line are ordered by (level, pos).
//
// The levels (here, in bracketWeight, targets, header, statement and expressions) only
// approximate the breadth-first order of ast.walk: operators add AST levels that no token
// shows. Any change to them needs the differential against code_language.py run again, on
// the repository's Python files at least.
type placedName struct {
	numbered
	level, pos int
}

func placed(t lineToken, line, level int) placedName {
	return placedName{numbered{line, t.text}, level, t.pos}
}

// logicalLines splits the tokens into logical lines without comments, NL, INDENT and DEDENT.
func logicalLines(tokens []pyToken) [][]lineToken {
	var lines [][]lineToken
	var line []lineToken
	for _, t := range tokens {
		switch t.kind {
		case pyComment, pyNL, pyIndent, pyDedent, pyEOF:
		case pyNewline:
			if len(line) > 0 {
				lines = append(lines, nested(line))
			}
			line = nil
		default:
			line = append(line, lineToken{pyToken: t, pos: len(line)})
		}
	}
	if len(line) > 0 {
		lines = append(lines, nested(line))
	}
	return lines
}

// nested fills in nest: the sum of bracketWeight over the brackets around each token.
func nested(line []lineToken) []lineToken {
	var weights []int
	sum := 0
	for i := range line {
		t := &line[i]
		if t.kind == pyOp && strings.Contains(")]}", t.text) && len(weights) > 0 {
			sum -= weights[len(weights)-1]
			weights = weights[:len(weights)-1]
		}
		t.nest = sum
		if t.kind == pyOp && strings.Contains("([{", t.text) {
			w := bracketWeight(line, i)
			weights = append(weights, w)
			sum += w
		}
	}
	return line
}

// bracketWeight is how many AST levels the opener at i puts between the expression it is
// part of and the expressions inside it: none for grouping parentheses, one for a call,
// subscript, display or tuple, two for a call whose argument is a bare generator.
func bracketWeight(line []lineToken, i int) int {
	open := line[i]
	comma, generator := false, false
	for _, t := range line[i+1 : closer(line, i)] {
		if t.depth == open.depth+1 {
			comma = comma || isOp(t, ",")
			generator = generator || isWord(t, "for")
		}
	}
	if i > 0 && open.text != "{" {
		prev := line[i-1]
		if prev.kind == pyName && !pyKeywords[prev.text] || prev.kind == pyStr || isOp(prev, ")", "]", "}") {
			if generator {
				return 2
			}
			return 1
		}
	}
	if open.text == "(" && !comma && !generator {
		return 0
	}
	return 1
}

// pyReader collects names and docstrings line by line. docPending says the next statement is
// the first of a module, class or function body.
type pyReader struct {
	names      []numbered
	docs       []Unit
	docPending bool
	line       []placedName // the names of the logical line being read
}

var pyKeywords = map[string]bool{
	"False": true, "None": true, "True": true, "and": true, "as": true, "assert": true,
	"async": true, "await": true, "break": true, "class": true, "continue": true, "def": true,
	"del": true, "elif": true, "else": true, "except": true, "finally": true, "for": true,
	"from": true, "global": true, "if": true, "import": true, "in": true, "is": true,
	"lambda": true, "nonlocal": true, "not": true, "or": true, "pass": true, "raise": true,
	"return": true, "try": true, "while": true, "with": true, "yield": true,
}

var pyAugOps = map[string]bool{
	"+=": true, "-=": true, "*=": true, "/=": true, "//=": true, "%=": true, "@=": true,
	"&=": true, "|=": true, "^=": true, ">>=": true, "<<=": true, "**=": true,
}

func isOp(t lineToken, texts ...string) bool {
	if t.kind != pyOp {
		return false
	}
	for _, s := range texts {
		if t.text == s {
			return true
		}
	}
	return false
}

func isWord(t lineToken, word string) bool { return t.kind == pyName && t.text == word }

// read reads one logical line: a compound header with its inline body, or simple
// statements separated by ";". The statements of an inline body sit one level deeper.
func (r *pyReader) read(line []lineToken) {
	r.line = nil
	pending := r.docPending
	r.docPending = false

	simple, bodyStart := line, 0
	if head := compoundKeyword(line); head != "" {
		colon := headerColon(line)
		header := line
		simple, bodyStart = nil, len(line)
		if colon >= 0 {
			header, simple, bodyStart = line[:colon], line[colon+1:], colon+1
		}
		r.header(head, header)
		pending = false
		if head == "def" || head == "class" {
			pending = true
			r.docPending = len(simple) == 0
		}
	}
	base := min(bodyStart, 1)
	for i, stmt := range splitTop(simple, ";") {
		if i == 0 && pending {
			r.docstring(stmt)
		}
		r.statement(stmt, base)
	}
	r.expressions(line, bodyStart)
	sort.SliceStable(r.line, func(i, j int) bool {
		a, b := r.line[i], r.line[j]
		return a.level < b.level || a.level == b.level && a.pos < b.pos
	})
	for _, n := range r.line {
		r.names = append(r.names, n.numbered)
	}
}

// compoundKeyword names the compound statement line starts, or "" for simple statements.
func compoundKeyword(line []lineToken) string {
	first := line[0]
	if first.kind != pyName {
		return ""
	}
	switch first.text {
	case "if", "elif", "else", "while", "for", "try", "except", "finally", "with", "def", "class":
		return first.text
	case "async":
		if len(line) > 1 && (isWord(line[1], "def") || isWord(line[1], "for") || isWord(line[1], "with")) {
			return line[1].text
		}
	case "match", "case":
		// soft keywords: a statement when a header colon ends a header that assigns nothing
		if len(line) < 2 || isOp(line[1], "=", ":", ".", ",", ")", "]", "}") || pyAugOps[line[1].text] {
			return ""
		}
		colon := headerColon(line)
		if colon < 0 {
			return ""
		}
		// a match body never shares its line, so "match(x).y: int = 3" is an annotation
		if first.text == "match" && colon != len(line)-1 {
			return ""
		}
		for i, t := range line[:colon] {
			if t.depth == 0 && (isOp(t, "=") || t.kind == pyOp && pyAugOps[t.text]) {
				return ""
			}
			// no pattern has an attribute after a call or subscript: "case(x).y" is a target
			if first.text == "case" && t.depth == 0 && isOp(t, ".") && isOp(line[i-1], ")", "]") {
				return ""
			}
		}
		return first.text
	}
	return ""
}

// headerColon is the index of the colon that ends a compound header: the first ":" at depth
// 0 that does not end the parameters of a lambda.
func headerColon(line []lineToken) int {
	lambdas := 0
	for i, t := range line {
		if t.depth != 0 {
			continue
		}
		if isWord(t, "lambda") {
			lambdas++
		}
		if isOp(t, ":") {
			if lambdas == 0 {
				return i
			}
			lambdas--
		}
	}
	return -1
}

// splitTop splits tokens at op sep on the depth of the first token; empty parts drop out.
func splitTop(tokens []lineToken, sep string) [][]lineToken {
	if len(tokens) == 0 {
		return nil
	}
	base := tokens[0].depth
	var parts [][]lineToken
	start := 0
	for i, t := range tokens {
		if t.depth == base && isOp(t, sep) {
			if i > start {
				parts = append(parts, tokens[start:i])
			}
			start = i + 1
		}
	}
	if start < len(tokens) {
		parts = append(parts, tokens[start:])
	}
	return parts
}

// header reads the names a compound header stores itself. FunctionDef, ClassDef and
// ExceptHandler report the line of their first token.
func (r *pyReader) header(head string, header []lineToken) {
	first := header[0].line
	switch head {
	case "def":
		at := 1
		if isWord(header[0], "async") {
			at = 2
		}
		if at >= len(header) || header[at].kind != pyName {
			return
		}
		r.line = append(r.line, placed(header[at], first, 0))
		at++
		if at < len(header) && isOp(header[at], "[") {
			at = closer(header, at) + 1 // type parameters are no names in ast
		}
		if at < len(header) && isOp(header[at], "(") {
			r.line = append(r.line, parameters(header[at+1:closer(header, at)], ")", 2)...)
		}
	case "class":
		if len(header) > 1 && header[1].kind == pyName {
			r.line = append(r.line, placed(header[1], first, 0))
		}
	case "for":
		at := 0
		if isWord(header[0], "async") {
			at = 1
		}
		r.line = append(r.line, forTarget(header, at, 1)...)
	case "with":
		for i, t := range header {
			if isWord(t, "as") {
				end := i + 1
				for end < len(header) && header[end].depth >= t.depth &&
					(header[end].depth != t.depth || !isOp(header[end], ",", ":")) {
					end++
				}
				r.line = append(r.line, targets(header[i+1:end], 2)...)
			}
		}
	case "except":
		for i, t := range header {
			if isWord(t, "as") && i+1 < len(header) && header[i+1].kind == pyName {
				r.line = append(r.line, placed(header[i+1], first, 0))
			}
		}
	}
}

// closer is the index of the closer that matches the opener at open, or len(tokens).
func closer(tokens []lineToken, open int) int {
	for i := open + 1; i < len(tokens); i++ {
		if tokens[i].depth == tokens[open].depth && tokens[i].kind == pyOp {
			return i
		}
	}
	return len(tokens)
}

// parameters are the names of a parameter list: a name at the start or after ",", or after
// a "*", "**" or "/" that follows the start or a ",", on the depth of the list, that a ":",
// "=", "," or end follows. The parameters of a lambda in a default belong to that lambda.
func parameters(list []lineToken, end string, level int) []placedName {
	if len(list) == 0 {
		return nil
	}
	var names []placedName
	depth := list[0].depth
	expect := true
	for i := 0; i < len(list); i++ {
		t := list[i]
		if t.depth != depth {
			continue
		}
		if isWord(t, "lambda") {
			i += len(lambdaParams(list, i))
			expect = false
			continue
		}
		next := lineToken{pyToken: pyToken{kind: pyOp, text: end}}
		if i+1 < len(list) {
			next = list[i+1]
		}
		if expect && t.kind == pyName && isOp(next, ":", "=", ",", end) {
			names = append(names, placed(t, t.line, level))
			expect = false
			continue
		}
		atStart := i == 0 || isOp(list[i-1], ",")
		expect = isOp(t, ",") || atStart && isOp(t, "*", "**", "/")
	}
	return names
}

// forTarget is the target of the "for" at index at, up to its "in".
func forTarget(tokens []lineToken, at, level int) []placedName {
	depth := tokens[at].depth
	end := at + 1
	for end < len(tokens) && tokens[end].depth >= depth && (tokens[end].depth != depth || !isWord(tokens[end], "in")) {
		end++
	}
	return targets(tokens[at+1:end], level)
}

// targets are the names an assignment target stores: a bare name, the last attribute of a
// chain, and both inside tuples, lists and after "*". A subscript stores no name. The
// elements of a tuple, a list and a starred target sit one level below it.
func targets(span []lineToken, level int) []placedName {
	elements := splitTop(span, ",")
	tuple := len(elements) > 1 || len(span) > 0 && isOp(span[len(span)-1], ",")
	if tuple {
		level++
	}
	var names []placedName
	for _, el := range elements {
		at := level
		if isOp(el[0], "*") {
			el = el[1:]
			at++
		}
		n := len(el)
		switch {
		case n == 0:
		case n == 1 && el[0].kind == pyName && !pyKeywords[el[0].text]:
			names = append(names, placed(el[0], el[0].line, at))
		case isOp(el[0], "[") && closer(el, 0) == n-1:
			names = append(names, targets(el[1:n-1], at+1)...)
		case isOp(el[0], "(") && closer(el, 0) == n-1:
			names = append(names, targets(el[1:n-1], at)...)
		case n >= 3 && el[n-1].kind == pyName && isOp(el[n-2], "."):
			names = append(names, placed(el[n-1], el[n-1].endLine, at))
		}
	}
	return names
}

// statement reads the names one simple statement stores itself; base is 1 for a statement
// in the inline body of a compound statement.
func (r *pyReader) statement(stmt []lineToken, base int) {
	first := stmt[0]
	if first.kind == pyName {
		switch first.text {
		case "import", "from":
			r.line = append(r.line, aliases(stmt, base+1)...)
			return
		case "global", "nonlocal", "del", "pass", "return", "raise", "assert", "break", "continue":
			return
		case "type":
			if len(stmt) > 2 && stmt[1].kind == pyName {
				for _, t := range stmt {
					if t.depth == 0 && isOp(t, "=") {
						r.line = append(r.line, placed(stmt[1], stmt[1].line, base+1))
						return
					}
				}
			}
		}
	}
	start := 0
	for i, t := range stmt {
		if t.depth != 0 {
			continue
		}
		if isWord(t, "lambda") {
			return
		}
		switch {
		case start == 0 && (isOp(t, ":") || t.kind == pyOp && pyAugOps[t.text]):
			r.line = append(r.line, targets(stmt[:i], base+1)...)
			return
		case isOp(t, "="):
			r.line = append(r.line, targets(stmt[start:i], base+1)...)
			start = i + 1
		}
	}
}

// aliases are the "as" names of an import statement, on the line where the alias starts.
func aliases(stmt []lineToken, level int) []placedName {
	var names []placedName
	for i, t := range stmt {
		if !isWord(t, "as") || i+1 >= len(stmt) || stmt[i+1].kind != pyName {
			continue
		}
		begin := i - 1
		for begin > 0 && !isOp(stmt[begin-1], ",", "(") && !isWord(stmt[begin-1], "import") {
			begin--
		}
		names = append(names, placed(stmt[i+1], stmt[max(begin, 0)].line, level))
	}
	return names
}

// expressions reads the names expressions store anywhere on a line: comprehension targets,
// walrus targets and lambda parameters. The target of a for statement is the header's.
// An expression sits at level 1 of its statement plus what its brackets add; tokens from
// bodyStart on are in an inline body, one level deeper.
func (r *pyReader) expressions(line []lineToken, bodyStart int) {
	statementFor := 0 // the index of the "for" of a for statement, which header reads
	if isWord(line[0], "async") {
		statementFor = 1
	}
	for i, t := range line {
		element := 1 + t.nest
		if i >= bodyStart {
			element++
		}
		switch {
		case isWord(t, "for") && i != statementFor:
			r.line = append(r.line, forTarget(line, i, element+1)...)
		case isOp(t, ":=") && i > 0 && line[i-1].kind == pyName:
			r.line = append(r.line, placed(line[i-1], line[i-1].line, element+1))
		case isWord(t, "lambda"):
			r.line = append(r.line, parameters(lambdaParams(line, i), ":", element+2)...)
		}
	}
}

// lambdaParams are the tokens between "lambda" at index at and its ":".
func lambdaParams(line []lineToken, at int) []lineToken {
	depth := line[at].depth
	nested := 0
	for j := at + 1; j < len(line); j++ {
		t := line[j]
		if t.depth < depth {
			return line[at+1 : j]
		}
		if t.depth > depth {
			continue
		}
		if isWord(t, "lambda") {
			nested++
		}
		if isOp(t, ":") {
			if nested == 0 {
				return line[at+1 : j]
			}
			nested--
		}
	}
	return line[at+1:]
}

// docstring records stmt as a docstring when it is only string literals without an f or b
// prefix, maybe in parentheses: the decoded, concatenated value after inspect.cleandoc.
func (r *pyReader) docstring(stmt []lineToken) {
	var value strings.Builder
	strs := 0
	for _, t := range stmt {
		switch {
		case isOp(t, "(", ")"):
		case t.kind == pyStr && !strings.ContainsAny(prefixOf(t.text), "bB"):
			value.WriteString(pyStringValue(t.text))
			strs++
		default:
			return
		}
	}
	if strs == 0 {
		return
	}
	doc := cleandoc(value.String())
	if strings.Contains(doc, marker) {
		return
	}
	text := ""
	if lines := splitLines(doc); len(lines) > 0 {
		text = lines[0]
	}
	r.docs = append(r.docs, Unit{Line: stmt[0].line, Kind: "docstring", Text: text, Body: doc})
}

func prefixOf(literal string) string {
	return literal[:strings.IndexAny(literal, `'"`)]
}

// pyStringValue is the value of a string literal: quotes off, line breaks normalized and,
// without an r prefix, escapes decoded.
func pyStringValue(literal string) string {
	prefix := prefixOf(literal)
	body := literal[len(prefix):]
	q := 1
	if len(body) >= 6 && body[:3] == strings.Repeat(body[:1], 3) {
		q = 3
	}
	body = strings.ReplaceAll(body[q:len(body)-q], "\r\n", "\n")
	if strings.ContainsAny(prefix, "rR") {
		return body
	}
	return decodeEscapes(body)
}

// decodeEscapes decodes the escapes of a Python str literal. An unknown escape stays as it
// is, as in Python. \N{name} stays as well: Go has no table of Unicode names.
func decodeEscapes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			i++
			continue
		}
		c := s[i+1]
		switch c {
		case '\n':
			i += 2
			continue
		case '\\', '\'', '"':
			b.WriteByte(c)
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte('\v')
		case '0', '1', '2', '3', '4', '5', '6', '7':
			j := i + 1
			for j < len(s) && j < i+4 && s[j] >= '0' && s[j] <= '7' {
				j++
			}
			v, _ := strconv.ParseUint(s[i+1:j], 8, 32)
			b.WriteRune(rune(v))
			i = j
			continue
		case 'x', 'u', 'U':
			size := map[byte]int{'x': 2, 'u': 4, 'U': 8}[c]
			if v, err := strconv.ParseUint(s[i+2:min(i+2+size, len(s))], 16, 32); err == nil && i+2+size <= len(s) {
				b.WriteRune(rune(v))
				i += 2 + size
				continue
			}
			b.WriteString(s[i : i+2])
		default:
			_, width := utf8.DecodeRuneInString(s[i+1:])
			b.WriteString(s[i : i+1+width])
			i += 1 + width
			continue
		}
		i += 2
	}
	return b.String()
}

// cleandoc is inspect.cleandoc of Python 3.12.
func cleandoc(doc string) string {
	lines := strings.Split(expandTabs(doc), "\n")
	margin := -1
	for _, line := range lines[1:] {
		content := utf8.RuneCountInString(strings.TrimLeftFunc(line, isPySpace))
		if content > 0 {
			indent := utf8.RuneCountInString(line) - content
			if margin < 0 || indent < margin {
				margin = indent
			}
		}
	}
	lines[0] = strings.TrimLeftFunc(lines[0], isPySpace)
	if margin >= 0 {
		for i := 1; i < len(lines); i++ {
			runes := []rune(lines[i])
			lines[i] = string(runes[min(margin, len(runes)):])
		}
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

// expandTabs is str.expandtabs(8): the column restarts after "\n" and "\r".
func expandTabs(s string) string {
	var b strings.Builder
	col := 0
	for _, r := range s {
		switch r {
		case '\t':
			spaces := 8 - col%8
			b.WriteString(strings.Repeat(" ", spaces))
			col += spaces
		case '\n', '\r':
			b.WriteRune(r)
			col = 0
		default:
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}

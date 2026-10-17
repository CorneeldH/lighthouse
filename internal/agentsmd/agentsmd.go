// Package agentsmd edits the managed blocks in AGENTS.md (spec, init punt 4).
//
// Each package owns one block between `<!-- lighthouse:begin <pkg> -->` and
// `<!-- lighthouse:end <pkg> -->`. Text outside the markers belongs to the project and
// is never touched. A marker line may carry surrounding spaces and a CRLF ending.
package agentsmd

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// File is the name used in error messages.
const File = "AGENTS.md"

var markerRE = regexp.MustCompile(`^<!-- lighthouse:(begin|end) (\S+) -->$`)

// Begin returns the begin marker of pkg.
func Begin(pkg string) string { return "<!-- lighthouse:begin " + pkg + " -->" }

// End returns the end marker of pkg.
func End(pkg string) string { return "<!-- lighthouse:end " + pkg + " -->" }

type span struct{ begin, end int } // line indexes of the two markers

// parse splits doc into lines (each with its ending) and validates every block.
func parse(doc []byte) ([]string, map[string]span, error) {
	lines := strings.SplitAfter(string(doc), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	spans := map[string]span{}
	open, openAt := "", -1
	for i, line := range lines {
		m := markerRE.FindStringSubmatch(strings.TrimSpace(strings.TrimRight(line, "\r\n")))
		if m == nil {
			continue
		}
		kind, pkg := m[1], m[2]
		switch {
		case kind == "begin" && open != "":
			return nil, nil, fmt.Errorf("%s:%d: begin of block %s inside block %s (line %d)", File, i+1, pkg, open, openAt+1)
		case kind == "begin":
			if _, dup := spans[pkg]; dup {
				return nil, nil, fmt.Errorf("%s:%d: second block %s", File, i+1, pkg)
			}
			open, openAt = pkg, i
		case open == "":
			return nil, nil, fmt.Errorf("%s:%d: end of block %s without a begin", File, i+1, pkg)
		case pkg != open:
			return nil, nil, fmt.Errorf("%s:%d: end of block %s inside block %s (line %d)", File, i+1, pkg, open, openAt+1)
		default:
			spans[pkg] = span{openAt, i}
			open, openAt = "", -1
		}
	}
	if open != "" {
		return nil, nil, fmt.Errorf("%s:%d: block %s has no end marker", File, openAt+1, open)
	}
	return lines, spans, nil
}

// Find returns the body of pkg's block: everything between the two marker lines.
func Find(doc []byte, pkg string) (body []byte, found bool, err error) {
	lines, spans, err := parse(doc)
	if err != nil {
		return nil, false, err
	}
	s, ok := spans[pkg]
	if !ok {
		return nil, false, nil
	}
	return []byte(strings.Join(lines[s.begin+1:s.end], "")), true, nil
}

// Set replaces the body of pkg's block, or appends the block after a blank line.
func Set(doc []byte, pkg string, body []byte) ([]byte, error) {
	lines, spans, err := parse(doc)
	if err != nil {
		return nil, err
	}
	if s, ok := spans[pkg]; ok {
		out := strings.Join(lines[:s.begin+1], "") + string(body) + strings.Join(lines[s.end:], "")
		return []byte(out), nil
	}
	nl := "\n"
	if bytes.Contains(doc, []byte("\r\n")) {
		nl = "\r\n"
	}
	out := string(doc)
	if out != "" {
		if !strings.HasSuffix(out, "\n") {
			out += nl
		}
		if !strings.HasSuffix(out, nl+nl) {
			out += nl
		}
	}
	out += Begin(pkg) + nl + string(body) + End(pkg) + nl
	return []byte(out), nil
}

// Remove deletes pkg's block and the blank line before it.
func Remove(doc []byte, pkg string) ([]byte, error) {
	lines, spans, err := parse(doc)
	if err != nil {
		return nil, err
	}
	s, ok := spans[pkg]
	if !ok {
		return doc, nil
	}
	from := s.begin
	if from > 0 && strings.TrimSpace(lines[from-1]) == "" {
		from--
	}
	return []byte(strings.Join(lines[:from], "") + strings.Join(lines[s.end+1:], "")), nil
}

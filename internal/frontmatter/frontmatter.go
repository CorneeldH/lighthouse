// Package frontmatter reads a field from the YAML frontmatter of a markdown file, the way the
// awk of the shell checks did: a flat `key: value` line, or a folded or literal block scalar.
package frontmatter

import (
	"os"
	"regexp"
	"strings"
)

// blockScalar is the value of a YAML folded (`>`) or literal (`|`) block scalar header.
var blockScalar = regexp.MustCompile(`^[>|][-+0-9]*[ \t]*$`)

// Field is a field from the frontmatter of file: "" when the field is missing OR carries no
// value, so an empty `upstream:` counts as missing, and when file is unreadable or has no
// frontmatter. A folded (`>`) or literal (`|`) block scalar is read from its indented
// continuation lines.
func Field(file, field string) string {
	ls := lines(file)
	if len(ls) == 0 || ls[0] != "---" {
		return ""
	}
	prefix := field + ": "
	collecting, fold := false, false
	var buf strings.Builder
	for _, l := range ls[1:] {
		if collecting {
			if l == "---" || (l != "" && !startsBlank(l)) {
				return strings.TrimRight(buf.String(), "\n")
			}
			line := strings.TrimLeft(l, " \t")
			text := buf.String()
			switch {
			case line == "":
				buf.WriteString("\n")
			case text == "" || strings.HasSuffix(text, "\n"):
				buf.WriteString(line)
			case fold:
				buf.WriteString(" " + line)
			default:
				buf.WriteString("\n" + line)
			}
			continue
		}
		if l == "---" {
			return ""
		}
		if value, found := strings.CutPrefix(l, prefix); found {
			if blockScalar.MatchString(value) {
				collecting, fold = true, value[0] == '>'
				continue
			}
			return value
		}
	}
	return strings.TrimRight(buf.String(), "\n")
}

// lines splits a file into lines as awk reads records: a final newline ends the last line, it
// does not start an empty one. A path that is not a readable regular file has no lines.
func lines(path string) []string {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func startsBlank(l string) bool { return l != "" && (l[0] == ' ' || l[0] == '\t') }

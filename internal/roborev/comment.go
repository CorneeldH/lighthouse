package roborev

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
)

// Limit is the number of characters GitHub accepts in a comment. It counts runes, as
// roborev_comment.py counts code points, not bytes.
const Limit = 65536

// Truncated is the note that follows a cut raw output.
const Truncated = "… ingekort tot de limiet van GitHub"

// LogLine is the one line `<!-- roborev-log: <json> -->` that round-trips through a JSON parser.
// The JSON is compact (`,` and `:` without spaces), non-ASCII stays raw, and `<`, `>` and `-`
// are written as \u003c, \u003e and \u002d so the HTML comment cannot be closed early.
func LogLine(sha, source string, pr int, findings []any) string {
	var b strings.Builder
	b.WriteString(`{"v":1,"sha":`)
	writePyJSON(&b, sha)
	b.WriteString(`,"bron":`)
	writePyJSON(&b, source)
	fmt.Fprintf(&b, `,"pr":%d,"bevindingen":`, pr)
	writePyJSON(&b, findings)
	b.WriteByte('}')
	payload := strings.NewReplacer("<", `\u003c`, ">", `\u003e`, "-", `\u002d`).Replace(b.String())
	return "<!-- roborev-log: " + payload + " -->"
}

// writePyJSON writes v as json.dumps(separators=(",", ":"), ensure_ascii=False) does: only the
// quote, the backslash and control characters are escaped.
func writePyJSON(b *strings.Builder, v any) {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		fmt.Fprint(b, t)
	case string:
		b.WriteByte('"')
		for _, r := range t {
			switch {
			case r == '"':
				b.WriteString(`\"`)
			case r == '\\':
				b.WriteString(`\\`)
			case r == '\n':
				b.WriteString(`\n`)
			case r == '\r':
				b.WriteString(`\r`)
			case r == '\t':
				b.WriteString(`\t`)
			case r == '\b':
				b.WriteString(`\b`)
			case r == '\f':
				b.WriteString(`\f`)
			case r < 0x20:
				fmt.Fprintf(b, `\u%04x`, r)
			default:
				b.WriteRune(r)
			}
		}
		b.WriteByte('"')
	case *orderedjson.Object:
		b.WriteByte('{')
		for i, k := range t.Keys() {
			if i > 0 {
				b.WriteByte(',')
			}
			writePyJSON(b, k)
			b.WriteByte(':')
			e, _ := t.Get(k)
			writePyJSON(b, e)
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			writePyJSON(b, e)
		}
		b.WriteByte(']')
	default: // json.Number and other numbers
		fmt.Fprint(b, t)
	}
}

// isPySpace is the whitespace str.split() folds: unicode.IsSpace plus U+001C to U+001F.
func isPySpace(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) }

// clean is one table cell: whitespace runs folded to a space, `|` escaped, so a row stays one line.
func clean(text string) string {
	return strings.ReplaceAll(strings.Join(strings.FieldsFunc(text, isPySpace), " "), "|", `\|`)
}

func field(finding any, key string) any {
	if o, ok := finding.(*orderedjson.Object); ok {
		v, _ := o.Get(key)
		return v
	}
	return nil
}

func cell(finding any) string {
	path := field(finding, "path")
	if !truthy(path) {
		return "—"
	}
	if line := field(finding, "line"); line != nil {
		return clean(fmt.Sprintf("`%s:%s`", pyStr(path), pyStr(line)))
	}
	return clean(fmt.Sprintf("`%s`", pyStr(path)))
}

func outcome(finding any) string {
	kind := pyStr(field(finding, "uitkomst"))
	if kind == "fixed" {
		return clean("fixed in " + pyStr(field(finding, "commit")))
	}
	reason := ""
	if r := field(finding, "reden"); truthy(r) {
		reason = pyStr(r)
	}
	return clean(kind + ": " + reason)
}

// table is the Markdown table, one row per finding; one explicit row when there are none.
func table(findings []any) string {
	rows := []string{"| Bevinding | Severity | Uitkomst |", "| --- | --- | --- |"}
	if len(findings) == 0 {
		rows = append(rows, "| geen bevindingen | — | — |")
	}
	for _, f := range findings {
		rows = append(rows, fmt.Sprintf("| %s | %s | %s |", cell(f), clean(pyStr(field(f, "severity"))), outcome(f)))
	}
	return strings.Join(rows, "\n")
}

// Render is the whole comment; when it is too long, only the raw output is cut.
func Render(state *orderedjson.Object, raw string, pr int, source string) (string, error) {
	shaValue, _ := state.Get("sha")
	sha := pyStr(shaValue)
	var findings []any
	if v, _ := state.Get("bevindingen"); v != nil {
		list, ok := v.([]any)
		if !ok {
			return "", valuef("state: bevindingen is not a list")
		}
		findings = list
	}
	head := "<!-- roborev-verwerkt: " + sha + " -->\n" +
		LogLine(sha, source, pr, findings) + "\n\n" +
		table(findings) + "\n\n" +
		"<details><summary>Ruwe roborev-output</summary>\n\n"
	const tail = "\n\n</details>"
	headLen, tailLen, rawLen := utf8.RuneCountInString(head), utf8.RuneCountInString(tail), utf8.RuneCountInString(raw)
	if headLen+rawLen+tailLen <= Limit {
		return head + raw + tail, nil
	}
	note := "\n" + Truncated
	noteLen := utf8.RuneCountInString(note)
	room := Limit - headLen - tailLen - noteLen
	if room < 0 {
		return "", valuef("log block and table alone are %d characters, over the limit of %d", headLen+tailLen+noteLen, Limit)
	}
	return head + string([]rune(raw)[:room]) + note + tail, nil
}

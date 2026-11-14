package roborev

import (
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
)

const (
	commentFixtureDir = "../../tests/pack/fixtures/roborev/"
	limitNote         = "… ingekort tot de limiet van GitHub"
)

var logLineRe = regexp.MustCompile(`^<!-- roborev-log: (.*) -->$`)

// finding builds a finding object as the state file carries it; over replaces values.
func finding(t *testing.T, over map[string]any) any {
	t.Helper()
	base := map[string]any{
		"path": "src/a.py", "line": 12, "severity": "high",
		"uitkomst": "fixed", "commit": "abc1234", "reden": "",
	}
	for k, v := range over {
		base[k] = v
	}
	obj := orderedjson.NewObject()
	for _, k := range []string{"path", "line", "severity", "uitkomst", "commit", "reden"} {
		v := base[k]
		if n, ok := v.(int); ok {
			v = json.Number(strconv.Itoa(n))
		}
		obj.Set(k, v)
	}
	return obj
}

func stateOf(findings ...any) *orderedjson.Object {
	s := orderedjson.NewObject()
	s.Set("range", "main..abc1234")
	s.Set("uitkomst", "done")
	s.Set("sha", "abc1234")
	s.Set("bevindingen", append([]any{}, findings...))
	return s
}

func mustRender(t *testing.T, state *orderedjson.Object, raw string) string {
	t.Helper()
	out, err := Render(state, raw, 7, "lokaal")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return out
}

func logPayload(t *testing.T, line string) string {
	t.Helper()
	m := logLineRe.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("not a log line: %q", line)
	}
	return m[1]
}

func TestLogBlockRoundTripsAHostileReason(t *testing.T) {
	reason := "sluit af --> <script>x</script>\nnieuwe regel"
	line := LogLine("abc1234", "lokaal", 7, []any{finding(t, map[string]any{"uitkomst": "refuted", "reden": reason})})
	if strings.Contains(line, "\n") || strings.Count(line, "-->") != 1 || !strings.HasSuffix(line, " -->") {
		t.Fatalf("log line is not one safe line: %q", line)
	}
	payload := logPayload(t, line)
	var parsed struct {
		Findings []struct {
			Reason string `json:"reden"`
		} `json:"bevindingen"`
	}
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Findings[0].Reason != reason {
		t.Errorf("reden = %q, want %q", parsed.Findings[0].Reason, reason)
	}
	for _, esc := range []string{`\u003c`, `\u003e`, `\u002d`} {
		if !strings.Contains(payload, esc) {
			t.Errorf("payload lacks %s", esc)
		}
	}
	if strings.ContainsAny(payload, "<>") {
		t.Errorf("payload still has a raw < or >: %q", payload)
	}
}

func TestLogBlockKeepsTheKeyOrderAndVersion(t *testing.T) {
	line := LogLine("abc1234", "ci", 7, nil)
	want := `<!-- roborev-log: {"v":1,"sha":"abc1234","bron":"ci","pr":7,"bevindingen":[]} -->`
	if line != want {
		t.Errorf("got %q, want %q", line, want)
	}
}

func TestLogLineEscapesHostileReason(t *testing.T) {
	state := stateOf(finding(t, map[string]any{"uitkomst": "refuted", "reden": "a --> b <x> é | c\nnieuwe regel"}))
	got := mustRender(t, state, "ruw")
	want, err := os.ReadFile(commentFixtureDir + "comment-vijandig.md")
	if err != nil {
		t.Fatal(err)
	}
	if got+"\n" != string(want) {
		t.Errorf("render differs from the Python fixture:\n got %q\nwant %q", got, want)
	}
	second := strings.Split(got, "\n")[1]
	if strings.Contains(second, `\u00e9`) || !strings.Contains(second, "é") {
		t.Errorf("non-ASCII must stay raw: %q", second)
	}
}

func TestRenderStartsWithTheMarkerAndTheLog(t *testing.T) {
	lines := strings.Split(mustRender(t, stateOf(finding(t, nil)), "ruw"), "\n")
	if lines[0] != "<!-- roborev-verwerkt: abc1234 -->" || !strings.HasPrefix(lines[1], "<!-- roborev-log: ") {
		t.Errorf("first lines: %q", lines[:2])
	}
}

func TestRenderWithoutFindingsSaysSo(t *testing.T) {
	out := mustRender(t, stateOf(), "ruw")
	if !strings.Contains(out, "| geen bevindingen | — | — |") || !strings.Contains(out, `"bevindingen":[]`) {
		t.Errorf("got %q", out)
	}
}

func TestRenderPutsTheRawOutputInDetails(t *testing.T) {
	out := mustRender(t, stateOf(), "RUWE TEKST")
	if !strings.Contains(out, "<details><summary>Ruwe roborev-output</summary>") ||
		!strings.HasSuffix(strings.TrimRight(out, "\n "), "</details>") ||
		strings.Index(out, "RUWE TEKST") > strings.LastIndex(out, "</details>") {
		t.Errorf("got %q", out)
	}
}

func longFindings(t *testing.T, n int, reason string) []any {
	var f []any
	for i := 0; i < n; i++ {
		f = append(f, finding(t, map[string]any{
			"path": "f" + strconv.Itoa(i) + ".py", "line": i, "reden": reason, "uitkomst": "left",
		}))
	}
	return f
}

func TestRenderTruncatesOnlyTheRawOutput(t *testing.T) {
	findings := longFindings(t, 30, strings.Repeat("r", 40))
	out := mustRender(t, stateOf(findings...), strings.Repeat("x", 200_000))
	if n := utf8.RuneCountInString(out); n > 65536 {
		t.Errorf("%d runes, over the limit", n)
	}
	for name, part := range map[string]string{
		"log": LogLine("abc1234", "lokaal", 7, findings), "table": table(findings), "note": limitNote,
	} {
		if !strings.Contains(out, part) {
			t.Errorf("%s missing from the truncated comment", name)
		}
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\n "), "</details>") {
		t.Errorf("does not end in </details>")
	}
}

func TestRenderCutsOnRunesNotBytes(t *testing.T) {
	state := stateOf(finding(t, nil))
	got := mustRender(t, state, strings.Repeat("é", 70_000))
	if n := utf8.RuneCountInString(got); n != 65536 {
		t.Fatalf("%d runes, want exactly 65536", n)
	}
	want, err := os.ReadFile(commentFixtureDir + "comment-lang.md")
	if err != nil {
		t.Fatal(err)
	}
	if got+"\n" != string(want) {
		t.Error("render differs from the Python fixture")
	}
	for _, part := range []string{"<!-- roborev-verwerkt: abc1234 -->", LogLine("abc1234", "lokaal", 7, []any{finding(t, nil)}),
		"| `src/a.py:12` | high | fixed in abc1234 |", limitNote + "\n\n</details>"} {
		if !strings.Contains(got, part) {
			t.Errorf("%q missing", part)
		}
	}
}

func TestRenderBasisMatchesTheFixture(t *testing.T) {
	state := stateOf(
		finding(t, nil),
		finding(t, map[string]any{"path": "b.py", "line": 3, "severity": "low", "uitkomst": "refuted", "reden": "klopt niet"}),
		finding(t, map[string]any{"path": nil, "line": nil, "uitkomst": "left", "reden": "later"}),
	)
	want, err := os.ReadFile(commentFixtureDir + "comment-basis.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := mustRender(t, state, "ruwe tekst"); got+"\n" != string(want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestRenderDoesNotTruncateShortOutput(t *testing.T) {
	if strings.Contains(mustRender(t, stateOf(), "kort"), limitNote) {
		t.Error("short raw output was cut")
	}
}

func TestTableRowsPerOutcome(t *testing.T) {
	rows := strings.Split(table([]any{
		finding(t, nil),
		finding(t, map[string]any{"path": "b.py", "line": 3, "severity": "low", "uitkomst": "refuted", "reden": "klopt niet"}),
		finding(t, map[string]any{"path": nil, "line": nil, "uitkomst": "left", "reden": "later"}),
	}), "\n")
	want := []string{
		"| Bevinding | Severity | Uitkomst |",
		"| --- | --- | --- |",
		"| `src/a.py:12` | high | fixed in abc1234 |",
		"| `b.py:3` | low | refuted: klopt niet |",
		"| — | high | left: later |",
	}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q", rows)
	}
}

func TestTableEscapesAPipeInAReason(t *testing.T) {
	row := strings.Split(table([]any{finding(t, map[string]any{"uitkomst": "left", "reden": "a|b"})}), "\n")[2]
	if !strings.HasSuffix(row, `left: a\|b |`) {
		t.Errorf("row %q", row)
	}
}

func TestTableKeepsAMultilineReasonOnOneRow(t *testing.T) {
	reason := "regel een\nregel twee\r\nregel drie"
	f := finding(t, map[string]any{"uitkomst": "left", "reden": reason, "severity": "hi\ngh"})
	rows := strings.Split(table([]any{f}), "\n")
	if len(rows) != 3 || rows[2] != "| `src/a.py:12` | hi gh | left: regel een regel twee regel drie |" {
		t.Errorf("rows %q", rows)
	}
	var parsed struct {
		Findings []struct {
			Reason string `json:"reden"`
		} `json:"bevindingen"`
	}
	if err := json.Unmarshal([]byte(logPayload(t, LogLine("abc1234", "lokaal", 7, []any{f}))), &parsed); err != nil || parsed.Findings[0].Reason != reason {
		t.Errorf("log does not round-trip the reason: %v %+v", err, parsed)
	}
}

func TestRenderRefusesWhenLogAndTableAloneExceedTheLimit(t *testing.T) {
	_, err := Render(stateOf(longFindings(t, 1000, strings.Repeat("r", 100))...), "ruw", 7, "lokaal")
	if err == nil || !strings.Contains(err.Error(), "65536") {
		t.Errorf("err = %v, want one naming 65536", err)
	}
}

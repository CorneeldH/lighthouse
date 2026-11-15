package unblock

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/procrun"
)

// TestQuoteAllEncodesEverythingButTheUnreservedSet pins quote(s, safe=”): url.PathEscape would
// leave `:@&=+$,;` alone and point the DELETE at another path.
func TestQuoteAllEncodesEverythingButTheUnreservedSet(t *testing.T) {
	for in, want := range map[string]string{
		"blocked-issue":       "blocked-issue",
		"3: blocked/issue +x": "3%3A%20blocked%2Fissue%20%2Bx",
		"a@b&c=d$e,f;g":       "a%40b%26c%3Dd%24e%2Cf%3Bg",
		"A_z.9-~":             "A_z.9-~",
		"é":                   "%C3%A9",
	} {
		if got := quoteAll(in); got != want {
			t.Errorf("quoteAll(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPyReprQuotesLikePython(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want string
	}{
		{"it's", `"it's"`},
		{`it's "x"`, `'it\'s "x"'`},
		{"a\nb\\", `'a\nb\\'`},
		{[]string{"gh", "api"}, "['gh', 'api']"},
	} {
		if got := pyRepr(tc.in); got != tc.want {
			t.Errorf("pyRepr(%v) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestDecodeWordsErrorsLikeJSONLoads(t *testing.T) {
	for in, want := range map[string]string{
		"":           "Expecting value: line 1 column 1 (char 0)",
		"\n  nope":   "Expecting value: line 2 column 3 (char 3)",
		`{} extra`:   "Extra data: line 1 column 4 (char 3)",
		"[1]\n\n  x": "Extra data: line 3 column 3 (char 7)",
	} {
		_, err := decode(in)
		if err == nil || err.Error() != want {
			t.Errorf("decode(%q) error = %v, want %q", in, err, want)
		}
	}
	if v, err := decode(" [1, 2]\n"); err != nil || len(v.([]any)) != 2 {
		t.Errorf("decode(valid) = %v, %v", v, err)
	}
}

// TestMissingGhIsAReadError covers what no fixture can reach: the fake gh is always on the PATH
// of the Python runner. The script crashed with a traceback; the port reports a red run.
func TestMissingGhIsAReadError(t *testing.T) {
	missing := func(context.Context, string, *string, string, ...string) (procrun.Result, error) {
		return procrun.Result{}, &procrun.NotFound{Name: "gh"}
	}
	code, lines := Run(context.Background(), missing, 1, "blocked-issue", false)
	want := "**Error:** #1 not read: gh: not found"
	if code != 1 || lines[len(lines)-1] != want {
		t.Errorf("Run = %d, %q; want 1, %q", code, lines, want)
	}
}

func TestReleaseLineIsWhatCloseIssueReads(t *testing.T) {
	if ReleaseLine != "Released lane after this close:" {
		t.Errorf("ReleaseLine = %q", ReleaseLine)
	}
	skill, err := readFile("../../pack/core/skills/close-issue/claude/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(skill, ReleaseLine) {
		t.Errorf("close-issue no longer names %q", ReleaseLine)
	}
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}

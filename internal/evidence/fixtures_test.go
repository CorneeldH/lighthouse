package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// defaultTestDir is `[test_gate] evidence_test_dir` of a case without one (the config default).
const defaultTestDir = "tests/"

// fixtureCase is one case of tests/pack/fixtures/evidence/cases.json; the format is described in
// tests/pack/test_evidence_fixtures.py.
type fixtureCase struct {
	Name            string          `json:"name"`
	Kind            string          `json:"kind"`
	Cell            string          `json:"cell"`
	EvidenceTestDir *string         `json:"evidence_test_dir"`
	Expect          json.RawMessage `json:"expect"`
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// useHeadings points the headings lookup at the pack's headings.toml.
func useHeadings(t *testing.T) {
	t.Helper()
	path, err := filepath.Abs("../../pack/core/headings.toml")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_HEADINGS", path)
}

// splitResult is the `split` form of SplitCommand: the argv, or {"reason": …}.
func splitResult(cell string) any {
	argv, reason := SplitCommand(cell)
	if reason != "" {
		return map[string]any{"reason": reason}
	}
	out := make([]any, len(argv))
	for i, a := range argv {
		out[i] = a
	}
	return out
}

// checkResult is `evidence.py check`: nil when allowed, otherwise the reason.
func checkResult(c fixtureCase) any {
	argv, reason := SplitCommand(c.Cell)
	if reason == "" {
		testDir := defaultTestDir
		if c.EvidenceTestDir != nil {
			testDir = *c.EvidenceTestDir
		}
		reason = Check(argv, testDir)
	}
	if reason == "" {
		return nil
	}
	return reason
}

func TestFixtures(t *testing.T) {
	useHeadings(t)
	var cases []fixtureCase
	readJSON(t, "../../tests/pack/fixtures/evidence/cases.json", &cases)
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			var want any
			if err := json.Unmarshal(c.Expect, &want); err != nil {
				t.Fatal(err)
			}
			var got any
			switch c.Kind {
			case "split":
				got = splitResult(c.Cell)
			case "check":
				got = checkResult(c)
			case "expected":
				fragment, afterPR := Expected(c.Cell)
				got = []any{fragment, afterPR}
			case "outcome_of":
				if key := OutcomeOf(c.Cell); key != "" {
					got = key
				}
			default:
				t.Fatalf("unknown kind %q", c.Kind)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("cell %q:\n got %#v\nwant %#v", c.Cell, got, want)
			}
		})
	}
}

// Besluit 4: the Go sets are the constants of evidence.py, as allowlist.json records them.
func TestAllowlistMatchesFixture(t *testing.T) {
	var want map[string][]string
	readJSON(t, "../../tests/pack/fixtures/evidence/allowlist.json", &want)
	got := map[string][]string{}
	for name, values := range Allowlist {
		got[name] = slices.Sorted(slices.Values(values))
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Allowlist differs from allowlist.json:\n got %v\nwant %v", got, want)
	}
}

// The expected values come from python3 -c 'print(repr(…))'.
func TestPyRepr(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"a", `'a'`},
		{"it's", `"it's"`},
		{`a"b'c`, `'a"b\'c'`},
		{"\n", `'\n'`},
		{"\x00", `'\x00'`},
		{"\\\r\t", `'\\\r\t'`},
		{"é\u200b😀\x7f\u00a0", `'é\u200b😀\x7f\xa0'`},
		{"\U000e0001", `'\U000e0001'`},
	} {
		if got := pyRepr(tc.in); got != tc.want {
			t.Errorf("pyRepr(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

// Python's lexer edges that the fixtures do not reach through SplitCommand: the raw tokens.
func TestSplitPosix(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{`a"b c"d`, []string{"ab cd"}},
		{`"a\nb" "a\\b" 'a\b'`, []string{`a\nb`, `a\b`, `a\b`}},
		{`\a\ b`, []string{"a b"}},
		{"a;b", []string{"a", ";", "b"}},
		{"a&&b||c", []string{"a", "&&", "b", "||", "c"}},
		{"(a)'b'", []string{"(", "a", ")", "b"}},
		{"''|x", []string{"", "|", "x"}},
		{"a\vb\fc", []string{"a\vb\fc"}},
		{"  ", nil},
		{"x# y", []string{"x#", "y"}},
	} {
		got, err := splitPosix(tc.in)
		if err != nil {
			t.Errorf("splitPosix(%q): %v", tc.in, err)
			continue
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("splitPosix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

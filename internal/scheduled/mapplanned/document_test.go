package mapplanned

import (
	"reflect"
	"strings"
	"testing"
)

// Classification: pure function. Fails if Issues stops reading integers under the key "issue"
// from a JSON document.
func TestIssuesFindsTheIssueKeyInJSON(t *testing.T) {
	got, err := Issues("loop.json", []byte(`{"chips":[{"id":"auto-answer","status":"planned","issue":168}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := map[int]bool{168: true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Classification: pure function. Fails if Issues stops reading integers under the key "issue"
// from a YAML document, or picks the format by anything but the extension.
func TestIssuesFindsTheIssueKeyInYAML(t *testing.T) {
	doc := "onderdelen:\n  - id: live\n    stand: deels\n    issue: 987\n"
	for _, path := range []string{"kaart.yaml", "kaart.yml"} {
		got, err := Issues(path, []byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		if want := map[int]bool{987: true}; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: got %v, want %v", path, got, want)
		}
	}
}

// Classification: pure function. Fails if Issues counts a number under another key, a
// fractional or string value under "issue", instead of only whole numbers under "issue".
func TestIssuesIgnoresOtherNumbers(t *testing.T) {
	got, err := Issues("loop.json", []byte(`{"min_lines": 168, "chips":[{"issue": 5},{"issue": "7"},{"issue": 2.5}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := map[int]bool{5: true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Classification: pure function. Fails if Issues accepts another extension or a broken
// document, or leaves the path out of the error.
func TestIssuesRefusesOtherFormatsAndBrokenDocuments(t *testing.T) {
	for _, path := range []string{"kaart.md", "stuk.json"} {
		_, err := Issues(path, []byte(`{"issue": `))
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Fatalf("%s: err = %v, want an error naming the path", path, err)
		}
	}
}

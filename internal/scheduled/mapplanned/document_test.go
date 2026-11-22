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

// Classification: pure function. Fails if Issues reads only the first value of a document and
// ignores what follows: a second JSON value or a second YAML document could hide an item from
// Check or count one the parser of the site never sees.
func TestIssuesRefusesASecondValue(t *testing.T) {
	for path, doc := range map[string]string{
		"loop.json":  `{"issue": 5} {"issue": 6}`,
		"trail.json": `{"issue": 5} x`,
		"kaart.yaml": "issue: 5\n---\nissue: 6\n",
	} {
		if _, err := Issues(path, []byte(doc)); err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("%s: err = %v, want an error naming the path", path, err)
		}
	}
	for path, doc := range map[string]string{
		"loop.json":  "{\"issue\": 5}\n\n",
		"kaart.yaml": "---\nissue: 5\n",
		"leeg.yaml":  "",
	} {
		if _, err := Issues(path, []byte(doc)); err != nil {
			t.Errorf("%s: err = %v, want nil", path, err)
		}
	}
}

// Classification: pure function. Fails if Issues skips a YAML mapping with a key that is not a
// string (yaml.v3 decodes it as map[any]any) and so misses the issue under it.
func TestIssuesWalksAYAMLMapWithOtherKeys(t *testing.T) {
	got, err := Issues("kaart.yaml", []byte("1:\n  - issue: 7\n2: {issue: 8}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := map[int]bool{7: true, 8: true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

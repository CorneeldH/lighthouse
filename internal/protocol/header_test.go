package protocol

import (
	"encoding/json"
	"maps"
	"os"
	"testing"
)

type headerCase struct {
	Name   string            `json:"name"`
	Body   string            `json:"body"`
	Kind   *string           `json:"kind"`
	Fields map[string]string `json:"fields"`
}

// The fixture is shared with tests/pack/test_protocol.py: both parsers must agree.
func TestParseHeaderSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../tests/pack/fixtures/protocol/headers.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []headerCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("fixture has no cases")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			header, ok := ParseHeader(c.Body)
			if c.Kind == nil {
				if ok {
					t.Fatalf("ParseHeader(%q) = %+v, want no header", c.Body, header)
				}
				return
			}
			if !ok {
				t.Fatalf("ParseHeader(%q) found no header, want kind %s", c.Body, *c.Kind)
			}
			if string(header.Kind) != *c.Kind {
				t.Fatalf("kind = %q, want %q", header.Kind, *c.Kind)
			}
			if c.Fields != nil && !maps.Equal(header.Fields, c.Fields) {
				t.Errorf("fields = %v, want %v", header.Fields, c.Fields)
			}
		})
	}
}

// An empty table would make every comment "not a header" and still pass the null cases.
func TestVariantsFromHeadings(t *testing.T) {
	want := map[Kind][]string{
		KindHandoff:  {"Handoff"},
		KindStarted:  {"Gestart", "Started"},
		KindQuestion: {"Vraag", "Question"},
		KindResumed:  {"Hervat", "Resumed"},
		KindDone:     {"Klaar", "Done"},
		KindReturned: {"Teruggegeven", "Returned"},
		KindRebased:  {"Herbaseerd", "Rebased"},
	}
	for kind, variants := range want {
		got := map[string]bool{}
		for _, v := range kindVariants() {
			if v.kind == kind {
				got[v.text] = true
			}
		}
		for _, v := range variants {
			if !got[v] {
				t.Errorf("kind %s lacks variant %q", kind, v)
			}
		}
	}
}

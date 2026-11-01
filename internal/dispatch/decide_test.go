package dispatch

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/protocol"
)

// fixture is one file of testdata/decide: an input and the actions Decide must return.
type fixture struct {
	Input Input    `json:"input"`
	Want  []Action `json:"want"`
}

// decideFixtures reads every file of testdata/decide, by name without .json.
func decideFixtures(t *testing.T) map[string]fixture {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "decide", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no fixtures in testdata/decide")
	}
	out := make(map[string]fixture, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		var f fixture
		if err := dec.Decode(&f); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if f.Want == nil {
			t.Fatalf("%s has no want", path)
		}
		out[strings.TrimSuffix(filepath.Base(path), ".json")] = f
	}
	return out
}

func TestDecideFixtures(t *testing.T) {
	for name, f := range decideFixtures(t) {
		t.Run(name, func(t *testing.T) {
			got := Decide(f.Input)
			if !reflect.DeepEqual(got, f.Want) {
				gotJSON, _ := json.MarshalIndent(got, "", "  ")
				wantJSON, _ := json.MarshalIndent(f.Want, "", "  ")
				t.Errorf("Decide:\ngot  %s\nwant %s", gotJSON, wantJSON)
			}
		})
	}
}

// Apply posts Reason and Skip through protocol.Heading, which panics on an unknown key. Every
// key a fixture expects must be in the headings table, in nl and en. A defer carries the
// literal night or blocker, which apply never looks up.
func TestDecideKeysAreHeadings(t *testing.T) {
	keys := map[string]bool{}
	for _, f := range decideFixtures(t) {
		for _, a := range f.Want {
			if a.Reason != "" && a.Kind != KindDefer {
				keys[a.Reason] = true
			}
			if a.Skip != "" {
				keys[a.Skip] = true
			}
		}
	}
	if len(keys) == 0 {
		t.Fatal("no Reason or Skip in any fixture")
	}
	for key := range keys {
		for _, language := range []string{"nl", "en"} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("Heading(%q, %q) panics: %v", key, language, r)
					}
				}()
				if protocol.Heading(key, language) == "" {
					t.Errorf("Heading(%q, %q) is empty", key, language)
				}
			}()
		}
	}
}

// A handoff branch reaches git as an argument: only a plain ref name passes.
func TestValidBranch(t *testing.T) {
	tests := []struct {
		branch string
		want   bool
	}{
		{"issue-7-bouw-de-keten", true},
		{"feature/a.b_c-1", true},
		{"", false},
		{"-x", false},
		{"--upload-pack=x", false},
		{"a..b", false},
		{"a b", false},
		{"a;b", false},
		{"$(x)", false},
		{"a`b`", false},
		{"a\nb", false},
	}
	for _, tt := range tests {
		if got := validBranch(tt.branch); got != tt.want {
			t.Errorf("validBranch(%q) = %v, want %v", tt.branch, got, tt.want)
		}
	}
}

package mdlinks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const fixturePath = "../../tests/pack/fixtures/md_links/links.json"

type textCase[T any] struct {
	Name   string `json:"name"`
	Text   string `json:"text"`
	Expect T      `json:"expect"`
}

type problemCase struct {
	Name   string            `json:"name"`
	Files  map[string]string `json:"files"`
	Dirs   []string          `json:"dirs"`
	MD     string            `json:"md"`
	Expect [][2]string       `json:"expect"`
}

type fixtures struct {
	StripCode []textCase[string]   `json:"strip_code"`
	Relative  []textCase[[]string] `json:"relative"`
	Absolute  []textCase[[]string] `json:"absolute"`
	Problems  []problemCase        `json:"problems"`
}

func loadFixtures(t *testing.T) fixtures {
	t.Helper()
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var f fixtures
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.StripCode) == 0 || len(f.Relative) == 0 || len(f.Absolute) == 0 || len(f.Problems) == 0 {
		t.Fatalf("a fixture section is empty: %d/%d/%d/%d",
			len(f.StripCode), len(f.Relative), len(f.Absolute), len(f.Problems))
	}
	return f
}

func sameList(got, want []string) bool {
	return len(got) == 0 && len(want) == 0 || reflect.DeepEqual(got, want)
}

// TestFixtures runs the cases of tests/pack/fixtures/md_links, written with the former Python link
// parser as the oracle.
func TestFixtures(t *testing.T) {
	f := loadFixtures(t)
	t.Run("strip_code", func(t *testing.T) {
		for _, c := range f.StripCode {
			t.Run(c.Name, func(t *testing.T) {
				if got := StripCode(c.Text); got != c.Expect {
					t.Errorf("StripCode(%q) = %q, want %q", c.Text, got, c.Expect)
				}
			})
		}
	})
	t.Run("relative", func(t *testing.T) {
		for _, c := range f.Relative {
			t.Run(c.Name, func(t *testing.T) {
				if got := RelativeLinks(c.Text); !sameList(got, c.Expect) {
					t.Errorf("RelativeLinks(%q) = %q, want %q", c.Text, got, c.Expect)
				}
			})
		}
	})
	t.Run("absolute", func(t *testing.T) {
		for _, c := range f.Absolute {
			t.Run(c.Name, func(t *testing.T) {
				if got := AbsoluteLinks(c.Text); !sameList(got, c.Expect) {
					t.Errorf("AbsoluteLinks(%q) = %q, want %q", c.Text, got, c.Expect)
				}
			})
		}
	})
	t.Run("problems", func(t *testing.T) {
		for _, c := range f.Problems {
			t.Run(c.Name, func(t *testing.T) {
				root := t.TempDir()
				for rel, body := range c.Files {
					p := filepath.Join(root, rel)
					if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				for _, d := range c.Dirs {
					if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				got, err := FindProblems(filepath.Join(root, c.MD), root)
				if err != nil {
					t.Fatal(err)
				}
				var pairs [][2]string
				for _, p := range got {
					pairs = append(pairs, [2]string{p.Kind, p.Target})
				}
				if len(pairs) == 0 && len(c.Expect) == 0 {
					return
				}
				if !reflect.DeepEqual(pairs, c.Expect) {
					t.Errorf("FindProblems = %v, want %v", pairs, c.Expect)
				}
			})
		}
	})
}

func TestFindProblemsReadError(t *testing.T) {
	root := t.TempDir()
	if _, err := FindProblems(filepath.Join(root, "missing.md"), root); err == nil {
		t.Fatal("want an error for an unreadable file, got nil")
	}
}

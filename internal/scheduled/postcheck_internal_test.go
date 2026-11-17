package scheduled

import (
	"reflect"
	"strings"
	"testing"
)

// Classification: pure-logic. Fails if a porcelain entry is dropped (a deletion, the original
// path of a rename or copy) or a field is misread.
func TestParsePorcelain(t *testing.T) {
	cases := map[string]struct {
		in   string
		want []string
	}{
		"empty":             {in: "", want: nil},
		"modify and add":    {in: " M out/a.md\x00?? out/b c.md\x00", want: []string{"out/a.md", "out/b c.md"}},
		"deletion":          {in: " D out/keep.md\x00D  out/gone.md\x00", want: []string{"out/keep.md", "out/gone.md"}},
		"rename both":       {in: "R  out/new.md\x00src/old.md\x00 M out/x.md\x00", want: []string{"out/new.md", "src/old.md", "out/x.md"}},
		"copy both":         {in: "C  out/copy.md\x00src/orig.md\x00", want: []string{"out/copy.md", "src/orig.md"}},
		"worktree rename":   {in: " R out/new.md\x00src/old.md\x00", want: []string{"out/new.md", "src/old.md"}},
		"newline in a name": {in: "?? out/a\nb.md\x00", want: []string{"out/a\nb.md"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := parsePorcelain(tc.in)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parsePorcelain = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	for _, bad := range []string{"M\x00", "R  out/new.md\x00", " M out/a.md"} {
		if got, err := parsePorcelain(bad); err == nil {
			t.Errorf("parsePorcelain(%q) = %q, want an error", bad, got)
		}
	}
}

// Classification: pure-logic. Fails if a name-status entry is dropped or misread.
func TestParseNameStatus(t *testing.T) {
	got, err := parseNameStatus("M\x00out/a.md\x00D\x00out/b.md\x00A\x00src/c.go\x00R100\x00x\x00y\x00")
	want := []string{"out/a.md", "out/b.md", "src/c.go", "x", "y"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("parseNameStatus = %q, %v; want %q", got, err, want)
	}
	if _, err := parseNameStatus("M\x00"); err == nil {
		t.Error("parseNameStatus accepted a status without a path")
	}
}

// Classification: pure-logic. Fails if a path that escapes the work tree or is not in its clean
// form passes, even when Allowed (a prefix match) would accept it.
func TestPathEscapes(t *testing.T) {
	for _, p := range []string{"", "/etc/passwd", "../x", "out/../../x", "out/../src/x", "out//a.md", "out/./a.md", "out/sub/", "."} {
		if !pathEscapes(p) {
			t.Errorf("pathEscapes(%q) = false", p)
		}
	}
	for _, p := range []string{"out/a.md", "a", "out/..a.md", strings.Repeat("d/", 3) + "f"} {
		if pathEscapes(p) {
			t.Errorf("pathEscapes(%q) = true", p)
		}
	}
}

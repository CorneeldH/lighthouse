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

// Classification: pure-logic. Fails if a diff-tree entry is dropped, or its path, new mode or
// new object is read from the wrong column (a deletion must keep mode 000000).
func TestParseDiffTree(t *testing.T) {
	z := strings.Repeat("0", 40)
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	in := ":100644 100644 " + a + " " + b + " M\x00out/a.md\x00" +
		":100644 000000 " + a + " " + z + " D\x00out/gone.md\x00" +
		":000000 120000 " + z + " " + b + " A\x00out/link\x00"
	got, err := parseDiffTree(in)
	want := []treeChange{
		{path: "out/a.md", mode: "100644", object: b},
		{path: "out/gone.md", mode: "000000", object: z},
		{path: "out/link", mode: "120000", object: b},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("parseDiffTree = %+v, %v; want %+v", got, err, want)
	}
	for _, bad := range []string{":100644 100644 " + a + " " + b + " M\x00", "100644 100644 x y M\x00p\x00", ":100644 x M\x00p\x00"} {
		if got, err := parseDiffTree(bad); err == nil {
			t.Errorf("parseDiffTree(%q) = %+v, want an error", bad, got)
		}
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

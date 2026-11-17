package scheduled

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
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

// Classification: pure-logic. Fails if a parse error echoes a file name that holds a token.
func TestParsePorcelainWithholdsNames(t *testing.T) {
	token := "ghp_" + strings.Repeat("a", 36)
	for _, bad := range []string{"R  out/" + token + ".md\x00", "XYZout/" + token + ".md\x00"} {
		_, err := parsePorcelain(bad)
		if err == nil || strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "(file name withheld: GitHub personal token)") {
			t.Errorf("parsePorcelain(%q) error = %v", bad, err)
		}
	}
}

// Classification: pure-logic. Fails if a git error is labelled with a global option instead of
// the subcommand.
func TestSubcommand(t *testing.T) {
	cases := map[string][]string{
		"commit-tree": {"-c", "user.name=x", "-c", "user.email=y", "commit-tree", "t"},
		"status":      {"status", "-z"},
		"git":         {"-c", "a=b"},
	}
	for want, args := range cases {
		if got := subcommand(args); got != want {
			t.Errorf("subcommand(%q) = %q, want %q", args, got, want)
		}
	}
}

// Classification: pure-logic. Fails if a git failure (could not run, or a non-zero exit) is
// labelled with a global option instead of the subcommand.
func TestGitErrorLabel(t *testing.T) {
	cases := map[string]struct {
		code int
		err  error
		want string
	}{
		"could not run": {code: -1, err: errors.New("boom"), want: "git commit-tree: boom"},
		"exit code":     {code: 128, want: "git commit-tree: exit code 128"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			g := gitIn{dir: "/w", run: func(context.Context, time.Duration, string, ...string) (string, int, error) {
				return "", tc.code, tc.err
			}}
			_, err := g.ok(context.Background(), "-c", "user.name=x", "commit-tree", "t")
			if err == nil || err.Error() != tc.want {
				t.Errorf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

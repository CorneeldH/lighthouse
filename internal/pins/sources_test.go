package pins

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const sha = "0123456789abcdef0123456789abcdef01234567"

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func skillDoc(meta string) string {
	return "---\nname: x\nmetadata:\n" + meta + "---\n\nbody\n"
}

func upstreamMeta(upstream, commit, path string) string {
	m := "  origin: external\n"
	if upstream != "" {
		m += "  upstream: " + upstream + "\n"
	}
	if commit != "" {
		m += "  upstream-commit: \"" + commit + "\"\n"
	}
	if path != "" {
		m += "  upstream-path: " + path + "\n"
	}
	return m
}

func TestFindSourcePins_variantsAreOnePin(t *testing.T) {
	root := t.TempDir()
	doc := skillDoc(upstreamMeta("https://github.com/o/r (MIT)", sha, "skills/x"))
	writeFile(t, root, "pack/core/skills/x/claude/SKILL.md", doc)
	writeFile(t, root, "pack/core/skills/x/agents/SKILL.md", doc)
	writeFile(t, root, "pack/core/skills/own/SKILL.md", skillDoc("  origin: own\n"))

	got, err := FindSourcePins(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []SourcePin{{
		Key: "core/skills/x", Repo: "o/r", Commit: sha, Path: "skills/x",
		Files: []string{"pack/core/skills/x/agents/SKILL.md", "pack/core/skills/x/claude/SKILL.md"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestFindSourcePins_variantsDisagreeIsError(t *testing.T) {
	for name, other := range map[string]string{
		"upstream": upstreamMeta("https://github.com/o/other", sha, "skills/x"),
		"commit":   upstreamMeta("https://github.com/o/r", strings.Repeat("a", 40), "skills/x"),
		"path":     upstreamMeta("https://github.com/o/r", sha, "skills/y"),
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "pack/core/skills/x/claude/SKILL.md", skillDoc(upstreamMeta("https://github.com/o/r", sha, "skills/x")))
			writeFile(t, root, "pack/core/skills/x/agents/SKILL.md", skillDoc(other))
			got, err := FindSourcePins(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Err == nil || got[0].Key != "core/skills/x" {
				t.Fatalf("want one pin with Err, got %+v", got)
			}
		})
	}
}

func TestFindSourcePins_ignoresInstalledCopies(t *testing.T) {
	root := t.TempDir()
	doc := skillDoc(upstreamMeta("https://github.com/o/r", sha, "skills/x"))
	writeFile(t, root, "pack/core/skills/x/SKILL.md", doc)
	writeFile(t, root, ".claude/skills/x/SKILL.md", doc)
	writeFile(t, root, ".agents/skills/x/SKILL.md", doc)
	got, err := FindSourcePins(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Err != nil {
		t.Fatalf("want one clean pin, got %+v", got)
	}
}

func TestFindSourcePins_normalizesFields(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pack/core/skills/x/SKILL.md",
		skillDoc(upstreamMeta("https://github.com/o/r.git", sha, "/skills/x/")))
	got, err := FindSourcePins(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Err != nil || got[0].Repo != "o/r" || got[0].Path != "skills/x" {
		t.Fatalf("got %+v", got)
	}
}

func TestFindSourcePins_sourcesToml(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pack/SOURCES.toml", `
[[file]]
path = "pack/core/lib/a.py"
new = true

[[file]]
path = "pack/core/skills/x/SKILL.md"
pin = "core/skills/x"

[[file]]
path = "pack/core/lib/b.py"
source_repo = "o/r"
source_sha = "`+sha+`"
source_path = "/src/b.py"
`)
	got, err := FindSourcePins(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []SourcePin{{
		Key: "pack/core/lib/b.py", Repo: "o/r", Commit: sha, Path: "src/b.py",
		Files: []string{"pack/core/lib/b.py"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestFindSourcePins_missingOrInvalidFieldIsError(t *testing.T) {
	cases := map[string]string{
		"missing commit":   skillDoc(upstreamMeta("https://github.com/o/r", "", "skills/x")),
		"short sha":        skillDoc(upstreamMeta("https://github.com/o/r", sha[:39], "skills/x")),
		"external no path": skillDoc(upstreamMeta("https://github.com/o/r", sha, "")),
		"bad upstream":     skillDoc(upstreamMeta("https://example.com/x", sha, "skills/x")),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "pack/core/skills/x/SKILL.md", doc)
			got, err := FindSourcePins(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Err == nil {
				t.Fatalf("want one pin with Err, got %+v", got)
			}
		})
	}
	t.Run("sources.toml bad sha", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "pack/SOURCES.toml", "[[file]]\npath = \"pack/a\"\nsource_repo = \"o/r\"\nsource_sha = \"abc\"\nsource_path = \"a\"\n")
		got, err := FindSourcePins(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Err == nil {
			t.Fatalf("want one pin with Err, got %+v", got)
		}
	})
}

func TestRepoFromUpstream(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/o/r (MIT)":                                "o/r",
		"https://github.com/o/r.git":                                  "o/r",
		"https://github.com/o/r/tree/main/x":                          "o/r",
		"https://github.com/o/r, MIT":                                 "o/r",
		"see https://github.com/o/r (MIT) and https://github.com/p/q": "o/r",
	} {
		got, err := RepoFromUpstream(in)
		if err != nil || got != want {
			t.Errorf("RepoFromUpstream(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"https://example.com/x", "", "https://github.com/onlyowner"} {
		if _, err := RepoFromUpstream(in); err == nil {
			t.Errorf("RepoFromUpstream(%q): want error", in)
		}
	}
}

func TestNormalizePath(t *testing.T) {
	for in, want := range map[string]string{"/a/b/": "a/b", "a/b": "a/b", "//a//": "a", "": ""} {
		if got := normalizePath(in); got != want {
			t.Errorf("normalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

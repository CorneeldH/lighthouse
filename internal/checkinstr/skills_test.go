package checkinstr

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runRepo runs every check on root with an empty lighthouse.toml outside it.
func runRepo(t *testing.T, root string) (code int, stdout string) {
	t.Helper()
	toml := filepath.Join(t.TempDir(), "lighthouse.toml")
	writeFile(t, toml, "")
	t.Setenv("LIGHTHOUSE_TOML", toml)
	var out, errOut bytes.Buffer
	code = Run(root, filepath.Join(t.TempDir(), "personal"), &out, &errOut)
	if errOut.Len() != 0 {
		t.Errorf("stderr: %q, want nothing", errOut.String())
	}
	return code, out.String()
}

// Red when a frontmatter value is read differently from the awk of the script: a block scalar
// read as its header, an empty value read as present, a field outside the frontmatter found.
func TestFrontmatterField(t *testing.T) {
	cases := []struct {
		name, text, field, want string
	}{
		{"plain", "---\nname: demo\n---\n", "name", "demo"},
		{"empty value is missing", "---\nupstream: \nname: demo\n---\n", "upstream", ""},
		{"no space is missing", "---\nupstream:\n---\n", "upstream", ""},
		{"folded", "---\ndescription: >\n  one\n  two\n\n  three\nname: demo\n---\n", "description", "one two\nthree"},
		{"folded with chomping", "---\ndescription: >-\n  one\n  two\n---\n", "description", "one two"},
		{"literal", "---\ndescription: |\n  one\n  two\n---\n", "description", "one\ntwo"},
		{"block at end of file", "---\ndescription: >\n  one\n  two", "description", "one two"},
		{"trailing empty lines dropped", "---\ndescription: |\n  one\n\n\n---\n", "description", "one"},
		{"after the frontmatter", "---\nname: demo\n---\ndescription: body\n", "description", ""},
		{"no frontmatter", "name: demo\n", "name", ""},
		{"value kept as written", "---\nname:  two  spaces \n---\n", "name", " two  spaces "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "SKILL.md")
			writeFile(t, path, tc.text)
			if got := frontmatterField(path, tc.field); got != tc.want {
				t.Errorf("frontmatterField(%q) = %q, want %q", tc.field, got, tc.want)
			}
		})
	}
}

// Red when a key outside the `metadata:` block counts, or a key after the block ended.
func TestSkillMeta(t *testing.T) {
	cases := []struct {
		name, text, want string
	}{
		{"under metadata", "---\nname: x\nmetadata:\n  binding: hard\n---\n", "hard"},
		{"top level does not count", "---\nbinding: hard\n---\n", ""},
		{"block ended by a top-level key", "---\nmetadata:\n  a: b\nname: x\n  binding: hard\n---\n", ""},
		{"empty line keeps the block", "---\nmetadata:\n  a: b\n\n\tbinding: hard\n---\n", "hard"},
		{"after the frontmatter", "---\nmetadata:\n  a: b\n---\n  binding: hard\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "SKILL.md"), tc.text)
			if got := skillMeta(dir, "binding"); got != tc.want {
				t.Errorf("skillMeta = %q, want %q", got, tc.want)
			}
		})
	}
}

// Red when a symlinked skill root or a symlinked skill directory is followed: `find <dir>` in the
// script follows neither, so neither carries a skill.
func TestSymlinkedSkillsAreNotFollowed(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(t.TempDir(), "skills")
	writeFile(t, filepath.Join(real, "solo", "SKILL.md"), "---\nname: solo\n---\n")
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, ".claude", "skills")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".agents", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(real, "solo"), filepath.Join(root, ".agents", "skills", "linked")); err != nil {
		t.Fatal(err)
	}

	code, out := runRepo(t, root)
	if code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, out)
	}
	for _, want := range []string{
		"ok    no names claimed twice\n",
		"ok    mirror .claude/skills <-> .agents/skills matches\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

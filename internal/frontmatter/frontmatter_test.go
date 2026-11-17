package frontmatter

import (
	"os"
	"path/filepath"
	"testing"
)

// Classification: this is a parser change; the likely regression surface is a block scalar read
// as its header, an empty value read as present, or a field outside the frontmatter found.

func write(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFieldFoldedAndLiteral(t *testing.T) {
	cases := []struct {
		name, text, want string
	}{
		{"folded", "---\ndescription: >\n  one\n  two\n---\n", "one two"},
		{"folded paragraphs", "---\ndescription: >\n  one\n  two\n\n  three\nname: demo\n---\n", "one two\nthree"},
		{"folded with chomping", "---\ndescription: >-\n  one\n  two\n---\n", "one two"},
		{"literal", "---\ndescription: |\n  one\n  two\n---\n", "one\ntwo"},
		{"block at end of file", "---\ndescription: >\n  one\n  two", "one two"},
		{"trailing empty lines dropped", "---\ndescription: |\n  one\n\n\n---\n", "one"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Field(write(t, tc.text), "description"); got != tc.want {
				t.Errorf("Field = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFieldMissingIsEmpty(t *testing.T) {
	cases := []struct {
		name, text, field string
	}{
		{"absent", "---\nname: demo\n---\n", "description"},
		{"empty value", "---\nupstream: \nname: demo\n---\n", "upstream"},
		{"no space after colon", "---\nupstream:\n---\n", "upstream"},
		{"after the frontmatter", "---\nname: demo\n---\ndescription: body\n", "description"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Field(write(t, tc.text), tc.field); got != "" {
				t.Errorf("Field = %q, want empty", got)
			}
		})
	}
	if got := Field(filepath.Join(t.TempDir(), "missing.md"), "name"); got != "" {
		t.Errorf("Field of a missing file = %q, want empty", got)
	}
}

func TestFieldNoFrontmatter(t *testing.T) {
	for _, text := range []string{"name: demo\n", "", "# Title\n\nname: demo\n"} {
		if got := Field(write(t, text), "name"); got != "" {
			t.Errorf("Field(%q) = %q, want empty", text, got)
		}
	}
}

func TestFieldValueKeptAsWritten(t *testing.T) {
	if got := Field(write(t, "---\nname:  two  spaces \n---\n"), "name"); got != " two  spaces " {
		t.Errorf("Field = %q", got)
	}
}

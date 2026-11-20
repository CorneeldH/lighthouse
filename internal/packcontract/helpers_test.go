package packcontract

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const packRoot = "../../pack"

// readPack reads packRoot/rel and fails the test when it cannot.
func readPack(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(packRoot, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// variants returns the SKILL.md text per variant of a skill directory (relative to packRoot).
// A shared SKILL.md counts for both variants.
func variants(t *testing.T, skillDir string) map[string]string {
	t.Helper()
	if _, err := os.Stat(filepath.Join(packRoot, skillDir, "SKILL.md")); err == nil {
		text := readPack(t, filepath.Join(skillDir, "SKILL.md"))
		return map[string]string{"claude": text, "agents": text}
	}
	return map[string]string{
		"claude": readPack(t, filepath.Join(skillDir, "claude", "SKILL.md")),
		"agents": readPack(t, filepath.Join(skillDir, "agents", "SKILL.md")),
	}
}

// skillDirs lists every pack/*/skills/* directory relative to packRoot, core first.
func skillDirs(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(packRoot, "*", "skills", "*"))
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil && info.IsDir() {
			rel, err := filepath.Rel(packRoot, m)
			if err != nil {
				t.Fatal(err)
			}
			dirs = append(dirs, filepath.ToSlash(rel))
		}
	}
	sort.SliceStable(dirs, func(i, j int) bool {
		ci, cj := strings.HasPrefix(dirs[i], "core/"), strings.HasPrefix(dirs[j], "core/")
		if ci != cj {
			return ci
		}
		return dirs[i] < dirs[j]
	})
	return dirs
}

// unitTexts returns every .md, .sh, .json and .txt file of a skill directory (recursively), or
// the one agent file, keyed by path relative to the unit.
func unitTexts(t *testing.T, unit string) map[string]string {
	t.Helper()
	root := filepath.Join(packRoot, unit)
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	texts := map[string]string{}
	if !info.IsDir() {
		texts[filepath.Base(unit)] = readPack(t, unit)
		return texts
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		switch filepath.Ext(path) {
		case ".md", ".sh", ".json", ".txt":
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			texts[filepath.ToSlash(rel)] = string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return texts
}

// section returns the text from start up to end; it fails when either is missing.
//
//nolint:unused // used by the tests of later tasks of #367
func section(t *testing.T, body, start, end string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, start)
	if !ok {
		t.Fatalf("no %q in the text", start)
	}
	got, _, ok := strings.Cut(rest, end)
	if !ok {
		t.Fatalf("no %q after %q", end, start)
	}
	return start + got
}

// flat collapses all whitespace runs to single spaces.
func flat(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// frontmatter returns the text between the first two --- lines.
func frontmatter(t *testing.T, body string) string {
	t.Helper()
	rest, ok := strings.CutPrefix(body, "---\n")
	if !ok {
		t.Fatal("no frontmatter")
	}
	front, _, ok := strings.Cut(rest, "\n---")
	if !ok {
		t.Fatal("frontmatter is not closed")
	}
	return front
}

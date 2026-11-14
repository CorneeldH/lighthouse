package codelang

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// repoRoot is the repository root, seen from this package.
const repoRoot = "../.."

// scanFiles scans every file in paths as language and returns the findings that are not a
// doubt, as "<path>:<line>: <text>".
func scanFiles(t *testing.T, language string, paths []string) []string {
	t.Helper()
	scorer, err := DefaultScorer()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		found, err := Scan(scorer, language, string(src))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, f := range found {
			if !f.Doubt { // a doubt is not a finding; layer 2 decides
				out = append(out, fmt.Sprintf("%s:%d: %s", path, f.Line, f.Text))
			}
		}
	}
	return out
}

// walk lists the files under dir whose name ends in suffix.
func walk(t *testing.T, dir, suffix string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, suffix) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no %s files under %s", suffix, dir)
	}
	return files
}

func TestCorpusStdlibClean(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(fixtureDir, "stdlib", "*.py"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 10 {
		t.Fatalf("%d stdlib files, want 10", len(files))
	}
	scorer, err := DefaultScorer()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		found, err := Scan(scorer, "python", string(src))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if len(found) != 0 {
			t.Errorf("%s: %+v, want nothing", path, found)
		}
	}
}

func TestCorpusGoEnglish(t *testing.T) {
	files := append(walk(t, filepath.Join(repoRoot, "internal"), ".go"),
		walk(t, filepath.Join(repoRoot, "cmd"), ".go")...)
	if found := scanFiles(t, "go", files); len(found) != 0 {
		t.Errorf("findings:\n%s", strings.Join(found, "\n"))
	}
}

func TestCorpusConfigPyEnglish(t *testing.T) {
	path := filepath.Join(repoRoot, "pack", "core", "lib", "config.py")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s is gone (removed in step 12): %v", path, err)
	}
	if found := scanFiles(t, "python", []string{path}); len(found) != 0 {
		t.Errorf("findings:\n%s", strings.Join(found, "\n"))
	}
}

func TestCorpusHooksEnglish(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(repoRoot, "pack", "core", "hooks", "*.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no hooks under pack/core/hooks")
	}
	if found := scanFiles(t, "shell", files); len(found) != 0 {
		t.Errorf("findings:\n%s", strings.Join(found, "\n"))
	}
}

func TestCorpusPackMarkdownEnglish(t *testing.T) {
	files := walk(t, filepath.Join(repoRoot, "pack"), ".md")
	if found := scanFiles(t, "markdown", files); len(found) != 0 {
		t.Errorf("findings:\n%s", strings.Join(found, "\n"))
	}
}

// TestCorpusRepoEnglish checks the whole repo as the hook sees it: lighthouse.toml decides
// scope and exemptions.
func TestCorpusRepoEnglish(t *testing.T) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(root, "lighthouse.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if v, err := config.Lookup(cfg, "project.code_language"); err != nil || v != "en" {
		t.Fatalf("project.code_language = %v (%v), want en", v, err)
	}
	raw, err := config.Lookup(cfg, "project.code_language_paths")
	if err != nil {
		t.Fatal(err)
	}
	globs := []string{}
	for _, p := range raw.([]any) {
		globs = append(globs, p.(string))
	}
	for _, need := range []string{"pack/**", "internal/**", "cmd/**"} {
		if !slices.Contains(globs, need) {
			t.Fatalf("code_language_paths %v lacks %s", globs, need)
		}
	}
	out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		path := filepath.Join(root, rel)
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			continue
		}
		got, err := Check(DefaultScorer, path, root, "")
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		for _, f := range got.Findings {
			found = append(found, fmt.Sprintf("%s:%d: %s: %s", rel, f.Line, f.Kind, f.Text))
		}
	}
	if len(found) != 0 {
		t.Errorf("findings:\n%s", strings.Join(found, "\n"))
	}
}

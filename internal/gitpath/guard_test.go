package gitpath

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const gitpathImport = `"github.com/CorneeldH/lighthouse/internal/gitpath"`

// markers are the strings that show a file asks git for a path.
var markers = []string{
	`"--show-toplevel"`, `"--git-common-dir"`, `"--git-dir"`, `"--absolute-git-dir"`,
	`"worktree", "list"`,
}

// allowlist holds the files that name a git-path argument without reading a
// path back from git, with the reason. Keys are slash paths under internal/.
var allowlist = map[string]string{
	"levellog/levellog.go":     "hash key of existing logs; its own filepath.Clean already makes the path native",
	"worktreeaudit/collect.go": "reads only the raw porcelain; rows.go converts it and does import gitpath",
	"gitinvocations/find.go":   "parses the global options of a git command line; reads no path from git",
}

// TestEveryGitPathReadUsesGitpath fails for a non-test file under internal/
// that asks git for a path but does not import internal/gitpath.
func TestEveryGitPathReadUsesGitpath(t *testing.T) {
	root := ".."
	used := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if d.Name() == "testdata" || rel == "gitpath" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(b)
		hit := false
		for _, m := range markers {
			if strings.Contains(src, m) {
				hit = true
				break
			}
		}
		if !hit || strings.Contains(src, gitpathImport) {
			return nil
		}
		if _, ok := allowlist[rel]; ok {
			used[rel] = true
			return nil
		}
		t.Errorf("%s asks git for a path but does not import internal/gitpath; use gitpath.Native or add it to the allowlist with a reason", rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel, reason := range allowlist {
		if reason == "" {
			t.Errorf("allowlist entry %s has no reason", rel)
		}
		if !used[rel] {
			t.Errorf("allowlist entry %s is not needed: the file imports gitpath or no longer matches", rel)
		}
	}
}

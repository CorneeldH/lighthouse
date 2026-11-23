package lintcheck

// A go test that runs from a git hook inherits GIT_DIR; without this import the fixture repositories
// of the tests commit in the real repository (#463).
import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	_ "github.com/CorneeldH/lighthouse/internal/gitenvscrub"
)

// writeRepo writes files (slash paths) into a fresh git repository and stages them, because
// importcheck.Discover sees tracked files only.
func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, argv := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		cmd := exec.Command("git", argv...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", argv, err, out)
		}
	}
	return root
}

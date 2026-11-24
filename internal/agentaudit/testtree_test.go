package agentaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

// skipOnWindows skips t on native Windows, where the behaviour under test does not exist.
func skipOnWindows(t *testing.T, why string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip(why + "; see host-assumptions.md")
	}
}

// tree writes files (slash paths relative to a new temporary directory) and returns that
// directory. A path ending in ".sh" is made executable.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeFiles(t, root, files)
	return root
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if filepath.Ext(p) == ".sh" {
			mode = 0o755
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
}

// hostless points HOME and CLAUDE_CONFIG_DIR at fresh temporary directories, so the host
// never counts, and returns the home and the config dir (<home>/.claude).
func hostless(t *testing.T) (home, configDir string) {
	t.Helper()
	home = t.TempDir()
	configDir = filepath.Join(home, ".claude")
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	return home, configDir
}

// treeHash is a hash over every path, mode and content under the given roots.
func treeHash(t *testing.T, roots ...string) string {
	t.Helper()
	var lines []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			line := p + " " + info.Mode().String()
			if d.Type().IsRegular() {
				data, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				sum := sha256.Sum256(data)
				line += " " + hex.EncodeToString(sum[:])
			}
			lines = append(lines, line)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(lines)
	h := sha256.New()
	for _, l := range lines {
		h.Write([]byte(l + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

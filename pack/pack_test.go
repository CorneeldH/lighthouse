package pack

import (
	"io/fs"
	"os"
	"testing"
)

// Every directory under pack/ is a package and lands in the binary, whole.
func TestEmbedsEveryPackageDir(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := fs.Stat(Files, e.Name()); err != nil {
			t.Errorf("package directory %s is not embedded: %v", e.Name(), err)
		}
	}
}

// The files in pack/ itself (pack.go, SOURCES.toml) are not packages and stay out.
func TestEmbedsNoTopLevelFile(t *testing.T) {
	entries, err := fs.ReadDir(Files, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			t.Errorf("top-level file %s is embedded", e.Name())
		}
	}
}

func TestEmbedsHiddenAndUnderscoreFiles(t *testing.T) {
	if _, err := fs.Stat(Files, "core/lib/__init__.py"); err != nil {
		t.Fatal(err)
	}
}

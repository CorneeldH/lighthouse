//go:build !windows

package stubbin

import (
	"os"
	"path/filepath"
	"testing"
)

// Make writes an executable stub name in dir and returns its path.
func Make(t *testing.T, dir, name string, s Spec) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(shText(s)), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

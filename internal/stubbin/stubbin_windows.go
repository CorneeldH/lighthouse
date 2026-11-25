//go:build windows

package stubbin

import (
	"os"
	"path/filepath"
	"testing"
)

// Make writes an executable stub name.cmd in dir and returns its path. The output lives in
// side files next to the stub, so it needs no escaping.
func Make(t *testing.T, dir, name string, s Spec) string {
	t.Helper()
	base := filepath.Join(dir, name)
	for ext, content := range map[string]string{".stdout": s.Stdout, ".stderr": s.Stderr} {
		if err := os.WriteFile(base+ext, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := base + ".cmd"
	if err := os.WriteFile(p, []byte(cmdText(s, base)), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

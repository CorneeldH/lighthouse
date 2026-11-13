package hooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/hook"
)

// call runs f with payload (a string as is, anything else encoded as JSON).
func call(t *testing.T, f hook.Func, stand string, payload any) hook.Result {
	t.Helper()
	raw, ok := payload.(string)
	if !ok {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		raw = string(b)
	}
	return f(context.Background(), "", stand, hook.Payload{Raw: []byte(raw)})
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// emptyPath points PATH at an empty directory: no lighthouse, no git.
func emptyPath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// realDir is dir with its symlinks resolved, as the hooks report paths.
func realDir(t *testing.T, dir string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

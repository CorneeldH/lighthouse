package workplace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "d")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "f.json")
	if err := WriteFileAtomic(p, []byte("x")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil || string(got) != "x" {
		t.Fatalf("content = %q, %v; want x", got, err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if m := st.Mode().Perm(); m != 0o600 {
		t.Errorf("file mode = %o, want 600", m)
	}
	dst, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m := dst.Mode().Perm(); m != 0o700 {
		t.Errorf("dir mode = %o, want 700", m)
	}
	tmps, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(tmps) != 0 {
		t.Errorf("temp files left: %v", tmps)
	}
}

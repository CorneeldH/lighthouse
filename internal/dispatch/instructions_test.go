package dispatch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteInstructions(t *testing.T) {
	run, err := os.ReadFile(filepath.Join("instructions", "run.md"))
	if err != nil {
		t.Fatal(err)
	}
	appendix, err := os.ReadFile(filepath.Join("instructions", "round_limit_question.md"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		atRoundLimit string
		want         string
	}{
		{"stop", string(run)},
		{"", string(run)},
		{"vraag", string(run) + string(appendix)},
	}
	for _, tc := range cases {
		t.Run("at round limit "+tc.atRoundLimit, func(t *testing.T) {
			state := t.TempDir()
			path, err := WriteInstructions(state, freshSession, tc.atRoundLimit)
			if err != nil {
				t.Fatalf("WriteInstructions: %v", err)
			}
			if want := filepath.Join(state, "runs", freshSession, "instructions.md"); path != want {
				t.Errorf("path = %q, want %q", path, want)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("content differs from instructions/*.md:\n%s", got)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
			}
			dir, err := os.Stat(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			if dir.Mode().Perm() != 0o700 {
				t.Errorf("dir mode = %v, want 0700", dir.Mode().Perm())
			}
		})
	}
}

func TestWriteInstructionsTightensAnExistingFile(t *testing.T) {
	state := t.TempDir()
	dir := filepath.Join(state, "runs", freshSession)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instructions.md"), []byte("old, longer than nothing"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := WriteInstructions(state, freshSession, "stop")
	if err != nil {
		t.Fatalf("WriteInstructions: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
	}
	if d, err := os.Stat(dir); err != nil || d.Mode().Perm() != 0o700 {
		t.Errorf("dir = %v, %v; want mode 0700", d, err)
	}
	run, err := os.ReadFile(filepath.Join("instructions", "run.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(run) {
		t.Errorf("content = %q, want run.md", got)
	}
}

func TestWriteInstructionsRejectsSessionPath(t *testing.T) {
	for _, session := range []string{"", ".", "..", "../x", "a/b"} {
		if _, err := WriteInstructions(t.TempDir(), session, "stop"); err == nil {
			t.Errorf("WriteInstructions(%q): want an error", session)
		}
	}
}

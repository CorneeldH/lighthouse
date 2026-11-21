package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyTree copies the directory src into dst, file by file.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestReleaseFixture proves release and notes write byte for byte what the Python script wrote
// in three real releases. Each testdata directory holds the fragments, the CHANGELOG.md before
// and after, the notes and a meta file (previous tag, then date).
func TestReleaseFixture(t *testing.T) {
	for _, tag := range []string{"v0.16.2", "v0.17.0", "v0.18.0"} {
		t.Run(tag, func(t *testing.T) {
			src := filepath.Join("testdata", "releases", tag)
			read := func(name string) []byte {
				data, err := os.ReadFile(filepath.Join(src, name))
				if err != nil {
					t.Fatal(err)
				}
				return data
			}
			meta := strings.Fields(string(read("meta")))
			if len(meta) != 2 {
				t.Fatalf("meta = %q, want previous tag and date", meta)
			}
			prev, date := meta[0], meta[1]

			dir := t.TempDir()
			copyTree(t, filepath.Join(src, "changelog.d"), filepath.Join(dir, "changelog.d"))
			if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), read("CHANGELOG.before"), 0o644); err != nil {
				t.Fatal(err)
			}

			got, err := release(dir, "workflow_dispatch", "auto", date, []string{prev})
			if err != nil || got != tag {
				t.Fatalf("release = %q, %v; want %s", got, err, tag)
			}
			text, err := os.ReadFile(filepath.Join(dir, "CHANGELOG.md"))
			if err != nil {
				t.Fatal(err)
			}
			if want := read("CHANGELOG.want"); !bytes.Equal(text, want) {
				t.Fatalf("CHANGELOG.md =\n%s\nwant\n%s", text, want)
			}
			left, _ := filepath.Glob(filepath.Join(dir, "changelog.d", "*.md"))
			if len(left) != 0 {
				t.Fatalf("fragments left behind: %v", left)
			}

			var stdout, stderr bytes.Buffer
			if code := run([]string{"notes", tag, "--root", dir}, &stdout, &stderr); code != 0 {
				t.Fatalf("notes exit %d, stderr %q", code, stderr.String())
			}
			if want := read("notes.want"); !bytes.Equal(stdout.Bytes(), want) {
				t.Fatalf("notes =\n%s\nwant\n%s", stdout.Bytes(), want)
			}
		})
	}
}

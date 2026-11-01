package cli

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeModeFile(t *testing.T, path string, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

func TestExportTree(t *testing.T) {
	isolateGit(t)
	root := t.TempDir()
	bare, dir := filepath.Join(root, "origin.git"), filepath.Join(root, "work")
	mustGit(t, root, "init", "-q", "--bare", "-b", "main", bare)
	mustGit(t, root, "init", "-q", "-b", "main", dir)
	mustGit(t, dir, "remote", "add", "origin", bare)

	files := map[string]string{
		"a.txt":          "alpha\n",
		"sub/b c é.txt":  "beta\n",
		"secret.txt":     "secret\n",
		".gitignore":     ".env\n",
		".gitattributes": "secret.txt export-ignore\n",
	}
	for name, data := range files {
		writeModeFile(t, filepath.Join(dir, name), data, 0o644)
	}
	writeModeFile(t, filepath.Join(dir, "run.sh"), "#!/bin/sh\n", 0o755)
	if err := os.Symlink("/etc/hostname", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "init")
	mustGit(t, dir, "push", "-q", "origin", "main")
	mustGit(t, dir, "fetch", "-q", "origin")

	writeModeFile(t, filepath.Join(dir, "local.txt"), "local\n", 0o644)
	mustGit(t, dir, "add", "local.txt")
	mustGit(t, dir, "commit", "-q", "-m", "local only")
	writeModeFile(t, filepath.Join(dir, "u.txt"), "untracked\n", 0o644)
	writeModeFile(t, filepath.Join(dir, ".env"), "ignored\n", 0o644)

	out, remove, err := exportTree(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(out), "lighthouse-triage-") {
		t.Errorf("dir %q does not start with lighthouse-triage-", out)
	}

	var got []string
	err = filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(out, p)
			got = append(got, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	want := []string{".gitattributes", ".gitignore", "a.txt", "link", "run.sh", "sub/b c é.txt"}
	if !slices.Equal(got, want) {
		t.Errorf("files = %q, want %q", got, want)
	}

	target, err := os.Readlink(filepath.Join(out, "link"))
	if err != nil || target != "/etc/hostname" {
		t.Errorf("Readlink(link) = %q, %v; want /etc/hostname", target, err)
	}
	if fi, err := os.Stat(filepath.Join(out, "run.sh")); err != nil || fi.Mode()&0o100 == 0 {
		t.Errorf("run.sh mode = %v, %v; want executable", fi, err)
	}
	for _, name := range []string{"a.txt", "sub/b c é.txt", "run.sh"} {
		b, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if wantB := mustGit(t, dir, "show", "origin/main:"+name); !bytes.Equal(bytes.TrimSpace(b), []byte(wantB)) {
			t.Errorf("%s = %q, want %q", name, b, wantB)
		}
	}

	remove()
	remove() // safe to call twice
	if _, err := os.Stat(out); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat after remove = %v, want ErrNotExist", err)
	}
}

func TestExportTreeBadRef(t *testing.T) {
	isolateGit(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	dir := t.TempDir()
	mustGit(t, dir, "init", "-q", "-b", "main")
	writeModeFile(t, filepath.Join(dir, "a.txt"), "a\n", 0o644)
	mustGit(t, dir, "add", "a.txt")
	mustGit(t, dir, "commit", "-q", "-m", "init")

	_, remove, err := exportTree(context.Background(), dir, "origin/nope")
	if err == nil {
		t.Fatal("exportTree with unknown ref: want error")
	}
	if remove != nil {
		t.Error("remove should be nil on error")
	}
	left, _ := filepath.Glob(filepath.Join(tmp, "lighthouse-triage-*"))
	if len(left) != 0 {
		t.Errorf("temp dirs left behind: %v", left)
	}
}

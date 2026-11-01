package cli

import (
	"archive/tar"
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
	files["run.sh"] = "#!/bin/sh\n"
	writeModeFile(t, filepath.Join(dir, "run.sh"), files["run.sh"], 0o755)
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
		if string(b) != files[name] {
			t.Errorf("%s = %q, want %q", name, b, files[name])
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

// When git dies in the middle of the stream, the operator reads git's own message next to the
// truncated-archive error.
func TestExportTreeGitDiesMidStream(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\necho 'fatal: pack is corrupt' >&2\nprintf short\nexit 128\n"
	writeModeFile(t, filepath.Join(bin, "git"), script, 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", t.TempDir())

	_, _, err := exportTree(context.Background(), t.TempDir(), "origin/main")
	if err == nil || !strings.Contains(err.Error(), "read archive") || !strings.Contains(err.Error(), "fatal: pack is corrupt") {
		t.Errorf("err = %v, want read archive and git's fatal message", err)
	}
}

func TestUnpackTarContainment(t *testing.T) {
	cases := []struct {
		name    string
		entries func(outside string) []tar.Header
	}{
		{"dotdot", func(string) []tar.Header { return []tar.Header{{Name: "../x", Typeflag: tar.TypeReg, Mode: 0o644}} }},
		{"absolute", func(outside string) []tar.Header {
			return []tar.Header{{Name: filepath.Join(outside, "abs"), Typeflag: tar.TypeReg, Mode: 0o644}}
		}},
		{"through symlink", func(outside string) []tar.Header {
			return []tar.Header{
				{Name: "link", Typeflag: tar.TypeSymlink, Linkname: outside},
				{Name: "link/f", Typeflag: tar.TypeReg, Mode: 0o644},
			}
		}},
		{"git dir", func(string) []tar.Header {
			return []tar.Header{{Name: ".git/config", Typeflag: tar.TypeReg, Mode: 0o644}}
		}},
		{"nested git dir upper case", func(string) []tar.Header {
			return []tar.Header{{Name: "sub/.GIT/hooks/x", Typeflag: tar.TypeReg, Mode: 0o644}}
		}},
		{"git file", func(string) []tar.Header {
			return []tar.Header{{Name: "sub/.git", Typeflag: tar.TypeReg, Mode: 0o644}}
		}},
		{"hard link", func(string) []tar.Header {
			return []tar.Header{{Name: "hl", Typeflag: tar.TypeLink, Linkname: "a.txt"}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parent := t.TempDir()
			rootDir := filepath.Join(parent, "root")
			outside := filepath.Join(parent, "outside")
			for _, d := range []string{rootDir, outside} {
				if err := os.Mkdir(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)
			for _, h := range c.entries(outside) {
				if err := tw.WriteHeader(&h); err != nil {
					t.Fatal(err)
				}
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(rootDir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = root.Close() }()
			if err := unpackTar(root, &buf); err == nil {
				t.Error("unpackTar: want error")
			}
			for _, d := range []string{parent, outside} {
				ents, _ := os.ReadDir(d)
				for _, e := range ents {
					if e.Name() != "root" && e.Name() != "outside" {
						t.Errorf("created outside root: %s/%s", d, e.Name())
					}
				}
			}
			if ents, _ := os.ReadDir(outside); len(ents) != 0 {
				t.Errorf("outside not empty: %v", ents)
			}
		})
	}
}

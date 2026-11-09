//go:build windows

package gitinvocations

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// On Windows the cwd of a payload is `C:\…`. A fresh directory below it must still land in the
// repo: with `\` in the path pyDirname found no parent and the commit fell outside every repo.
func TestFindWindowsCwd(t *testing.T) {
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "-b", "main", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	sub := filepath.Join(repo, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, command, cwd, want string }{
		{"fresh dir", "mkdir new && cd new && git commit -m x", repo, filepath.ToSlash(repo)},
		{"subdir cwd", "git commit -m x", sub, filepath.ToSlash(sub)},
		{"cd up", "cd .. && git commit -m x", sub, filepath.ToSlash(repo)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found, err := Find(tc.command, tc.cwd, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if len(found) != 1 || found[0].Target != tc.want {
				t.Fatalf("Find(%q, %q) = %+v, want one commit in %q", tc.command, tc.cwd, found, tc.want)
			}
		})
	}
}

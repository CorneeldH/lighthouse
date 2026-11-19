//go:build unix

package scheduled

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Classification: io (real git, a fake git binary). Fails if the push's object-dir lookup runs
// without the remote timeout, if the git calls drop the hardening options, or if the state
// directory is a symlink, foreign or writable by others and still gets a temporary repository.

func testRemote(tempDir, path string) HTTPSRemote {
	env := map[string]string{"PATH": path, "TOKEN": "tok"}
	return HTTPSRemote{
		URL: "https://example.invalid/o/r.git", User: "u", TokenVar: "TOKEN", TempDir: tempDir,
		Getenv: func(k string) string { return env[k] },
	}
}

func TestObjectDirHonoursTheRemoteTimeout(t *testing.T) {
	bin := t.TempDir()
	fake := filepath.Join(bin, "git")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := remoteTimeout
	remoteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { remoteTimeout = old })
	t.Setenv("PATH", bin+":/usr/bin:/bin") // exec resolves git in the process's PATH
	r := testRemote(t.TempDir(), os.Getenv("PATH"))
	start := time.Now()
	_, err := r.objectDir(context.Background(), t.TempDir())
	if err == nil {
		t.Fatal("objectDir of a hanging git: err = nil, want a timeout error")
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("objectDir took %v, want it cut off by the remote timeout", d)
	}
}

func TestObjectDirPassesTheHardeningOptions(t *testing.T) {
	bin := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\necho /nonexistent/.git\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin") // exec resolves git in the process's PATH
	r := testRemote(t.TempDir(), os.Getenv("PATH"))
	if _, err := r.objectDir(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("objectDir: %v", err)
	}
	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains("\n"+string(got), "\nhttp.sslVerify=true\n") {
		t.Errorf("git args %q lack -c http.sslVerify=true", got)
	}
	// An empty value reaches curl as "": no CA bundle, no proxy.
	for _, bad := range []string{"http.sslCAInfo=", "http.sslCAPath=", "http.proxy="} {
		if strings.Contains("\n"+string(got), "\n"+bad+"\n") {
			t.Errorf("git args %q carry the empty option %s", got, bad)
		}
	}
}

func TestScratchRefusesAnUnsafeStateDir(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	open := filepath.Join(t.TempDir(), "open")
	if err := os.Mkdir(open, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o777); err != nil {
		t.Fatal(err)
	}
	for name, dir := range map[string]string{"symlink": link, "writable by others": open} {
		t.Run(name, func(t *testing.T) {
			r := testRemote(dir, os.Getenv("PATH"))
			if repo, err := r.scratch(context.Background(), ""); err == nil {
				_ = os.RemoveAll(repo)
				t.Errorf("scratch in a %s state dir: err = nil, want a refusal", name)
			}
		})
	}
	if err := checkOwner(mustStat(t, real), os.Getuid()+1); err == nil {
		t.Error("checkOwner for another uid: err = nil, want a refusal")
	}
}

func mustStat(t *testing.T, p string) os.FileInfo {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi
}

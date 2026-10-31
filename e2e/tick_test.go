//go:build e2e

package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// tickEnv is the child environment of a tick run: cleanEnv without the token and runtime
// variables of the host, then the given ones set explicitly. The runtime dir is always a
// temp dir, so a test never touches the real tick.lock.
func tickEnv(runtimeDir, hostToml string, extra ...string) []string {
	var env []string
	for _, kv := range cleanEnv() {
		if strings.HasPrefix(kv, "GH_TOKEN=") || strings.HasPrefix(kv, "GITHUB_TOKEN=") ||
			strings.HasPrefix(kv, "XDG_RUNTIME_DIR=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "XDG_RUNTIME_DIR="+runtimeDir, "LIGHTHOUSE_HOST_TOML="+hostToml)
	return append(env, extra...)
}

// runTick runs `lighthouse tick` once and returns its exit code and stderr.
func runTick(t *testing.T, bin string, env []string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"tick"}, args...)...)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return 0, stderr.String()
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("run tick: %v", err)
	}
	return ee.ExitCode(), stderr.String()
}

func emptyHostToml(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "host.toml")
	if err := os.WriteFile(path, []byte("[dispatch]\nrepos = []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTickBesideServe(t *testing.T) {
	bin := buildLighthouse(t)
	db := filepath.Join(t.TempDir(), "l.db")
	first := startServe(t, bin, cleanEnv(), "serve", "--addr", "127.0.0.1:0", "--db", db)

	env := tickEnv(t.TempDir(), emptyHostToml(t), "GH_TOKEN=x")
	if code, stderr := runTick(t, bin, env, "--db", db); code != 0 {
		t.Fatalf("tick beside serve exit code = %d, want 0; stderr: %s", code, stderr)
	}

	cmd := exec.Command(bin, "serve", "--addr", "127.0.0.1:0", "--db", db)
	cmd.Env = cleanEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	go func() { done <- cmd.Wait() }()
	if code := exitCode(t, done); code != 1 {
		t.Fatalf("second serve exit code = %d, want 1; stderr: %s", code, stderr.String())
	}
	for _, want := range []string{"another lighthouse serve holds", "l.db.lock"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr lacks %q: %s", want, stderr.String())
		}
	}
	health(t, first.addr)
}

func TestTickExitCodes(t *testing.T) {
	bin := buildLighthouse(t)
	hostToml := emptyHostToml(t)

	t.Run("no token", func(t *testing.T) {
		db := filepath.Join(t.TempDir(), "l.db")
		code, stderr := runTick(t, bin, tickEnv(t.TempDir(), hostToml), "--db", db)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1; stderr: %s", code, stderr)
		}
		if !strings.Contains(stderr, "no GitHub token") {
			t.Errorf("stderr lacks %q: %s", "no GitHub token", stderr)
		}
	})

	t.Run("busy tick.lock", func(t *testing.T) {
		db := filepath.Join(t.TempDir(), "l.db")
		runtimeDir := t.TempDir()
		dir := filepath.Join(runtimeDir, "lighthouse")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(filepath.Join(dir, "tick.lock"), os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		code, stderr := runTick(t, bin, tickEnv(runtimeDir, hostToml, "GH_TOKEN=x"), "--db", db)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr)
		}
		if !strings.Contains(stderr, "previous tick still running") {
			t.Errorf("stderr lacks %q: %s", "previous tick still running", stderr)
		}
	})
}

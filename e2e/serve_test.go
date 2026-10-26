//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type serveProc struct {
	cmd  *exec.Cmd
	addr string
	done chan error // receives cmd.Wait's result after stderr reached EOF
	// stderr holds everything the child wrote; read it only after done fired.
	stderr  *bytes.Buffer
	started chan string // the address from the "listening on" line
}

// startServe starts `lighthouse serve` and returns once it printed its start line. The
// child is killed at cleanup, so a red test leaves no server behind.
func startServe(t *testing.T, bin string, env []string, args ...string) *serveProc {
	t.Helper()
	p := startRaw(t, bin, env, args...)
	select {
	case addr := <-p.started:
		p.addr = addr
	case err := <-p.done:
		t.Fatalf("serve exited before listening: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("no start line within 10s")
	}
	return p
}

// startRaw starts the binary and collects its stderr. A reader reads the pipe to EOF
// before it calls Wait, as the os/exec contract requires.
func startRaw(t *testing.T, bin string, env []string, args ...string) *serveProc {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	pipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &serveProc{
		cmd:     cmd,
		done:    make(chan error, 1),
		stderr:  &bytes.Buffer{},
		started: make(chan string, 1),
	}
	go func() {
		sc := bufio.NewScanner(pipe)
		announced := false
		for sc.Scan() {
			p.stderr.WriteString(sc.Text() + "\n")
			if _, rest, ok := strings.Cut(sc.Text(), "listening on "); ok && !announced {
				announced = true
				p.started <- rest
			}
		}
		p.done <- cmd.Wait()
	}()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return p
}

func health(t *testing.T, addr string) {
	t.Helper()
	c := http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get("http://" + addr + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health = %d, want 200", resp.StatusCode)
	}
}

func exitCode(t *testing.T, done <-chan error) int {
	t.Helper()
	select {
	case err := <-done:
		if err == nil {
			return 0
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		t.Fatal(err)
	case <-time.After(15 * time.Second):
		t.Fatal("process did not exit within 15s")
	}
	return -1
}

func TestServeSignals(t *testing.T) {
	bin := buildLighthouse(t)
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			p := startServe(t, bin, cleanEnv(), "serve", "--addr", "127.0.0.1:0", "--db", filepath.Join(t.TempDir(), "l.db"))
			health(t, p.addr)
			if err := p.cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			if code := exitCode(t, p.done); code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}
		})
	}
}

func TestServeSecondInstanceRefused(t *testing.T) {
	bin := buildLighthouse(t)
	db := filepath.Join(t.TempDir(), "l.db")
	first := startServe(t, bin, cleanEnv(), "serve", "--addr", "127.0.0.1:0", "--db", db)

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
		t.Fatalf("second instance exit code = %d, want 1; stderr: %s", code, stderr.String())
	}
	for _, want := range []string{"another lighthouse serve holds", "l.db.lock"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr lacks %q: %s", want, stderr.String())
		}
	}
	health(t, first.addr)
}

func TestServePortInUse(t *testing.T) {
	bin := buildLighthouse(t)
	db := filepath.Join(t.TempDir(), "l.db")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	failing := startRaw(t, bin, cleanEnv(), "serve", "--addr", ln.Addr().String(), "--db", db)
	if code := exitCode(t, failing.done); code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr: %s", code, failing.stderr.String())
	}
	if !strings.Contains(failing.stderr.String(), "address already in use") {
		t.Errorf("stderr lacks the net.Listen error: %s", failing.stderr.String())
	}

	p := startServe(t, bin, cleanEnv(), "serve", "--addr", "127.0.0.1:0", "--db", db)
	health(t, p.addr)
}

func TestServeUsageE2E(t *testing.T) {
	bin := buildLighthouse(t)
	fixture, err := filepath.Abs("../internal/usage/testdata/usage_full.json")
	if err != nil {
		t.Fatal(err)
	}
	xdg, binDir := t.TempDir(), t.TempDir()
	writeScript(t, binDir, "claude", "cat '"+fixture+"'\n")
	env := usageEnv(t, xdg, binDir)
	runUsage(t, bin, env, "usage", "refresh")

	// The last value wins in cmd.Env: a real codex on the developer's PATH or HOME must not run.
	env = append(env,
		"PATH="+binDir+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin",
		"HOME="+t.TempDir(),
	)
	p := startServe(t, bin, env, "serve", "--addr", "127.0.0.1:0", "--db", filepath.Join(t.TempDir(), "l.db"))

	c := http.Client{Timeout: 30 * time.Second}
	resp, err := c.Get("http://" + p.addr + "/api/usage")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Claude struct {
			Session struct {
				State string `json:"state"`
				Pct   *int   `json:"pct"`
			} `json:"session"`
		} `json:"claude"`
		Codex struct {
			Session struct {
				State string `json:"state"`
			} `json:"session"`
		} `json:"codex"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if s := body.Claude.Session; s.State != "ok" || s.Pct == nil || *s.Pct != 99 {
		t.Errorf("claude.session = %+v, want state ok, pct 99", s)
	}
	if got := body.Codex.Session.State; got != "not_measurable" {
		t.Errorf("codex.session.state = %q, want not_measurable", got)
	}
}

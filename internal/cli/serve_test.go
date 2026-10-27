package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

// syncBuf is a buffer the serve goroutine writes while the test reads (-race).
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestServeDefaults(t *testing.T) {
	var out bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &out)
	root.SetArgs([]string{"serve", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"127.0.0.1:8127", "--db", "--repo", "owner/name", "--forge-url", "https://api.github.com"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("serve --help lacks %q:\n%s", want, out.String())
		}
	}
}

func TestGithubToken(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"both set", map[string]string{"GH_TOKEN": "gh", "GITHUB_TOKEN": "github"}, "gh"},
		{"only GITHUB_TOKEN", map[string]string{"GITHUB_TOKEN": "github"}, "github"},
		{"none", map[string]string{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := githubToken(func(k string) string { return tt.env[k] }); got != tt.want {
				t.Errorf("githubToken = %q, want %q", got, tt.want)
			}
		})
	}
}

// startServe runs serve on a free port and returns its address and a stop func that
// cancels and waits for the result.
func startServe(t *testing.T) (addr string, stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errOut := &syncBuf{}
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, "127.0.0.1:0", filepath.Join(t.TempDir(), "l.db"), forge.New("", ""), errOut)
	}()

	const marker = "lighthouse: listening on "
	deadline := time.Now().Add(5 * time.Second)
	for addr == "" {
		if _, rest, ok := strings.Cut(errOut.String(), marker); ok && strings.Contains(rest, "\n") {
			addr = strings.TrimSpace(rest)
		} else if time.Now().After(deadline) {
			t.Fatalf("no start line within 5s; stderr: %q", errOut.String())
		} else {
			select {
			case err := <-done:
				t.Fatalf("serve returned early: %v", err)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	return addr, func() error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(11 * time.Second):
			return errors.New("serve did not stop within 11s")
		}
	}
}

func TestServeStopsOnCancel(t *testing.T) {
	addr, stop := startServe(t)
	resp, err := http.Get("http://" + addr + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health = %d, want 200", resp.StatusCode)
	}
	if err := stop(); err != nil {
		t.Fatalf("serve = %v, want nil", err)
	}
}

func TestServeForgeNotConfigured(t *testing.T) {
	addr, stop := startServe(t)
	resp, err := http.Get("http://" + addr + "/api/questions")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("questions = %d, want 503", resp.StatusCode)
	}
	if err := stop(); err != nil {
		t.Fatalf("serve = %v, want nil", err)
	}
}

func TestNewForgeTrimsSlash(t *testing.T) {
	getenv := func(k string) string {
		if k == "GH_TOKEN" {
			return "tok"
		}
		return ""
	}
	c := newForge("o/r", "http://x/", getenv)
	if c.BaseURL != "http://x" {
		t.Errorf("BaseURL = %q, want http://x", c.BaseURL)
	}
	if c.Token != "tok" || c.Repo != "o/r" {
		t.Errorf("Token = %q, Repo = %q, want tok and o/r", c.Token, c.Repo)
	}
}

func TestServeForgeURL(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/issues":
			_, _ = io.WriteString(w, `[{"number":7,"title":"T","html_url":"https://h/7","comments":1}]`)
		case "/repos/o/r/issues/7/comments":
			_, _ = io.WriteString(w, `[{"body":"🤖 Vraag\n\nA of B?","created_at":"2026-10-27T10:00:00Z","html_url":"https://h/7#c1","reactions":{"rocket":0}}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	t.Setenv("GH_TOKEN", "t")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errOut := &syncBuf{}
	root := NewRoot(strings.NewReader(""), io.Discard, errOut)
	root.SetArgs([]string{"serve", "--addr", "127.0.0.1:0", "--db", filepath.Join(t.TempDir(), "l.db"),
		"--repo", "o/r", "--forge-url", fake.URL + "/"})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	const marker = "lighthouse: listening on "
	var addr string
	deadline := time.Now().Add(5 * time.Second)
	for addr == "" {
		if _, rest, ok := strings.Cut(errOut.String(), marker); ok && strings.Contains(rest, "\n") {
			addr = strings.TrimSpace(rest)
		} else if time.Now().After(deadline) {
			t.Fatalf("no start line within 5s; stderr: %q", errOut.String())
		} else {
			select {
			case err := <-done:
				t.Fatalf("serve returned early: %v", err)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}

	resp, err := http.Get("http://" + addr + "/api/questions")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("questions = %d, want 200: %s", resp.StatusCode, body)
	}
	for _, want := range []string{`"number":7`, "A of B?"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("body lacks %q: %s", want, body)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve = %v, want nil", err)
		}
	case <-time.After(11 * time.Second):
		t.Fatal("serve did not stop within 11s")
	}
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

// noHostToml points LIGHTHOUSE_HOST_TOML at a missing file, so a command test never reads
// the real host.toml.
func noHostToml(t *testing.T) {
	t.Helper()
	t.Setenv("LIGHTHOUSE_HOST_TOML", filepath.Join(t.TempDir(), "host.toml"))
}

func TestServeDefaults(t *testing.T) {
	noHostToml(t)
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

// waitListening waits for the serve start line and returns the address it names.
func waitListening(t *testing.T, errOut *syncBuf, done <-chan error) string {
	t.Helper()
	const marker = "lighthouse: listening on "
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, rest, ok := strings.Cut(errOut.String(), marker); ok && strings.Contains(rest, "\n") {
			return strings.TrimSpace(rest)
		}
		if time.Now().After(deadline) {
			t.Fatalf("no start line within 5s; stderr: %q", errOut.String())
		}
		select {
		case err := <-done:
			t.Fatalf("serve returned early: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
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
		done <- serve(ctx, "127.0.0.1:0", filepath.Join(t.TempDir(), "l.db"), forge.New("", ""), nil, errOut)
	}()

	addr = waitListening(t, errOut, done)
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

	noHostToml(t)
	t.Setenv("GH_TOKEN", "t")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errOut := &syncBuf{}
	root := NewRoot(strings.NewReader(""), io.Discard, errOut)
	root.SetArgs([]string{"serve", "--addr", "127.0.0.1:0", "--db", filepath.Join(t.TempDir(), "l.db"),
		"--repo", "o/r", "--forge-url", fake.URL + "/"})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	addr := waitListening(t, errOut, done)

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

// hostToml writes a host.toml whose [dispatch] serves repos and returns its path.
func hostToml(t *testing.T, repos ...string) string {
	t.Helper()
	quoted := make([]string, len(repos))
	for i, r := range repos {
		quoted[i] = fmt.Sprintf("%q", r)
	}
	path := filepath.Join(t.TempDir(), "host.toml")
	body := "[dispatch]\nrepos = [" + strings.Join(quoted, ", ") + "]\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestServeCommandFiltersController(t *testing.T) {
	question := func(n int) string {
		return fmt.Sprintf(`[{"body":"🤖 Vraag\n\nVraag op %d?","created_at":"2026-10-27T10:00:00Z","html_url":"https://h/%d#c1","reactions":{"rocket":0}}]`, n, n)
	}
	board, err := json.Marshal(map[string]any{"data": map[string]any{"node": map[string]any{"items": map[string]any{
		"pageInfo": map[string]any{"hasNextPage": false},
		"nodes": []any{
			boardNode("I7", "c1", issueContent(7, "zeven", nil)),
			boardNode("I8", "h1", issueContent(8, "acht", nil)),
		},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /graphql":
			_, _ = w.Write(board)
		case "GET /repos/o/r/issues":
			_, _ = io.WriteString(w, `[{"number":7,"title":"zeven","html_url":"https://h/7","comments":1},`+
				`{"number":8,"title":"acht","html_url":"https://h/8","comments":1}]`)
		case "GET /repos/o/r/issues/7/comments":
			_, _ = io.WriteString(w, question(7))
		case "GET /repos/o/r/issues/8/comments":
			_, _ = io.WriteString(w, question(8))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	t.Setenv("LIGHTHOUSE_HOST_TOML", hostToml(t, repoDir(t, completeRepo)))
	t.Setenv("GH_TOKEN", "t")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errOut := &syncBuf{}
	root := NewRoot(strings.NewReader(""), io.Discard, errOut)
	root.SetArgs([]string{"serve", "--addr", "127.0.0.1:0", "--db", filepath.Join(t.TempDir(), "l.db"),
		"--repo", "o/r", "--forge-url", fake.URL})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	addr := waitListening(t, errOut, done)

	resp, err := http.Get("http://" + addr + "/api/questions")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("questions = %d, want 200: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"number":7`) {
		t.Errorf("body lacks #7, which is in Controller: %s", body)
	}
	if strings.Contains(string(body), `"number":8`) {
		t.Errorf("body has #8, which is not in Controller: %s", body)
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

// noController is a repo whose board has no Controller option.
const noController = `[forge]
repo = "o/r"

[forge.board]
project_id = "PVT_x"
status_field_id = "SF"

[forge.board.status_options]
"On hold" = "h1"
`

func TestControllerFilterFromHostToml(t *testing.T) {
	served := repoDir(t, completeRepo)
	broken := repoDir(t, "[forge")
	unreadable := unreadableHostToml(t)
	tests := []struct {
		name       string
		hostToml   string
		repo       string
		wantFilter bool
		wantErrOut string
	}{
		{"served repo", hostToml(t, served), "o/r", true, ""},
		{"other slug", hostToml(t, served), "x/y", false, ""},
		{"no host.toml", filepath.Join(t.TempDir(), "host.toml"), "o/r", false, ""},
		{"broken repo is skipped", hostToml(t, broken, served), "o/r", true, broken},
		{"no Controller option", hostToml(t, repoDir(t, noController)), "o/r", false, "Controller"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var errOut bytes.Buffer
			f, err := controllerFilter(tt.hostToml, tt.repo, forge.New(tt.repo, "t"), &errOut)
			if err != nil {
				t.Fatalf("controllerFilter: %v", err)
			}
			if (f != nil) != tt.wantFilter {
				t.Errorf("filter != nil is %v, want %v", f != nil, tt.wantFilter)
			}
			if tt.wantErrOut == "" && errOut.Len() != 0 {
				t.Errorf("errOut = %q, want empty", errOut.String())
			}
			if !strings.Contains(errOut.String(), tt.wantErrOut) {
				t.Errorf("errOut = %q, want it to name %q", errOut.String(), tt.wantErrOut)
			}
		})
	}

	if _, err := controllerFilter(unreadable, "o/r", forge.New("o/r", "t"), io.Discard); err == nil {
		t.Error("controllerFilter on an unreadable host.toml = nil error, want one")
	}

	// Without --repo no filter applies, so a broken host.toml does not stop serve.
	f, err := controllerFilter(unreadable, "", forge.New("", "t"), io.Discard)
	if err != nil || f != nil {
		t.Errorf("controllerFilter without repo = (%v, %v), want (nil, nil)", f != nil, err)
	}
}

// unreadableHostToml writes a host.toml that does not parse and returns its path.
func unreadableHostToml(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "host.toml")
	if err := os.WriteFile(path, []byte("[dispatch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestServeCommandStopsOnUnreadableHostToml(t *testing.T) {
	t.Setenv("LIGHTHOUSE_HOST_TOML", unreadableHostToml(t))
	t.Setenv("GH_TOKEN", "t")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errOut := &syncBuf{}
	root := NewRoot(strings.NewReader(""), io.Discard, errOut)
	root.SetArgs([]string{"serve", "--addr", "127.0.0.1:0", "--db", filepath.Join(t.TempDir(), "l.db"),
		"--repo", "o/r"})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	select {
	case err := <-done:
		if err == nil {
			t.Error("serve = nil, want the host.toml error")
		}
	case <-time.After(5 * time.Second):
		cancel()
		err := <-done
		t.Fatalf("serve still running after 5s, want it to stop on the host.toml error; after cancel: %v", err)
	}
	if strings.Contains(errOut.String(), "listening") {
		t.Errorf("serve started listening: %q", errOut.String())
	}
}

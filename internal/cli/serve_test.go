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
	"sync/atomic"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/overview"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/server"
	"github.com/CorneeldH/lighthouse/internal/store"
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
		done <- serve(ctx, "127.0.0.1:0", filepath.Join(t.TempDir(), "l.db"), nil, server.RunsMeta{}, server.RefreshInterval, errOut)
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

func TestForgeReposFromHostToml(t *testing.T) {
	served := repoDir(t, completeRepo)
	tests := []struct {
		name       string
		hostToml   string
		repo       string
		wantSlugs  string
		wantFilter bool
		wantErrOut string
	}{
		{"served repo", hostToml(t, served), "o/r", "o/r", true, ""},
		{"served without --repo", hostToml(t, served), "", "o/r", true, ""},
		{"no Controller option", hostToml(t, repoDir(t, noController)), "o/r", "o/r", false,
			"lighthouse: " + "%s" + ": missing forge.board.status_options.Controller: overview shows every open issue\n"},
		{"no board", hostToml(t, repoDir(t, "[forge]\nrepo = \"o/r\"\n")), "o/r", "o/r", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var errOut bytes.Buffer
			got, err := forgeRepos(tt.hostToml, tt.repo, "http://forge", func(string) string { return "t" }, &errOut)
			if err != nil {
				t.Fatalf("forgeRepos: %v", err)
			}
			if s := slugs(got); s != tt.wantSlugs {
				t.Fatalf("slugs = %q, want %q", s, tt.wantSlugs)
			}
			if (got[0].InController != nil) != tt.wantFilter {
				t.Errorf("filter != nil is %v, want %v", got[0].InController != nil, tt.wantFilter)
			}
			want := tt.wantErrOut
			if strings.Contains(want, "%s") {
				want = fmt.Sprintf(want, firstRepo(t, tt.hostToml))
			}
			if errOut.String() != want {
				t.Errorf("errOut = %q, want %q", errOut.String(), want)
			}
		})
	}
}

// slugs joins the slugs of repos with commas.
func slugs(repos []overview.RepoSource) string {
	s := make([]string, len(repos))
	for i, r := range repos {
		s[i] = r.Slug
	}
	return strings.Join(s, ",")
}

// firstRepo returns the first path in [dispatch] repos of the host.toml at path.
func firstRepo(t *testing.T, path string) string {
	t.Helper()
	cfg, err := hostconfig.LoadDispatch(path)
	if err != nil || len(cfg.Repos) == 0 {
		t.Fatalf("host.toml %s: %v, repos %v", path, err, cfg.Repos)
	}
	return cfg.Repos[0]
}

func TestForgeReposDispatchClientPerRepo(t *testing.T) {
	withBoard := repoDir(t, completeRepo)
	noBoard := repoDir(t, "[forge]\nrepo = \"o/s\"\n")
	var errOut bytes.Buffer
	got, err := forgeRepos(hostToml(t, withBoard, noBoard), "", "http://forge", func(string) string { return "t" }, &errOut)
	if err != nil {
		t.Fatalf("forgeRepos: %v", err)
	}
	if s := slugs(got); s != "o/r,o/s" {
		t.Fatalf("slugs = %q, want o/r,o/s", s)
	}
	wantFilter := []bool{true, false}
	for i, g := range got {
		c, ok := g.Src.(*forge.Client)
		if !ok || c.Repo != g.Slug {
			t.Errorf("source %s: Src = %+v, want the forge client of %s", g.Slug, g.Src, g.Slug)
		}
		if (g.InController != nil) != wantFilter[i] {
			t.Errorf("source %s: filter != nil is %v, want %v", g.Slug, g.InController != nil, wantFilter[i])
		}
	}
	if errOut.Len() != 0 {
		t.Errorf("errOut = %q, want empty", errOut.String())
	}
}

func TestForgeReposSkipsBrokenRepo(t *testing.T) {
	broken := repoDir(t, "[forge")
	other := repoDir(t, strings.Replace(completeRepo, `repo = "o/r"`, `repo = "o/s"`, 1))
	var errOut bytes.Buffer
	got, err := forgeRepos(hostToml(t, broken, other), "", "http://forge", func(string) string { return "t" }, &errOut)
	if err != nil {
		t.Fatalf("forgeRepos: %v", err)
	}
	if s := slugs(got); s != "o/s" {
		t.Errorf("slugs = %q, want o/s", s)
	}
	lines := strings.Split(strings.TrimSuffix(errOut.String(), "\n"), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "lighthouse: skip "+broken+": ") {
		t.Errorf("errOut = %q, want one line starting %q", errOut.String(), "lighthouse: skip "+broken+": ")
	}
}

func TestForgeReposWithoutDispatch(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "host.toml")
	getenv := func(k string) string {
		if k == "GH_TOKEN" {
			return "tok"
		}
		return ""
	}
	var errOut bytes.Buffer
	got, err := forgeRepos(missing, "o/r", "http://forge/", getenv, &errOut)
	if err != nil {
		t.Fatalf("forgeRepos: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "o/r" || got[0].InController != nil {
		t.Fatalf("forgeRepos = %+v, want one source o/r without filter", got)
	}
	c, ok := got[0].Src.(*forge.Client)
	if !ok || c.Repo != "o/r" || c.Token != "tok" || c.BaseURL != "http://forge" {
		t.Errorf("Src = %+v, want the forge client of o/r at http://forge with token tok", got[0].Src)
	}
	if errOut.Len() != 0 {
		t.Errorf("errOut = %q, want empty", errOut.String())
	}

	// Without --repo there is nothing to serve.
	got, err = forgeRepos(missing, "", "http://forge", getenv, &errOut)
	if err != nil || len(got) != 0 {
		t.Errorf("forgeRepos without repo = (%+v, %v), want (none, nil)", got, err)
	}
}

func TestForgeReposIgnoresRepoFlag(t *testing.T) {
	var errOut bytes.Buffer
	got, err := forgeRepos(hostToml(t, repoDir(t, completeRepo)), "x/y", "http://forge", func(string) string { return "t" }, &errOut)
	if err != nil {
		t.Fatalf("forgeRepos: %v", err)
	}
	if s := slugs(got); s != "o/r" {
		t.Errorf("slugs = %q, want o/r", s)
	}
	if want := "lighthouse: --repo x/y ignored: serving the repos of [dispatch]\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
}

func TestForgeReposUnreadableHostToml(t *testing.T) {
	for _, repo := range []string{"o/r", ""} {
		if _, err := forgeRepos(unreadableHostToml(t), repo, "http://forge", func(string) string { return "t" }, io.Discard); err == nil {
			t.Errorf("forgeRepos(repo %q) on an unreadable host.toml = nil error, want one", repo)
		}
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

// startServeCommand runs `lighthouse serve` on a free port with args added and returns its
// address and a stop func that cancels and waits for the result.
func startServeCommand(t *testing.T, args ...string) (addr string, stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errOut := &syncBuf{}
	root := NewRoot(strings.NewReader(""), io.Discard, errOut)
	root.SetArgs(append([]string{"serve", "--addr", "127.0.0.1:0"}, args...))
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

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

// getBody returns the status and body of GET path on addr.
func getBody(t *testing.T, addr, path string) (int, string) {
	t.Helper()
	resp, err := http.Get("http://" + addr + path)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

func TestServeRunsWithoutDispatch(t *testing.T) {
	noHostToml(t)
	t.Setenv("LIGHTHOUSE_FORGE_REPO", "")
	addr, stop := startServeCommand(t, "--db", filepath.Join(t.TempDir(), "l.db"))
	// A missing host.toml gives the defaults of hostconfig.LoadDispatch.
	const want = `{"runs":[],"deferred":[],"workspace_url":"http://127.0.0.1:8091","night":"21:00-08:00"}`
	if code, body := getBody(t, addr, "/api/runs"); code != http.StatusOK || strings.TrimSpace(body) != want {
		t.Errorf("runs = %d %s, want 200 %s", code, body, want)
	}
	if code, body := getBody(t, addr, "/api/questions"); code != http.StatusServiceUnavailable {
		t.Errorf("questions = %d %s, want 503", code, body)
	}
	if err := stop(); err != nil {
		t.Fatalf("serve = %v, want nil", err)
	}
}

func TestServeRunsFromTickState(t *testing.T) {
	db := filepath.Join(t.TempDir(), "l.db")
	ctx := context.Background()
	st, err := store.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	for _, repo := range []string{"o/r", "x/y"} {
		views := []dispatch.RunView{{Issue: 7, Title: "zeven in " + repo, State: "open", Stand: "working"}}
		if err := dispatch.SaveRunView(ctx, st.DB, repo, views, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LIGHTHOUSE_HOST_TOML", hostToml(t, repoDir(t, completeRepo)))
	addr, stop := startServeCommand(t, "--db", db)
	code, body := getBody(t, addr, "/api/runs")
	if code != http.StatusOK {
		t.Fatalf("runs = %d %s, want 200", code, body)
	}
	var got struct {
		Runs []dispatch.RunView `json:"runs"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Runs) != 1 || got.Runs[0].Repo != "o/r" || got.Runs[0].Title != "zeven in o/r" || !got.Runs[0].At.Equal(at) {
		t.Errorf("runs = %+v, want only #7 of o/r, the served repo, at %s", got.Runs, at)
	}
	if err := stop(); err != nil {
		t.Fatalf("serve = %v, want nil", err)
	}
}

// countingSource is an overview.Source without issues that counts its OpenIssues calls.
type countingSource struct{ calls atomic.Int32 }

func (c *countingSource) OpenIssues(context.Context) ([]forge.Issue, error) {
	c.calls.Add(1)
	return nil, nil
}

func (c *countingSource) Comments(context.Context, int) ([]protocol.Comment, error) {
	return nil, nil
}

func TestStartOverviewNoReposIsNotConfigured(t *testing.T) {
	read, stopped := startOverview(context.Background(), nil, server.RefreshInterval)
	if _, err := read(context.Background()); !errors.Is(err, forge.ErrNotConfigured) {
		t.Fatalf("err = %v, want forge.ErrNotConfigured", err)
	}
	done := make(chan struct{})
	go func() { stopped(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stopped() blocked without repos")
	}
}

func TestStartOverviewRebuildsOnTheInterval(t *testing.T) {
	src := &countingSource{}
	ctx, cancel := context.WithCancel(context.Background())
	read, stopped := startOverview(ctx, []overview.RepoSource{{Slug: "o/r", Src: src}}, 20*time.Millisecond)
	defer func() { cancel(); stopped() }()
	if _, err := read(ctx); err != nil {
		t.Fatalf("read: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for src.calls.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("OpenIssues calls = %d after 5s at a 20ms interval, want at least 3", src.calls.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestServeRefusesANonPositiveOverviewInterval(t *testing.T) {
	for _, v := range []string{"0s", "-1s"} {
		errOut := &syncBuf{}
		root := NewRoot(strings.NewReader(""), io.Discard, errOut)
		root.SetArgs([]string{"serve", "--addr", "127.0.0.1:0", "--db", filepath.Join(t.TempDir(), "l.db"),
			"--overview-interval", v})
		if err := root.ExecuteContext(context.Background()); err == nil || !strings.Contains(err.Error(), "--overview-interval must be positive") {
			t.Errorf("%s: err = %v, want the positive-interval refusal", v, err)
		}
	}
}

func TestStartOverviewBuildsOnceAndStops(t *testing.T) {
	src := &countingSource{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	read, stopped := startOverview(ctx, []overview.RepoSource{{Slug: "o/r", Src: src}}, server.RefreshInterval)
	ov, err := read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ov.Questions == nil || len(ov.Questions) != 0 || ov.Returned == nil || len(ov.Returned) != 0 {
		t.Fatalf("overview = %+v, want empty non-nil lists", ov)
	}
	if n := src.calls.Load(); n != 1 {
		t.Fatalf("OpenIssues calls = %d, want 1", n)
	}
	cancel()
	done := make(chan struct{})
	go func() { stopped(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stopped() did not return within 1s of cancel")
	}
}

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
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/forge/gitlabtest"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/overview"
	"github.com/CorneeldH/lighthouse/internal/priorities"
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
	return startServeRepos(t, nil)
}

// startServeRepos is startServe with repos feeding the forge endpoints.
func startServeRepos(t *testing.T, repos []overview.RepoSource) (addr string, stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errOut := &syncBuf{}
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, "127.0.0.1:0", filepath.Join(t.TempDir(), "l.db"), repos, nil, server.RunsMeta{}, nil, server.RefreshInterval, server.PrioritiesInterval, errOut)
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
	question := func(n int) []map[string]any {
		return []map[string]any{{"body": fmt.Sprintf("🤖 Vraag\n\nVraag op %d?", n), "created_at": "2026-10-27T10:00:00Z",
			"html_url": fmt.Sprintf("https://h/%d#c1", n), "reactions": map[string]any{"rocket": 0}}}
	}
	fake := forgetest.New(t)
	fake.ProjectID = "PVT_x"
	fake.BoardNodes = []map[string]any{
		boardNode("I7", "c1", issueContent(7, "zeven", nil)),
		boardNode("I8", "h1", issueContent(8, "acht", nil)),
	}
	fake.Issues = map[int]map[string]any{
		7: {"number": 7, "title": "zeven", "html_url": "https://h/7", "comments": 1},
		8: {"number": 8, "title": "acht", "html_url": "https://h/8", "comments": 1},
	}
	fake.Comments = map[int][]map[string]any{7: question(7), 8: question(8)}

	t.Setenv("LIGHTHOUSE_HOST_TOML", hostToml(t, repoDir(t, completeRepo)))
	t.Setenv("GH_TOKEN", "t")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errOut := &syncBuf{}
	root := NewRoot(strings.NewReader(""), io.Discard, errOut)
	root.SetArgs([]string{"serve", "--addr", "127.0.0.1:0", "--db", filepath.Join(t.TempDir(), "l.db"),
		"--repo", "o/r", "--forge-url", fake.URL()})
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
			got, _, err := forgeRepos(tt.hostToml, tt.repo, "http://forge", func(string) string { return "t" }, &errOut)
			if err != nil {
				t.Fatalf("forgeRepos: %v", err)
			}
			if s := slugs(got); s != tt.wantSlugs {
				t.Fatalf("slugs = %q, want %q", s, tt.wantSlugs)
			}
			if got[0].Controller != tt.wantFilter {
				t.Errorf("filter != nil is %v, want %v", got[0].Controller, tt.wantFilter)
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
	got, _, err := forgeRepos(hostToml(t, withBoard, noBoard), "", "http://forge", func(string) string { return "t" }, &errOut)
	if err != nil {
		t.Fatalf("forgeRepos: %v", err)
	}
	if s := slugs(got); s != "o/r,o/s" {
		t.Fatalf("slugs = %q, want o/r,o/s", s)
	}
	wantFilter := []bool{true, false}
	for i, g := range got {
		c, ok := g.Src.(*forge.GitHub)
		if !ok || c.Repo != g.Slug {
			t.Errorf("source %s: Src = %+v, want the forge client of %s", g.Slug, g.Src, g.Slug)
		}
		if g.Controller != wantFilter[i] {
			t.Errorf("source %s: filter != nil is %v, want %v", g.Slug, g.Controller, wantFilter[i])
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
	got, _, err := forgeRepos(hostToml(t, broken, other), "", "http://forge", func(string) string { return "t" }, &errOut)
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
	got, _, err := forgeRepos(missing, "o/r", "http://forge/", getenv, &errOut)
	if err != nil {
		t.Fatalf("forgeRepos: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "o/r" || got[0].Controller || got[0].Board {
		t.Fatalf("forgeRepos = %+v, want one source o/r without filter", got)
	}
	c, ok := got[0].Src.(*forge.GitHub)
	if !ok || c.Repo != "o/r" || c.Token != "tok" || c.BaseURL != "http://forge" {
		t.Errorf("Src = %+v, want the forge client of o/r at http://forge with token tok", got[0].Src)
	}
	if errOut.Len() != 0 {
		t.Errorf("errOut = %q, want empty", errOut.String())
	}

	// Without --repo there is nothing to serve.
	got, _, err = forgeRepos(missing, "", "http://forge", getenv, &errOut)
	if err != nil || len(got) != 0 {
		t.Errorf("forgeRepos without repo = (%+v, %v), want (none, nil)", got, err)
	}
}

func TestForgeReposIgnoresRepoFlag(t *testing.T) {
	var errOut bytes.Buffer
	got, _, err := forgeRepos(hostToml(t, repoDir(t, completeRepo)), "x/y", "http://forge", func(string) string { return "t" }, &errOut)
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
		if _, _, err := forgeRepos(unreadableHostToml(t), repo, "http://forge", func(string) string { return "t" }, io.Discard); err == nil {
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

// countingSource is a forge.Forge without issues that counts its OpenIssues calls.
type countingSource struct {
	forge.Forge
	calls atomic.Int32
}

func (c *countingSource) OpenIssues(context.Context) ([]forge.Issue, error) {
	c.calls.Add(1)
	return nil, nil
}

func (c *countingSource) Comments(context.Context, int) ([]protocol.Comment, error) {
	return nil, nil
}

func TestStartOverviewNoReposIsNotConfigured(t *testing.T) {
	read, _, stopped := startOverview(context.Background(), nil, server.RefreshInterval)
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
	read, _, stopped := startOverview(ctx, []overview.RepoSource{{Slug: "o/r", Src: src}}, 20*time.Millisecond)
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
	read, _, stopped := startOverview(ctx, []overview.RepoSource{{Slug: "o/r", Src: src}}, server.RefreshInterval)
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

// A served repo without its token is skipped for the forge, with one line, but its slug stays
// among those /api/runs shows.
func TestServeSkipsRepoWithoutToken(t *testing.T) {
	withToken := repoDir(t, completeRepo)
	without := repoDir(t, "[forge]\nrepo = \"o/s\"\ntoken_env = \"MISSING\"\n")
	getenv := func(k string) string { return map[string]string{"GH_TOKEN": "t"}[k] }
	var errOut bytes.Buffer
	got, served, err := forgeRepos(hostToml(t, withToken, without), "", "http://forge", getenv, &errOut)
	if err != nil {
		t.Fatalf("forgeRepos: %v", err)
	}
	if s := slugs(got); s != "o/r" {
		t.Errorf("sources = %q, want o/r", s)
	}
	if want := "lighthouse: skip " + without + ": no GitHub token: set MISSING\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
	if !slices.Equal(served, []string{"o/r", "o/s"}) {
		t.Errorf("slugs = %q, want [o/r o/s]", served)
	}
}

// Without any token serve still starts: /api/runs shows the runs of the served repo from the
// database, and the forge endpoints answer 503.
func TestServeRunsOfTokenlessRepo(t *testing.T) {
	db := filepath.Join(t.TempDir(), "l.db")
	ctx := context.Background()
	st, err := store.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	views := []dispatch.RunView{{Issue: 7, Title: "zeven", State: "open", Stand: "working"}}
	if err := dispatch.SaveRunView(ctx, st.DB, "o/r", views, at); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
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
	if len(got.Runs) != 1 || got.Runs[0].Repo != "o/r" || got.Runs[0].Issue != 7 {
		t.Errorf("runs = %+v, want #7 of o/r", got.Runs)
	}
	if code, body := getBody(t, addr, "/api/questions"); code != http.StatusServiceUnavailable {
		t.Errorf("questions = %d %s, want 503", code, body)
	}
	if err := stop(); err != nil {
		t.Fatalf("serve = %v, want nil", err)
	}
}

// --forge-url defaults to "": the opener then sends a GitHub repo to https://api.github.com and
// a GitLab repo to its own host, never one URL for both.
func TestServeForgeURLDefaultEmpty(t *testing.T) {
	noHostToml(t)
	t.Setenv("LIGHTHOUSE_FORGE_URL", "")
	var out bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &out)
	cmd, _, err := root.Find([]string{"serve"})
	if err != nil {
		t.Fatal(err)
	}
	if f := cmd.Flags().Lookup("forge-url"); f == nil || f.DefValue != "" {
		t.Fatalf("--forge-url = %+v, want a flag with default \"\"", f)
	}
	root.SetArgs([]string{"serve", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "https://api.github.com") {
		t.Errorf("serve --help lacks https://api.github.com:\n%s", out.String())
	}
}

// nodeServeHost writes a host.toml with [node] and [dispatch.forge] (and extra [dispatch] text)
// and returns the paths of the host.toml, the node token and the forge token. forgeToken ""
// means no forge token file at all.
func nodeServeHost(t *testing.T, forgeURL, forgeToken, extra string) (nodeToken string) {
	t.Helper()
	dir := t.TempDir()
	nodeToken = filepath.Join(dir, "node-token")
	if err := os.WriteFile(nodeToken, []byte("node-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	forgeTokenFile := filepath.Join(dir, "forge-token")
	if forgeToken != "" {
		if err := os.WriteFile(forgeTokenFile, []byte(forgeToken), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	body := fmt.Sprintf("[node]\ntoken_file = %q\n\n[dispatch.forge]\nurl = %q\ntoken_file = %q\n\n%s",
		nodeToken, forgeURL, forgeTokenFile, extra)
	path := filepath.Join(dir, "host.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_HOST_TOML", path)
	t.Setenv("LIGHTHOUSE_FORGE_REPO", "")
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	return nodeToken
}

func nodeGet(t *testing.T, addr, path, token string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, string(b)
}

func TestServeWithoutNodeSectionHasNoNodeRoutes(t *testing.T) {
	noHostToml(t)
	t.Setenv("LIGHTHOUSE_FORGE_REPO", "")
	addr, stop := startServeCommand(t, "--db", filepath.Join(t.TempDir(), "l.db"))
	if code, body := getBody(t, addr, "/api/node/v1/info"); code != http.StatusNotFound {
		t.Errorf("node info = %d %s, want 404", code, body)
	}
	if err := stop(); err != nil {
		t.Fatalf("serve = %v, want nil", err)
	}
}

func TestServeWithNodeSectionAndNoForgeTokenFails(t *testing.T) {
	nodeServeHost(t, "http://forge.invalid", "", "")
	root := NewRoot(strings.NewReader(""), io.Discard, io.Discard)
	root.SetArgs([]string{"serve", "--addr", "127.0.0.1:0", "--db", filepath.Join(t.TempDir(), "l.db")})
	err := root.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "dispatch.forge.token_file") {
		t.Fatalf("serve = %v, want an error naming dispatch.forge.token_file", err)
	}
}

func TestServeWithNodeSectionMountsRoutes(t *testing.T) {
	tok := nodeServeHost(t, "http://forge.invalid", "forge-secret\n", "")
	addr, stop := startServeCommand(t, "--db", filepath.Join(t.TempDir(), "l.db"))
	if code, body := nodeGet(t, addr, "/api/node/v1/info", ""); code != http.StatusUnauthorized {
		t.Errorf("info without token = %d %s, want 401", code, body)
	}
	if code, body := nodeGet(t, addr, "/api/node/v1/info", "wrong"); code != http.StatusForbidden {
		t.Errorf("info with wrong token = %d %s, want 403", code, body)
	}
	secret, err := os.ReadFile(tok)
	if err != nil {
		t.Fatal(err)
	}
	if code, body := nodeGet(t, addr, "/api/node/v1/info", strings.TrimSpace(string(secret))); code != http.StatusOK {
		t.Errorf("info with token = %d %s, want 200", code, body)
	}
	if err := stop(); err != nil {
		t.Fatalf("serve = %v, want nil", err)
	}
}

func TestServeFailsOnNodeWithoutServeKeys(t *testing.T) {
	noHostToml(t)
	path := filepath.Join(t.TempDir(), "host.toml")
	body := "[dispatch.nodes.a]\nnode_id = \"node-a\"\nslots = 1\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_HOST_TOML", path)
	root := NewRoot(strings.NewReader(""), io.Discard, io.Discard)
	root.SetArgs([]string{"serve", "--addr", "127.0.0.1:0", "--db", filepath.Join(t.TempDir(), "l.db")})
	err := root.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "dispatch.nodes.a.serve_url") || !strings.Contains(err.Error(), "add serve_url") {
		t.Fatalf("serve = %v, want an error naming dispatch.nodes.a.serve_url and the step", err)
	}
}

func TestServeUnknownProfileIs400(t *testing.T) {
	tok := nodeServeHost(t, "http://forge.invalid", "forge-secret", "[dispatch.profiles.software]\ntarget_key = \"t-software\"\n")
	addr, stop := startServeCommand(t, "--db", filepath.Join(t.TempDir(), "l.db"))
	secret, _ := os.ReadFile(tok)
	path := "/api/node/v1/workspaces/ws1/transcript/" + "11111111-1111-4111-8111-111111111111" + "?profile=elders"
	if code, body := nodeGet(t, addr, path, strings.TrimSpace(string(secret))); code != http.StatusBadRequest {
		t.Errorf("transcript with unknown profile = %d %s, want 400", code, body)
	}
	if err := stop(); err != nil {
		t.Fatalf("serve = %v, want nil", err)
	}
}

// TestServeProfileFromNodeHostToml: a profile of [dispatch.profiles] on the node passes the
// profile check; the request then fails on the forge (workspace gone), not with 400.
func TestServeProfileFromNodeHostToml(t *testing.T) {
	forgeSrv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(forgeSrv.Close)
	tok := nodeServeHost(t, forgeSrv.URL, "forge-secret", "[dispatch.profiles.software]\ntarget_key = \"t-software\"\n")
	addr, stop := startServeCommand(t, "--db", filepath.Join(t.TempDir(), "l.db"))
	secret, _ := os.ReadFile(tok)
	path := "/api/node/v1/workspaces/ws1/transcript/" + "11111111-1111-4111-8111-111111111111" + "?profile=software"
	code, body := nodeGet(t, addr, path, strings.TrimSpace(string(secret)))
	if code != http.StatusNotFound || !strings.Contains(body, "workspace_gone") {
		t.Errorf("transcript with known profile = %d %s, want 404 workspace_gone", code, body)
	}
	if err := stop(); err != nil {
		t.Fatalf("serve = %v, want nil", err)
	}
}

// A GitLab repo of [dispatch] repos is served from its own forge: its open issues and their
// comments reach the overview, and the GitHub URL and token are never part of it.
func TestServeShowsGitLabRepo(t *testing.T) {
	srv := gitlabtest.New(t)
	srv.Login = "lh-bot"
	srv.Project = "g/sub/p"
	srv.SetStatuses("To do")
	srv.AddIssue(map[string]any{"iid": 7, "id": 9007, "title": "zeven", "state": "opened", "labels": []string{},
		"web_url": "https://gl/g/sub/p/-/issues/7", "user_notes_count": 1, "updated_at": time.Now().UTC().Format(time.RFC3339)})
	srv.AddNote(7, "lh-bot", "🤖 Vraag\n\nWelke kleur?", false)
	dir := repoDir(t, `[forge]
kind = "gitlab"
repo = "g/sub/p"

[forge.board]

[forge.board.status_options]
"Controller" = "To do"
`)
	vars := map[string]string{"GITLAB_TOKEN": "gl-secret", "GH_TOKEN": "gh-secret"}
	github := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("GitHub got %s %s, want no request for a GitLab repo", r.Method, r.URL)
	}))
	defer github.Close()
	o := newForgeOpener(func(k string) string { return vars[k] }, github.URL)
	o.gitlabURL = srv.URL()
	var errOut bytes.Buffer
	repos, slugs, err := forgeReposWith(hostToml(t, dir), "", o, &errOut)
	if err != nil || len(repos) != 1 || len(slugs) != 1 || slugs[0] != "g/sub/p" {
		t.Fatalf("forgeReposWith = %v, %v, %v; want the one GitLab repo", repos, slugs, err)
	}
	if errOut.Len() != 0 {
		t.Errorf("errOut = %q, want nothing", errOut.String())
	}
	srv.SetWorkItemStatus(7, "To do")
	ov, err := overview.BuildAll(t.Context(), repos)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if len(ov.Questions) != 1 || ov.Questions[0].Number != 7 || ov.Questions[0].Repo != "g/sub/p" ||
		!strings.Contains(ov.Questions[0].Question, "Welke kleur?") {
		t.Errorf("Questions = %+v, want the question on g/sub/p#7", ov.Questions)
	}
}

// Every production source gets a NewTrusted with the answer_permissions of [dispatch]: an
// answer of a login without such a role leaves the question open, one of an admin closes it.
func TestForgeReposSourcesCarryNewTrusted(t *testing.T) {
	srv := gitlabtest.New(t)
	srv.Login = "lh-bot"
	srv.Project = "g/sub/p"
	srv.SetStatuses("To do")
	srv.AddIssue(map[string]any{"iid": 7, "id": 9007, "title": "zeven", "state": "opened", "labels": []string{},
		"web_url": "https://gl/g/sub/p/-/issues/7", "user_notes_count": 2, "updated_at": time.Now().UTC().Format(time.RFC3339)})
	srv.AddNote(7, "lh-bot", "🤖 Vraag\n\nWelke kleur?", false)
	srv.AddNote(7, "guest", "blauw", false)
	srv.SetMember("guest", 10)
	srv.SetMember("owner", 50)
	dir := repoDir(t, `[forge]
kind = "gitlab"
repo = "g/sub/p"

[forge.board]

[forge.board.status_options]
"Controller" = "To do"
`)
	vars := map[string]string{"GITLAB_TOKEN": "gl-secret"}
	o := newForgeOpener(func(k string) string { return vars[k] }, "http://127.0.0.1:1")
	o.gitlabURL = srv.URL()
	repos, _, err := forgeReposWith(hostToml(t, dir), "", o, io.Discard)
	if err != nil || len(repos) != 1 {
		t.Fatalf("forgeReposWith = %v, %v; want the one repo", repos, err)
	}
	if repos[0].NewTrusted == nil {
		t.Fatal("NewTrusted is nil: an answer of anyone would count")
	}
	srv.SetWorkItemStatus(7, "To do")
	ov, err := overview.BuildAll(t.Context(), repos)
	if err != nil || len(ov.Questions) != 1 {
		t.Fatalf("BuildAll = %+v, %v; want the question open after an answer of a read member", ov, err)
	}
	srv.AddNote(7, "owner", "blauw dus", false)
	ov, err = overview.BuildAll(t.Context(), repos)
	if err != nil || len(ov.Questions) != 0 {
		t.Errorf("BuildAll = %+v, %v; want no question after an answer of an admin", ov, err)
	}
}

func TestStartPrioritiesWithoutReposReportsNotConfigured(t *testing.T) {
	overviewRead := func(context.Context) (overview.Overview, error) {
		t.Error("overview read without sources")
		return overview.Overview{}, nil
	}
	read, _, stopped := startPriorities(context.Background(), nil, overviewRead, server.PrioritiesInterval)
	if _, err := read(context.Background()); !errors.Is(err, forge.ErrNotConfigured) {
		t.Fatalf("err = %v, want forge.ErrNotConfigured", err)
	}
	done := make(chan struct{})
	go func() { stopped(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stopped() blocked without sources")
	}
}

func TestStartPrioritiesFailedOverviewFailsBuild(t *testing.T) {
	boom := errors.New("overview down")
	overviewRead := func(context.Context) (overview.Overview, error) { return overview.Overview{}, boom }
	ctx, cancel := context.WithCancel(context.Background())
	read, _, stopped := startPriorities(ctx, []priorities.Source{{Slug: "o/r", Src: &countingSource{}}}, overviewRead, server.PrioritiesInterval)
	defer func() { cancel(); stopped() }()
	if _, err := read(ctx); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the overview error", err)
	}
}

// planningSource is a forge.Forge with one milestone whose description it keeps. The embedded
// forge.Forge is nil: a method not defined here would panic, so a test reaches only these.
type planningSource struct {
	forge.Forge
	mu          sync.Mutex
	description string
}

func (p *planningSource) Planning(context.Context) (forge.Planning, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return forge.Planning{Milestones: []forge.MilestoneCount{{Milestone: forge.Milestone{Number: 3, Title: "M", Description: p.description}}}}, nil
}

func (p *planningSource) OpenIssues(context.Context) ([]forge.Issue, error) { return nil, nil }

func (p *planningSource) Comments(context.Context, int) ([]protocol.Comment, error) { return nil, nil }

func (p *planningSource) MilestoneByNumber(context.Context, int) (forge.Milestone, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return forge.Milestone{Number: 3, Title: "M", State: "open", Description: p.description}, nil
}

func (p *planningSource) EditMilestone(_ context.Context, number int, _, description, _ *string) (forge.Milestone, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.description = *description
	return forge.Milestone{Number: number, Title: "M", State: "open", Description: p.description}, nil
}

func TestStartPrioritiesPatchChangesTheCachedValue(t *testing.T) {
	overviewRead := func(context.Context) (overview.Overview, error) { return overview.Overview{}, nil }
	ctx, cancel := context.WithCancel(context.Background())
	src := &planningSource{description: "Priority: 1"}
	read, patch, stopped := startPriorities(ctx, []priorities.Source{{Slug: "o/r", Src: src}}, overviewRead, server.PrioritiesInterval)
	defer func() { cancel(); stopped() }()
	if _, err := read(ctx); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !patch(func(repos []priorities.Repo) []priorities.Repo {
		return priorities.WithDescription(repos, "o/r", 3, "Priority: 2")
	}) {
		t.Fatal("patch = false, want true after a successful build")
	}
	got, err := read(ctx)
	if err != nil {
		t.Fatalf("read after patch: %v", err)
	}
	if len(got) != 1 || len(got[0].Milestones) != 1 || got[0].Milestones[0].Description != "Priority: 2" {
		t.Errorf("read = %+v, want the patched description Priority: 2 without a new build", got)
	}
}

func TestStartPrioritiesWithoutSourcesPatchIsANoOp(t *testing.T) {
	read, patch, stopped := startPriorities(context.Background(), nil, nil, server.PrioritiesInterval)
	defer stopped()
	called := false
	if patch(func(r []priorities.Repo) []priorities.Repo { called = true; return r }) || called {
		t.Errorf("patch ran or returned true without a refresher (called=%v)", called)
	}
	if _, err := read(context.Background()); !errors.Is(err, forge.ErrNotConfigured) {
		t.Errorf("err = %v, want forge.ErrNotConfigured", err)
	}
}

// The rank endpoint writes to the forge and the cache shows the new description at once, with
// no build in between (the refresh interval is far longer than the test).
func TestServeMilestoneRankEndToEnd(t *testing.T) {
	src := &planningSource{description: "Priority: 1"}
	addr, stop := startServeRepos(t, []overview.RepoSource{{Slug: "o/r", Src: src}})
	get := func() string {
		resp, err := http.Get("http://" + addr + "/api/priorities")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	if body := get(); !strings.Contains(body, "Priority: 1") {
		t.Fatalf("before = %s, want Priority: 1", body)
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/api/priorities/milestone-ranks",
		strings.NewReader(`{"repo":"o/r","changes":[{"number":3,"from":1,"to":2}]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://"+addr)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST = %d %s, want 200", resp.StatusCode, b)
	}
	if body := get(); !strings.Contains(body, "Priority: 2") || strings.Contains(body, "Priority: 1") {
		t.Errorf("after = %s, want Priority: 2 only", body)
	}
	if err := stop(); err != nil {
		t.Fatalf("serve = %v", err)
	}
}

// servePriorities serves the fake with host.toml hostPath and the --repo o/r, and returns the
// issue label keys of /api/priorities by issue number.
func servePriorities(t *testing.T, hostPath string, fake *forgetest.Server) map[int][]string {
	t.Helper()
	t.Setenv("LIGHTHOUSE_HOST_TOML", hostPath)
	t.Setenv("GH_TOKEN", "t")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errOut := &syncBuf{}
	root := NewRoot(strings.NewReader(""), io.Discard, errOut)
	root.SetArgs([]string{"serve", "--addr", "127.0.0.1:0", "--db", filepath.Join(t.TempDir(), "l.db"),
		"--repo", "o/r", "--forge-url", fake.URL()})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	addr := waitListening(t, errOut, done)

	var body []byte
	for deadline := time.Now().Add(5 * time.Second); ; {
		resp, err := http.Get("http://" + addr + "/api/priorities")
		if err != nil {
			t.Fatal(err)
		}
		body, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("priorities = %d: %s", resp.StatusCode, body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var got struct {
		Repos []struct {
			Issues []struct {
				Number int      `json:"number"`
				Labels []string `json:"labels"`
			} `json:"issues"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(body, &got); err != nil || len(got.Repos) != 1 {
		t.Fatalf("body = %s, err %v", body, err)
	}
	keys := map[int][]string{}
	for _, i := range got.Repos[0].Issues {
		keys[i.Number] = i.Labels
	}
	return keys
}

func TestServePrioritiesLabelKeyFromRepoConfig(t *testing.T) {
	fake := forgetest.New(t)
	fake.Issues = map[int]map[string]any{
		1: {"number": 1, "title": "one", "html_url": "https://h/1", "state": "open", "labels": []any{map[string]any{"name": "shape-me"}}},
		2: {"number": 2, "title": "two", "html_url": "https://h/2", "state": "open", "labels": []any{map[string]any{"name": "needs-mind"}}},
	}
	repo := repoDir(t, "[forge]\nrepo = \"o/r\"\n\n[labels]\nneeds_mind = \"shape-me\"\n")
	got := servePriorities(t, hostToml(t, repo), fake)
	if !reflect.DeepEqual(got[1], []string{"needs_mind"}) || len(got[2]) != 0 {
		t.Errorf("label keys = %v, want #1 needs_mind through the configured name and #2 none", got)
	}
}

func TestServePrioritiesLabelKeyFromRepoFlagUsesDefaults(t *testing.T) {
	fake := forgetest.New(t)
	fake.Issues = map[int]map[string]any{
		1: {"number": 1, "title": "one", "html_url": "https://h/1", "state": "open", "labels": []any{map[string]any{"name": "needs-mind"}}},
	}
	got := servePriorities(t, filepath.Join(t.TempDir(), "host.toml"), fake)
	if !reflect.DeepEqual(got[1], []string{"needs_mind"}) {
		t.Errorf("label keys = %v, want #1 needs_mind through the default name", got)
	}
}

// TestServeTelemetryOnAFreshDatabase: without a database file, store.Open creates it, so the
// read-only open beside it succeeds and the telemetry endpoints serve.
func TestServeTelemetryOnAFreshDatabase(t *testing.T) {
	addr, stop := startServe(t)
	code, body := getBody(t, addr, "/api/telemetry/coverage")
	if code != http.StatusOK || !strings.Contains(body, `"not_migrated":false`) {
		t.Errorf("coverage = %d %s, want 200 on a migrated database", code, body)
	}
	if code, body := getBody(t, addr, "/api/telemetry/trajectories"); code != http.StatusOK || strings.TrimSpace(body) != `{"trajectories":[]}` {
		t.Errorf("trajectories = %d %s, want 200 with none", code, body)
	}
	if err := stop(); err != nil {
		t.Fatalf("serve = %v, want nil", err)
	}
}

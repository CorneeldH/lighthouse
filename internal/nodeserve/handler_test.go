package nodeserve

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

const nodeToken = "node-secret-7f3a"

// fakeForge is the node's own forge: GET /api/v1/workspaces/{id} answers the worktree_path
// in paths, and 404 for any other id; GET /api/v1/workspaces/{id}/agent-sessions answers the
// raw body in sessions, and 404 for an id without one. It counts every request.
type fakeForge struct {
	paths    map[string]string
	sessions map[string]string
	fail     atomic.Bool // answer 503 to everything
	hits     atomic.Int32
}

func (f *fakeForge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.hits.Add(1)
	if f.fail.Load() {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	id, ok := strings.CutPrefix(r.URL.Path, "/api/v1/workspaces/")
	if wsID, isSessions := strings.CutSuffix(id, "/agent-sessions"); ok && isSessions {
		body, known := f.sessions[wsID]
		if r.Method != http.MethodGet || !known {
			http.Error(w, `{"detail":"workspace not found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
		return
	}
	p, known := f.paths[id]
	if r.Method != http.MethodGet || !ok || !known {
		http.Error(w, `{"detail":"workspace not found"}`, http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "worktree_path": p, "git_head_ref": "issue-1-x"})
}

// countingRunner runs git for real and counts the calls.
type countingRunner struct{ calls atomic.Int32 }

func (c *countingRunner) run(ctx context.Context, timeout time.Duration, name string, args ...string) (string, int, error) {
	c.calls.Add(1)
	return workplace.ExecRunner()(ctx, timeout, name, args...)
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// gitRepo makes a git repository in a temp dir and returns its path.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return dir
}

type fixture struct {
	h      *handler
	forge  *fakeForge
	runner *countingRunner
}

// newFixture builds the node handler over a fake forge that knows paths. The node's token
// file holds nodeTokenContent.
func newFixture(t *testing.T, paths map[string]string, nodeTokenContent string) fixture {
	t.Helper()
	return newForgeFixture(t, &fakeForge{paths: paths}, nodeTokenContent)
}

// newForgeFixture builds the node handler over the fake forge ff, set up before it serves.
func newForgeFixture(t *testing.T, ff *fakeForge, nodeTokenContent string) fixture {
	t.Helper()
	dir := t.TempDir()
	srv := httptest.NewServer(ff)
	t.Cleanup(srv.Close)
	cr := &countingRunner{}
	h := newHandler(Deps{
		StateDir:  filepath.Join(dir, "state"),
		TokenFile: writeFile(t, dir, "node.token", nodeTokenContent),
		Forge: hostconfig.DispatchForge{
			URL:       srv.URL,
			TokenFile: writeFile(t, dir, "forge.token", "forge-token\n"),
		},
		Profiles: map[string]bool{"default": true},
		Home:     filepath.Join(dir, "home"),
		Run:      cr.run,
	})
	// A probe route that only resolves, so the tests see resolve's errors as HTTP answers.
	type probeIn struct {
		ID string `path:"id"`
	}
	type probeOut struct {
		Body struct {
			Path string `json:"path"`
		}
	}
	huma.Get(h.api, nodeapi.Prefix+"/workspaces/{id}/probe", func(ctx context.Context, in *probeIn) (*probeOut, error) {
		ws, err := h.resolve(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		out := &probeOut{}
		out.Body.Path = ws.Path
		return out, nil
	}, func(op *huma.Operation) { op.OperationID = "probe" })
	return fixture{h: h, forge: ff, runner: cr}
}

func call(h http.Handler, path, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func problem(t *testing.T, rec *httptest.ResponseRecorder) nodeapi.Problem {
	t.Helper()
	var p nodeapi.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("answer is not a problem: %v: %s", err, rec.Body.String())
	}
	return p
}

const bearer = "Bearer " + nodeToken

func TestNoTokenIs401(t *testing.T) {
	f := newFixture(t, nil, nodeToken+"\n")
	rec := call(f.h, nodeapi.PathInfo, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

func TestWrongTokenIs403(t *testing.T) {
	f := newFixture(t, nil, nodeToken+"\n")
	for _, auth := range []string{"Bearer node-secret-7f3b", "Bearer " + nodeToken + "x", "Bearer "} {
		rec := call(f.h, nodeapi.PathInfo, auth)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%q: status = %d, want 403", auth, rec.Code)
		}
	}
}

func TestEmptyTokenFileIsError(t *testing.T) {
	f := newFixture(t, nil, "  \n")
	// An empty token file must never let the empty bearer in.
	for _, auth := range []string{bearer, "Bearer "} {
		rec := call(f.h, nodeapi.PathInfo, auth)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%q: status = %d, want 500", auth, rec.Code)
		}
	}
	// The token is read per request: once it is written, the next request passes.
	if err := os.WriteFile(f.h.d.TokenFile, []byte(nodeToken), 0o600); err != nil {
		t.Fatal(err)
	}
	if rec := call(f.h, nodeapi.PathInfo, bearer); rec.Code != http.StatusOK {
		t.Fatalf("after writing the token: status = %d, want 200", rec.Code)
	}
}

func TestInfoReportsVersionOne(t *testing.T) {
	f := newFixture(t, nil, nodeToken)
	rec := call(f.h, nodeapi.PathInfo, bearer)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var info nodeapi.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Version != 1 {
		t.Errorf("version = %d, want 1", info.Version)
	}
	if want := runtime.GOOS + "/" + runtime.GOARCH; info.Platform != want {
		t.Errorf("platform = %q, want %q", info.Platform, want)
	}
	if info.Binary == "" {
		t.Error("binary version is empty")
	}
}

func TestUnknownWorkspaceIs404WithCode(t *testing.T) {
	f := newFixture(t, map[string]string{}, nodeToken)
	rec := call(f.h, nodeapi.Prefix+"/workspaces/ws-9/probe", bearer)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if p := problem(t, rec); p.Code != "workspace_gone" {
		t.Errorf("code = %q, want workspace_gone", p.Code)
	}
	if n := f.forge.hits.Load(); n != 1 {
		t.Errorf("forge requests = %d, want 1", n)
	}
	if n := f.runner.calls.Load(); n != 0 {
		t.Errorf("runner calls = %d, want 0", n)
	}
}

func TestForgeServerErrorIsNotGone(t *testing.T) {
	f := newFixture(t, map[string]string{}, nodeToken)
	f.forge.fail.Store(true)
	rec := call(f.h, nodeapi.Prefix+"/workspaces/ws-1/probe", bearer)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", rec.Code, rec.Body.String())
	}
	if p := problem(t, rec); p.Code != "" {
		t.Errorf("code = %q, want none", p.Code)
	}
}

func TestUnsafeIDIs400BeforeAnyCall(t *testing.T) {
	f := newFixture(t, map[string]string{}, nodeToken)
	for _, id := range []string{"_x", ".hidden", "a%20b", "-x", "local-1"} {
		rec := call(f.h, nodeapi.Prefix+"/workspaces/"+id+"/probe", bearer)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400", id, rec.Code)
		}
	}
	if n := f.forge.hits.Load(); n != 0 {
		t.Errorf("forge requests = %d, want 0", n)
	}
	if n := f.runner.calls.Load(); n != 0 {
		t.Errorf("runner calls = %d, want 0", n)
	}
}

func TestResolveAcceptsToplevel(t *testing.T) {
	repo := gitRepo(t)
	f := newFixture(t, map[string]string{"ws-1": repo}, nodeToken)
	ws, err := f.h.resolve(context.Background(), "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if ws.ID != "ws-1" || ws.Path != want || ws.Branch != "issue-1-x" {
		t.Errorf("workspace = %+v, want ws-1 at %s on issue-1-x", ws, want)
	}
}

func TestResolveRejectsRelativePath(t *testing.T) {
	f := newFixture(t, map[string]string{"ws-1": "relative/repo"}, nodeToken)
	_, err := f.h.resolve(context.Background(), "ws-1")
	assertProblem(t, err, http.StatusBadGateway)
	if n := f.runner.calls.Load(); n != 0 {
		t.Errorf("runner calls = %d, want 0", n)
	}
}

func TestResolveRejectsPathNotToplevel(t *testing.T) {
	repo := gitRepo(t)
	sub := filepath.Join(repo, "sub")
	if err := os.Mkdir(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	notRepo := t.TempDir()
	f := newFixture(t, map[string]string{"ws-sub": sub, "ws-plain": notRepo}, nodeToken)
	for _, id := range []string{"ws-sub", "ws-plain"} {
		_, err := f.h.resolve(context.Background(), id)
		assertProblem(t, err, http.StatusInternalServerError)
	}
}

func TestResolveFollowsSymlink(t *testing.T) {
	repo := gitRepo(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(repo, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	f := newFixture(t, map[string]string{"ws-1": link}, nodeToken)
	ws, err := f.h.resolve(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Path != want {
		t.Errorf("path = %q, want %q", ws.Path, want)
	}
}

func assertProblem(t *testing.T, err error, status int) {
	t.Helper()
	var p *nodeapi.Problem
	if !errors.As(err, &p) {
		t.Fatalf("err = %v, want a *nodeapi.Problem", err)
	}
	if p.Status != status {
		t.Errorf("status = %d, want %d (%s)", p.Status, status, p.Detail)
	}
}

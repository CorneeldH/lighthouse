package kennforge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

const testSessionID = "11111111-1111-4111-8111-111111111111"

// fakeForge scripts the forge routes and counts the requests per route.
type fakeForge struct {
	t  *testing.T
	mu sync.Mutex

	counts map[string]int
	bodies map[string]string

	// listBody answers GET /api/v1/workspaces.
	listBody string
	// createBody answers the POST that creates a workspace.
	createBody string
	// statuses answers GET /workspaces/ws1, one entry per call; the last repeats.
	statuses []string
	path     string
	// headRef is the git_head_ref of GET /workspaces/ws1; getStatus, when set, answers it instead.
	headRef   string
	getStatus int
	// sessions answers the agent-sessions reads, one entry per call; the last repeats.
	sessions []string
	// sessionsStatus, when set, answers the agent-sessions reads instead.
	sessionsStatus int
	// handoff answers the handoff POST.
	handoffStatus int
	handoffBody   string
	handoffBlock  bool
	// deleteStatus answers the DELETE.
	deleteStatus int
	release      chan struct{}
	// onHandoff runs when the handoff POST arrives.
	onHandoff func()
	// offline makes the fake hub answer this node as a spoke without a tunnel.
	offline bool
	// back: the hub answers this node as offline, but the next snapshot already reports it
	// reachable again, as when the tunnel comes back between the two requests.
	back bool
}

// newForgeNode is a fake forge without a server of its own, as a node behind newFakeHub.
func newForgeNode(t *testing.T) *fakeForge {
	t.Helper()
	f := &fakeForge{
		t: t, counts: map[string]int{}, bodies: map[string]string{},
		listBody: `{"workspaces":[]}`, createBody: `{"id":"ws1"}`,
		statuses: []string{"ready"}, sessions: []string{`{"sessions":[]}`},
		handoffStatus: 200, handoffBody: `{"session":{"key":"k-new"}}`,
		deleteStatus: 204, release: make(chan struct{}),
	}
	t.Cleanup(func() { close(f.release) })
	return f
}

func newFakeForge(t *testing.T) (*fakeForge, *Adapter) {
	t.Helper()
	f := newForgeNode(t)
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	tok := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tok, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &Adapter{
		Client: New(srv.URL, tok), Kind: "github", Repo: "CorneeldH/lighthouse",
		StateDir: t.TempDir(), Targets: map[string]string{"software": "paia-software"},
		UnitPrefix: "paia-forge-",
		Sleep:      func(time.Duration) { f.mu.Lock(); f.counts["sleep"]++; f.mu.Unlock() },
	}
	return f, a
}

func (f *fakeForge) count(k string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[k]
}

func (f *fakeForge) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.counts)
}

func (f *fakeForge) body(k string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[k]
}

func next(list []string, n int) string {
	if n >= len(list) {
		return list[len(list)-1]
	}
	return list[n]
}

func (f *fakeForge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	key := r.Method + " " + r.URL.EscapedPath()
	n := f.counts[key]
	f.counts[key]++
	if r.Body != nil {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		b, _ := json.Marshal(m)
		f.bodies[key] = string(b)
	}
	f.mu.Unlock()
	switch key {
	case "GET /api/v1/workspaces":
		_, _ = w.Write([]byte(f.listBody))
	case "POST /api/v1/repo/github/CorneeldH/lighthouse/workspaces":
		_, _ = w.Write([]byte(f.createBody))
	case "GET /api/v1/workspaces/ws1":
		if f.getStatus != 0 {
			w.WriteHeader(f.getStatus)
			return
		}
		_, _ = fmt.Fprintf(w, `{"id":"ws1","status":%q,"worktree_path":%q,"git_head_ref":%q}`, next(f.statuses, n), f.path, f.headRef)
	case "GET /api/v1/workspaces/ws1/agent-sessions":
		if f.sessionsStatus != 0 {
			w.WriteHeader(f.sessionsStatus)
			return
		}
		_, _ = w.Write([]byte(next(f.sessions, n)))
	case "POST /api/v1/workspaces/ws1/runtime/agent-handoffs":
		if f.onHandoff != nil {
			f.onHandoff()
		}
		if f.handoffBlock {
			select {
			case <-r.Context().Done():
			case <-f.release:
			}
			return
		}
		w.WriteHeader(f.handoffStatus)
		_, _ = w.Write([]byte(f.handoffBody))
	case "DELETE /api/v1/workspaces/ws1":
		w.WriteHeader(f.deleteStatus)
	default:
		f.t.Errorf("unexpected request %s", key)
		w.WriteHeader(404)
	}
}

func listWith(ref string) string {
	return fmt.Sprintf(`{"workspaces":[{"id":"ws1","git_head_ref":%q,"repo":{"provider":"github","repo_path":"corneeldh/lighthouse"}}]}`, ref)
}

func TestPrepareFindsExisting(t *testing.T) {
	f, a := newFakeForge(t)
	f.listBody = listWith("refs/heads/issue-1-x")
	f.path = t.TempDir()
	a.Run = func(context.Context, time.Duration, string, ...string) (string, int, error) { return "", 0, nil }
	ws, err := a.Prepare(context.Background(), "issue-1-x")
	if err != nil {
		t.Fatal(err)
	}
	if ws.ID != "ws1" || ws.Path != f.path || ws.Branch != "issue-1-x" {
		t.Errorf("ws = %+v", ws)
	}
	if n := f.count("POST /api/v1/repo/github/CorneeldH/lighthouse/workspaces"); n != 0 {
		t.Errorf("create POSTs = %d, want 0", n)
	}
}

func TestPrepareMatchesBareBranchName(t *testing.T) {
	f, a := newFakeForge(t)
	f.listBody = listWith("issue-1-x")
	f.path = t.TempDir()
	a.Run = func(context.Context, time.Duration, string, ...string) (string, int, error) { return "", 0, nil }
	if _, err := a.Prepare(context.Background(), "issue-1-x"); err != nil {
		t.Fatal(err)
	}
	if n := f.count("POST /api/v1/repo/github/CorneeldH/lighthouse/workspaces"); n != 0 {
		t.Errorf("create POSTs = %d, want 0", n)
	}
}

func TestPrepareCreatesAndWaitsReady(t *testing.T) {
	f, a := newFakeForge(t)
	f.statuses = []string{"creating", "creating", "ready"}
	f.path = t.TempDir()
	a.Run = func(context.Context, time.Duration, string, ...string) (string, int, error) { return "", 0, nil }
	if _, err := a.Prepare(context.Background(), "issue-1-x"); err != nil {
		t.Fatal(err)
	}
	if n := f.count("POST /api/v1/repo/github/CorneeldH/lighthouse/workspaces"); n != 1 {
		t.Errorf("create POSTs = %d, want 1", n)
	}
	if got := f.body("POST /api/v1/repo/github/CorneeldH/lighthouse/workspaces"); got != `{"branch":"issue-1-x"}` {
		t.Errorf("create body = %s", got)
	}
	if n := f.count("sleep"); n != 2 {
		t.Errorf("sleeps = %d, want 2", n)
	}
}

func TestPrepareFoundWaitsReady(t *testing.T) {
	f, a := newFakeForge(t)
	f.listBody = listWith("issue-1-x")
	f.statuses = []string{"creating", "ready"}
	f.path = t.TempDir()
	a.Run = func(context.Context, time.Duration, string, ...string) (string, int, error) { return "", 0, nil }
	if _, err := a.Prepare(context.Background(), "issue-1-x"); err != nil {
		t.Fatal(err)
	}
	if n := f.count("POST /api/v1/repo/github/CorneeldH/lighthouse/workspaces"); n != 0 {
		t.Errorf("create POSTs = %d, want 0", n)
	}
	if n := f.count("sleep"); n != 1 {
		t.Errorf("sleeps = %d, want 1", n)
	}
}

func TestPrepareReadyTimeout(t *testing.T) {
	f, a := newFakeForge(t)
	f.statuses = []string{"creating"}
	f.path = t.TempDir()
	a.ReadyWait = 3 * time.Second
	_, err := a.Prepare(context.Background(), "issue-1-x")
	if err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("err = %v, want a not ready error", err)
	}
	if n := f.count("sleep"); n != 3 {
		t.Errorf("sleeps = %d, want 3", n)
	}
}

// --- real git -------------------------------------------------------------

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com",
		"-c", "commit.gpgsign=false", "-c", "protocol.file.allow=always"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, file string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", file)
	git(t, dir, "commit", "-m", file)
	return git(t, dir, "rev-parse", "HEAD")
}

// gitWorld has a bare origin with main, and a workspace clone made before any
// issue branch exists.
type gitWorld struct {
	origin, ws, other string
	m1                string
}

func newGitWorld(t *testing.T) gitWorld {
	t.Helper()
	root := t.TempDir()
	w := gitWorld{origin: filepath.Join(root, "origin.git"), ws: filepath.Join(root, "ws"), other: filepath.Join(root, "other")}
	git(t, root, "init", "--bare", "-b", "main", w.origin)
	git(t, root, "clone", w.origin, w.other)
	git(t, w.other, "checkout", "-b", "main")
	w.m1 = commit(t, w.other, "m1")
	git(t, w.other, "push", "origin", "main")
	git(t, root, "clone", w.origin, w.ws)
	return w
}

// pushBranch makes issue-1-x on origin one commit past main as it was at m1.
func (w gitWorld) pushBranch(t *testing.T) string {
	t.Helper()
	git(t, w.other, "checkout", "-b", "issue-1-x", w.m1)
	b := commit(t, w.other, "b")
	git(t, w.other, "push", "origin", "issue-1-x")
	return b
}

func gitAdapter(t *testing.T, ws string, statuses ...string) *Adapter {
	f, a := newFakeForge(t)
	f.path = ws
	f.listBody = listWith("issue-1-x")
	if len(statuses) > 0 {
		f.statuses = statuses
	}
	a.Run = workplace.ExecRunner()
	return a
}

func TestPrepareFollowsRemoteBranch(t *testing.T) {
	w := newGitWorld(t)
	b := w.pushBranch(t)
	f, a := newFakeForge(t)
	f.path = w.ws // created workspace: forge puts it on origin/HEAD = m1
	a.Run = workplace.ExecRunner()
	if _, err := a.Prepare(context.Background(), "issue-1-x"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, w.ws, "rev-parse", "HEAD"); got != b {
		t.Errorf("HEAD = %s, want %s", got, b)
	}
	if got := git(t, w.ws, "rev-parse", "--abbrev-ref", "HEAD"); got != "issue-1-x" {
		t.Errorf("branch = %s, want issue-1-x", got)
	}
	if got := git(t, w.ws, "rev-parse", "--abbrev-ref", "issue-1-x@{upstream}"); got != "origin/issue-1-x" {
		t.Errorf("upstream = %s", got)
	}
}

func TestPrepareNoRemoteBranchLeavesHEAD(t *testing.T) {
	w := newGitWorld(t)
	f, a := newFakeForge(t)
	f.path = w.ws
	a.Run = workplace.ExecRunner()
	if _, err := a.Prepare(context.Background(), "issue-1-x"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, w.ws, "rev-parse", "HEAD"); got != w.m1 {
		t.Errorf("HEAD = %s, want %s", got, w.m1)
	}
}

func TestPrepareFoundBehindFastForwards(t *testing.T) {
	w := newGitWorld(t)
	b := w.pushBranch(t)
	// Found workspace on a local branch at m1, a true ancestor of origin/issue-1-x.
	git(t, w.ws, "checkout", "-b", "issue-1-x")
	a := gitAdapter(t, w.ws)
	if _, err := a.Prepare(context.Background(), "issue-1-x"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, w.ws, "rev-parse", "HEAD"); got != b {
		t.Errorf("HEAD = %s, want %s", got, b)
	}
}

// ahead gives the found workspace issue-1-x tracking origin/issue-1-x with one commit c on
// top that was never pushed, as a run leaves it that paused or handed off without a push
// (#356). It returns c.
func (w gitWorld) ahead(t *testing.T) string {
	t.Helper()
	w.pushBranch(t)
	git(t, w.ws, "fetch", "origin")
	git(t, w.ws, "checkout", "-b", "issue-1-x", "--track", "origin/issue-1-x")
	return commit(t, w.ws, "c")
}

// A HEAD that already holds origin/<branch> is no stale HEAD: the run starts on it, and the
// commit origin lacks stays (#356). Until #356 this case handed the run back.
func TestPrepareFoundAheadStartsOnHead(t *testing.T) {
	w := newGitWorld(t)
	c := w.ahead(t)
	a := gitAdapter(t, w.ws)
	ws, err := a.Prepare(context.Background(), "issue-1-x")
	if err != nil {
		t.Fatalf("Prepare: %v, want a start on HEAD", err)
	}
	if ws.ID != "ws1" {
		t.Errorf("workspace = %+v, want ws1", ws)
	}
	if got := git(t, w.ws, "rev-parse", "HEAD"); got != c {
		t.Errorf("HEAD = %s, want own commit %s", got, c)
	}
}

// A dirty work tree does not change that, as with HEAD equal to origin.
func TestPrepareFoundAheadDirtyStartsOnHead(t *testing.T) {
	w := newGitWorld(t)
	c := w.ahead(t)
	if err := os.WriteFile(filepath.Join(w.ws, "c"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.ws, "new"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := gitAdapter(t, w.ws)
	if _, err := a.Prepare(context.Background(), "issue-1-x"); err != nil {
		t.Fatalf("Prepare: %v, want a start on HEAD", err)
	}
	if got := git(t, w.ws, "rev-parse", "HEAD"); got != c {
		t.Errorf("HEAD = %s, want own commit %s", got, c)
	}
	if got := git(t, w.ws, "status", "--porcelain"); got != "M c\n?? new" {
		t.Errorf("status = %q, want the changes kept", got)
	}
}

// Diverged: origin/issue-1-x has b, HEAD has c, both on m1. Starting on HEAD would miss b,
// and a checkout would drop c: the run goes back (#269).
func TestPrepareFoundDivergedOwnCommitHandsBack(t *testing.T) {
	w := newGitWorld(t)
	w.pushBranch(t)
	git(t, w.ws, "checkout", "-b", "issue-1-x")
	c := commit(t, w.ws, "c")
	a := gitAdapter(t, w.ws)
	_, err := a.Prepare(context.Background(), "issue-1-x")
	var own *workplace.OwnWorkError
	if !errors.As(err, &own) {
		t.Fatalf("err = %v, want an OwnWorkError", err)
	}
	if own.Workspace.ID != "ws1" || own.Commits != 1 || own.Dirty {
		t.Errorf("own = %+v, want ws1 with 1 commit, clean", own)
	}
	if got := git(t, w.ws, "rev-parse", "HEAD"); got != c {
		t.Errorf("HEAD = %s, want own commit %s", got, c)
	}
}

// rebase gives the found workspace a pushed commit c on issue-1-x, then rebases issue-1-x
// on a newer main elsewhere and force-pushes it. It returns the new origin/issue-1-x.
func (w gitWorld) rebase(t *testing.T) string {
	t.Helper()
	w.pushBranch(t)
	git(t, w.ws, "fetch", "origin")
	git(t, w.ws, "checkout", "-b", "issue-1-x", "--track", "origin/issue-1-x")
	commit(t, w.ws, "c")
	git(t, w.ws, "push", "origin", "issue-1-x")
	git(t, w.other, "checkout", "main")
	commit(t, w.other, "m2")
	git(t, w.other, "push", "origin", "main")
	git(t, w.other, "checkout", "issue-1-x")
	git(t, w.other, "pull", "--ff-only", "origin", "issue-1-x")
	git(t, w.other, "rebase", "main")
	git(t, w.other, "push", "--force", "origin", "issue-1-x")
	return git(t, w.other, "rev-parse", "HEAD")
}

func TestPrepareFoundRebasedFollowsRemote(t *testing.T) {
	w := newGitWorld(t)
	want := w.rebase(t)
	a := gitAdapter(t, w.ws)
	if _, err := a.Prepare(context.Background(), "issue-1-x"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, w.ws, "rev-parse", "HEAD"); got != want {
		t.Errorf("HEAD = %s, want rebased %s", got, want)
	}
	if got := git(t, w.ws, "rev-parse", "--abbrev-ref", "issue-1-x@{upstream}"); got != "origin/issue-1-x" {
		t.Errorf("upstream = %s", got)
	}
}

func TestPrepareFoundRebasedWithOwnCommitHandsBack(t *testing.T) {
	w := newGitWorld(t)
	w.rebase(t)
	d := commit(t, w.ws, "d")
	a := gitAdapter(t, w.ws)
	_, err := a.Prepare(context.Background(), "issue-1-x")
	var own *workplace.OwnWorkError
	if !errors.As(err, &own) {
		t.Fatalf("err = %v, want an OwnWorkError", err)
	}
	// b and c are on origin under other SHAs; only d is own work.
	if own.Commits != 1 || own.Dirty {
		t.Errorf("own = %+v, want 1 commit, clean", own)
	}
	if got := git(t, w.ws, "rev-parse", "HEAD"); got != d {
		t.Errorf("HEAD = %s, want own commit %s", got, d)
	}
}

func TestPrepareFoundRebasedDirtyHandsBack(t *testing.T) {
	w := newGitWorld(t)
	w.rebase(t)
	head := git(t, w.ws, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(w.ws, "c"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := gitAdapter(t, w.ws)
	_, err := a.Prepare(context.Background(), "issue-1-x")
	var own *workplace.OwnWorkError
	if !errors.As(err, &own) {
		t.Fatalf("err = %v, want an OwnWorkError", err)
	}
	if own.Commits != 0 || !own.Dirty {
		t.Errorf("own = %+v, want 0 commits, dirty", own)
	}
	if got := git(t, w.ws, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD = %s, want unmoved %s", got, head)
	}
}

// An untracked file is own work too: checkout -B would refuse one the branch tracks, and
// workplace.Dirty counts it.
func TestPrepareFoundRebasedUntrackedHandsBack(t *testing.T) {
	w := newGitWorld(t)
	w.rebase(t)
	head := git(t, w.ws, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(w.ws, "m2"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := gitAdapter(t, w.ws)
	_, err := a.Prepare(context.Background(), "issue-1-x")
	var own *workplace.OwnWorkError
	if !errors.As(err, &own) {
		t.Fatalf("err = %v, want an OwnWorkError", err)
	}
	if own.Commits != 0 || !own.Dirty {
		t.Errorf("own = %+v, want 0 commits, dirty", own)
	}
	if got := git(t, w.ws, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD = %s, want unmoved %s", got, head)
	}
}

func TestPrepareFoundOnDivergedMain(t *testing.T) {
	w := newGitWorld(t)
	b := w.pushBranch(t)
	// main moves on to m2; the workspace sits on main at m2 without own commits.
	git(t, w.other, "checkout", "main")
	commit(t, w.other, "m2")
	git(t, w.other, "push", "origin", "main")
	git(t, w.ws, "pull", "--ff-only")
	a := gitAdapter(t, w.ws)
	if _, err := a.Prepare(context.Background(), "issue-1-x"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, w.ws, "rev-parse", "HEAD"); got != b {
		t.Errorf("HEAD = %s, want %s", got, b)
	}
}

// --- Spawn ----------------------------------------------------------------

func spec() workplace.LaunchSpec {
	return workplace.LaunchSpec{
		Repo: "lighthouse", Issue: 7, SessionID: testSessionID, Mode: "start",
		Profile: "software", Prompt: "do the thing", InstructionsFile: "/abs/instr.md",
	}
}

var ws1 = workplace.Workspace{ID: "ws1", Path: "/x", Branch: "issue-1-x"}

const handoffKey = "POST /api/v1/workspaces/ws1/runtime/agent-handoffs"

func carrierExists(a *Adapter) bool {
	_, err := os.Stat(workplace.CarrierPath(a.StateDir, "", "ws1"))
	return err == nil
}

func sessionsJSON(entries ...string) string {
	return `{"sessions":[` + strings.Join(entries, ",") + `]}`
}

func entry(key, sid string) string {
	return fmt.Sprintf(`{"runtime_session_key":%q,"session_id":%q,"state":"working"}`, key, sid)
}

func TestSpawnAccepted(t *testing.T) {
	f, a := newFakeForge(t)
	s, err := a.Spawn(context.Background(), ws1, spec())
	if err != nil {
		t.Fatal(err)
	}
	if s.Key != "k-new" || s.WorkspaceID != "ws1" || s.SessionID != testSessionID {
		t.Errorf("session = %+v", s)
	}
	if n := f.count(handoffKey); n != 1 {
		t.Errorf("POSTs = %d, want 1", n)
	}
	if got := f.body(handoffKey); got != `{"message":"do the thing","target_key":"paia-software"}` {
		t.Errorf("body = %s", got)
	}
	if !carrierExists(a) {
		t.Error("carrier missing")
	}
}

func TestSpawn5xxNoSessionStartFailed(t *testing.T) {
	f, a := newFakeForge(t)
	f.handoffStatus = 503
	_, err := a.Spawn(context.Background(), ws1, spec())
	if !errors.Is(err, workplace.ErrNotAccepted) {
		t.Fatalf("err = %v, want ErrNotAccepted", err)
	}
	if n := f.count(handoffKey); n != 1 {
		t.Errorf("POSTs = %d, want 1", n)
	}
}

func TestSpawn5xxRemovesCarrier(t *testing.T) {
	f, a := newFakeForge(t)
	f.handoffStatus = 503
	_, _ = a.Spawn(context.Background(), ws1, spec())
	_, err := os.Stat(workplace.CarrierPath(a.StateDir, "", "ws1"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stat carrier = %v, want ErrNotExist", err)
	}
}

func TestSpawn5xxSessionAppeared(t *testing.T) {
	f, a := newFakeForge(t)
	f.handoffStatus = 503
	f.sessions = []string{sessionsJSON(), sessionsJSON(entry("k1", testSessionID))}
	s, err := a.Spawn(context.Background(), ws1, spec())
	if err != nil {
		t.Fatal(err)
	}
	if s.Key != "k1" || s.SessionID != testSessionID {
		t.Errorf("session = %+v", s)
	}
	if n := f.count(handoffKey); n != 1 {
		t.Errorf("POSTs = %d, want 1", n)
	}
}

func TestSpawn5xxNewKeyWithoutSessionID(t *testing.T) {
	f, a := newFakeForge(t)
	f.handoffStatus = 503
	f.sessions = []string{sessionsJSON(), sessionsJSON(entry("k1", ""))}
	s, err := a.Spawn(context.Background(), ws1, spec())
	if err != nil {
		t.Fatal(err)
	}
	if s.Key != "k1" || s.SessionID != testSessionID {
		t.Errorf("session = %+v", s)
	}
	if !carrierExists(a) {
		t.Error("carrier removed, want kept")
	}
}

func TestSpawn5xxForeignNewKey(t *testing.T) {
	f, a := newFakeForge(t)
	f.handoffStatus = 503
	f.sessions = []string{sessionsJSON(), sessionsJSON(entry("k1", "22222222-2222-4222-8222-222222222222"))}
	_, err := a.Spawn(context.Background(), ws1, spec())
	if !errors.Is(err, workplace.ErrNotAccepted) {
		t.Fatalf("err = %v, want ErrNotAccepted", err)
	}
	if carrierExists(a) {
		t.Error("carrier kept, want removed")
	}
}

func TestSpawn5xxOldKeyIsNotNew(t *testing.T) {
	f, a := newFakeForge(t)
	f.handoffStatus = 503
	same := sessionsJSON(entry("old", testSessionID))
	f.sessions = []string{same, same}
	_, err := a.Spawn(context.Background(), ws1, spec())
	if !errors.Is(err, workplace.ErrNotAccepted) {
		t.Fatalf("err = %v, want ErrNotAccepted", err)
	}
}

func TestSpawnTimeoutNeverRespawns(t *testing.T) {
	f, a := newFakeForge(t)
	f.handoffBlock = true
	a.spawnTimeout = 100 * time.Millisecond
	_, err := a.Spawn(context.Background(), ws1, spec())
	if !errors.Is(err, workplace.ErrUncertain) {
		t.Fatalf("err = %v, want ErrUncertain", err)
	}
	if n := f.count(handoffKey); n != 1 {
		t.Errorf("POSTs = %d, want 1", n)
	}
	if !carrierExists(a) {
		t.Error("carrier removed, want kept")
	}
}

func TestSpawnTimeoutSessionAppeared(t *testing.T) {
	f, a := newFakeForge(t)
	f.handoffBlock = true
	f.sessions = []string{sessionsJSON(), sessionsJSON(entry("k1", testSessionID))}
	a.spawnTimeout = 100 * time.Millisecond
	s, err := a.Spawn(context.Background(), ws1, spec())
	if err != nil {
		t.Fatal(err)
	}
	if s.Key != "k1" {
		t.Errorf("key = %s, want k1", s.Key)
	}
}

func TestSpawnNoKeyUncertain(t *testing.T) {
	f, a := newFakeForge(t)
	f.handoffBody = `{"session":{}}`
	_, err := a.Spawn(context.Background(), ws1, spec())
	if !errors.Is(err, workplace.ErrUncertain) {
		t.Fatalf("err = %v, want ErrUncertain", err)
	}
	if !carrierExists(a) {
		t.Error("carrier removed, want kept")
	}
}

func TestSpawn4xxNotAccepted(t *testing.T) {
	f, a := newFakeForge(t)
	f.handoffStatus = 422
	_, err := a.Spawn(context.Background(), ws1, spec())
	if !errors.Is(err, workplace.ErrNotAccepted) {
		t.Fatalf("err = %v, want ErrNotAccepted", err)
	}
	if carrierExists(a) {
		t.Error("carrier kept, want removed")
	}
}

func TestSpawnUnknownProfile(t *testing.T) {
	f, a := newFakeForge(t)
	l := spec()
	l.Profile = "nope"
	_, err := a.Spawn(context.Background(), ws1, l)
	if !errors.Is(err, workplace.ErrNotAccepted) {
		t.Fatalf("err = %v, want ErrNotAccepted", err)
	}
	if n := f.total(); n != 0 {
		t.Errorf("requests = %d, want none", n)
	}
}

func TestSpawnInvalidSpecNotAccepted(t *testing.T) {
	f, a := newFakeForge(t)
	l := spec()
	l.InstructionsFile = "relative.md"
	_, err := a.Spawn(context.Background(), ws1, l)
	if !errors.Is(err, workplace.ErrNotAccepted) {
		t.Fatalf("err = %v, want ErrNotAccepted", err)
	}
	if n := f.total(); n != 0 {
		t.Errorf("requests = %d, want none", n)
	}
}

// --- Sessions, Remove -----------------------------------------------------

func TestSessionsParses(t *testing.T) {
	f, a := newFakeForge(t)
	f.sessions = []string{`{"sessions":[
		{"runtime_session_key":"a","state":"idle","session_id":"s1","updated_at":"2026-10-31T10:00:00Z"},
		{"runtime_session_key":"b","state":null,"session_id":"s2","updated_at":"garbage"},
		{"state":"working","session_id":"s3"}]}`}
	got, err := a.Sessions(context.Background(), "ws1")
	if err != nil {
		t.Fatal(err)
	}
	want := []workplace.Session{
		{Key: "a", WorkspaceID: "ws1", State: "idle", SessionID: "s1", UpdatedAt: time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)},
		{Key: "b", WorkspaceID: "ws1", State: "", SessionID: "s2"},
	}
	if len(got) != len(want) {
		t.Fatalf("sessions = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("session %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestSessionsWithoutListUncertain(t *testing.T) {
	f, a := newFakeForge(t)
	f.sessions = []string{`{"other":1}`}
	if _, err := a.Sessions(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) {
		t.Fatalf("err = %v, want ErrUncertain", err)
	}
}

func TestSessionsOtherStatusNotGone(t *testing.T) {
	for _, status := range []int{403, 500} {
		f, a := newFakeForge(t)
		f.sessionsStatus = status
		if _, err := a.Sessions(context.Background(), "ws1"); err == nil || errors.Is(err, workplace.ErrGone) {
			t.Fatalf("%d: err = %v, want an error that is not ErrGone", status, err)
		}
	}
}

func TestLookupOtherStatusNotGone(t *testing.T) {
	f, a := newFakeForge(t)
	f.getStatus = 500
	if _, err := a.Lookup(context.Background(), "ws1"); err == nil || errors.Is(err, workplace.ErrGone) {
		t.Fatalf("err = %v, want an error that is not ErrGone", err)
	}
}

func TestLookupNoPath(t *testing.T) {
	f, a := newFakeForge(t)
	f.headRef = "issue-7"
	if got, err := a.Lookup(context.Background(), "ws1"); err == nil || errors.Is(err, workplace.ErrGone) {
		t.Fatalf("Lookup = %+v, %v; want an error that is not ErrGone", got, err)
	}
}

// writeCarrier puts a carrier for ws1 in the state dir, as a spawn leaves it.
func writeCarrier(t *testing.T, a *Adapter) {
	t.Helper()
	c, err := workplace.NewCarrier(spec(), a.StateDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workplace.WriteCarrier(a.StateDir, "", "ws1", c); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveWipesCarrier(t *testing.T) {
	for _, status := range []int{204, 404} {
		f, a := newFakeForge(t)
		f.deleteStatus = status
		writeCarrier(t, a)
		if err := a.Remove(context.Background(), "ws1"); err != nil {
			t.Fatalf("%d: err = %v", status, err)
		}
		if carrierExists(a) {
			t.Errorf("%d: carrier kept, want wiped", status)
		}
	}
}

func TestRemoveRefusedKeepsCarrier(t *testing.T) {
	for _, status := range []int{409, 500} {
		f, a := newFakeForge(t)
		f.deleteStatus = status
		writeCarrier(t, a)
		if err := a.Remove(context.Background(), "ws1"); err == nil {
			t.Fatalf("%d: err = nil, want an error", status)
		}
		if !carrierExists(a) {
			t.Errorf("%d: carrier wiped, want kept", status)
		}
	}
}

func TestRemoveReportsFailedCarrierWipe(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	_, a := newFakeForge(t)
	writeCarrier(t, a)
	dir := filepath.Dir(workplace.CarrierPath(a.StateDir, "", "ws1"))
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := a.Remove(context.Background(), "ws1"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("err = %v, want the wipe error", err)
	}
}

func TestDiscardWipesCarrier(t *testing.T) {
	_, a := newFakeForge(t)
	writeCarrier(t, a)
	if err := a.Discard(context.Background(), "ws1"); err != nil {
		t.Fatal(err)
	}
	if carrierExists(a) {
		t.Error("carrier kept, want wiped")
	}
	if err := a.Discard(context.Background(), "ws1"); err != nil {
		t.Errorf("second Discard = %v, want nil for a missing carrier", err)
	}
}

func TestRemoveOtherErrorPassedOn(t *testing.T) {
	f, a := newFakeForge(t)
	f.deleteStatus = 500
	if err := a.Remove(context.Background(), "ws1"); err == nil || errors.Is(err, workplace.ErrDirty) {
		t.Fatalf("err = %v, want a non-dirty error", err)
	}
}

func TestSpawnTimeoutPrefersOwnSessionOverOtherNewKey(t *testing.T) {
	f, a := newFakeForge(t)
	f.handoffBlock = true
	f.sessions = []string{sessionsJSON(), sessionsJSON(
		entry("other", "22222222-2222-4222-8222-222222222222"), entry("mine", testSessionID))}
	a.spawnTimeout = 100 * time.Millisecond
	s, err := a.Spawn(context.Background(), ws1, spec())
	if err != nil {
		t.Fatal(err)
	}
	if s.Key != "mine" {
		t.Errorf("key = %s, want mine", s.Key)
	}
}

// We pass --session-id, so a new key with another session_id is provably not ours.
func TestSpawnTimeoutOtherSessionIDNotTaken(t *testing.T) {
	f, a := newFakeForge(t)
	f.handoffBlock = true
	f.sessions = []string{sessionsJSON(), sessionsJSON(entry("other", "22222222-2222-4222-8222-222222222222"))}
	a.spawnTimeout = 100 * time.Millisecond
	s, err := a.Spawn(context.Background(), ws1, spec())
	if !errors.Is(err, workplace.ErrUncertain) || errors.Is(err, workplace.ErrNotAccepted) {
		t.Fatalf("s = %+v, err = %v, want only ErrUncertain", s, err)
	}
	if n := f.count(handoffKey); n != 1 {
		t.Errorf("POSTs = %d, want 1", n)
	}
	if !carrierExists(a) {
		t.Error("carrier removed, want kept")
	}
}

func TestSpawnTimeoutPrefersOwnSessionOverEmptySessionID(t *testing.T) {
	f, a := newFakeForge(t)
	f.handoffBlock = true
	f.sessions = []string{sessionsJSON(), sessionsJSON(entry("blank", ""), entry("mine", testSessionID))}
	a.spawnTimeout = 100 * time.Millisecond
	s, err := a.Spawn(context.Background(), ws1, spec())
	if err != nil || s.Key != "mine" {
		t.Fatalf("s = %+v, err = %v, want mine", s, err)
	}
}

func TestSpawnTimeoutNewKeyWithoutSessionID(t *testing.T) {
	f, a := newFakeForge(t)
	f.handoffBlock = true
	f.sessions = []string{sessionsJSON(), sessionsJSON(entry("k1", ""))}
	a.spawnTimeout = 100 * time.Millisecond
	s, err := a.Spawn(context.Background(), ws1, spec())
	if err != nil || s.Key != "k1" || s.SessionID != testSessionID {
		t.Fatalf("s = %+v, err = %v, want k1 accepted", s, err)
	}
}

func TestPrepareSkipsWorkspaceOfOtherRepo(t *testing.T) {
	f, a := newFakeForge(t)
	f.listBody = `{"workspaces":[
		{"id":"other","git_head_ref":"refs/heads/issue-1-x","repo":{"provider":"github","repo_path":"corneeldh/personal_ai_agent"}},
		{"id":"gl","git_head_ref":"refs/heads/issue-1-x","repo":{"provider":"gitlab","repo_path":"corneeldh/lighthouse"}}]}`
	f.path = t.TempDir()
	a.Run = func(context.Context, time.Duration, string, ...string) (string, int, error) { return "", 0, nil }
	ws, err := a.Prepare(context.Background(), "issue-1-x")
	if err != nil {
		t.Fatal(err)
	}
	if ws.ID != "ws1" {
		t.Errorf("id = %s, want the created ws1", ws.ID)
	}
	if n := f.count("POST /api/v1/repo/github/CorneeldH/lighthouse/workspaces"); n != 1 {
		t.Errorf("create POSTs = %d, want 1", n)
	}
}

func TestPrepareFoundOnUnmovedMainDoesNotMoveMain(t *testing.T) {
	w := newGitWorld(t)
	b := w.pushBranch(t)
	// The workspace sits on local main at m1, an ancestor of origin/issue-1-x.
	a := gitAdapter(t, w.ws)
	if _, err := a.Prepare(context.Background(), "issue-1-x"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, w.ws, "rev-parse", "--abbrev-ref", "HEAD"); got != "issue-1-x" {
		t.Errorf("branch = %s, want issue-1-x", got)
	}
	if got := git(t, w.ws, "rev-parse", "HEAD"); got != b {
		t.Errorf("HEAD = %s, want %s", got, b)
	}
	if got := git(t, w.ws, "rev-parse", "main"); got != w.m1 {
		t.Errorf("main = %s, want unmoved %s", got, w.m1)
	}
}

func TestSpawnSessionsReadFailsNotAccepted(t *testing.T) {
	f, a := newFakeForge(t)
	f.sessions = []string{`{"other":1}`}
	_, err := a.Spawn(context.Background(), ws1, spec())
	if !errors.Is(err, workplace.ErrNotAccepted) {
		t.Fatalf("err = %v, want ErrNotAccepted", err)
	}
	if n := f.count(handoffKey); n != 0 {
		t.Errorf("POSTs = %d, want 0", n)
	}
	if carrierExists(a) {
		t.Error("carrier kept, want removed")
	}
}

func TestSpawn3xxNotAccepted(t *testing.T) {
	f, a := newFakeForge(t)
	f.handoffStatus = 302
	_, err := a.Spawn(context.Background(), ws1, spec())
	if !errors.Is(err, workplace.ErrNotAccepted) {
		t.Fatalf("err = %v, want ErrNotAccepted", err)
	}
	if carrierExists(a) {
		t.Error("carrier kept, want removed")
	}
}

// A failed re-read after a 5xx cannot tell whether an agent started: uncertain,
// and the carrier stays for Decide to settle.
func TestSpawn5xxRereadFailsUncertain(t *testing.T) {
	f, a := newFakeForge(t)
	f.handoffStatus = 503
	f.sessions = []string{sessionsJSON(), `{"other":1}`}
	_, err := a.Spawn(context.Background(), ws1, spec())
	if !errors.Is(err, workplace.ErrUncertain) || errors.Is(err, workplace.ErrNotAccepted) {
		t.Fatalf("err = %v, want only ErrUncertain", err)
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("err = %v, want the 503 in it", err)
	}
	if n := f.count(handoffKey); n != 1 {
		t.Errorf("POSTs = %d, want 1", n)
	}
	if !carrierExists(a) {
		t.Error("carrier removed, want kept")
	}
}

func TestSpawnNotAcceptedReportsFailedCarrierRemoval(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	f, a := newFakeForge(t)
	f.handoffStatus = 422
	dir := filepath.Dir(workplace.CarrierPath(a.StateDir, "", "ws1"))
	f.onHandoff = func() { _ = os.Chmod(dir, 0o500) }
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	_, err := a.Spawn(context.Background(), ws1, spec())
	if !errors.Is(err, workplace.ErrNotAccepted) {
		t.Fatalf("err = %v, want ErrNotAccepted", err)
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("err = %v, want the removal error joined", err)
	}
}

func TestLocalIDIsNoForgeWorkspace(t *testing.T) {
	const id = "local-issue-1-x-a1b2c3"
	calls := map[string]func(*Adapter) error{
		"Sessions":   func(a *Adapter) error { _, err := a.Sessions(context.Background(), id); return err },
		"Stop":       func(a *Adapter) error { return a.Stop(context.Background(), id, []string{testSessionID}) },
		"Kill":       func(a *Adapter) error { return a.Kill(context.Background(), id, []string{testSessionID}) },
		"Remove":     func(a *Adapter) error { return a.Remove(context.Background(), id) },
		"Lookup":     func(a *Adapter) error { _, err := a.Lookup(context.Background(), id); return err },
		"Discard":    func(a *Adapter) error { return a.Discard(context.Background(), id) },
		"Transcript": func(a *Adapter) error { _, err := a.Transcript(context.Background(), id, testSessionID); return err },
	}
	for name, call := range calls {
		f, a := newFakeForge(t)
		ran := 0
		a.Run = func(context.Context, time.Duration, string, ...string) (string, int, error) { ran++; return "", 0, nil }
		err := call(a)
		if err == nil || errors.Is(err, workplace.ErrGone) {
			t.Errorf("%s: err = %v, want an error that is not ErrGone", name, err)
		}
		if f.total() != 0 || ran != 0 {
			t.Errorf("%s: requests = %d, commands = %d, want none", name, f.total(), ran)
		}
	}

	f, a := newFakeForge(t)
	ran := 0
	a.Run = func(context.Context, time.Duration, string, ...string) (string, int, error) { ran++; return "", 0, nil }
	_, err := a.Spawn(context.Background(), workplace.Workspace{ID: id}, spec())
	if !errors.Is(err, workplace.ErrNotAccepted) {
		t.Errorf("Spawn: err = %v, want ErrNotAccepted", err)
	}
	if f.total() != 0 || ran != 0 {
		t.Errorf("Spawn: requests = %d, commands = %d, want none", f.total(), ran)
	}
	if _, statErr := os.Stat(workplace.CarrierPath(a.StateDir, "", id)); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("Spawn: carrier for %s exists (stat err = %v)", id, statErr)
	}
}

// --- On another node: through serve (#327) ----------------------------------

// AC 1: the carrier goes to the node with the run's session id and the content of the run
// instructions, before the hub sees the handoff; the tick keeps no carrier of its own.
func TestSpawnOnNodeWritesCarrierBeforeHandoff(t *testing.T) {
	_, _, sa, a := twoNodes(t, "node-a")
	l := nodeSpec(t)
	l.MCPConfig = "/node/mcp.json"
	if _, err := a.Spawn(context.Background(), ws1, l); err != nil {
		t.Fatal(err)
	}
	want := []string{"serve " + launchRoute, "serve " + sessionsRoute, "handoff node-a"}
	if got := sa.ev.all(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("order = %q, want %q", got, want)
	}
	req := decodeLaunch(t, sa)
	wantReq := nodeapi.LaunchRequest{
		Repo: "lighthouse", Issue: 7, SessionID: testSessionID, Mode: "start", Profile: "software",
		Prompt: "do the thing", MCPConfig: "/node/mcp.json", Instructions: "# instructions of the run\n",
	}
	if req != wantReq {
		t.Errorf("launch = %+v, want %+v", req, wantReq)
	}
	if strings.Contains(sa.body(launchRoute), l.InstructionsFile) {
		t.Errorf("launch body %s names the tick's instructions path", sa.body(launchRoute))
	}
	noTickCarriers(t, a)
}

// A launch that fails sends no handoff: no agent can start without its carrier.
func TestSpawnLaunchFailureSendsNoHandoff(t *testing.T) {
	cases := map[string]answer{
		"refused":      {http.StatusBadRequest, nodeapi.Problem{Status: 400, Detail: "prompt starts with -"}},
		"server error": {http.StatusInternalServerError, nodeapi.Problem{Status: 500, Detail: "disk full"}},
		"auth":         {http.StatusForbidden, nodeapi.Problem{Status: 403, Detail: "wrong bearer token"}},
	}
	for name, ans := range cases {
		t.Run(name, func(t *testing.T) {
			fa, _, sa, a := twoNodes(t, "node-a")
			sa.on(launchRoute, ans.status, ans.body)
			_, err := a.Spawn(context.Background(), ws1, nodeSpec(t))
			if !errors.Is(err, workplace.ErrNotAccepted) || errors.Is(err, workplace.ErrUncertain) {
				t.Errorf("err = %v, want ErrNotAccepted and not ErrUncertain", err)
			}
			if n := fa.count(handoffKey); n != 0 {
				t.Errorf("handoffs = %d, want 0", n)
			}
		})
	}
	t.Run("unreachable", func(t *testing.T) {
		fa, _, _, a := twoNodes(t, "node-a")
		dead := newFakeServe(t, &events{})
		a.Serve = dead.client(t)
		dead.srv.Close()
		if _, err := a.Spawn(context.Background(), ws1, nodeSpec(t)); !errors.Is(err, workplace.ErrNotAccepted) {
			t.Errorf("err = %v, want ErrNotAccepted", err)
		}
		if n := fa.count(handoffKey); n != 0 {
			t.Errorf("handoffs = %d, want 0", n)
		}
	})
	t.Run("no instructions", func(t *testing.T) {
		fa, _, sa, a := twoNodes(t, "node-a")
		l := spec() // its instructions file does not exist
		if _, err := a.Spawn(context.Background(), ws1, l); !errors.Is(err, workplace.ErrNotAccepted) {
			t.Errorf("err = %v, want ErrNotAccepted", err)
		}
		if n := fa.count(handoffKey) + sa.requests(); n != 0 {
			t.Errorf("requests = %d, want 0", n)
		}
	})
}

// A launch that may have written the carrier (5xx, or no answer) has it deleted; a launch the
// node refused (4xx) wrote nothing to delete.
func TestSpawnLaunch5xxDeletesCarrier(t *testing.T) {
	cases := []struct {
		name    string
		ans     answer
		deletes int
	}{
		{"server error", answer{http.StatusBadGateway, nodeapi.Problem{Status: 502, Detail: "forge down"}}, 1},
		{"no answer", answer{hang, nil}, 1},
		{"refused", answer{http.StatusBadRequest, nodeapi.Problem{Status: 400, Detail: "bad session"}}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, sa, a := twoNodes(t, "node-a")
			sa.on(launchRoute, c.ans.status, c.ans.body)
			if c.ans.status == hang {
				a.Serve.HTTP.Timeout = 300 * time.Millisecond
			}
			if _, err := a.Spawn(context.Background(), ws1, nodeSpec(t)); !errors.Is(err, workplace.ErrNotAccepted) {
				t.Errorf("err = %v, want ErrNotAccepted", err)
			}
			if n := sa.count(carrierRoute); n != c.deletes {
				t.Errorf("carrier deletes = %d, want %d", n, c.deletes)
			}
		})
	}
	// The tick's context ran out while launch hung: the delete still goes out, on a context of
	// its own, so no carrier is left for a handoff that never came.
	t.Run("caller gives up", func(t *testing.T) {
		_, _, sa, a := twoNodes(t, "node-a")
		sa.on(launchRoute, hang, nil)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		if _, err := a.Spawn(ctx, ws1, nodeSpec(t)); !errors.Is(err, workplace.ErrNotAccepted) {
			t.Errorf("err = %v, want ErrNotAccepted", err)
		}
		if n := sa.count(carrierRoute); n != 1 {
			t.Errorf("carrier deletes = %d, want 1 after the caller's context ran out", n)
		}
	})
	t.Run("delete fails", func(t *testing.T) {
		_, _, sa, a := twoNodes(t, "node-a")
		sa.on(launchRoute, http.StatusInternalServerError, nodeapi.Problem{Status: 500, Detail: "disk full"})
		sa.on(carrierRoute, http.StatusInternalServerError, nodeapi.Problem{Status: 500, Detail: "carrier stuck"})
		_, err := a.Spawn(context.Background(), ws1, nodeSpec(t))
		if !errors.Is(err, workplace.ErrNotAccepted) || !strings.Contains(err.Error(), "carrier stuck") {
			t.Errorf("err = %v, want ErrNotAccepted with the failed delete", err)
		}
	})
}

// A handoff without an answer may have started an agent: the carrier on the node stays, and no
// second handoff goes out. A refused handoff deletes it.
func TestSpawnUncertainKeepsCarrier(t *testing.T) {
	fa, _, sa, a := twoNodes(t, "node-a")
	fa.handoffBlock = true
	a.spawnTimeout = 100 * time.Millisecond
	if _, err := a.Spawn(context.Background(), ws1, nodeSpec(t)); !errors.Is(err, workplace.ErrUncertain) {
		t.Fatalf("err = %v, want ErrUncertain", err)
	}
	if n := sa.count(carrierRoute); n != 0 {
		t.Errorf("carrier deletes = %d, want 0: an agent may read it", n)
	}
	if n := fa.count(handoffKey); n != 1 {
		t.Errorf("handoffs = %d, want 1", n)
	}

	fa, _, sa, a = twoNodes(t, "node-a")
	fa.handoffStatus = http.StatusUnprocessableEntity
	if _, err := a.Spawn(context.Background(), ws1, nodeSpec(t)); !errors.Is(err, workplace.ErrNotAccepted) {
		t.Fatalf("refused handoff: err = %v, want ErrNotAccepted", err)
	}
	if n := sa.count(carrierRoute); n != 1 {
		t.Errorf("carrier deletes after a refused handoff = %d, want 1", n)
	}
}

// AC 1: Sessions on a node carry the session id, the state and updated_at, so the rules of
// self apply: the run's own key is recognised, and an open run's working session is busy.
func TestSessionsOnNodeCarrySessionID(t *testing.T) {
	fa, _, sa, a := twoNodes(t, "node-a")
	sa.on(sessionsRoute, http.StatusOK, []nodeapi.Session{
		{Key: "k1", State: "working", SessionID: testSessionID, UpdatedAt: "2026-11-15T10:00:00Z"},
		{Key: "k2", State: "done", SessionID: "22222222-2222-4222-8222-222222222222"},
	})
	ss, err := a.Sessions(context.Background(), "ws1")
	if err != nil {
		t.Fatal(err)
	}
	want := []workplace.Session{
		{Key: "k1", WorkspaceID: "ws1", State: "working", SessionID: testSessionID, UpdatedAt: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)},
		{Key: "k2", WorkspaceID: "ws1", State: "done", SessionID: "22222222-2222-4222-8222-222222222222"},
	}
	if len(ss) != 2 || !ss[0].UpdatedAt.Equal(want[0].UpdatedAt) || ss[0].Key != want[0].Key || ss[0].SessionID != want[0].SessionID || ss[0].State != want[0].State || ss[1] != want[1] {
		t.Fatalf("sessions = %+v, want %+v", ss, want)
	}
	own := workplace.Own{"k1": testSessionID}
	if !own.Has(ss[0]) {
		t.Errorf("Own.Has(%+v) = false, want the run's own session", ss[0])
	}
	if s, busy := workplace.Busy(ss, own, map[string]bool{testSessionID: true}, nil, time.Now(), time.Hour); !busy || s.Key != "k1" {
		t.Errorf("Busy = %+v, %v; want k1 busy", s, busy)
	}
	if n := fa.total(); n != 0 {
		t.Errorf("requests on the hub's node-a = %d, want 0: sessions come from serve", n)
	}
}

// AC 3: Prepare on a node finds the workspace through the hub and then follows the remote
// branch through serve, never with git on this host.
func TestPrepareOnNodeFollowsRemote(t *testing.T) {
	fa, _, sa, a := twoNodes(t, "node-a")
	fa.listBody = listWith("issue-1-x")
	fa.path = "/node/ws1"
	ws, err := a.Prepare(context.Background(), "issue-1-x")
	if err != nil {
		t.Fatal(err)
	}
	if ws != (workplace.Workspace{ID: "ws1", Path: "/node/ws1", Branch: "issue-1-x"}) {
		t.Errorf("ws = %+v", ws)
	}
	if got := sa.body(followRoute); got != `{"branch":"issue-1-x","created":false}` {
		t.Errorf("follow body = %s, want the branch of a found workspace", got)
	}
}

// AC 3: own work on the node comes back as the *workplace.OwnWorkError serve sent, with the
// same fields as on self.
func TestPrepareOnNodeOwnWorkReturnsOwnWorkError(t *testing.T) {
	fa, _, sa, a := twoNodes(t, "node-a")
	fa.listBody = listWith("issue-1-x")
	fa.path = "/node/ws1"
	sent := workplace.Workspace{ID: "ws1", Path: "/node/ws1", Branch: "issue-1-x"}
	sa.on(followRoute, http.StatusConflict, nodeapi.OwnWork{
		Status: 409, Code: nodeapi.CodeOwnWork, Detail: "own work", Workspace: sent, Commits: 2, Dirty: true,
	})
	_, err := a.Prepare(context.Background(), "issue-1-x")
	var own *workplace.OwnWorkError
	if !errors.As(err, &own) {
		t.Fatalf("err = %v, want *workplace.OwnWorkError", err)
	}
	if *own != (workplace.OwnWorkError{Workspace: sent, Commits: 2, Dirty: true}) {
		t.Errorf("own work = %+v", *own)
	}
}

// A serve that does not answer the follow fails Prepare, as an unreachable node does.
func TestPrepareFollowUnreachableFails(t *testing.T) {
	fa, _, sa, a := twoNodes(t, "node-a")
	fa.listBody = listWith("issue-1-x")
	fa.path = "/node/ws1"
	sa.srv.Close()
	_, err := a.Prepare(context.Background(), "issue-1-x")
	var own *workplace.OwnWorkError
	if err == nil || errors.As(err, &own) || !strings.Contains(err.Error(), "follow") {
		t.Errorf("err = %v, want a follow error", err)
	}
}

// Stop and Kill on a node end claude through serve, with the run's profile and session ids.
func TestStopOnNodeEndsClaude(t *testing.T) {
	for _, name := range []string{"Stop", "Kill"} {
		t.Run(name, func(t *testing.T) {
			fa, _, sa, a := twoNodes(t, "node-a")
			a.UnitPrefix = "" // the tick's own unit_prefix plays no part on a node
			call, route := a.Stop, stopRoute
			if name == "Kill" {
				call, route = a.Kill, killRoute
			}
			if err := call(context.Background(), "ws1", []string{testSessionID}); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			want := `{"profile":"software","session_ids":["` + testSessionID + `"]}`
			if got := sa.body(route); got != want {
				t.Errorf("%s body = %s, want %s", name, got, want)
			}
			if n := fa.total(); n != 0 {
				t.Errorf("requests on the hub's node-a = %d, want 0", n)
			}
		})
	}
}

// Discard and Remove on a node delete the carrier on the node, never one on this host.
func TestDiscardAndRemoveDeleteNodeCarrier(t *testing.T) {
	_, _, sa, a := twoNodes(t, "node-a")
	if err := a.Discard(context.Background(), "ws1"); err != nil {
		t.Fatal(err)
	}
	if n := sa.count(carrierRoute); n != 1 {
		t.Errorf("carrier deletes after Discard = %d, want 1", n)
	}
	if err := a.Remove(context.Background(), "ws1"); err != nil {
		t.Fatal(err)
	}
	if n := sa.count(carrierRoute); n != 2 {
		t.Errorf("carrier deletes after Remove = %d, want 2", n)
	}
	sa.on(carrierRoute, http.StatusInternalServerError, nodeapi.Problem{Status: 500, Detail: "carrier stuck"})
	if err := a.Discard(context.Background(), "ws1"); err == nil || !strings.Contains(err.Error(), "carrier stuck") {
		t.Errorf("Discard with a failing delete: err = %v, want it", err)
	}
}

// A 404 workspace_gone of serve is "gone" only while the hub does not know the workspace either
// and reports the node reachable; otherwise uncertain. For Stop and Kill a confirmed gone
// workspace leaves nothing serve can stop: no error.
func TestRemoveGoneNeedsHubConfirmation(t *testing.T) {
	cases := []struct {
		name      string
		hub       func(fa *fakeForge)
		confirmed bool
	}{
		{"hub knows it", func(*fakeForge) {}, false},
		{"hub 404, node reachable", func(fa *fakeForge) { fa.getStatus = http.StatusNotFound }, true},
		{"hub does not reach the node", func(fa *fakeForge) { fa.offline = true }, false},
		{"hub fails", func(fa *fakeForge) { fa.getStatus = http.StatusInternalServerError }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fa, _, sa, a := twoNodes(t, "node-a")
			fa.path = "/node/ws1"
			c.hub(fa)
			for _, route := range []string{sessionsRoute, stopRoute, killRoute} {
				sa.on(route, http.StatusNotFound, gone())
			}
			_, err := a.Sessions(context.Background(), "ws1")
			if c.confirmed != errors.Is(err, workplace.ErrGone) || c.confirmed == errors.Is(err, workplace.ErrUncertain) {
				t.Errorf("Sessions: err = %v, want gone %v, else uncertain", err, c.confirmed)
			}
			for name, call := range map[string]func(context.Context, string, []string) error{"Stop": a.Stop, "Kill": a.Kill} {
				err := call(context.Background(), "ws1", []string{testSessionID})
				if c.confirmed && err != nil {
					t.Errorf("%s: err = %v, want nil for a workspace that is gone", name, err)
				}
				if !c.confirmed && !errors.Is(err, workplace.ErrUncertain) {
					t.Errorf("%s: err = %v, want ErrUncertain", name, err)
				}
			}
		})
	}
}

// A token serve refuses is a configuration error: not measurable, never gone.
func TestAuthFailureIsNotMeasurable(t *testing.T) {
	_, _, sa, a := twoNodes(t, "node-a")
	sa.token = "another-token"
	_, err := a.Sessions(context.Background(), "ws1")
	if !errors.Is(err, workplace.ErrNotMeasurable) || errors.Is(err, workplace.ErrGone) || !strings.Contains(err.Error(), "configuration error") {
		t.Errorf("Sessions: err = %v, want a configuration error that is not measurable", err)
	}
	if _, err := a.Dirty(context.Background(), "ws1"); !errors.Is(err, workplace.ErrNotMeasurable) {
		t.Errorf("Dirty: err = %v, want not measurable", err)
	}
}

// The git measurements of the sweep on a node come from serve, with the merged head as asked.
func TestGitMeterOnNodeAsksServe(t *testing.T) {
	const head = "0123456789abcdef0123456789abcdef01234567"
	_, _, sa, a := twoNodes(t, "node-a")
	var m workplace.GitMeter = a
	sa.on(gitRoute, http.StatusOK, nodeapi.GitState{Dirty: true, Unpushed: false})
	dirty, err := m.Dirty(context.Background(), "ws1")
	if err != nil || !dirty {
		t.Errorf("Dirty = %v, %v; want true", dirty, err)
	}
	sa.on(gitRoute, http.StatusOK, nodeapi.GitState{Dirty: false, Unpushed: true})
	unpushed, err := m.Unpushed(context.Background(), "ws1", head)
	if err != nil || !unpushed {
		t.Errorf("Unpushed = %v, %v; want true", unpushed, err)
	}
	if got := sa.query(gitRoute); got != "merged_head="+head {
		t.Errorf("git query = %q, want the merged head", got)
	}
	sa.on(gitRoute, http.StatusInternalServerError, nodeapi.Problem{Status: 500, Detail: "git status failed"})
	if _, err := m.Dirty(context.Background(), "ws1"); !errors.Is(err, workplace.ErrNotMeasurable) {
		t.Errorf("Dirty with a failing git: err = %v, want not measurable", err)
	}
}

func TestRelocateOnNodeAsksServe(t *testing.T) {
	_, _, sa, a := twoNodes(t, "node-a")
	var r workplace.Relocator = a
	sa.on(relocateRoute, http.StatusOK, nodeapi.Relocation{Command: "/plan work/plan.md"})
	got, err := r.Relocate(context.Background(), "ws1", "/plan /old/work/plan.md")
	if err != nil || got != "/plan work/plan.md" {
		t.Errorf("Relocate = %q, %v; want the node's command", got, err)
	}
	if body := sa.body(relocateRoute); body != `{"command":"/plan /old/work/plan.md"}` {
		t.Errorf("relocate body = %s", body)
	}
	sa.on(relocateRoute, http.StatusUnprocessableEntity, nodeapi.Problem{Status: 422, Detail: "plan.md not in the worktree"})
	if _, err := r.Relocate(context.Background(), "ws1", "/plan x.md"); err == nil || !strings.Contains(err.Error(), "plan.md not in the worktree") {
		t.Errorf("Relocate refused: err = %v, want the node's reason", err)
	}
}

func TestPermissionOnNodeAsksServe(t *testing.T) {
	_, _, sa, a := twoNodes(t, "node-a")
	var p workplace.PermissionReader = a
	sa.on(permissionRoute, http.StatusOK, map[string]string{"tool": "Bash"})
	got, err := p.Permission(context.Background(), testSessionID)
	if err != nil || strings.TrimSpace(string(got)) != `{"tool":"Bash"}` {
		t.Errorf("Permission = %q, %v; want the raw note", got, err)
	}
	sa.on(permissionRoute, http.StatusNotFound, nodeapi.Problem{Status: 404, Code: nodeapi.CodeNoPermission, Detail: "none"})
	if got, err := p.Permission(context.Background(), testSessionID); got != nil || err != nil {
		t.Errorf("Permission without a note = %q, %v; want nil, nil", got, err)
	}
}

// The adapter on self has no serve: the node measurements are not measurable there, so a caller
// that asks them of self by mistake never reads a clean or empty answer.
func TestNodeMeasurementsOnSelfNotMeasurable(t *testing.T) {
	f, a := newFakeForge(t)
	calls := map[string]func() error{
		"Dirty":      func() error { _, err := a.Dirty(context.Background(), "ws1"); return err },
		"Unpushed":   func() error { _, err := a.Unpushed(context.Background(), "ws1", ""); return err },
		"Relocate":   func() error { _, err := a.Relocate(context.Background(), "ws1", "/plan x.md"); return err },
		"Permission": func() error { _, err := a.Permission(context.Background(), testSessionID); return err },
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, workplace.ErrNotMeasurable) {
			t.Errorf("%s on self: err = %v, want not measurable", name, err)
		}
	}
	if n := f.total(); n != 0 {
		t.Errorf("requests = %d, want 0", n)
	}
}

// An adapter on a node without a serve client cannot reach the node: every route that needs
// serve fails, and none falls back to this host.
func TestNodeWithoutServeFails(t *testing.T) {
	fa, _, _, a := twoNodes(t, "node-a")
	a.Serve = nil
	if _, err := a.Spawn(context.Background(), ws1, nodeSpec(t)); !errors.Is(err, workplace.ErrNotAccepted) {
		t.Errorf("Spawn: err = %v, want ErrNotAccepted", err)
	}
	if _, err := a.Sessions(context.Background(), "ws1"); !errors.Is(err, workplace.ErrNotMeasurable) {
		t.Errorf("Sessions: err = %v, want not measurable", err)
	}
	if err := a.Kill(context.Background(), "ws1", []string{testSessionID}); err == nil {
		t.Error("Kill: err = nil, want an error")
	}
	if err := a.Discard(context.Background(), "ws1"); err == nil {
		t.Error("Discard: err = nil, want an error")
	}
	if n := fa.count(handoffKey); n != 0 {
		t.Errorf("handoffs = %d, want 0", n)
	}
	noTickCarriers(t, a)
}

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
	// sessions answers the agent-sessions reads, one entry per call; the last repeats.
	sessions []string
	// handoff answers the handoff POST.
	handoffStatus int
	handoffBody   string
	handoffBlock  bool
	// deleteStatus answers the DELETE.
	deleteStatus int
	release      chan struct{}
	// onHandoff runs when the handoff POST arrives.
	onHandoff func()
}

func newFakeForge(t *testing.T) (*fakeForge, *Adapter) {
	t.Helper()
	f := &fakeForge{
		t: t, counts: map[string]int{}, bodies: map[string]string{},
		listBody: `{"workspaces":[]}`, createBody: `{"id":"ws1"}`,
		statuses: []string{"ready"}, sessions: []string{`{"sessions":[]}`},
		handoffStatus: 200, handoffBody: `{"session":{"key":"k-new"}}`,
		deleteStatus: 204, release: make(chan struct{}),
	}
	srv := httptest.NewServer(f)
	t.Cleanup(func() { close(f.release); srv.Close() })
	tok := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tok, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &Adapter{
		Client: New(srv.URL, tok), Kind: "github", Repo: "CorneeldH/lighthouse",
		StateDir: t.TempDir(), Targets: map[string]string{"software": "paia-software"},
		Sleep: func(time.Duration) { f.mu.Lock(); f.counts["sleep"]++; f.mu.Unlock() },
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
		_, _ = fmt.Fprintf(w, `{"id":"ws1","status":%q,"worktree_path":%q}`, next(f.statuses, n), f.path)
	case "GET /api/v1/workspaces/ws1/agent-sessions":
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

func TestPrepareFoundOwnCommitUntouched(t *testing.T) {
	w := newGitWorld(t)
	w.pushBranch(t)
	git(t, w.ws, "checkout", "-b", "issue-1-x")
	c := commit(t, w.ws, "c")
	a := gitAdapter(t, w.ws)
	if _, err := a.Prepare(context.Background(), "issue-1-x"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, w.ws, "rev-parse", "HEAD"); got != c {
		t.Errorf("HEAD = %s, want own commit %s", got, c)
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
	_, err := os.Stat(workplace.CarrierPath(a.StateDir, "ws1"))
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
	_, err := os.Stat(workplace.CarrierPath(a.StateDir, "ws1"))
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

func TestRemove409Dirty(t *testing.T) {
	f, a := newFakeForge(t)
	f.deleteStatus = 409
	if err := a.Remove(context.Background(), "ws1"); !errors.Is(err, workplace.ErrDirty) {
		t.Fatalf("err = %v, want ErrDirty", err)
	}
}

func TestRemove404Gone(t *testing.T) {
	f, a := newFakeForge(t)
	f.deleteStatus = 404
	if err := a.Remove(context.Background(), "ws1"); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

// writeCarrier puts a carrier for ws1 in the state dir, as a spawn leaves it.
func writeCarrier(t *testing.T, a *Adapter) {
	t.Helper()
	c, err := workplace.NewCarrier(spec(), a.StateDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workplace.WriteCarrier(a.StateDir, "ws1", c); err != nil {
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
	dir := filepath.Dir(workplace.CarrierPath(a.StateDir, "ws1"))
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
// and the carrier stays for CheckOpen to settle.
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
	dir := filepath.Dir(workplace.CarrierPath(a.StateDir, "ws1"))
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

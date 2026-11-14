package kennforge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/CorneeldH/lighthouse/internal/workplace/workplacetest"
)

func TestContract(t *testing.T) { workplacetest.Run(t, newForgeFixture) }

// onNodeSkips are the cases a node behind the hub cannot keep until #327: forge's /runtime has
// no session_id, and lighthouse sends no process signal to another node.
var onNodeSkips = map[string]string{
	"SpawnAcceptedShowsSession": "#327: Sessions on a node has no session_id (forge v0.7.0 /runtime)",
	"KillSurvivorIsError":       "#327: no process signals on a node",
	"KillUnmeasurableIsError":   "#327: no process measurement on a node",
	"KillLeavesOtherSession":    "#327: stands by session_id; Sessions on a node has none",
	"EndedSessionIsDone":        "#327: stands by session_id; Sessions on a node has none",
}

// TestContractOnNode runs the suite against an adapter on node-a behind the fake hub.
func TestContractOnNode(t *testing.T) {
	for _, c := range workplacetest.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if reason, ok := onNodeSkips[c.Name]; ok {
				t.Skip(reason)
			}
			c.Run(t, newForgeNodeFixture(t))
		})
	}
}

// forgeFixture is a stateful forge: workspaces are real clones of a bare origin, sessions
// come from the carriers of the handoffs, and the Runner plays the processes of the host.
type forgeFixture struct {
	t      *testing.T
	a      *Adapter
	origin string
	root   string
	onNode bool // behind the fake hub: sessions come from /runtime

	mu         sync.Mutex
	workspaces map[string]*forgeWorkspace
	created    int
	keys       int
	launches   int
	timeout    bool
	live       map[string]bool
	survivors  map[string]bool
	unmeasured bool
	procCalls  int
	signals    int
}

type forgeWorkspace struct {
	path, branch string
	dirty, gone  bool
	sessions     []forgeSession
}

type forgeSession struct{ key, sid string }

const createPath = "/api/v1/repo/github/CorneeldH/lighthouse/workspaces"

func newForgeFixture(t *testing.T) workplacetest.Fixture {
	f := newForgeState(t)
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	f.a = f.adapter(t, srv.URL, "")
	return f
}

// newForgeNodeFixture is the same forge as node-a behind the fake hub. Its Runner counts every
// call as a process call, git included: nothing may run on the tick host for a node.
func newForgeNodeFixture(t *testing.T) workplacetest.Fixture {
	f := newForgeState(t)
	f.onNode = true
	hub := newFakeHub(t, map[string]hubNode{"node-a": f, "node-b": newForgeNode(t)})
	f.a = f.adapter(t, hub.URL, "node-a")
	f.a.Run = func(_ context.Context, _ time.Duration, name string, args ...string) (string, int, error) {
		f.mu.Lock()
		f.procCalls++
		f.mu.Unlock()
		t.Errorf("Runner called on a node: %s %v", name, args)
		return "", -1, errors.New("no runner on a node")
	}
	return f
}

func newForgeState(t *testing.T) *forgeFixture {
	return &forgeFixture{
		t: t, origin: newGitWorld(t).origin, root: t.TempDir(),
		workspaces: map[string]*forgeWorkspace{},
		live:       map[string]bool{}, survivors: map[string]bool{},
	}
}

func (f *forgeFixture) adapter(t *testing.T, url, node string) *Adapter {
	tok := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tok, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Adapter{
		Client: New(url, tok), Kind: "github", Repo: "CorneeldH/lighthouse", Node: node,
		StateDir: t.TempDir(), Targets: map[string]string{"infra": "paia-infra"}, Profile: "infra",
		UnitPrefix: "paia-forge-",
		Run:        f.run, Sleep: func(time.Duration) {}, spawnTimeout: 100 * time.Millisecond,
	}
}

// routable and reachable: the fake hub always reaches this forge.
func (f *forgeFixture) routable() bool  { return true }
func (f *forgeFixture) reachable() bool { return true }

func (f *forgeFixture) Workplace() workplace.Workplace { return f.a }

func (f *forgeFixture) Workspace(t *testing.T, branch string) workplace.Workspace {
	t.Helper()
	ws, err := f.a.Prepare(t.Context(), branch)
	if err != nil {
		t.Fatalf("Prepare %s: %v", branch, err)
	}
	return ws
}

func (f *forgeFixture) Spec(*testing.T) workplace.LaunchSpec {
	return workplace.LaunchSpec{Profile: "infra", Mode: "start", Prompt: "do the thing", InstructionsFile: "/abs/instr.md"}
}

func (f *forgeFixture) MakeDirty(t *testing.T, ws workplace.Workspace) {
	f.change(t, ws, func(w *forgeWorkspace) { w.dirty = true })
}

func (f *forgeFixture) Forget(t *testing.T, ws workplace.Workspace) {
	f.change(t, ws, func(w *forgeWorkspace) { w.gone = true })
}

// change applies fn to a workspace the forge knows, under the lock the handlers take.
func (f *forgeFixture) change(t *testing.T, ws workplace.Workspace, fn func(*forgeWorkspace)) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.workspaces[ws.ID]
	if !ok {
		t.Fatalf("workspace %s unknown to the forge", ws.ID)
	}
	fn(w)
}

func (f *forgeFixture) Live(_ *testing.T, sid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.live[sid] = true
}

func (f *forgeFixture) Survivor(_ *testing.T, sid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.survivors[sid] = true
}

func (f *forgeFixture) Unmeasurable(*testing.T) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unmeasured = true
}

func (f *forgeFixture) SpawnTimesOut(*testing.T) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.timeout = true
}

func (f *forgeFixture) Launches() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.launches
}

func (f *forgeFixture) ProcCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.procCalls
}

func (f *forgeFixture) Signals() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.signals
}

// run is the host: git runs for real, pgrep, pkill and systemctl are played.
func (f *forgeFixture) run(ctx context.Context, timeout time.Duration, name string, args ...string) (string, int, error) {
	if name == "git" {
		return workplace.ExecRunner()(ctx, timeout, name, args...)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.procCalls++
	cmd := strings.Join(append([]string{name}, args...), " ")
	switch {
	case name == "pgrep" && len(args) == 2 && args[0] == "-f":
		if f.unmeasured {
			return "", -1, errors.New("pgrep: cannot read /proc")
		}
		if f.live[args[1]] {
			return "42\n", 0, nil
		}
		return "", 1, nil
	case name == "pkill" && len(args) == 2 && args[0] == "-f":
		f.signals++
		if !f.survivors[args[1]] {
			delete(f.live, args[1])
		}
		return "", 0, nil
	case name == "systemctl" && len(args) == 3 && args[1] == "stop":
		return "", 0, nil
	case name == "systemctl" && len(args) == 3 && args[1] == "is-active":
		return "inactive\n", 3, nil
	}
	f.t.Errorf("unexpected command %q", cmd)
	return "", -1, fmt.Errorf("unexpected command %q", cmd)
}

type wireRepo struct {
	Provider string `json:"provider"`
	RepoPath string `json:"repo_path"`
}

type wireListed struct {
	ID         string   `json:"id"`
	GitHeadRef string   `json:"git_head_ref"`
	Repo       wireRepo `json:"repo"`
}

type wireSession struct {
	Key       string `json:"runtime_session_key"`
	SessionID string `json:"session_id"`
	State     string `json:"state"`
}

func (f *forgeFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.EscapedPath()
	if path == createPath && r.Method == http.MethodPost {
		f.create(w, r)
		return
	}
	if path == "/api/v1/workspaces" && r.Method == http.MethodGet {
		f.list(w)
		return
	}
	id, rest, _ := strings.Cut(strings.TrimPrefix(path, "/api/v1/workspaces/"), "/")
	if rest == "runtime/agent-handoffs" && r.Method == http.MethodPost {
		f.handoff(w, r, id)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	ws, ok := f.workspaces[id]
	if !ok || ws.gone {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	switch {
	case rest == "" && r.Method == http.MethodGet:
		writeJSON(w, map[string]string{"id": id, "status": "ready", "worktree_path": ws.path, "git_head_ref": ws.branch})
	case rest == "" && r.Method == http.MethodDelete:
		if ws.dirty {
			w.WriteHeader(http.StatusConflict)
			return
		}
		ws.gone = true
		w.WriteHeader(http.StatusNoContent)
	case rest == "runtime" && r.Method == http.MethodGet && f.onNode:
		writeJSON(w, map[string]any{"launch_targets": []any{}, "sessions": f.runtimeSessions(id, ws)})
	case rest == "agent-sessions" && r.Method == http.MethodGet && !f.onNode:
		out := []wireSession{}
		for _, s := range ws.sessions {
			state := "done"
			if f.live[s.sid] {
				state = "working"
			}
			out = append(out, wireSession{Key: s.key, SessionID: s.sid, State: state})
		}
		writeJSON(w, map[string]any{"sessions": out})
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, path)
		w.WriteHeader(http.StatusNotFound)
	}
}

// create clones origin into a fresh directory: the new workspace is ready at once.
func (f *forgeFixture) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Branch string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Branch == "" {
		f.t.Errorf("create body: %v (branch %q)", err, body.Branch)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.created++
	id := fmt.Sprintf("ws%d", f.created)
	f.mu.Unlock()
	dir := filepath.Join(f.root, id)
	if out, err := exec.Command("git", "clone", "--quiet", f.origin, dir).CombinedOutput(); err != nil {
		f.t.Errorf("clone origin: %v\n%s", err, out)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	f.mu.Lock()
	f.workspaces[id] = &forgeWorkspace{path: dir, branch: body.Branch}
	f.mu.Unlock()
	writeJSON(w, map[string]string{"id": id})
}

func (f *forgeFixture) list(w http.ResponseWriter) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []wireListed{}
	for id, ws := range f.workspaces {
		if !ws.gone {
			out = append(out, wireListed{ID: id, GitHeadRef: ws.branch, Repo: wireRepo{Provider: "github", RepoPath: "corneeldh/lighthouse"}})
		}
	}
	writeJSON(w, map[string]any{"workspaces": out})
}

// handoff is one launch: it reads the session id from the carrier the adapter wrote and adds
// the session. After SpawnTimesOut it answers nothing until the request gives up.
func (f *forgeFixture) handoff(w http.ResponseWriter, r *http.Request, id string) {
	// Only a server that has read the body notices the client give up.
	_, _ = io.Copy(io.Discard, r.Body)
	f.mu.Lock()
	f.launches++
	timeout := f.timeout
	ws, ok := f.workspaces[id]
	known := ok && !ws.gone
	f.mu.Unlock()
	if timeout {
		<-r.Context().Done()
		return
	}
	if !known {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	raw, err := os.ReadFile(workplace.CarrierPath(f.a.StateDir, f.a.Node, id))
	var c workplace.Carrier
	if err == nil {
		err = json.Unmarshal(raw, &c)
	}
	if err != nil || c.SessionID == "" {
		f.t.Errorf("read carrier of %s: %v (session id %q)", id, err, c.SessionID)
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	f.mu.Lock()
	f.keys++
	key := fmt.Sprintf("k%d", f.keys)
	ws.sessions = append(ws.sessions, forgeSession{key: key, sid: c.SessionID})
	f.mu.Unlock()
	writeJSON(w, map[string]any{"session": map[string]string{"key": key}})
}

// runtimeSessions is the sessions list of forge v0.7.0's /runtime (see wireRuntime): an agent
// whose process lives runs, any other has exited. It carries no session_id. Callers hold f.mu.
func (f *forgeFixture) runtimeSessions(id string, ws *forgeWorkspace) []map[string]any {
	out := []map[string]any{}
	for _, s := range ws.sessions {
		status := "exited"
		if f.live[s.sid] {
			status = "running"
		}
		out = append(out, map[string]any{
			"key": s.key, "workspace_id": id, "target_key": "paia-infra", "label": "Claude",
			"kind": "agent", "status": status, "display_region": "main",
			"created_at": "2026-11-13T10:00:00Z",
		})
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

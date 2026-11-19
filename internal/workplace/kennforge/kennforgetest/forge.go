package kennforgetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// CreatePath is the route Forge creates workspaces of CorneeldH/lighthouse on.
const CreatePath = "/api/v1/repo/github/CorneeldH/lighthouse/workspaces"

// Forge is a stateful kenn-forge: workspaces are real clones of a bare origin, sessions come
// from the carriers of the handoffs, and Run plays the processes of the host it runs on. Its
// methods arrange the world as a workplacetest.Fixture does, by workspace id.
type Forge struct {
	t      testing.TB
	origin string
	root   string
	// CarrierPath is where the handoff reads the carrier of a workspace: where the adapter, or
	// serve on the node, wrote it.
	CarrierPath func(id string) string

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

// dirtyFile is the untracked file MakeDirty leaves in the clone.
const dirtyFile = "untracked.txt"

type forgeWorkspace struct {
	path, branch string
	dirty, gone  bool
	sessions     []forgeSession
}

type forgeSession struct{ key, sid string }

// NewForge is a forge whose workspaces clone origin.
func NewForge(t testing.TB, origin string) *Forge {
	return &Forge{
		t: t, origin: origin, root: t.TempDir(),
		workspaces: map[string]*forgeWorkspace{},
		live:       map[string]bool{}, survivors: map[string]bool{},
	}
}

// NewOrigin is a bare repository with one commit on main.
func NewOrigin(t testing.TB) string {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	work := filepath.Join(root, "work")
	gitIn(t, root, "init", "--bare", "-b", "main", origin)
	gitIn(t, root, "clone", "--quiet", origin, work)
	gitIn(t, work, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "m1"), []byte("m1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work, "add", "m1")
	gitIn(t, work, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "--quiet", "-m", "m1")
	gitIn(t, work, "push", "--quiet", "origin", "main")
	return origin
}

func gitIn(t testing.TB, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// Routable and Reachable: the fake hub always reaches this forge.
func (f *Forge) Routable() bool { return true }

// Reachable is true: see Routable.
func (f *Forge) Reachable() bool { return true }

// MakeDirty makes the workspace refuse a DELETE, until the untracked file it leaves in the clone
// is gone (a forced Remove cleans it).
func (f *Forge) MakeDirty(t testing.TB, id string) {
	f.change(t, id, func(w *forgeWorkspace) {
		if err := os.WriteFile(filepath.Join(w.path, dirtyFile), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		w.dirty = true
	})
}

// Forget makes the forge answer 404 for the workspace.
func (f *Forge) Forget(t testing.TB, id string) {
	f.change(t, id, func(w *forgeWorkspace) { w.gone = true })
}

// change applies fn to a workspace the forge knows, under the lock the handlers take.
func (f *Forge) change(t testing.TB, id string, fn func(*forgeWorkspace)) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.workspaces[id]
	if !ok {
		t.Fatalf("workspace %s unknown to the forge", id)
	}
	fn(w)
}

// Live makes a process of sid run: its session is working.
func (f *Forge) Live(sid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.live[sid] = true
}

// Survivor makes the process of sid outlive every signal.
func (f *Forge) Survivor(sid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.survivors[sid] = true
}

// Unmeasurable makes pgrep fail.
func (f *Forge) Unmeasurable() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unmeasured = true
}

// SpawnTimesOut makes every later handoff answer nothing until the request gives up.
func (f *Forge) SpawnTimesOut() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.timeout = true
}

// Launches counts the handoffs.
func (f *Forge) Launches() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.launches
}

// ProcCalls counts the process calls of Run, git left out.
func (f *Forge) ProcCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.procCalls
}

// Signals counts the pkill calls of Run.
func (f *Forge) Signals() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.signals
}

// Run is the host: git runs for real, pgrep, pkill and systemctl are played.
func (f *Forge) Run(ctx context.Context, timeout time.Duration, name string, args ...string) (string, int, error) {
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

// ServeHTTP serves the forge routes the kenn-forge adapter uses; any other request is an error.
func (f *Forge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.EscapedPath()
	if path == CreatePath && r.Method == http.MethodPost {
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
		if _, err := os.Stat(filepath.Join(ws.path, dirtyFile)); ws.dirty && err == nil {
			w.WriteHeader(http.StatusConflict)
			return
		}
		ws.gone = true
		w.WriteHeader(http.StatusNoContent)
	case rest == "agent-sessions" && r.Method == http.MethodGet:
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

// create clones origin and adds a worktree in a fresh directory: the new workspace is ready at once.
func (f *Forge) create(w http.ResponseWriter, r *http.Request) {
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
	// Like kenn-forge, the workspace is a linked worktree of a main checkout, on its branch.
	main := dir + "-main"
	if out, err := exec.Command("git", "clone", "--quiet", f.origin, main).CombinedOutput(); err != nil {
		f.t.Errorf("clone origin: %v\n%s", err, out)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if out, err := exec.Command("git", "-C", main, "worktree", "add", "--quiet", "-b", body.Branch, dir).CombinedOutput(); err != nil {
		f.t.Errorf("worktree add: %v\n%s", err, out)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	f.mu.Lock()
	f.workspaces[id] = &forgeWorkspace{path: dir, branch: body.Branch}
	f.mu.Unlock()
	writeJSON(w, map[string]string{"id": id})
}

func (f *Forge) list(w http.ResponseWriter) {
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

// handoff is one launch: it reads the session id from the carrier at CarrierPath and adds the
// session. After SpawnTimesOut it answers nothing until the request gives up.
func (f *Forge) handoff(w http.ResponseWriter, r *http.Request, id string) {
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
	raw, err := os.ReadFile(f.CarrierPath(id))
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

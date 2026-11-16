package kennforge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

func TestFleetPath(t *testing.T) {
	cases := []struct{ node, in, want string }{
		{"", "/api/v1/workspaces", "/api/v1/workspaces"},
		{"", "/api/v1/workspaces/ws1/runtime", "/api/v1/workspaces/ws1/runtime"},
		{"node-a", "/api/v1/workspaces", "/api/v1/fleet/hosts/node-a/workspaces"},
		{"node-a", "/api/v1/workspaces/ws1/runtime", "/api/v1/fleet/hosts/node-a/workspaces/ws1/runtime"},
		{"node-a", "/api/v1/repo/github/o/r/workspaces", "/api/v1/fleet/hosts/node-a/repo/github/o/r/workspaces"},
		{"node a/b", "/api/v1/workspaces/ws1", "/api/v1/fleet/hosts/node%20a%2Fb/workspaces/ws1"},
	}
	for _, c := range cases {
		a := &Adapter{Node: c.node}
		if got := a.path(c.in); got != c.want {
			t.Errorf("Node %q: path(%q) = %q, want %q", c.node, c.in, got, c.want)
		}
	}
}

// events is the order in which the hub and serve saw the requests of a test.
type events struct {
	mu   sync.Mutex
	list []string
}

func (e *events) add(s string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, s)
}

func (e *events) all() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.list...)
}

// answer is what fakeServe answers on one route: a status and a JSON body (nil: none). Status
// hang answers nothing until the client gives up.
type answer struct {
	status int
	body   any
}

const hang = -1

// fakeServe is lighthouse serve on a node, in the shape fakeNode answers (bearer check, info,
// problem+json), with every route of the node-API the adapter uses. Each route logs
// "serve <METHOD> <route>" in events, keeps its last request body, and answers what on sets,
// or its default: a carrier for launch, no sessions, a clean git state, 204 for the rest.
type fakeServe struct {
	*fakeNode
	ev *events

	mu      sync.Mutex
	answers map[string]answer
	bodies  map[string]string
	queries map[string]string
	release chan struct{}
}

// The routes of fakeServe, as "<METHOD> <route>".
var (
	launchRoute     = "PUT " + nodeapi.PathLaunch
	carrierRoute    = "DELETE " + nodeapi.PathCarrier
	followRoute     = "POST " + nodeapi.PathFollow
	sessionsRoute   = "GET " + nodeapi.PathSessions
	stopRoute       = "POST " + nodeapi.PathStop
	killRoute       = "POST " + nodeapi.PathKill
	gitRoute        = "GET " + nodeapi.PathGit
	relocateRoute   = "POST " + nodeapi.PathRelocate
	permissionRoute = "GET " + nodeapi.PathPermission
)

func newFakeServe(t *testing.T, ev *events) *fakeServe {
	t.Helper()
	s := &fakeServe{
		fakeNode: newFakeNode(t), ev: ev,
		answers: map[string]answer{
			launchRoute:   {http.StatusOK, workplace.Carrier{SessionID: testSessionID}},
			sessionsRoute: {http.StatusOK, []nodeapi.Session{}},
			gitRoute:      {http.StatusOK, nodeapi.GitState{}},
		},
		bodies: map[string]string{}, queries: map[string]string{}, release: make(chan struct{}),
	}
	t.Cleanup(func() { close(s.release) })
	for _, route := range []string{launchRoute, carrierRoute, followRoute, sessionsRoute, stopRoute, killRoute, gitRoute, relocateRoute, permissionRoute} {
		s.mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) { s.serve(w, r, route) })
	}
	return s
}

func (s *fakeServe) serve(w http.ResponseWriter, r *http.Request, route string) {
	raw, _ := io.ReadAll(r.Body)
	s.ev.add("serve " + route)
	s.mu.Lock()
	s.bodies[route] = string(raw)
	s.queries[route] = r.URL.RawQuery
	a, ok := s.answers[route]
	s.mu.Unlock()
	switch {
	case !ok:
		w.WriteHeader(http.StatusNoContent)
	case a.status == hang:
		select {
		case <-r.Context().Done():
		case <-s.release:
		}
	case a.body == nil:
		w.WriteHeader(a.status)
	default:
		writeNode(w, a.status, a.body)
	}
}

// on sets the answer of route.
func (s *fakeServe) on(route string, status int, body any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.answers[route] = answer{status, body}
}

// gone is the 404 of serve for a workspace its forge no longer knows.
func gone() nodeapi.Problem {
	return nodeapi.Problem{Status: http.StatusNotFound, Code: nodeapi.CodeWorkspaceGone, Detail: "workspace not found"}
}

func (s *fakeServe) body(route string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[route]
}

func (s *fakeServe) query(route string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queries[route]
}

// count is the number of requests on route.
func (s *fakeServe) count(route string) int {
	n := 0
	for _, e := range s.ev.all() {
		if e == "serve "+route {
			n++
		}
	}
	return n
}

// requests counts every request on serve but info.
func (s *fakeServe) requests() int {
	n := 0
	for _, e := range s.ev.all() {
		if strings.HasPrefix(e, "serve ") {
			n++
		}
	}
	return n
}

// twoNodes is a fake hub with node-a and node-b, and an adapter on node with serve sa. Every
// handoff on node-a or node-b logs "handoff <node>" in the events of sa.
func twoNodes(t *testing.T, node string) (fa, fb *fakeForge, sa *fakeServe, a *Adapter) {
	t.Helper()
	fa, fb = newForgeNode(t), newForgeNode(t)
	ev := &events{}
	fa.onHandoff = func() { ev.add("handoff node-a") }
	fb.onHandoff = func() { ev.add("handoff node-b") }
	hub := newFakeHub(t, map[string]*fakeForge{"node-a": fa, "node-b": fb})
	tok := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tok, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sa = newFakeServe(t, ev)
	a = &Adapter{
		Client: New(hub.URL, tok), Kind: "github", Repo: "CorneeldH/lighthouse", Node: node,
		Serve:    sa.client(t),
		StateDir: t.TempDir(), Targets: map[string]string{"software": "paia-software"},
		Profile: "software", UnitPrefix: "paia-forge-",
		Run: func(_ context.Context, _ time.Duration, name string, args ...string) (string, int, error) {
			t.Errorf("Runner called on a node: %s %v", name, args)
			return "", -1, errors.New("no runner on a node")
		},
		Sleep: func(time.Duration) {},
	}
	return fa, fb, sa, a
}

// nodeSpec is spec with run instructions in a real file: Spawn on a node sends their content.
func nodeSpec(t *testing.T) workplace.LaunchSpec {
	t.Helper()
	l := spec()
	l.InstructionsFile = filepath.Join(t.TempDir(), "instructions.md")
	if err := os.WriteFile(l.InstructionsFile, []byte("# instructions of the run\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return l
}

// noTickCarriers fails when the tick's state dir holds any carrier: on a node it lives there.
func noTickCarriers(t *testing.T, a *Adapter) {
	t.Helper()
	if entries, err := os.ReadDir(filepath.Join(a.StateDir, "carriers")); err == nil && len(entries) > 0 {
		t.Errorf("carriers in the tick's state dir: %v, want none", entries)
	}
}

func TestRemotePrepareCreatesOnItsNode(t *testing.T) {
	fa, fb, sa, a := twoNodes(t, "node-a")
	fa.path = "/srv/node-a/ws1"
	ws, err := a.Prepare(context.Background(), "issue-1-x")
	if err != nil {
		t.Fatal(err)
	}
	if ws.ID != "ws1" || ws.Path != "/srv/node-a/ws1" || ws.Branch != "issue-1-x" {
		t.Errorf("ws = %+v", ws)
	}
	if n := fa.count("POST /api/v1/repo/github/CorneeldH/lighthouse/workspaces"); n != 1 {
		t.Errorf("create POSTs on node-a = %d, want 1", n)
	}
	if n := fa.count("GET /api/v1/workspaces"); n != 1 {
		t.Errorf("list on node-a = %d, want 1", n)
	}
	if got := sa.body(followRoute); got != `{"branch":"issue-1-x","created":true}` {
		t.Errorf("follow body = %s, want the branch of a created workspace", got)
	}
	if n := fb.total(); n != 0 {
		t.Errorf("requests on node-b = %d, want 0", n)
	}
}

func TestRemotePrepareFindsOnItsNode(t *testing.T) {
	fa, fb, sa, a := twoNodes(t, "node-b")
	fa.listBody = listWith("issue-1-x")
	fb.path = "/srv/node-b/ws1"
	ws, err := a.Prepare(context.Background(), "issue-1-x")
	if err != nil {
		t.Fatal(err)
	}
	if ws.Path != "/srv/node-b/ws1" {
		t.Errorf("Path = %q, want the workspace of node-b", ws.Path)
	}
	if n := fb.count("POST /api/v1/repo/github/CorneeldH/lighthouse/workspaces"); n != 1 {
		t.Errorf("create POSTs on node-b = %d, want 1: node-a's workspace is not node-b's", n)
	}
	if n := fa.total(); n != 0 {
		t.Errorf("requests on node-a = %d, want 0", n)
	}
	if n := sa.count(followRoute); n != 1 {
		t.Errorf("follows on serve = %d, want 1", n)
	}
}

func TestRemoteSpawnHandoffOnItsNode(t *testing.T) {
	fa, fb, sa, a := twoNodes(t, "node-a")
	s, err := a.Spawn(context.Background(), ws1, nodeSpec(t))
	if err != nil {
		t.Fatal(err)
	}
	if s.Key != "k-new" || s.WorkspaceID != "ws1" || s.SessionID != testSessionID {
		t.Errorf("session = %+v", s)
	}
	if n := fa.count(handoffKey); n != 1 {
		t.Errorf("handoffs on node-a = %d, want 1", n)
	}
	if got := fa.body(handoffKey); got != `{"message":"do the thing","target_key":"paia-software"}` {
		t.Errorf("body = %s", got)
	}
	if n := fa.count("GET /api/v1/workspaces/ws1/agent-sessions"); n != 0 {
		t.Errorf("agent-sessions reads on the hub = %d, want 0: the hub has no such route", n)
	}
	if n := sa.count(sessionsRoute); n != 1 {
		t.Errorf("sessions reads on serve = %d, want 1 (before the handoff)", n)
	}
	if n := fb.total(); n != 0 {
		t.Errorf("requests on node-b = %d, want 0", n)
	}
	noTickCarriers(t, a)
}

// Stop and Kill on a node with an empty session id send nothing: serve would refuse it, but
// the tick refuses first, as on self.
func TestRemoteStopEmptySessionIDRejected(t *testing.T) {
	for _, name := range []string{"Stop", "Kill"} {
		t.Run(name, func(t *testing.T) {
			fa, _, sa, a := twoNodes(t, "node-a")
			call := a.Stop
			if name == "Kill" {
				call = a.Kill
			}
			if err := call(context.Background(), "ws1", []string{testSessionID, ""}); err == nil {
				t.Errorf("%s: err = nil, want an error for an empty session id", name)
			}
			if n := fa.total() + sa.requests(); n != 0 {
				t.Errorf("requests on node-a = %d, want 0", n)
			}
		})
	}
}

func TestRemoteStopUnmeasurableIsError(t *testing.T) {
	for _, name := range []string{"Stop", "Kill"} {
		t.Run(name, func(t *testing.T) {
			_, _, sa, a := twoNodes(t, "node-a")
			call, route := a.Stop, stopRoute
			if name == "Kill" {
				call, route = a.Kill, killRoute
			}
			sa.on(route, http.StatusInternalServerError, nodeapi.Problem{Status: 500, Detail: "pgrep: cannot read /proc"})
			err := call(context.Background(), "ws1", []string{testSessionID})
			if err == nil || errors.Is(err, workplace.ErrGone) {
				t.Errorf("%s: err = %v, want an error, not ErrGone", name, err)
			}
		})
	}
}

// Review focus 2: the hub answers 404 for a spoke it does not reach. That is never "gone", and
// a 404 workspace_gone of serve is not either while the hub cannot confirm it.
func TestRemoteNotFoundUnreachableIsUncertain(t *testing.T) {
	for _, node := range []string{"node-a", "node-c"} { // node-c: not known to the hub at all
		t.Run(node, func(t *testing.T) {
			fa, _, sa, a := twoNodes(t, node)
			fa.offline = true
			sa.on(sessionsRoute, http.StatusNotFound, gone())
			if _, err := a.Lookup(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) || errors.Is(err, workplace.ErrGone) {
				t.Errorf("Lookup: err = %v, want ErrUncertain and not ErrGone", err)
			}
			if _, err := a.Sessions(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) || errors.Is(err, workplace.ErrGone) {
				t.Errorf("Sessions: err = %v, want ErrUncertain and not ErrGone", err)
			}
			if err := a.Remove(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) {
				t.Errorf("Remove: err = %v, want ErrUncertain", err)
			}
			if n := sa.count(carrierRoute); n != 0 {
				t.Errorf("carrier deletes = %d after an unconfirmed 404, want 0: the carrier stays", n)
			}
			if n := fa.total(); n != 0 {
				t.Errorf("requests on node-a = %d, want 0: the hub does not reach it", n)
			}
		})
	}
}

// Review focus 2, the race: the hub answers its own 404 for a spoke it does not reach, and the
// tunnel is back before the snapshot is read. The hub's own 404 is never "gone", whatever the
// snapshot says.
func TestRemoteHubNotFoundIsUncertainWhileSnapshotReachable(t *testing.T) {
	fa, _, sa, a := twoNodes(t, "node-a")
	fa.back = true
	sa.on(sessionsRoute, http.StatusNotFound, gone())
	if _, err := a.Lookup(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) || errors.Is(err, workplace.ErrGone) {
		t.Errorf("Lookup: err = %v, want ErrUncertain and not ErrGone", err)
	}
	if _, err := a.Sessions(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) || errors.Is(err, workplace.ErrGone) {
		t.Errorf("Sessions: err = %v, want ErrUncertain and not ErrGone", err)
	}
	if err := a.Remove(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) {
		t.Errorf("Remove: err = %v, want ErrUncertain", err)
	}
	if n := sa.count(carrierRoute); n != 0 {
		t.Errorf("carrier deletes = %d after the hub's own 404, want 0", n)
	}
	if n := fa.total(); n != 0 {
		t.Errorf("requests on node-a = %d, want 0", n)
	}
}

// A node id with a space and a slash reaches its node through the hub, escaped as one segment.
func TestRemoteNodeIDEscaped(t *testing.T) {
	fn := newForgeNode(t)
	other := newForgeNode(t)
	hub := newFakeHub(t, map[string]*fakeForge{"node a/b": fn, "node a": other})
	tok := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tok, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fn.path = "/srv/ws1"
	fn.headRef = "issue-1-x"
	a := &Adapter{Client: New(hub.URL, tok), Node: "node a/b", StateDir: t.TempDir()}
	got, err := a.Lookup(context.Background(), "ws1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "/srv/ws1" {
		t.Errorf("Lookup = %+v, want the workspace of node a/b", got)
	}
	if n := fn.count("GET /api/v1/workspaces/ws1"); n != 1 {
		t.Errorf("reads on node a/b = %d, want 1", n)
	}
	if n := other.total(); n != 0 {
		t.Errorf("requests on node a = %d, want 0", n)
	}
}

// A hub whose snapshot fails cannot confirm a 404: uncertain, never gone.
func TestRemoteNotFoundSnapshotFailsIsUncertain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/snapshot":
			w.WriteHeader(http.StatusInternalServerError)
		case "/api/v1/fleet/hosts/node-a/workspaces/ws1":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	t.Cleanup(srv.Close)
	tok := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tok, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sa := newFakeServe(t, &events{})
	a := &Adapter{Client: New(srv.URL, tok), Node: "node-a", Serve: sa.client(t), StateDir: t.TempDir()}
	if _, err := a.Lookup(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) || errors.Is(err, workplace.ErrGone) {
		t.Errorf("Lookup: err = %v, want ErrUncertain and not ErrGone", err)
	}
	if err := a.Remove(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) {
		t.Errorf("Remove: err = %v, want ErrUncertain", err)
	}
	if n := sa.requests(); n != 0 {
		t.Errorf("requests on serve = %d, want 0: the carrier stays", n)
	}
}

func TestRemoteNotFoundReachableIsGone(t *testing.T) {
	fa, _, sa, a := twoNodes(t, "node-a")
	fa.getStatus = http.StatusNotFound
	fa.deleteStatus = http.StatusNotFound
	sa.on(sessionsRoute, http.StatusNotFound, gone())
	if _, err := a.Lookup(context.Background(), "ws1"); !errors.Is(err, workplace.ErrGone) {
		t.Errorf("Lookup: err = %v, want ErrGone", err)
	}
	if _, err := a.Sessions(context.Background(), "ws1"); !errors.Is(err, workplace.ErrGone) {
		t.Errorf("Sessions: err = %v, want ErrGone", err)
	}
	if err := a.Remove(context.Background(), "ws1"); err != nil {
		t.Errorf("Remove: err = %v, want nil for a workspace that is gone", err)
	}
	if n := sa.count(carrierRoute); n != 1 {
		t.Errorf("carrier deletes on serve = %d, want 1", n)
	}
}

func TestRemoteTranscriptNotMeasurable(t *testing.T) {
	fa, _, sa, a := twoNodes(t, "node-a")
	_, err := a.Transcript(context.Background(), "ws1", testSessionID)
	if !errors.Is(err, workplace.ErrNotMeasurable) || !strings.Contains(err.Error(), "node-a") {
		t.Errorf("err = %v, want ErrNotMeasurable naming node-a", err)
	}
	if n := fa.total() + sa.requests(); n != 0 {
		t.Errorf("requests on node-a = %d, want 0", n)
	}
}

func TestNodeReachable(t *testing.T) {
	host := func(id string, reachable string) string {
		return `{"id":"host-` + id + `","configKey":"` + id + `","nodeID":"` + id + `","name":"` + id + `","kind":"remote","federationRole":"spoke","platform":"linux","preferredTransport":"direct","reachable":` + reachable + `}`
	}
	cases := []struct {
		name, body string
		status     int
		want       bool
		wantErr    bool
	}{
		{"reachable", `{"hosts":[` + host("node-b", "false") + `,` + host("node-a", "true") + `]}`, 200, true, false},
		{"unreachable", `{"hosts":[` + host("node-a", "false") + `,` + host("node-b", "true") + `]}`, 200, false, false},
		{"absent", `{"hosts":[` + host("node-b", "true") + `]}`, 200, false, false},
		{"no hosts list", `{"projects":[]}`, 200, false, true},
		{"server error", `{}`, 500, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/snapshot" || r.URL.Query().Get("include_peers") != "true" {
					t.Errorf("request %s %s, want GET /api/v1/snapshot?include_peers=true", r.Method, r.URL)
				}
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			t.Cleanup(srv.Close)
			tok := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(tok, []byte("secret\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			a := &Adapter{Client: New(srv.URL, tok), Node: "node-a"}
			got, err := a.nodeReachable(context.Background())
			if (err != nil) != c.wantErr || got != c.want {
				t.Errorf("nodeReachable = %v, %v; want %v, error %v", got, err, c.want, c.wantErr)
			}
		})
	}
}

// decodeLaunch is the LaunchRequest serve received.
func decodeLaunch(t *testing.T, sa *fakeServe) nodeapi.LaunchRequest {
	t.Helper()
	var req nodeapi.LaunchRequest
	if err := json.Unmarshal([]byte(sa.body(launchRoute)), &req); err != nil {
		t.Fatalf("launch body %q: %v", sa.body(launchRoute), err)
	}
	return req
}

package kennforge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// twoNodes is a fake hub with node-a and node-b, and an adapter on node.
func twoNodes(t *testing.T, node string) (fa, fb *fakeForge, a *Adapter) {
	t.Helper()
	fa, fb = newForgeNode(t), newForgeNode(t)
	hub := newFakeHub(t, map[string]hubNode{"node-a": fa, "node-b": fb})
	tok := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tok, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a = &Adapter{
		Client: New(hub.URL, tok), Kind: "github", Repo: "CorneeldH/lighthouse", Node: node,
		StateDir: t.TempDir(), Targets: map[string]string{"software": "paia-software"},
		Profile: "software", UnitPrefix: "paia-forge-",
		Run: func(_ context.Context, _ time.Duration, name string, args ...string) (string, int, error) {
			t.Errorf("Runner called on a node: %s %v", name, args)
			return "", -1, errors.New("no runner on a node")
		},
		Sleep: func(time.Duration) {},
	}
	return fa, fb, a
}

const runtimeKey = "GET /api/v1/workspaces/ws1/runtime"

func TestRemotePrepareCreatesOnItsNode(t *testing.T) {
	fa, fb, a := twoNodes(t, "node-a")
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
	if n := fb.total(); n != 0 {
		t.Errorf("requests on node-b = %d, want 0", n)
	}
}

func TestRemotePrepareFindsOnItsNode(t *testing.T) {
	fa, fb, a := twoNodes(t, "node-b")
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
}

func TestRemoteSpawnHandoffOnItsNode(t *testing.T) {
	fa, fb, a := twoNodes(t, "node-a")
	s, err := a.Spawn(context.Background(), ws1, spec())
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
		t.Errorf("agent-sessions reads = %d, want 0: the hub has no such route", n)
	}
	if n := fb.total(); n != 0 {
		t.Errorf("requests on node-b = %d, want 0", n)
	}
	if _, err := os.Stat(workplace.CarrierPath(a.StateDir, "node-a", "ws1")); err != nil {
		t.Errorf("carrier of node-a: %v", err)
	}
	if _, err := os.Stat(workplace.CarrierPath(a.StateDir, "", "ws1")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("carrier of self: err = %v, want none", err)
	}
}

// The runtime answer of forge v0.7.0 (see wireRuntime): sessions without session_id.
const runtimeBody = `{"launch_targets":[],"sessions":[` +
	`{"key":"k1","workspace_id":"ws1","target_key":"paia-software","label":"Claude","kind":"agent","status":"running","display_region":"main","created_at":"2026-11-13T10:00:00Z"},` +
	`{"key":"k2","workspace_id":"ws1","target_key":"shell","label":"Shell","kind":"shell","status":"running","display_region":"main","created_at":"2026-11-13T10:01:00Z"},` +
	`{"key":"k3","workspace_id":"ws1","target_key":"paia-software","label":"Claude","kind":"agent","status":"exited","display_region":"main","created_at":"2026-11-13T09:00:00Z","exited_at":"2026-11-13T09:30:00Z","exit_code":0}]}`

func TestRemoteSessionsReadRuntime(t *testing.T) {
	fa, fb, a := twoNodes(t, "node-a")
	fa.runtime = []string{runtimeBody}
	ss, err := a.Sessions(context.Background(), "ws1")
	if err != nil {
		t.Fatal(err)
	}
	want := []workplace.Session{
		{Key: "k1", WorkspaceID: "ws1", State: "running"},
		{Key: "k3", WorkspaceID: "ws1", State: "done"},
	}
	if len(ss) != len(want) {
		t.Fatalf("sessions = %+v, want %+v", ss, want)
	}
	for i := range want {
		if ss[i] != want[i] {
			t.Errorf("session %d = %+v, want %+v", i, ss[i], want[i])
		}
	}
	if ss[0].Stand() != workplace.Waiting || ss[1].Stand() != workplace.Done {
		t.Errorf("stands = %v, %v; want waiting, done", ss[0].Stand(), ss[1].Stand())
	}
	if n := fa.count(runtimeKey); n != 1 {
		t.Errorf("runtime reads on node-a = %d, want 1", n)
	}
	if n := fb.total(); n != 0 {
		t.Errorf("requests on node-b = %d, want 0", n)
	}
}

func TestRemoteSessionsWithoutListUncertain(t *testing.T) {
	fa, _, a := twoNodes(t, "node-a")
	fa.runtime = []string{`{"launch_targets":[]}`}
	if _, err := a.Sessions(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) {
		t.Errorf("err = %v, want ErrUncertain", err)
	}
}

// Forge's /runtime in v0.7.0 has no session_id, so no session carries the id Stop gets: it sends
// no DELETE, runs no systemctl or podman, and needs no unit_prefix.
func TestRemoteStopSendsNoDelete(t *testing.T) {
	for _, name := range []string{"Stop", "Kill"} {
		t.Run(name, func(t *testing.T) {
			fa, fb, a := twoNodes(t, "node-a")
			a.UnitPrefix = ""
			fa.runtime = []string{runtimeBody}
			call := a.Stop
			if name == "Kill" {
				call = a.Kill
			}
			if err := call(context.Background(), "ws1", []string{testSessionID}); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if n := fa.count(runtimeKey); n != 1 {
				t.Errorf("runtime reads on node-a = %d, want 1", n)
			}
			for _, key := range []string{"DELETE /api/v1/workspaces/ws1/runtime/sessions/k1", "GET /api/v1/workspaces/ws1"} {
				if n := fa.count(key); n != 0 {
					t.Errorf("%s on node-a = %d, want 0", key, n)
				}
			}
			if n := fb.total(); n != 0 {
				t.Errorf("requests on node-b = %d, want 0", n)
			}
		})
	}
}

func TestRemoteStopUnmeasurableIsError(t *testing.T) {
	for _, name := range []string{"Stop", "Kill"} {
		t.Run(name, func(t *testing.T) {
			fa, _, a := twoNodes(t, "node-a")
			fa.runtimeStatus = http.StatusInternalServerError
			call := a.Stop
			if name == "Kill" {
				call = a.Kill
			}
			if err := call(context.Background(), "ws1", []string{testSessionID}); err == nil {
				t.Errorf("%s: err = nil, want an error when the sessions cannot be read", name)
			}
		})
	}
}

func TestRemoteStopEmptySessionIDRejected(t *testing.T) {
	fa, _, a := twoNodes(t, "node-a")
	if err := a.Stop(context.Background(), "ws1", []string{""}); err == nil {
		t.Error("Stop: err = nil, want an error for an empty session id")
	}
	if n := fa.total(); n != 0 {
		t.Errorf("requests on node-a = %d, want 0", n)
	}
}

// Review focus 2: the hub answers 404 for a spoke it does not reach. That is never "gone".
func TestRemoteNotFoundUnreachableIsUncertain(t *testing.T) {
	for _, node := range []string{"node-a", "node-c"} { // node-c: not known to the hub at all
		t.Run(node, func(t *testing.T) {
			fa, _, a := twoNodes(t, node)
			fa.offline = true
			writeCarrierOn(t, a, node)
			if _, err := a.Lookup(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) || errors.Is(err, workplace.ErrGone) {
				t.Errorf("Lookup: err = %v, want ErrUncertain and not ErrGone", err)
			}
			if _, err := a.Sessions(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) || errors.Is(err, workplace.ErrGone) {
				t.Errorf("Sessions: err = %v, want ErrUncertain and not ErrGone", err)
			}
			if err := a.Remove(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) {
				t.Errorf("Remove: err = %v, want ErrUncertain", err)
			}
			if _, err := os.Stat(workplace.CarrierPath(a.StateDir, node, "ws1")); err != nil {
				t.Errorf("carrier after an unconfirmed 404: %v, want it kept", err)
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
	fa, _, a := twoNodes(t, "node-a")
	fa.back = true
	writeCarrierOn(t, a, "node-a")
	if _, err := a.Lookup(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) || errors.Is(err, workplace.ErrGone) {
		t.Errorf("Lookup: err = %v, want ErrUncertain and not ErrGone", err)
	}
	if _, err := a.Sessions(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) || errors.Is(err, workplace.ErrGone) {
		t.Errorf("Sessions: err = %v, want ErrUncertain and not ErrGone", err)
	}
	if err := a.Remove(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) {
		t.Errorf("Remove: err = %v, want ErrUncertain", err)
	}
	if _, err := os.Stat(workplace.CarrierPath(a.StateDir, "node-a", "ws1")); err != nil {
		t.Errorf("carrier after the hub's own 404: %v, want it kept", err)
	}
	if n := fa.total(); n != 0 {
		t.Errorf("requests on node-a = %d, want 0", n)
	}
}

// A node id with a space and a slash reaches its node through the hub, escaped as one segment.
func TestRemoteNodeIDEscaped(t *testing.T) {
	fn := newForgeNode(t)
	other := newForgeNode(t)
	hub := newFakeHub(t, map[string]hubNode{"node a/b": fn, "node a": other})
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
	a := &Adapter{Client: New(srv.URL, tok), Node: "node-a", StateDir: t.TempDir()}
	if _, err := a.Lookup(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) || errors.Is(err, workplace.ErrGone) {
		t.Errorf("Lookup: err = %v, want ErrUncertain and not ErrGone", err)
	}
	if err := a.Remove(context.Background(), "ws1"); !errors.Is(err, workplace.ErrUncertain) {
		t.Errorf("Remove: err = %v, want ErrUncertain", err)
	}
}

func TestRemoteNotFoundReachableIsGone(t *testing.T) {
	fa, _, a := twoNodes(t, "node-a")
	fa.getStatus = http.StatusNotFound
	fa.runtimeStatus = http.StatusNotFound
	fa.deleteStatus = http.StatusNotFound
	writeCarrierOn(t, a, "node-a")
	if _, err := a.Lookup(context.Background(), "ws1"); !errors.Is(err, workplace.ErrGone) {
		t.Errorf("Lookup: err = %v, want ErrGone", err)
	}
	if _, err := a.Sessions(context.Background(), "ws1"); !errors.Is(err, workplace.ErrGone) {
		t.Errorf("Sessions: err = %v, want ErrGone", err)
	}
	if err := a.Remove(context.Background(), "ws1"); err != nil {
		t.Errorf("Remove: err = %v, want nil for a workspace that is gone", err)
	}
	if _, err := os.Stat(workplace.CarrierPath(a.StateDir, "node-a", "ws1")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("carrier after Remove: err = %v, want it wiped", err)
	}
}

func TestRemoteTranscriptNotMeasurable(t *testing.T) {
	fa, _, a := twoNodes(t, "node-a")
	_, err := a.Transcript(context.Background(), "ws1", testSessionID)
	if !errors.Is(err, workplace.ErrNotMeasurable) || !strings.Contains(err.Error(), "node-a") {
		t.Errorf("err = %v, want ErrNotMeasurable naming node-a", err)
	}
	if n := fa.total(); n != 0 {
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

// writeCarrierOn puts a carrier for ws1 of node in the state dir, as a spawn leaves it.
func writeCarrierOn(t *testing.T, a *Adapter, node string) {
	t.Helper()
	c, err := workplace.NewCarrier(spec(), a.StateDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workplace.WriteCarrier(a.StateDir, node, "ws1", c); err != nil {
		t.Fatal(err)
	}
}

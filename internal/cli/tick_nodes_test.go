package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
	"github.com/CorneeldH/lighthouse/internal/workplace/kennforge"
)

// hubNode is one forge node behind fleetHub, in the shape forge v0.7.0 serves (see
// internal/workplace/kennforge/fleet.go): workspaces per branch, and /runtime sessions with
// key, kind and status, without session_id.
type hubNode struct {
	mu sync.Mutex
	// listStatus, when set, answers GET /workspaces: a node that cannot prepare.
	listStatus int
	// handoff is "" (accept with a new key) or "hang" (no answer until the client gives up).
	handoff    string
	workspaces map[string]string   // id -> branch
	sessions   map[string][]string // workspace id -> session keys, status running
	posts      int                 // handoff POSTs
	release    chan struct{}       // closed at the end of the test: a hanging handoff returns
}

// fleetHub is a kenn-forge hub as v0.7.0 serves it: /api/v1/fleet/hosts/{id}/... goes to
// nodes[id] with the path back under /api/v1, and the snapshot lists every node reachable.
// Every request is logged as "<node> <method> <path>"; a request for the hub's own /api/v1
// (self) is logged as "self ...".
type fleetHub struct {
	srv   *httptest.Server
	nodes map[string]*hubNode
	mu    sync.Mutex
	log   []string
}

func newFleetHub(t *testing.T, ids ...string) *fleetHub {
	t.Helper()
	h := &fleetHub{nodes: map[string]*hubNode{}}
	for _, id := range ids {
		h.nodes[id] = &hubNode{workspaces: map[string]string{}, sessions: map[string][]string{}, release: make(chan struct{})}
	}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.EscapedPath()
		if path == "/api/v1/snapshot" && r.Method == http.MethodGet && r.URL.Query().Get("include_peers") == "true" {
			var hosts []string
			for id := range h.nodes {
				hosts = append(hosts, fmt.Sprintf(`{"nodeID":%q,"federationRole":"spoke","reachable":true}`, id))
			}
			_, _ = fmt.Fprintf(w, `{"protocolVersion":1,"hosts":[%s]}`, strings.Join(hosts, ","))
			return
		}
		rest, ok := strings.CutPrefix(path, "/api/v1/fleet/hosts/")
		if !ok {
			h.add("self " + r.Method + " " + path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, tail, _ := strings.Cut(rest, "/")
		id, _ := url.PathUnescape(raw)
		h.add(id + " " + r.Method + " /api/v1/" + tail)
		n, known := h.nodes[id]
		if !known {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"status":404,"detail":"fleet host not found"}`)
			return
		}
		n.serve(t, w, r, r.Method+" /"+tail)
	}))
	t.Cleanup(h.srv.Close)
	t.Cleanup(func() {
		for _, n := range h.nodes {
			close(n.release)
		}
	})
	return h
}

func (h *fleetHub) add(line string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.log = append(h.log, line)
}

// requests returns the logged requests that start with prefix.
func (h *fleetHub) requests(prefix string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, l := range h.log {
		if strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return out
}

func (n *hubNode) serve(t *testing.T, w http.ResponseWriter, r *http.Request, route string) {
	n.mu.Lock()
	hang := false
	defer func() {
		n.mu.Unlock()
		if hang {
			// The body read to its end lets the server see the client give up.
			_, _ = io.Copy(io.Discard, r.Body)
			select {
			case <-r.Context().Done():
			case <-n.release:
			}
		}
	}()
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.TrimPrefix(route, r.Method+" /"), "/")
	switch {
	case route == "GET /workspaces":
		if n.listStatus != 0 {
			w.WriteHeader(n.listStatus)
			return
		}
		var list []string
		for id, branch := range n.workspaces {
			list = append(list, n.workspace(id, branch))
		}
		_, _ = fmt.Fprintf(w, `{"workspaces":[%s]}`, strings.Join(list, ","))
	case r.Method == http.MethodPost && len(parts) == 5 && parts[0] == "repo" && parts[4] == "workspaces":
		var body struct{ Branch string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		id := fmt.Sprintf("ws%d", len(n.workspaces)+1)
		n.workspaces[id] = body.Branch
		_, _ = fmt.Fprintf(w, `{"id":%q}`, id)
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "workspaces":
		branch, ok := n.workspaces[parts[1]]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprint(w, n.workspace(parts[1], branch))
	case r.Method == http.MethodGet && len(parts) == 3 && parts[2] == "runtime":
		var ss []string
		for _, k := range n.sessions[parts[1]] {
			ss = append(ss, fmt.Sprintf(`{"key":%q,"workspace_id":%q,"kind":"agent","status":"running"}`, k, parts[1]))
		}
		_, _ = fmt.Fprintf(w, `{"launch_targets":[],"sessions":[%s]}`, strings.Join(ss, ","))
	case r.Method == http.MethodPost && len(parts) == 4 && parts[3] == "agent-handoffs":
		n.posts++
		if n.handoff == "hang" {
			hang = true
			return
		}
		key := fmt.Sprintf("k%d", n.posts)
		n.sessions[parts[1]] = append(n.sessions[parts[1]], key)
		_, _ = fmt.Fprintf(w, `{"session":{"key":%q}}`, key)
	default:
		t.Errorf("fake node: unexpected %s", route)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (n *hubNode) workspace(id, branch string) string {
	return fmt.Sprintf(`{"id":%q,"status":"ready","worktree_path":"/node/work/%s","git_head_ref":"refs/heads/%s","repo":{"provider":"github","repo_path":"o/r"}}`,
		id, id, branch)
}

func (n *hubNode) postCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.posts
}

// nodeRepo is completeRepo with location labels: 1-loc:mac runs on profile mac.
var nodeRepo = strings.Replace(completeRepo, "[controller]\n",
	"[labels]\nlocation_prefix = \"1-loc:\"\n\n[controller]\nprofile = \"software\"\n\n[controller.locations]\nmac = \"mac\"\n", 1)

// nodeHost is the host.toml of the node tests: profile mac on node-a, profile other on node-b,
// software on self, all behind the forge at hub.
func nodeHost(t *testing.T, env tickEnv, hub string) {
	t.Helper()
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	appendHostToml(t, env, fmt.Sprintf(`max_runtime = "1h"

[dispatch.forge]
url = %q
token_file = %q
unit_prefix = "lh-"

[dispatch.nodes.a]
node_id = "node-a"
slots = 1

[dispatch.nodes.b]
node_id = "node-b"
slots = 1

[dispatch.profiles.software]
target_key = "t-software"

[dispatch.profiles.mac]
target_key = "t-mac"
node = "a"

[dispatch.profiles.other]
target_key = "t-other"
node = "b"
`, hub, token))
}

// macIssue is issue n of o/r in Controller with the location label 1-loc:mac.
func macIssue(n int) map[string]any {
	return boardNode(fmt.Sprintf("I%d", n), "c1", issueContent(n, "Issue", map[string]any{
		"labels": map[string]any{"nodes": []any{map[string]any{"name": "1-loc:mac"}}},
	}))
}

// useNodes gives self the fake workplace w and every other node the real kenn-forge adapter
// tick builds; client, when set, replaces the adapter's HTTP client.
func useNodes(t *testing.T, env *tickEnv, w *fakeWorkplace, client *http.Client) {
	t.Helper()
	real := *env
	real.Workplace = nil
	env.Workplace = func(r repoconfig.Repo, node, profile string) workplace.Workplace {
		if node == "" {
			return w
		}
		cfg, err := hostconfig.LoadDispatch(env.HostToml)
		if err != nil {
			t.Fatal(err)
		}
		wp := real.workplace(cfg, r, env.StateDir, node, profile)
		if a, ok := wp.(*kennforge.Adapter); ok && client != nil {
			a.Client.HTTP = client
		}
		return wp
	}
}

// A run on node-a outlives the tick that started it: after a restart, its timeout stops it
// through node-a, and nothing goes to node-b or to the hub's own forge. That holds too when
// profile mac lost its node in host.toml meanwhile: the run row keeps node-a.
func TestTickRestartStopsOnNode(t *testing.T) {
	for _, lostNode := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost node %t", lostNode), func(t *testing.T) { testTickRestartStopsOnNode(t, lostNode) })
	}
}

func testTickRestartStopsOnNode(t *testing.T, lostNode bool) {
	env, db := tickFixture(t, repoDir(t, nodeRepo))
	hub := newFleetHub(t, "node-a", "node-b")
	nodeHost(t, env, hub.srv.URL)
	srv, w := startable(t, &env)
	srv.BoardNodes = []map[string]any{macIssue(7)}
	useNodes(t, &env, w, nil)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick 1 = %v\n%s", err, errOut.String())
	}
	conn := openStore(t, db)
	rows := history(t, conn, "o/r")
	if len(rows) != 1 || !rows[0].Open || rows[0].Node != "node-a" || rows[0].Profile != "mac" {
		t.Fatalf("runs after tick 1 = %+v, want one open on node-a with profile mac", rows)
	}
	if n := hub.nodes["node-a"].postCount(); n != 1 {
		t.Fatalf("handoffs on node-a = %d, want 1", n)
	}
	if len(w.spawned()) != 0 {
		t.Fatalf("spawns on self = %+v, want none", w.spawned())
	}
	before := len(hub.requests("node-a "))
	if lostNode {
		host, err := os.ReadFile(env.HostToml)
		if err != nil {
			t.Fatal(err)
		}
		lost := strings.Replace(string(host), "target_key = \"t-mac\"\nnode = \"a\"\n", "target_key = \"t-mac\"\n", 1)
		if lost == string(host) {
			t.Fatal("host.toml has no node for profile mac")
		}
		if err := os.WriteFile(env.HostToml, []byte(lost), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	env.Now = func() time.Time { return t0.Add(2 * time.Hour) }
	srv.Now = env.Now
	errOut.Reset()
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick 2 = %v\n%s", err, errOut.String())
	}
	rows = history(t, conn, "o/r")
	if rows[0].Open || rows[0].Outcome != runs.Returned {
		t.Errorf("run after tick 2 = %+v, want closed as returned (timeout)", rows[0])
	}
	after := hub.requests("node-a ")[before:]
	if !slices.Contains(after, "node-a GET /api/v1/workspaces/ws1/runtime") {
		t.Errorf("node-a requests in tick 2 = %q, want the sessions read of the stop", after)
	}
	for _, l := range after {
		if strings.Contains(l, " DELETE ") || strings.Contains(l, " POST ") {
			t.Errorf("node-a request in tick 2 = %q: /runtime has no session_id, so Stop finds nothing to delete, and nothing restarts", l)
		}
	}
	if got := append(hub.requests("node-b "), hub.requests("self ")...); len(got) != 0 {
		t.Errorf("requests to node-b or self = %q, want none", got)
	}
}

// A node that cannot prepare fails only its own start: the start on self in the same tick goes
// on, node-a gets no run, and the repo keeps no fingerprint, so the next tick tries again.
func TestTickUnreachableNodeDoesNotBlockOther(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, nodeRepo))
	hub := newFleetHub(t, "node-a", "node-b")
	hub.nodes["node-a"].listStatus = http.StatusServiceUnavailable
	nodeHost(t, env, hub.srv.URL)
	srv, w := startable(t, &env)
	srv.BoardNodes = []map[string]any{macIssue(7), boardNode("I8", "c1", issueContent(8, "Issue", nil))}
	useNodes(t, &env, w, nil)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != errSilent {
		t.Fatalf("tick = %v, want errSilent\n%s", err, errOut.String())
	}
	if !strings.Contains(errOut.String(), "lighthouse: apply o/r: o/r#7 start: prepare: list workspaces: ") {
		t.Errorf("errOut = %q, want an apply line for the start of #7 on node-a", errOut.String())
	}
	conn := openStore(t, db)
	rows := history(t, conn, "o/r")
	if len(rows) != 1 || rows[0].Issue != 8 || rows[0].Node != "" || !rows[0].Open {
		t.Errorf("runs = %+v, want only the open run of #8 on self", rows)
	}
	if s := w.spawned(); len(s) != 1 || s[0].Issue != 8 {
		t.Errorf("spawns on self = %+v, want the one of #8", s)
	}
	if s := tickState(t, conn, "o/r"); s.Fingerprint != "" {
		t.Errorf("tick state = %+v, want no fingerprint: a start failed", s)
	}
}

// A handoff to node-a that times out leaves an uncertain run: one POST in that tick, and none
// in the next.
func TestTickSpawnTimeoutNoSecondPost(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, nodeRepo))
	hub := newFleetHub(t, "node-a", "node-b")
	hub.nodes["node-a"].handoff = "hang"
	nodeHost(t, env, hub.srv.URL)
	srv, w := startable(t, &env)
	srv.BoardNodes = []map[string]any{macIssue(7)}
	useNodes(t, &env, w, &http.Client{Timeout: 300 * time.Millisecond})
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick 1 = %v\n%s", err, errOut.String())
	}
	if n := hub.nodes["node-a"].postCount(); n != 1 {
		t.Fatalf("handoffs after tick 1 = %d, want 1", n)
	}
	conn := openStore(t, db)
	rows := history(t, conn, "o/r")
	if len(rows) != 1 || !rows[0].Open || rows[0].Node != "node-a" {
		t.Fatalf("runs after tick 1 = %+v, want one open on node-a", rows)
	}
	env.Now = func() time.Time { return t0.Add(time.Minute) }
	srv.Now = env.Now
	errOut.Reset()
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick 2 = %v\n%s", err, errOut.String())
	}
	if n := hub.nodes["node-a"].postCount(); n != 1 {
		t.Errorf("handoffs after tick 2 = %d, want still 1", n)
	}
	if rows := history(t, conn, "o/r"); len(rows) != 1 {
		t.Errorf("runs after tick 2 = %+v, want still one", rows)
	}
}

// measure reads a run on the node its row names: a run on node-a whose workspace id self does
// not know is not gone.
func TestMeasureUsesNodeOfRun(t *testing.T) {
	gone := &measureWorkplace{errs: map[string]error{"ws-1": fmt.Errorf("404: %w", workplace.ErrGone)}}
	nodeA := &measureWorkplace{sessions: map[string][]workplace.Session{
		"ws-1": {{Key: "k1", WorkspaceID: "ws-1", State: "running"}},
	}}
	var asked []string
	env := tickEnv{StateDir: t.TempDir(), Workplace: func(_ repoconfig.Repo, node, profile string) workplace.Workplace {
		asked = append(asked, node+"/"+profile)
		if node == "node-a" {
			return nodeA
		}
		return gone
	}}
	open := []runs.OpenRun{{ID: 1, Issue: 7, SessionID: sidMeasured, WorkspaceID: "ws-1", WorkplaceSession: "k1",
		Node: "node-a", Profile: "mac"}}
	var errOut bytes.Buffer
	stands, states, err := measure(context.Background(), env, hostconfig.Dispatch{}, repoconfig.Repo{Slug: "o/r"}, open, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[int64]dispatch.RunState{1: {}}; !reflect.DeepEqual(states, want) {
		t.Errorf("states = %+v, want %+v (not gone)", states, want)
	}
	if want := map[int64]workplace.Stand{1: workplace.Waiting}; !reflect.DeepEqual(stands, want) {
		t.Errorf("stands = %v, want %v", stands, want)
	}
	if want := []string{"node-a/mac"}; !reflect.DeepEqual(asked, want) {
		t.Errorf("workplaces asked = %q, want %q", asked, want)
	}
	if errOut.Len() != 0 {
		t.Errorf("errOut = %q, want empty", errOut.String())
	}
}

// places builds one workplace per repo, node and profile, and gives the same one back on a
// second ask: self and node-a in the same profile are two workplaces.
func TestTickPlacesPerNodeAndProfile(t *testing.T) {
	env := tickEnv{Getenv: func(string) string { return "" }}
	cfg := hostconfig.Dispatch{Forge: hostconfig.DispatchForge{URL: "http://localhost:1"}}
	places := env.places(cfg, "/state")
	r := repoconfig.Repo{Slug: "o/r", Kind: "github"}
	type place struct{ node, profile string }
	for _, p := range []place{{"", "mac"}, {"node-a", "mac"}, {"node-a", "software"}} {
		a, ok := places(r, p.node, p.profile).(*kennforge.Adapter)
		if !ok {
			t.Fatalf("workplace %v = %T, want *kennforge.Adapter", p, places(r, p.node, p.profile))
		}
		if a.Node != p.node || a.Profile != p.profile {
			t.Errorf("workplace %v: Node, Profile = %q, %q", p, a.Node, a.Profile)
		}
		if places(r, p.node, p.profile) != a {
			t.Errorf("workplace %v: built twice", p)
		}
	}
}

package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
	"github.com/CorneeldH/lighthouse/internal/workplace/kennforge"
)

// hubNode is one forge node behind fleetHub, in the shape forge v0.7.0 serves (see
// internal/workplace/kennforge/fleet.go): workspaces per branch and handoffs. Beside it runs
// lighthouse serve on that node (serveSrv), the fake of nodeserve: the carrier launch puts the
// session id a handoff starts, and sessions answers it as the node's /agent-sessions would.
type hubNode struct {
	mu sync.Mutex
	// listStatus, when set, answers GET /workspaces: a node that cannot prepare.
	listStatus int
	// handoff is "" (accept with a new key) or "hang" (no answer until the client gives up).
	handoff    string
	workspaces map[string]string   // id -> branch
	sessions   map[string][]string // workspace id -> session keys
	sids       map[string]string   // session key -> session id of its carrier
	carriers   map[string]string   // workspace id -> session id of the carrier serve holds
	stopped    map[string]bool     // session ids serve stopped or killed
	posts      int                 // handoff POSTs
	release    chan struct{}       // closed at the end of the test: a hanging handoff returns

	serveSrv   *httptest.Server
	serveToken string

	// The usage brake on the node (#327): its resume markers, the PUT usage bodies it took, the
	// DELETE markers it took ("<session> <key>"), and its permission notes per session.
	markers     []park.Marker
	usage       []nodeapi.UsageRequest
	deleted     []string
	permissions map[string][]byte
	postFailed  map[string][]byte
	// markersStatus, usageStatus and authStatus, when set, answer GET markers, PUT usage and
	// every request (a bad token) with that status.
	markersStatus, usageStatus, authStatus int
	// sessionState and sessionUpdated, when set, are the state and updated_at of every session
	// that was not stopped; transcriptStatus, when set, answers GET transcript.
	sessionState, sessionUpdated string
	transcriptStatus             int
}

// fleetHub is a kenn-forge hub as v0.7.0 serves it: /api/v1/fleet/hosts/{id}/... goes to
// nodes[id] with the path back under /api/v1, and the snapshot lists every node reachable.
// Every request is logged as "<node> <method> <path>"; a request for the hub's own /api/v1
// (self) is logged as "self ...".
type fleetHub struct {
	srv   *httptest.Server
	ids   []string // the node ids, in the order newFleetHub got them
	nodes map[string]*hubNode
	mu    sync.Mutex
	log   []string
}

func newFleetHub(t *testing.T, ids ...string) *fleetHub {
	t.Helper()
	h := &fleetHub{ids: ids, nodes: map[string]*hubNode{}}
	for _, id := range ids {
		n := &hubNode{
			workspaces: map[string]string{}, sessions: map[string][]string{}, sids: map[string]string{},
			carriers: map[string]string{}, stopped: map[string]bool{}, release: make(chan struct{}),
			serveToken: "serve-" + id, permissions: map[string][]byte{}, postFailed: map[string][]byte{},
		}
		n.serveSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.add(id + " serve " + r.Method + " " + r.URL.EscapedPath())
			n.serveNode(t, w, r)
		}))
		t.Cleanup(n.serveSrv.Close)
		h.nodes[id] = n
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
	case r.Method == http.MethodPost && len(parts) == 4 && parts[3] == "agent-handoffs":
		n.posts++
		if n.carriers[parts[1]] == "" {
			t.Errorf("fake node: handoff in %s without a carrier from serve", parts[1])
		}
		if n.handoff == "hang" {
			hang = true
			return
		}
		key := fmt.Sprintf("k%d", n.posts)
		n.sessions[parts[1]] = append(n.sessions[parts[1]], key)
		n.sids[key] = n.carriers[parts[1]]
		_, _ = fmt.Fprintf(w, `{"session":{"key":%q}}`, key)
	default:
		t.Errorf("fake node: unexpected %s", route)
		w.WriteHeader(http.StatusNotFound)
	}
}

// serveNode is lighthouse serve on the node, as nodeserve answers the routes the tick uses:
// the bearer check, info, launch (the carrier), carrier, follow, relocate, sessions (with session_id,
// working until stopped), transcript (none yet), stop, kill, usage, markers and permission. Any
// other route is an error of the tick.
func (n *hubNode) serveNode(t *testing.T, w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+n.serveToken {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if n.authStatus != 0 {
		w.WriteHeader(n.authStatus)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	rest, _ := strings.CutPrefix(r.URL.Path, nodeapi.Prefix+"/")
	parts := strings.Split(rest, "/")
	route := r.Method + " " + rest
	switch {
	case route == "GET info":
		_, _ = fmt.Fprintf(w, `{"version":%d,"binary":"test","platform":"linux/amd64"}`, nodeapi.Version)
	case route == "GET markers":
		if n.markersStatus != 0 {
			w.WriteHeader(n.markersStatus)
			return
		}
		out := append([]park.Marker{}, n.markers...)
		_ = json.NewEncoder(w).Encode(out)
	case r.Method == http.MethodDelete && len(parts) == 2 && parts[0] == "markers":
		key := r.URL.Query().Get("key")
		n.deleted = append(n.deleted, parts[1]+" "+key)
		n.markers = slices.DeleteFunc(n.markers, func(m park.Marker) bool { return m.SessionID == parts[1] && m.Key() == key })
		w.WriteHeader(http.StatusNoContent)
	case route == "PUT usage":
		if n.usageStatus != 0 {
			w.WriteHeader(n.usageStatus)
			return
		}
		var req nodeapi.UsageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("fake serve: usage: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		n.usage = append(n.usage, req)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "runs" && parts[2] == "permission":
		data, ok := n.permissions[parts[1]]
		if !ok {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(nodeapi.Problem{Status: 404, Code: nodeapi.CodeNoPermission, Detail: "no permission note"})
			return
		}
		_, _ = w.Write(data)
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "runs" && parts[2] == "post-failed":
		data, ok := n.postFailed[parts[1]]
		if !ok {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(nodeapi.Problem{Status: 404, Code: nodeapi.CodeNoPostFailed, Detail: "no post-failed note"})
			return
		}
		w.Header().Set(nodeapi.HeaderModTime, hubPostFailedModTime)
		_, _ = w.Write(data)
	case len(parts) == 3 && parts[0] == "workspaces":
		n.serveWorkspace(t, w, r, parts[1], r.Method+" "+parts[2])
	case r.Method == http.MethodGet && len(parts) == 4 && parts[0] == "workspaces" && parts[2] == "transcript" && n.transcriptStatus != 0:
		w.WriteHeader(n.transcriptStatus)
		_ = json.NewEncoder(w).Encode(nodeapi.Problem{Status: n.transcriptStatus, Detail: "transcript failed"})
	case r.Method == http.MethodGet && len(parts) == 4 && parts[0] == "workspaces" && parts[2] == "transcript":
		// No transcript yet on the node: the tick measures the run without one.
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(nodeapi.Problem{Status: 404, Code: nodeapi.CodeNoTranscript, Detail: "no transcript"})
	default:
		t.Errorf("fake serve: unexpected %s", route)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (n *hubNode) serveWorkspace(t *testing.T, w http.ResponseWriter, r *http.Request, id, route string) {
	switch route {
	case "PUT launch":
		var req nodeapi.LaunchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SessionID == "" || req.Instructions == "" {
			t.Errorf("fake serve: launch %+v: %v", req, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		n.carriers[id] = req.SessionID
		_ = json.NewEncoder(w).Encode(workplace.Carrier{SessionID: req.SessionID})
	case "DELETE carrier":
		delete(n.carriers, id)
		w.WriteHeader(http.StatusNoContent)
	case "POST follow":
		w.WriteHeader(http.StatusNoContent)
	case "POST relocate":
		// The start command names no path in the worktree: serve hands it back unchanged.
		var req nodeapi.Relocation
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Command == "" {
			t.Errorf("fake serve: relocate %+v: %v", req, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(req)
	case "GET sessions":
		if _, ok := n.workspaces[id]; !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(nodeapi.Problem{Status: 404, Code: nodeapi.CodeWorkspaceGone, Detail: "gone"})
			return
		}
		out := []nodeapi.Session{}
		for _, k := range n.sessions[id] {
			state := "working"
			if n.sessionState != "" {
				state = n.sessionState
			}
			if n.stopped[n.sids[k]] {
				state = "done"
			}
			out = append(out, nodeapi.Session{Key: k, State: state, SessionID: n.sids[k], UpdatedAt: n.sessionUpdated})
		}
		_ = json.NewEncoder(w).Encode(out)
	case "POST stop", "POST kill":
		var req nodeapi.StopRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Profile == "" {
			t.Errorf("fake serve: %s %+v: %v", route, req, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, sid := range req.SessionIDs {
			n.stopped[sid] = true
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		t.Errorf("fake serve: unexpected %s in %s", route, id)
		w.WriteHeader(http.StatusNotFound)
	}
}

// tokenFile is a file with the node's serve token.
func (n *hubNode) tokenFile(t *testing.T) string {
	t.Helper()
	tok := filepath.Join(t.TempDir(), "serve-token")
	if err := os.WriteFile(tok, []byte(n.serveToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return tok
}

// set changes the node's state under its lock.
func (n *hubNode) set(f func(n *hubNode)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	f(n)
}

// got reads the node's state under its lock.
func got[T any](n *hubNode, f func(n *hubNode) T) T {
	n.mu.Lock()
	defer n.mu.Unlock()
	return f(n)
}

func (n *hubNode) workspace(id, branch string) string {
	return fmt.Sprintf(`{"id":%q,"status":"ready","worktree_path":"/node/work/%s","git_head_ref":"refs/heads/%s","repo":{"provider":"github","repo_path":"o/r"}}`,
		id, id, branch)
}

// carrier is the session id of the carrier serve holds for workspace id.
func (n *hubNode) carrier(id string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.carriers[id]
}

// wasStopped reports whether serve stopped or killed session sid.
func (n *hubNode) wasStopped(sid string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.stopped[sid]
}

func (n *hubNode) postCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.posts
}

// nodeRepo is completeRepo with location labels: 1-loc:mac runs on profile mac.
var nodeRepo = strings.Replace(completeRepo, "[controller]\n",
	"[labels]\nlocation_prefix = \"1-loc:\"\n\n[controller]\nprofile = \"software\"\n\n[controller.locations]\nmac = \"mac\"\n", 1)

// nodeHost is the host.toml of the node tests: profile mac on the first node of hub (name a),
// profile other on the second (name b), software on self, all behind the forge at hub, each
// node with its serve and token.
func nodeHost(t *testing.T, env tickEnv, hub *fleetHub) {
	t.Helper()
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, b := hub.nodes[hub.ids[0]], hub.nodes[hub.ids[1]]
	appendHostToml(t, env, fmt.Sprintf(`max_runtime = "1h"

[dispatch.forge]
url = %q
token_file = %q
unit_prefix = "lh-"

[dispatch.nodes.a]
node_id = %q
slots = 1
serve_url = %q
serve_token_file = %q

[dispatch.nodes.b]
node_id = %q
slots = 1
serve_url = %q
serve_token_file = %q

[dispatch.profiles.software]
target_key = "t-software"

[dispatch.profiles.mac]
target_key = "t-mac"
node = "a"

[dispatch.profiles.other]
target_key = "t-other"
node = "b"
`, hub.srv.URL, token, hub.ids[0], a.serveSrv.URL, a.tokenFile(t), hub.ids[1], b.serveSrv.URL, b.tokenFile(t)))
}

// macIssue is issue n of o/r in Controller with the location label 1-loc:mac.
func macIssue(n int) map[string]any {
	return boardNode(fmt.Sprintf("I%d", n), "c1", issueContent(n, "Issue", map[string]any{
		"labels": map[string]any{"nodes": []any{map[string]any{"name": "1-loc:mac"}}},
	}))
}

// useNodes gives self the fake workplace w and every other node the real kenn-forge adapter
// tick builds, with the serve client of the tick; client, when set, replaces the adapter's HTTP
// client of the hub.
func useNodes(t *testing.T, env *tickEnv, w *fakeWorkplace, client *http.Client) {
	t.Helper()
	real := *env
	real.Workplace = nil
	env.Workplace = func(r repoconfig.Repo, node, profile string) workplace.Workplace {
		if node == "" {
			return w
		}
		if client == nil {
			return nil // the workplace tick builds
		}
		cfg, err := hostconfig.LoadDispatch(env.HostToml)
		if err != nil {
			t.Fatal(err)
		}
		wp := real.workplace(cfg, r, env.StateDir, node, profile)
		a, ok := wp.(*kennforge.Adapter)
		if !ok {
			t.Fatalf("workplace on %s = %T, want *kennforge.Adapter", node, wp)
		}
		a.Client.HTTP = client
		return wp
	}
}

// A run on node-a outlives the tick that started it: after a restart, its timeout stops it
// through serve on node-a, and nothing goes to node-b or to the hub's own forge. That holds too when
// profile mac lost its node in host.toml meanwhile: the run row keeps node-a.
func TestTickRestartStopsOnNode(t *testing.T) {
	for _, lostNode := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost node %t", lostNode), func(t *testing.T) { testTickRestartStopsOnNode(t, lostNode) })
	}
}

func testTickRestartStopsOnNode(t *testing.T, lostNode bool) {
	env, db := tickFixture(t, repoDir(t, nodeRepo))
	hub := newFleetHub(t, "node-a", "node-b")
	nodeHost(t, env, hub)
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
	if got := hub.nodes["node-a"].carrier("ws1"); got != rows[0].SessionID {
		t.Fatalf("carrier on node-a = %q, want the run's session %s", got, rows[0].SessionID)
	}
	if entries, err := os.ReadDir(filepath.Join(env.StateDir, "carriers")); err == nil && len(entries) > 0 {
		t.Errorf("carriers on the tick host = %v, want none: the carrier is on node-a", entries)
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
	if !slices.Contains(after, "node-a serve POST /api/node/v1/workspaces/ws1/stop") {
		t.Errorf("node-a requests in tick 2 = %q, want the stop through serve", after)
	}
	sid := rows[0].SessionID
	if !hub.nodes["node-a"].wasStopped(sid) {
		t.Errorf("session %s not stopped on node-a, want the run's session stopped", sid)
	}
	for _, l := range after {
		if strings.HasPrefix(l, "node-a POST ") || strings.Contains(l, "/launch") {
			t.Errorf("node-a request in tick 2 = %q: nothing restarts", l)
		}
	}
	// Every tick reads the markers of every node and pushes its usage there; nothing else.
	perTick := []string{"node-b serve GET " + nodeapi.PathInfo, "node-b serve GET " + nodeapi.PathMarkers, "node-b serve PUT " + nodeapi.PathUsage}
	got := slices.DeleteFunc(append(hub.requests("node-b "), hub.requests("self ")...), func(l string) bool { return slices.Contains(perTick, l) })
	if len(got) != 0 {
		t.Errorf("requests to node-b or self = %q, want none but markers and usage", got)
	}
}

// A node that cannot prepare fails only its own start: the start on self in the same tick goes
// on, node-a gets no run, and the repo keeps no fingerprint, so the next tick tries again.
func TestTickUnreachableNodeDoesNotBlockOther(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, nodeRepo))
	hub := newFleetHub(t, "node-a", "node-b")
	hub.nodes["node-a"].listStatus = http.StatusServiceUnavailable
	nodeHost(t, env, hub)
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
	nodeHost(t, env, hub)
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
	if want := map[int64]dispatch.RunState{1: {TranscriptUnmeasurable: true}}; !reflect.DeepEqual(states, want) {
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

// randomNode is a node id no fixture shares.
func randomNode() string { return "node-" + strings.ToLower(rand.Text()[:8]) }

// nodeTick is a tick fixture on repo o/r (nodeRepo) with the board items, two nodes of random
// ids behind a fleet hub (nodeHost), and the fake workplace on self.
func nodeTick(t *testing.T, items ...map[string]any) (tickEnv, string, *fleetHub, *fakeWorkplace) {
	t.Helper()
	env, db := tickFixture(t, repoDir(t, nodeRepo))
	hub := newFleetHub(t, randomNode(), randomNode())
	nodeHost(t, env, hub)
	srv, w := startable(t, &env)
	srv.BoardNodes = items
	useNodes(t, &env, w, nil)
	return env, db, hub, w
}

// nodeConfig is the default [dispatch] with the nodes of hub, named n0, n1, …, each with its serve.
func nodeConfig(t *testing.T, hub *fleetHub) hostconfig.Dispatch {
	t.Helper()
	cfg := hostconfig.DefaultDispatch()
	cfg.Nodes = map[string]hostconfig.Node{}
	for i, id := range hub.ids {
		n := hub.nodes[id]
		cfg.Nodes[fmt.Sprintf("n%d", i)] = hostconfig.Node{NodeID: id, Slots: 1, ServeURL: n.serveSrv.URL, ServeTokenFile: n.tokenFile(t)}
	}
	return cfg
}

// parkOn opens a run of session on repo#issue on node, in profile mac, and closes it as parked.
func parkOn(t *testing.T, db *sql.DB, repo string, issue int, session, node string) {
	t.Helper()
	ctx := context.Background()
	r, err := runs.Take(ctx, db, runs.Claim{Repo: repo, Issue: issue, SessionID: session, Mode: "start", Trigger: "handoff",
		TriggerKey: fmt.Sprintf("start:%d", issue), Command: "/lighthouse-start-issue", Prompt: "p", Profile: "mac", Node: node,
		Branch: "b", Now: t0.Add(-3 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.Release(ctx, db, r.ID, runs.Parked, "", t0.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
}

// controllerMarker is the controller marker of session, parked at t0-3h, whose window reset at reset.
func controllerMarker(session string, reset time.Time) park.Marker {
	return park.Marker{SessionID: session, Cwd: "/work", Window: "session", ResetAt: reset.Format(time.RFC3339),
		CreatedAt: t0.Add(-3 * time.Hour).Format(time.RFC3339), Source: "guard", Controller: true}
}

// Every adapter on a node gets the serve client of that node from host.toml, and the adapters of
// one node share it within a tick, so GET info goes once per node; self gets none.
func TestWorkplaceOnNodeGetsServeClient(t *testing.T) {
	one, two := randomNode(), randomNode()
	cfg := hostconfig.Dispatch{Forge: hostconfig.DispatchForge{URL: "http://localhost:1"}, Nodes: map[string]hostconfig.Node{
		"one": {NodeID: one, Slots: 1, ServeURL: "https://one.test", ServeTokenFile: "/tok/one"},
		"two": {NodeID: two, Slots: 1, ServeURL: "https://two.test", ServeTokenFile: "/tok/two"},
	}}
	env := tickEnv{Getenv: func(string) string { return "" }}
	env.serves = newNodeServes(cfg, io.Discard)
	places := env.places(cfg, "/state")
	r := repoconfig.Repo{Slug: "o/r", Kind: "github"}
	adapter := func(node, profile string) *kennforge.Adapter {
		a, ok := places(r, node, profile).(*kennforge.Adapter)
		if !ok {
			t.Fatalf("workplace %s/%s = %T, want *kennforge.Adapter", node, profile, places(r, node, profile))
		}
		return a
	}
	a, a2, b, self := adapter(one, "mac"), adapter(one, "software"), adapter(two, "mac"), adapter("", "mac")
	if a.Serve == nil || a.Serve.URL != "https://one.test" || a.Serve.TokenFile != "/tok/one" {
		t.Errorf("Serve on %s = %+v, want https://one.test with /tok/one", one, a.Serve)
	}
	if a2.Serve != a.Serve {
		t.Errorf("two profiles on %s got two serve clients, want one shared", one)
	}
	if b.Serve == nil || b.Serve.URL != "https://two.test" || b.Serve.TokenFile != "/tok/two" {
		t.Errorf("Serve on %s = %+v, want https://two.test with /tok/two", two, b.Serve)
	}
	if self.Serve != nil {
		t.Errorf("Serve on self = %+v, want none", self.Serve)
	}
}

// A tick asks GET info once per node, however many routes it uses there (markers, usage,
// launch, follow, relocate, sessions, …), and the next tick asks again.
func TestTickAsksInfoOncePerNodePerTick(t *testing.T) {
	env, db, hub, _ := nodeTick(t, macIssue(7))
	var errOut bytes.Buffer
	for i := 1; i <= 2; i++ {
		env.Now = func() time.Time { return t0.Add(time.Duration(i) * time.Minute) }
		errOut.Reset()
		if err := tick(context.Background(), env, db, &errOut); err != nil {
			t.Fatalf("tick %d = %v\n%s", i, err, errOut.String())
		}
		for _, id := range hub.ids {
			if got := hub.requests(id + " serve GET " + nodeapi.PathInfo); len(got) != i {
				t.Errorf("after tick %d: GET info on %s = %d, want %d", i, id, len(got), i)
			}
		}
	}
	if n := len(hub.requests(hub.ids[0] + " serve PUT /api/node/v1/workspaces/")); n == 0 {
		t.Errorf("no launch on %s: the tick used no workspace route there", hub.ids[0])
	}
}

// permWorkplace is a measureWorkplace whose permission note comes from reader.
type permWorkplace struct {
	measureWorkplace
	reader workplace.PermissionReader
}

func (w *permWorkplace) Permission(ctx context.Context, sessionID string) ([]byte, error) {
	return w.reader.Permission(ctx, sessionID)
}

// The permission note of a run on a node comes through serve and reads as the same note on
// self; no note is nil; a serve that does not answer makes the run unmeasurable, never a tick
// error. A run on self reads the file, also when its workplace can read notes on a node.
func TestRunProbeReadsPermissionViaServe(t *testing.T) {
	note := []byte(`{"version":1,"at":"2026-10-31T11:00:00Z","tool":"Bash","path":"/work/x","thread":""}`)
	selfDir := t.TempDir()
	writeTestFile(t, dispatch.PermissionPath(selfDir, sidMeasured), string(note))
	want, err := dispatch.ReadPermission(selfDir, sidMeasured)
	if err != nil || want == nil {
		t.Fatalf("ReadPermission on self = %v, %v", want, err)
	}
	hub := newFleetHub(t, randomNode(), randomNode())
	id := hub.ids[0]
	node := hub.nodes[id]
	node.set(func(n *hubNode) { n.permissions[sidMeasured] = note })
	sessions := map[string][]workplace.Session{"ws-1": {{Key: "k1", WorkspaceID: "ws-1", State: "running"}}}
	onNode := func(serve *kennforge.NodeClient) *permWorkplace {
		return &permWorkplace{measureWorkplace: measureWorkplace{sessions: sessions},
			reader: &kennforge.Adapter{Node: id, Serve: serve}}
	}
	run := runs.OpenRun{ID: 1, Issue: 7, SessionID: sidMeasured, WorkspaceID: "ws-1", WorkplaceSession: "k1", Node: id, Profile: "mac"}
	ctx := context.Background()

	t.Run("same note as on self", func(t *testing.T) {
		m := runProbe{w: onNode(kennforge.NewNodeClient(node.serveSrv.URL, node.tokenFile(t))), stateDir: t.TempDir()}
		_, _, s, err := m.measure(ctx, run)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.Permission, want) {
			t.Errorf("Permission = %+v, want %+v, as on self", s.Permission, want)
		}
	})
	t.Run("no note", func(t *testing.T) {
		m := runProbe{w: onNode(kennforge.NewNodeClient(node.serveSrv.URL, node.tokenFile(t))), stateDir: selfDir}
		r := run
		r.SessionID = sidGone // a note on self under this id would be read by mistake
		writeTestFile(t, dispatch.PermissionPath(selfDir, sidGone), string(note))
		_, _, s, err := m.measure(ctx, r)
		if err != nil || s.Permission != nil {
			t.Errorf("measure = %+v, %v; want no permission", s.Permission, err)
		}
	})
	t.Run("serve unreachable", func(t *testing.T) {
		dead := httptest.NewServer(http.NotFoundHandler())
		dead.Close()
		w := onNode(kennforge.NewNodeClient(dead.URL, node.tokenFile(t)))
		env := tickEnv{StateDir: selfDir, Workplace: func(repoconfig.Repo, string, string) workplace.Workplace { return w }}
		var errOut bytes.Buffer
		stands, states, err := measure(ctx, env, hostconfig.Dispatch{}, repoconfig.Repo{Slug: "o/r"}, []runs.OpenRun{run}, &errOut)
		if err != nil {
			t.Fatalf("measure = %v, want no tick error", err)
		}
		if len(stands) != 0 || len(states) != 0 {
			t.Errorf("stands %v, states %v; want the run unmeasured", stands, states)
		}
		if !strings.Contains(errOut.String(), "lighthouse: measure o/r: run 1: ") {
			t.Errorf("errOut = %q, want a measure line for run 1", errOut.String())
		}
	})
	t.Run("self reads the file", func(t *testing.T) {
		w := &permWorkplace{measureWorkplace: measureWorkplace{sessions: sessions}, reader: &kennforge.Adapter{}}
		r := run
		r.Node = ""
		m := runProbe{w: w, stateDir: selfDir}
		_, _, s, err := m.measure(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.Permission, want) {
			t.Errorf("Permission on self = %+v, want %+v from the file", s.Permission, want)
		}
	})
}

// A run on a node gets its post-failed note through serve, an unreadable one as Unreadable and
// no error; a run on self reads the file; a serve that does not answer is an error.
func TestRunProbeReadsPostFailedViaServe(t *testing.T) {
	note := []byte(`{"version":1,"at":"2026-11-20T10:00:00.000Z","issue":7,"command":"ask","error":"boom","body":"hi"}`)
	hub := newFleetHub(t, randomNode(), randomNode())
	id := hub.ids[0]
	node := hub.nodes[id]
	node.set(func(n *hubNode) {
		n.postFailed[sidMeasured] = note
		n.postFailed[sidGone] = []byte("not json")
	})
	sessions := map[string][]workplace.Session{"ws-1": {{Key: "k1", WorkspaceID: "ws-1", State: "running"}}}
	adapter := func(serve *kennforge.NodeClient) *measureWorkplaceAdapter {
		return &measureWorkplaceAdapter{measureWorkplace: measureWorkplace{sessions: sessions}, reader: &kennforge.Adapter{Node: id, Serve: serve}}
	}
	run := runs.OpenRun{ID: 1, Issue: 7, SessionID: sidMeasured, WorkspaceID: "ws-1", WorkplaceSession: "k1", Node: id, Profile: "mac"}
	ctx := context.Background()
	live := func(t *testing.T) *kennforge.NodeClient {
		return kennforge.NewNodeClient(node.serveSrv.URL, node.tokenFile(t))
	}

	t.Run("note via serve", func(t *testing.T) {
		m := runProbe{w: adapter(live(t)), stateDir: t.TempDir()}
		_, _, s, err := m.measure(ctx, run)
		if err != nil {
			t.Fatal(err)
		}
		want := &dispatch.PostFailed{At: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC), Issue: 7, Command: "ask", Error: "boom", Body: "hi"}
		if !reflect.DeepEqual(s.PostFailed, want) {
			t.Errorf("PostFailed = %+v, want %+v", s.PostFailed, want)
		}
	})
	t.Run("unreadable note is no error", func(t *testing.T) {
		m := runProbe{w: adapter(live(t)), stateDir: t.TempDir()}
		r := run
		r.SessionID = sidGone
		_, _, s, err := m.measure(ctx, r)
		want := &dispatch.PostFailed{At: time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC), Unreadable: true}
		if err != nil || !reflect.DeepEqual(s.PostFailed, want) {
			t.Errorf("measure = %+v, %v; want %+v (dated by the node's mtime) and no error", s.PostFailed, err, want)
		}
	})
	t.Run("no note", func(t *testing.T) {
		selfDir := t.TempDir()
		writeTestFile(t, dispatch.PostFailedPath(selfDir, sidNoFile), string(note)) // self must not be read for a node run
		m := runProbe{w: adapter(live(t)), stateDir: selfDir}
		r := run
		r.SessionID = sidNoFile
		_, _, s, err := m.measure(ctx, r)
		if err != nil || s.PostFailed != nil {
			t.Errorf("measure = %+v, %v; want no note", s.PostFailed, err)
		}
	})
	t.Run("serve unreachable", func(t *testing.T) {
		dead := httptest.NewServer(http.NotFoundHandler())
		dead.Close()
		m := runProbe{w: adapter(kennforge.NewNodeClient(dead.URL, node.tokenFile(t))), stateDir: t.TempDir()}
		if _, _, _, err := m.measure(ctx, run); err == nil {
			t.Error("measure = nil error, want an error for a serve that does not answer")
		}
	})
	t.Run("self reads the file", func(t *testing.T) {
		selfDir := t.TempDir()
		writeTestFile(t, dispatch.PostFailedPath(selfDir, sidMeasured), string(note))
		r := run
		r.Node = ""
		m := runProbe{w: adapter(live(t)), stateDir: selfDir}
		_, _, s, err := m.measure(ctx, r)
		if err != nil || s.PostFailed == nil || s.PostFailed.Command != "ask" {
			t.Errorf("measure = %+v, %v; want the note of the file", s.PostFailed, err)
		}
	})
	t.Run("self unreadable file", func(t *testing.T) {
		selfDir := t.TempDir()
		writeTestFile(t, dispatch.PostFailedPath(selfDir, sidMeasured), "garbage")
		r := run
		r.Node = ""
		m := runProbe{w: adapter(live(t)), stateDir: selfDir}
		_, _, s, err := m.measure(ctx, r)
		if err != nil || s.PostFailed == nil || !s.PostFailed.Unreadable {
			t.Errorf("measure = %+v, %v; want Unreadable and no error", s.PostFailed, err)
		}
	})
}

// measureWorkplaceAdapter is a measureWorkplace that reads notes like a kenn-forge adapter.
type measureWorkplaceAdapter struct {
	measureWorkplace
	reader workplace.PostFailedReader
}

func (w *measureWorkplaceAdapter) PostFailed(ctx context.Context, sessionID string) ([]byte, time.Time, error) {
	return w.reader.PostFailed(ctx, sessionID)
}

// hubPostFailedModTime is the modification time the hub's serve sends with every post-failed note.
const hubPostFailedModTime = "2026-11-20T09:00:00Z"

// usageBodies is the PUT usage bodies node took, with the pointers read out ("<nil>" for none).
func usageBodies(n *hubNode) []string {
	return got(n, func(n *hubNode) []string {
		var out []string
		for _, u := range n.usage {
			text := func(p *string) string {
				if p == nil {
					return "<nil>"
				}
				return *p
			}
			out = append(out, "pause="+text(u.Pause)+" alarm="+text(u.Alarm))
		}
		return out
	})
}

// usageFiles writes the tick's PAUSE and alarm mirror and returns the path of PAUSE.
func usageFiles(t *testing.T, env *tickEnv, pause, alarm string) string {
	t.Helper()
	path, err := park.PauseFile(env.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, pause)
	env.Meter.Paths.Mirror = filepath.Join(t.TempDir(), "usage-window.alarm")
	writeTestFile(t, env.Meter.Paths.Mirror, alarm)
	return path
}

// Every tick sends its PAUSE and alarm mirror to every node, unchanged, after it read the
// node's markers.
func TestTickPushesPauseAndAlarmToEveryNode(t *testing.T) {
	env, db, hub, _ := nodeTick(t)
	usageFiles(t, &env, "pause-bytes\n", `{"alarm":"bytes"}`)
	for i := 1; i <= 2; i++ {
		var errOut bytes.Buffer
		if err := tick(context.Background(), env, db, &errOut); err != nil {
			t.Fatalf("tick %d = %v\n%s", i, err, errOut.String())
		}
	}
	for _, id := range hub.ids {
		want := []string{`pause=pause-bytes` + "\n" + ` alarm={"alarm":"bytes"}`, `pause=pause-bytes` + "\n" + ` alarm={"alarm":"bytes"}`}
		if got := usageBodies(hub.nodes[id]); !reflect.DeepEqual(got, want) {
			t.Errorf("usage on %s = %q, want %q", id, got, want)
		}
		reqs := hub.requests(id + " serve ")
		markers := slices.Index(reqs, id+" serve GET "+nodeapi.PathMarkers)
		put := slices.Index(reqs, id+" serve PUT "+nodeapi.PathUsage)
		if markers < 0 || put < markers {
			t.Errorf("requests on %s = %q, want GET markers before PUT usage", id, reqs)
		}
	}
}

// A PAUSE that is gone on the tick is sent as none, so serve removes it on the node; an alarm
// mirror that is gone likewise.
func TestTickPushesNilWhenPauseCleared(t *testing.T) {
	env, db, hub, _ := nodeTick(t)
	pause := usageFiles(t, &env, "pause-bytes", "alarm-bytes")
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick 1 = %v\n%s", err, errOut.String())
	}
	if err := os.Remove(pause); err != nil {
		t.Fatal(err)
	}
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick 2 = %v\n%s", err, errOut.String())
	}
	if err := os.Remove(env.Meter.Paths.Mirror); err != nil {
		t.Fatal(err)
	}
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick 3 = %v\n%s", err, errOut.String())
	}
	want := []string{"pause=pause-bytes alarm=alarm-bytes", "pause=<nil> alarm=alarm-bytes", "pause=<nil> alarm=<nil>"}
	for _, id := range hub.ids {
		if got := usageBodies(hub.nodes[id]); !reflect.DeepEqual(got, want) {
			t.Errorf("usage on %s = %q, want %q", id, got, want)
		}
	}
}

// A node that refuses PUT usage keeps what it had: the tick logs it, sends nothing else to clear
// it, still pushes to the other node, and is no tick error.
func TestUsagePushFailureKeepsLastState(t *testing.T) {
	env, db, hub, _ := nodeTick(t)
	usageFiles(t, &env, "pause-bytes", "alarm-bytes")
	down, up := hub.nodes[hub.ids[0]], hub.nodes[hub.ids[1]]
	down.set(func(n *hubNode) { n.usageStatus = http.StatusServiceUnavailable })
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v, want no tick error\n%s", err, errOut.String())
	}
	if !strings.Contains(errOut.String(), "lighthouse: usage to node a: ") {
		t.Errorf("errOut = %q, want a usage line for node a", errOut.String())
	}
	if got := hub.requests(hub.ids[0] + " serve PUT " + nodeapi.PathUsage); len(got) != 1 {
		t.Errorf("PUT usage on node a = %q, want one, not retried", got)
	}
	if got := hub.requests(hub.ids[0] + " serve DELETE "); len(got) != 0 {
		t.Errorf("DELETE on node a = %q, want none", got)
	}
	if got := usageBodies(up); !reflect.DeepEqual(got, []string{"pause=pause-bytes alarm=alarm-bytes"}) {
		t.Errorf("usage on node b = %q, want the push", got)
	}
}

// A node whose markers do not read takes no start this tick: its occupancy is unknown.
func TestTickNodeMarkersUnreadableNoStart(t *testing.T) {
	env, db, hub, _ := nodeTick(t, macIssue(7))
	a := hub.nodes[hub.ids[0]]
	a.set(func(n *hubNode) { n.markersStatus = http.StatusBadGateway })
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	if !strings.Contains(errOut.String(), "lighthouse: deferred o/r#7") || !strings.Contains(errOut.String(), "markers not readable") {
		t.Errorf("errOut = %q, want #7 deferred for markers not readable", errOut.String())
	}
	if n := a.postCount(); n != 0 {
		t.Errorf("handoffs on node a = %d, want none", n)
	}
	if rows := history(t, openStore(t, db), "o/r"); len(rows) != 0 {
		t.Errorf("runs = %+v, want none", rows)
	}
}

// A controller marker on a node whose issue left the board is dropped there in one tick: the
// DELETE goes to that node, with the marker's key.
func TestNodeMarkerOfClosedIssueGoneAfterOneTick(t *testing.T) {
	env, db, hub, _ := nodeTick(t)
	id := hub.ids[0]
	const sess = "sess-closed"
	parkOn(t, openStore(t, db), "o/r", 7, sess, id)
	m := controllerMarker(sess, t0.Add(-time.Hour))
	a := hub.nodes[id]
	a.set(func(n *hubNode) { n.markers = []park.Marker{m} })
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	if left := got(a, func(n *hubNode) []park.Marker { return n.markers }); len(left) != 0 {
		t.Errorf("markers on %s after one tick = %+v, want none", id, left)
	}
	if del := got(a, func(n *hubNode) []string { return n.deleted }); !reflect.DeepEqual(del, []string{sess + " " + m.Key()}) {
		t.Errorf("DELETE markers on %s = %q, want the one of %s", id, del, sess)
	}
}

// drop_marker removes each marker where it came from: one of the second node there, not on the
// first and not in the local marker directory; a local one locally.
func TestDropMarkerDeletesOnOriginatingNode(t *testing.T) {
	env, db, hub, _ := nodeTick(t)
	conn := openStore(t, db)
	first, second := hub.ids[0], hub.ids[1]
	const onNode, local = "sess-on-node", "sess-local"
	parkOn(t, conn, "o/r", 7, onNode, second)
	claim(t, conn, "o/r", 8, local, "software", "handoff", "start:8", runs.Parked)
	m := controllerMarker(onNode, t0.Add(-time.Hour))
	hub.nodes[second].set(func(n *hubNode) { n.markers = []park.Marker{m} })
	writeMarker(t, env, local, true)
	dir, err := park.MarkerDir(env.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	if del := got(hub.nodes[second], func(n *hubNode) []string { return n.deleted }); !reflect.DeepEqual(del, []string{onNode + " " + m.Key()}) {
		t.Errorf("DELETE markers on the second node = %q, want the one of %s", del, onNode)
	}
	if del := got(hub.nodes[first], func(n *hubNode) []string { return n.deleted }); len(del) != 0 {
		t.Errorf("DELETE markers on the first node = %q, want none", del)
	}
	if _, err := os.Stat(filepath.Join(dir, local+".json")); !os.IsNotExist(err) {
		t.Errorf("local marker of %s: stat = %v, want it removed", local, err)
	}
}

// A run parked on a node resumes there once its window reset, and its marker on that node is
// gone after the resume.
func TestResumeOnNodeRemovesMarkerThere(t *testing.T) {
	env, db, hub, _ := nodeTick(t, macIssue(7))
	id := hub.ids[0]
	const sess = "33333333-3333-4333-8333-333333333333"
	conn := openStore(t, db)
	parkOn(t, conn, "o/r", 7, sess, id)
	m := controllerMarker(sess, t0.Add(-time.Hour))
	a := hub.nodes[id]
	a.set(func(n *hubNode) { n.markers = []park.Marker{m} })
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	rows := history(t, conn, "o/r")
	if len(rows) != 2 || !rows[1].Open || rows[1].Node != id || rows[1].Trigger != "usage" || rows[1].SessionID != sess {
		t.Fatalf("runs = %+v, want the parked run resumed on %s by usage\n%s", rows, id, errOut.String())
	}
	if got := a.carrier("ws1"); got != sess {
		t.Errorf("carrier on %s = %q, want the resumed session %s", id, got, sess)
	}
	if left := got(a, func(n *hubNode) []park.Marker { return n.markers }); len(left) != 0 {
		t.Errorf("markers on %s after the resume = %+v, want none", id, left)
	}
	if del := got(a, func(n *hubNode) []string { return n.deleted }); !reflect.DeepEqual(del, []string{sess + " " + m.Key()}) {
		t.Errorf("DELETE markers on %s = %q, want the one of the resume", id, del)
	}
}

// A 401 or 403 of serve is a configuration error: the tick names the node once per tick,
// whatever route met it.
func TestNodeAuthErrorLoggedOncePerTick(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			env, db, hub, _ := nodeTick(t, macIssue(7))
			hub.nodes[hub.ids[0]].set(func(n *hubNode) { n.authStatus = status })
			for i := 1; i <= 2; i++ {
				var errOut bytes.Buffer
				if err := tick(context.Background(), env, db, &errOut); err != nil && err != errSilent {
					t.Fatalf("tick %d = %v\n%s", i, err, errOut.String())
				}
				want := fmt.Sprintf("lighthouse: node a (%s): serve answered %d: configuration error", hub.ids[0], status)
				if n := strings.Count(errOut.String(), want); n != 1 {
					t.Errorf("tick %d: %q lines = %d, want 1\n%s", i, want, n, errOut.String())
				}
				if strings.Contains(errOut.String(), "lighthouse: node b ") {
					t.Errorf("tick %d: errOut = %q, want no line for node b", i, errOut.String())
				}
			}
		})
	}
}

// The sweep removes what closed node runs left on the tick: the tick-side carrier of #187
// (carriers/<node>/<id>.json) and the transcript mirror. Those of an open run stay, and so does a
// carrier on self.
func TestSweepRemovesStaleTickSideCarriersAndMirrors(t *testing.T) {
	env, db := tickFixture(t)
	conn := openStore(t, db)
	ctx := context.Background()
	one, two := randomNode(), randomNode()
	const sOpen, sClosed = "44444444-4444-4444-8444-444444444444", "55555555-5555-4555-8555-555555555555"
	open, err := runs.Take(ctx, conn, runs.Claim{Repo: "o/r", Issue: 1, SessionID: sOpen, Mode: "start", Trigger: "handoff",
		TriggerKey: "start:1", Command: "c", Prompt: "p", Profile: "mac", Node: one, Branch: "b", Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.SetWorkplace(ctx, conn, open.ID, "ws-open", "b"); err != nil {
		t.Fatal(err)
	}
	parkOn(t, conn, "o/r", 2, sClosed, one)
	dir := env.StateDir
	keep := []string{
		workplace.CarrierPath(dir, one, "ws-open"),
		workplace.CarrierPath(dir, "", "ws-self"),
		filepath.Join(dir, "mirror", one, sOpen, sOpen+".jsonl"),
	}
	gone := []string{
		workplace.CarrierPath(dir, one, "ws-closed"),
		workplace.CarrierPath(dir, two, "ws-open"), // the open run is on the first node
		filepath.Join(dir, "mirror", one, sClosed, sClosed+".jsonl"),
		filepath.Join(dir, "mirror", two, sOpen, sOpen+".jsonl"),
	}
	for _, p := range append(slices.Clone(keep), gone...) {
		writeTestFile(t, p, "{}")
	}
	if err := sweepNodeLeftovers(ctx, conn, dir); err != nil {
		t.Fatal(err)
	}
	for _, p := range keep {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s: %v, want it kept", p, err)
		}
	}
	for _, p := range gone {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s: stat = %v, want it removed", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "carriers", two)); !os.IsNotExist(err) {
		t.Errorf("carriers/<second node>: stat = %v, want the empty dir removed", err)
	}

	// The tick runs the sweep: a leftover of a closed run is gone after one tick.
	tenv, tdb, hub, _ := nodeTick(t)
	parkOn(t, openStore(t, tdb), "o/r", 2, sClosed, hub.ids[0])
	leftovers := []string{
		workplace.CarrierPath(tenv.StateDir, hub.ids[0], "ws-closed"),
		filepath.Join(tenv.StateDir, "mirror", hub.ids[0], sClosed, sClosed+".jsonl"),
	}
	for _, p := range leftovers {
		writeTestFile(t, p, "{}")
	}
	var errOut bytes.Buffer
	if err := tick(ctx, tenv, tdb, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	for _, p := range leftovers {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("after a tick %s: stat = %v, want it removed", p, err)
		}
	}
}

// A run on a node that waits, with a forge time older than max_wait, while serve fails its
// transcript (5xx, unreachable) is not measured this tick: never stopped as silent, and no tick
// error. With no transcript at all (404 no_transcript) the forge time still decides.
func TestNodeTranscriptFailureIsNotSilent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		stopped bool
	}{
		{"5xx", http.StatusBadGateway, false},
		{"401", http.StatusUnauthorized, false},
		{"no transcript", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, db, hub, _ := nodeTick(t, macIssue(7))
			host, err := os.ReadFile(env.HostToml)
			if err != nil {
				t.Fatal(err)
			}
			waits := strings.Replace(string(host), "max_runtime = \"1h\"\n", "max_runtime = \"1h\"\nmax_wait = \"30m\"\n", 1)
			if err := os.WriteFile(env.HostToml, []byte(waits), 0o600); err != nil {
				t.Fatal(err)
			}
			id := hub.ids[0]
			a := hub.nodes[id]
			var errOut bytes.Buffer
			if err := tick(context.Background(), env, db, &errOut); err != nil {
				t.Fatalf("tick 1 = %v\n%s", err, errOut.String())
			}
			conn := openStore(t, db)
			rows := history(t, conn, "o/r")
			if len(rows) != 1 || !rows[0].Open || rows[0].Node != id {
				t.Fatalf("runs after tick 1 = %+v, want one open on %s", rows, id)
			}
			a.set(func(n *hubNode) {
				n.sessionState, n.sessionUpdated, n.transcriptStatus = "waiting", t0.Format(time.RFC3339), tc.status
			})
			env.Now = func() time.Time { return t0.Add(45 * time.Minute) } // past max_wait (30m), within max_runtime (1h)
			errOut.Reset()
			if err := tick(context.Background(), env, db, &errOut); err != nil {
				t.Fatalf("tick 2 = %v, want no tick error\n%s", err, errOut.String())
			}
			rows = history(t, conn, "o/r")
			if got := !rows[0].Open; got != tc.stopped {
				t.Errorf("run closed = %t (%+v), want %t\n%s", got, rows[0], tc.stopped, errOut.String())
			}
			if a.wasStopped(rows[0].SessionID) != tc.stopped {
				t.Errorf("session stopped on %s = %t, want %t", id, a.wasStopped(rows[0].SessionID), tc.stopped)
			}
			if !tc.stopped && !strings.Contains(errOut.String(), "lighthouse: measure o/r: run ") {
				t.Errorf("errOut = %q, want a measure line: the run is not measurable", errOut.String())
			}
		})
	}
}

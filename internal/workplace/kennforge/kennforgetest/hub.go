// Package kennforgetest holds the HTTP fakes of kenn-forge for tests: a fleet hub and a stateful
// forge. It never imports kennforge, so a package that kennforge itself imports (nodeserve) can
// run the kenn-forge adapter against them; each test package builds its own adapter.
package kennforgetest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Node is one node behind the fake hub: a fake forge that says whether the hub reaches it now
// (Routable) and what the next snapshot reports (Reachable).
type Node interface {
	http.Handler
	Routable() bool
	Reachable() bool
}

// HubID is the node id of the fake hub itself.
const HubID = "hub-x"

// NewHub is a kenn-forge hub as v0.7.0 serves it (see fleet.go in kennforge):
// /api/v1/fleet/hosts/{id}/... goes to nodes[id] with the path back under /api/v1, and
// GET /api/v1/snapshot?include_peers=true lists the hub and every node with its reachability.
// A node the hub does not reach, or does not know, gets the hub's own 404 "fleet host not
// found", as resolveFleetHostTarget answers it. Without include_peers the snapshot lists only
// the hub, as buildFleetSnapshot does. Any other path is an error of the adapter.
func NewHub(t testing.TB, nodes map[string]Node) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.EscapedPath()
		if path == "/api/v1/snapshot" && r.Method == http.MethodGet {
			serveSnapshot(w, nodes, r.URL.Query().Get("include_peers") == "true")
			return
		}
		rest, ok := strings.CutPrefix(path, "/api/v1/fleet/hosts/")
		if !ok {
			t.Errorf("fake hub: unexpected request %s %s", r.Method, path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		rawID, tail, _ := strings.Cut(rest, "/")
		id, err := url.PathUnescape(rawID)
		if err != nil {
			t.Errorf("fake hub: host key %q: %v", rawID, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		node, known := nodes[id]
		if !known || !node.Routable() {
			// The body of fleetHostNotFoundProblem through writeProblemResponse (fleet_proxy.go).
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"title":"Not Found","status":404,"detail":"fleet host not found","code":"notFound","details":{"hostKey":%q}}`, id)
			return
		}
		u, err := url.Parse("/api/v1/" + tail)
		if err != nil {
			t.Errorf("fake hub: path %q: %v", tail, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		u.RawQuery = r.URL.RawQuery
		r2 := r.Clone(r.Context())
		r2.URL = u
		node.ServeHTTP(w, r2)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func serveSnapshot(w http.ResponseWriter, nodes map[string]Node, peers bool) {
	hosts := []string{snapshotHost(HubID, "hub", true)}
	if peers {
		for id, n := range nodes {
			hosts = append(hosts, snapshotHost(id, "spoke", n.Reachable()))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"protocolVersion":1,"generation":1,"hosts":[%s],"projects":[],"worktrees":[],"sessions":[],"workspaces":[]}`,
		strings.Join(hosts, ","))
}

// snapshotHost is one HostSummary of forge v0.7.0, with only the fields lighthouse reads
// and the ones beside it that identify the host.
func snapshotHost(id, role string, reachable bool) string {
	return fmt.Sprintf(`{"id":"host-%s","configKey":%q,"nodeID":%q,"name":%q,"kind":"remote","federationRole":%q,"platform":"linux","preferredTransport":"direct","reachable":%t}`,
		id, id, id, id, role, reachable)
}

package kennforge

import (
	"net/http/httptest"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/workplace/kennforge/kennforgetest"
)

// Routable: the hub routes to this node unless it is offline, or the tunnel is just coming back.
func (f *fakeForge) Routable() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.offline && !f.back
}

// Reachable is what the next snapshot reports for this node.
func (f *fakeForge) Reachable() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.offline
}

// newFakeHub is the fake hub of kennforgetest with the scripted fake forges of this package.
func newFakeHub(t *testing.T, nodes map[string]*fakeForge) *httptest.Server {
	t.Helper()
	m := make(map[string]kennforgetest.Node, len(nodes))
	for id, n := range nodes {
		m[id] = n
	}
	return kennforgetest.NewHub(t, m)
}

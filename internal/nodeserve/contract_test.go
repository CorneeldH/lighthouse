package nodeserve_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/nodeserve"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
	"github.com/CorneeldH/lighthouse/internal/workplace/kennforge"
	"github.com/CorneeldH/lighthouse/internal/workplace/kennforge/kennforgetest"
	"github.com/CorneeldH/lighthouse/internal/workplace/workplacetest"
)

// TestContractOnNode runs the suite against the kenn-forge adapter on another node: hub routes
// through the fake hub, everything else through a real serve on that node, above its own forge
// and a git repository in a temp dir. Nothing runs on the tick host.
func TestContractOnNode(t *testing.T) { workplacetest.Run(t, newNodeFixture) }

// nodeFixture is one node behind the hub, with serve on it, and the tick's adapter for it.
type nodeFixture struct {
	*kennforgetest.Forge
	a         *kennforge.Adapter
	nodeState string // the state dir of serve on the node
}

func randomID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func tokenFile(t *testing.T, token string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func newNodeFixture(t *testing.T) workplacetest.Fixture {
	origin := kennforgetest.NewOrigin(t)
	forge := kennforgetest.NewForge(t, origin)
	nodeID, otherID := "n-"+randomID(t), "n-"+randomID(t)
	hub := kennforgetest.NewHub(t, map[string]kennforgetest.Node{nodeID: forge, otherID: kennforgetest.NewForge(t, origin)})
	nodeForge := httptest.NewServer(forge) // the node's own forge, which serve asks
	t.Cleanup(nodeForge.Close)
	f := &nodeFixture{Forge: forge, nodeState: t.TempDir()}
	forge.CarrierPath = func(id string) string { return workplace.CarrierPath(f.nodeState, "", id) }
	forgeToken, nodeToken := randomID(t), randomID(t)
	serve := httptest.NewServer(nodeserve.New(nodeserve.Deps{
		StateDir: f.nodeState, TokenFile: tokenFile(t, nodeToken),
		Forge:    hostconfig.DispatchForge{URL: nodeForge.URL, TokenFile: tokenFile(t, forgeToken), UnitPrefix: "paia-forge-"},
		Profiles: map[string]bool{"infra": true}, Home: t.TempDir(),
		Run: forge.Run, Sleep: func(time.Duration) {},
	}))
	t.Cleanup(serve.Close)
	client := kennforge.New(hub.URL, tokenFile(t, forgeToken))
	// The handoff of SpawnTimesOut hangs until the client gives up; the default is 30s.
	client.HTTP = &http.Client{Timeout: 3 * time.Second}
	f.a = &kennforge.Adapter{
		Client: client, Kind: "github", Repo: "CorneeldH/lighthouse", Node: nodeID,
		Serve:    kennforge.NewNodeClient(serve.URL, tokenFile(t, nodeToken)),
		StateDir: t.TempDir(), Targets: map[string]string{"infra": "paia-infra"}, Profile: "infra",
		Run: func(_ context.Context, _ time.Duration, name string, args ...string) (string, int, error) {
			t.Errorf("Runner called on the tick host for a node: %s %v", name, args)
			return "", -1, errors.New("no runner on the tick host")
		},
		Sleep: func(time.Duration) {},
	}
	t.Cleanup(func() {
		if entries, err := os.ReadDir(filepath.Join(f.a.StateDir, "carriers")); err == nil && len(entries) > 0 {
			t.Errorf("tick state dir has carriers %v, want none: a node's carrier lives on the node", entries)
		}
	})
	return f
}

func (f *nodeFixture) Workplace() workplace.Workplace { return f.a }

func (f *nodeFixture) Workspace(t *testing.T, branch string) workplace.Workspace {
	t.Helper()
	ws, err := f.a.Prepare(t.Context(), branch)
	if err != nil {
		t.Fatalf("Prepare %s: %v", branch, err)
	}
	return ws
}

// Spec has run instructions in a real file: Spawn sends their content to the node.
func (f *nodeFixture) Spec(t *testing.T) workplace.LaunchSpec {
	p := filepath.Join(t.TempDir(), "instructions.md")
	if err := os.WriteFile(p, []byte("# run instructions\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return workplace.LaunchSpec{Profile: "infra", Mode: "start", Prompt: "do the thing", InstructionsFile: p}
}

func (f *nodeFixture) MakeDirty(t *testing.T, ws workplace.Workspace) { f.Forge.MakeDirty(t, ws.ID) }
func (f *nodeFixture) Forget(t *testing.T, ws workplace.Workspace)    { f.Forge.Forget(t, ws.ID) }
func (f *nodeFixture) Live(_ *testing.T, sid string)                  { f.Forge.Live(sid) }
func (f *nodeFixture) Survivor(_ *testing.T, sid string)              { f.Forge.Survivor(sid) }
func (f *nodeFixture) Unmeasurable(*testing.T)                        { f.Forge.Unmeasurable() }
func (f *nodeFixture) SpawnTimesOut(*testing.T)                       { f.Forge.SpawnTimesOut() }

// AC 1: Spawn on a node writes the carrier in the node's state dir, with the run's session id
// and the node's own path of the run instructions, holding the content the tick sent.
func TestSpawnOnNodeCarrierHasNodePaths(t *testing.T) {
	f := newNodeFixture(t).(*nodeFixture)
	ws := f.Workspace(t, "issue-20-x")
	l := f.Spec(t)
	l.SessionID = runs.NewSessionID()
	if _, err := f.a.Spawn(t.Context(), ws, l); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	raw, err := os.ReadFile(workplace.CarrierPath(f.nodeState, "", ws.ID))
	if err != nil {
		t.Fatalf("carrier on the node: %v", err)
	}
	var c workplace.Carrier
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	want := dispatch.InstructionsPath(f.nodeState, l.SessionID)
	if c.SessionID != l.SessionID || !slices.Contains(c.Args, want) || slices.Contains(c.Args, l.InstructionsFile) {
		t.Errorf("carrier = %+v, want session %s and instructions %s, not the tick's %s", c, l.SessionID, want, l.InstructionsFile)
	}
	got, err := os.ReadFile(want)
	if err != nil || string(got) != "# run instructions\n" {
		t.Errorf("instructions on the node = %q, %v; want the content the tick sent", got, err)
	}
}

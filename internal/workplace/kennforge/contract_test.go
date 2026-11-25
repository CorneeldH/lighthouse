package kennforge

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/testpath"
	"github.com/CorneeldH/lighthouse/internal/workplace"
	"github.com/CorneeldH/lighthouse/internal/workplace/kennforge/kennforgetest"
	"github.com/CorneeldH/lighthouse/internal/workplace/workplacetest"
)

// TestContract runs the suite against the adapter on self. The run on a node, through a real
// serve, is TestContractOnNode in internal/nodeserve: nodeserve imports kennforge.
func TestContract(t *testing.T) { workplacetest.Run(t, newForgeFixture) }

// forgeFixture is the stateful fake forge of kennforgetest with the adapter on self in front.
type forgeFixture struct {
	*kennforgetest.Forge
	a *Adapter
}

func newForgeFixture(t *testing.T) workplacetest.Fixture {
	forge := kennforgetest.NewForge(t, kennforgetest.NewOrigin(t))
	srv := httptest.NewServer(forge)
	t.Cleanup(srv.Close)
	f := &forgeFixture{Forge: forge, a: adapter(t, srv.URL, forge)}
	forge.CarrierPath = func(id string) string { return workplace.CarrierPath(f.a.StateDir, "", id) }
	return f
}

// adapter is the adapter on self at url, with forge as its host.
func adapter(t *testing.T, url string, forge *kennforgetest.Forge) *Adapter {
	tok := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tok, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Adapter{
		Client: New(url, tok), Kind: "github", Repo: "CorneeldH/lighthouse",
		StateDir: t.TempDir(), Targets: map[string]string{"infra": "paia-infra"}, Profile: "infra",
		UnitPrefix: "paia-forge-",
		Run:        forge.Run, Sleep: func(time.Duration) {}, spawnTimeout: 100 * time.Millisecond,
	}
}

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
	return workplace.LaunchSpec{Profile: "infra", Mode: "start", Prompt: "do the thing", InstructionsFile: testpath.Abs("abs", "instr.md")}
}

func (f *forgeFixture) MakeDirty(t *testing.T, ws workplace.Workspace) { f.Forge.MakeDirty(t, ws.ID) }
func (f *forgeFixture) Forget(t *testing.T, ws workplace.Workspace)    { f.Forge.Forget(t, ws.ID) }
func (f *forgeFixture) Live(_ *testing.T, sid string)                  { f.Forge.Live(sid) }
func (f *forgeFixture) Survivor(_ *testing.T, sid string)              { f.Forge.Survivor(sid) }
func (f *forgeFixture) Unmeasurable(*testing.T)                        { f.Forge.Unmeasurable() }
func (f *forgeFixture) SpawnTimesOut(*testing.T)                       { f.Forge.SpawnTimesOut() }

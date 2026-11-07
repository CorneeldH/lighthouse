//go:build linux

package local

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/workplace"
	"github.com/CorneeldH/lighthouse/internal/workplace/workplacetest"
)

func TestContract(t *testing.T) {
	workplacetest.Run(t, newLocalFixture)
}

// localFixture is a real repo with a bare origin, git that runs for real, and fake processes.
type localFixture struct {
	a    *Adapter
	host *fakeHost
}

func newLocalFixture(t *testing.T) workplacetest.Fixture {
	repo, _ := newRepo(t)
	h := newFakeHost(t)
	a, _ := h.adapter(t, repo)
	return &localFixture{a: a, host: h}
}

func (f *localFixture) Workplace() workplace.Workplace { return f.a }

func (f *localFixture) Workspace(t *testing.T, branch string) workplace.Workspace {
	t.Helper()
	ws, err := f.a.Prepare(t.Context(), branch)
	if err != nil {
		t.Fatalf("Prepare %s: %v", branch, err)
	}
	return ws
}

func (f *localFixture) Spec(*testing.T) workplace.LaunchSpec {
	return workplace.LaunchSpec{Profile: "infra", Mode: "start", Prompt: "do it", InstructionsFile: "/abs/instr.md"}
}

func (f *localFixture) MakeDirty(t *testing.T, ws workplace.Workspace) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(ws.Path, "untracked.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *localFixture) Forget(t *testing.T, ws workplace.Workspace) {
	t.Helper()
	if err := os.RemoveAll(ws.Path); err != nil {
		t.Fatal(err)
	}
}

func (f *localFixture) Live(_ *testing.T, sid string) {
	pid := f.host.pidOf(sid)
	p := f.host.procs
	p.mu.Lock()
	defer p.mu.Unlock()
	if !slices.Contains(p.marked[sid], pid) {
		p.marked[sid] = append(p.marked[sid], pid)
	}
	p.start[pid] = 5000
}

func (f *localFixture) Survivor(_ *testing.T, sid string) {
	p := f.host.procs
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, pid := range p.marked[sid] {
		p.survivors[pid] = true
	}
}

func (f *localFixture) Unmeasurable(*testing.T) {
	p := f.host.procs
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = errors.New("fake procs: cannot read /proc")
}

func (f *localFixture) SpawnTimesOut(*testing.T) {
	f.host.mu.Lock()
	defer f.host.mu.Unlock()
	f.host.timeout = true
}

func (f *localFixture) Launches() int {
	f.host.mu.Lock()
	defer f.host.mu.Unlock()
	return f.host.launches
}

func (f *localFixture) ProcCalls() int {
	p := f.host.procs
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (f *localFixture) Signals() int {
	p := f.host.procs
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sent)
}

// The fake must end a signalled pid, or the Kill cases of the suite prove nothing.
func TestFakeProcsSignal(t *testing.T) {
	p := newFakeProcs()
	p.marked["s"] = []int{1, 2, 3}
	p.start[1], p.start[2], p.start[3] = 5000, 5000, 5000
	p.survivors[2] = true
	p.termProof[3] = true
	for _, s := range []sentSignal{{1, syscall.SIGTERM}, {2, syscall.SIGKILL}, {3, syscall.SIGTERM}} {
		if err := p.Signal(s.pid, s.sig); err != nil {
			t.Fatal(err)
		}
	}
	got, err := p.Marked("s")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("Marked after signals = %v, want [2 3]", got)
	}
	if err := p.Signal(3, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := p.StartTime(3); ok {
		t.Error("StartTime(3) after SIGKILL: ok, want gone")
	}
	if p.calls != 6 || len(p.sent) != 4 {
		t.Errorf("calls = %d, sent = %d; want 6 and 4", p.calls, len(p.sent))
	}
}

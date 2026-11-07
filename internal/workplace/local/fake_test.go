//go:build linux

package local

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

type sentSignal struct {
	pid int
	sig syscall.Signal
}

// fakeProcs is the process table of a test host. A signal ends a pid, unless the pid is a
// survivor, or termProof and the signal is SIGTERM.
type fakeProcs struct {
	mu        sync.Mutex
	marked    map[string][]int
	start     map[int]uint64
	survivors map[int]bool
	termProof map[int]bool // SIGTERM does nothing
	err       error        // Marked and StartTime fail with it
	calls     int          // every call of Marked, StartTime and Signal
	sent      []sentSignal
}

func newFakeProcs() *fakeProcs {
	return &fakeProcs{
		marked: map[string][]int{}, start: map[int]uint64{},
		survivors: map[int]bool{}, termProof: map[int]bool{},
	}
}

func (p *fakeProcs) Marked(sid string) ([]int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	if sid == "" {
		return nil, errors.New("fake procs: empty session id")
	}
	return append([]int{}, p.marked[sid]...), nil
}

func (p *fakeProcs) StartTime(pid int) (uint64, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.err != nil {
		return 0, false, p.err
	}
	start, ok := p.start[pid]
	return start, ok, nil
}

func (p *fakeProcs) Signal(pid int, sig syscall.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.sent = append(p.sent, sentSignal{pid: pid, sig: sig})
	if p.survivors[pid] || p.termProof[pid] && sig == syscall.SIGTERM {
		return nil
	}
	for sid, pids := range p.marked {
		kept := []int{}
		for _, q := range pids {
			if q != pid {
				kept = append(kept, q)
			}
		}
		p.marked[sid] = kept
	}
	delete(p.start, pid)
	return nil
}

// fakeHost is the host the adapter runs on in tests: git runs for real, processes live in
// procs, and every other command fails the test.
type fakeHost struct {
	t     *testing.T
	procs *fakeProcs

	mu       sync.Mutex
	launches int
	timeout  bool           // the next launch has an unknown outcome
	pids     map[string]int // the pid of each session id
}

func newFakeHost(t *testing.T) *fakeHost {
	return &fakeHost{t: t, procs: newFakeProcs(), pids: map[string]int{}}
}

// adapter is an Adapter for repoDir on this host; the int counts the calls of Sleep.
func (h *fakeHost) adapter(t *testing.T, repoDir string) (*Adapter, *int) {
	slept := new(int)
	return &Adapter{
		RepoDir: repoDir, DefaultBranch: "main", Repo: "o/r",
		StateDir: t.TempDir(), Home: t.TempDir(),
		Run: h.fixtureRunner, Procs: h.procs,
		Sleep: func(time.Duration) { *slept++ },
	}, slept
}

// fixtureRunner runs git for real; any other command fails the test.
func (h *fakeHost) fixtureRunner(ctx context.Context, timeout time.Duration, name string, args ...string) (string, int, error) {
	if name == "git" {
		return workplace.ExecRunner()(ctx, timeout, name, args...)
	}
	cmd := strings.Join(append([]string{name}, args...), " ")
	h.t.Errorf("unexpected command %q", cmd)
	return "", -1, fmt.Errorf("unexpected command %q", cmd)
}

// pidOf is the pid of sid: the one its launch got, or a new one.
func (h *fakeHost) pidOf(sid string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	pid, ok := h.pids[sid]
	if !ok {
		pid = 20000 + len(h.pids)
		h.pids[sid] = pid
	}
	return pid
}

// newAdapter is an Adapter for repoDir on a fresh fake host.
func newAdapter(t *testing.T, repoDir string) (*Adapter, *fakeProcs, *int) {
	h := newFakeHost(t)
	a, slept := h.adapter(t, repoDir)
	return a, h.procs, slept
}

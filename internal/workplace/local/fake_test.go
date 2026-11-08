//go:build linux

package local

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
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
// survivor, or termProof and the signal is SIGTERM. A pid not marked with the sid of the signal
// is skipped: no signal is sent.
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

func (p *fakeProcs) Signal(pid int, sid string, sig syscall.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if !slices.Contains(p.marked[sid], pid) {
		return nil
	}
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

	noClaude   bool       // command -v claude exits 1
	launchCode int        // exit code of the next launches, when nothing else says so
	launchErr  error      // the launch fails with it, after its work
	noPidFile  bool       // the launch writes no pid file
	argvs      [][]string // the args of every launch
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

// fixtureRunner runs git for real, answers command -v claude and the launch, and fails the
// test on any other command. A launch counts, records its args and writes the pid file the
// child would write: the pid of its session id and start time 5000.
func (h *fakeHost) fixtureRunner(ctx context.Context, timeout time.Duration, name string, args ...string) (string, int, error) {
	if name == "git" {
		return workplace.ExecRunner()(ctx, timeout, name, args...)
	}
	if name == "sh" && len(args) == 2 && args[0] == "-c" && args[1] == "command -v claude" {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.noClaude {
			return "", 1, nil
		}
		return "/usr/bin/claude\n", 0, nil
	}
	if name == "sh" && len(args) > 7 && args[0] == "-c" && args[1] == launchScript {
		sid := strings.TrimPrefix(args[7], markVar+"=")
		h.mu.Lock()
		h.launches++
		h.argvs = append(h.argvs, append([]string{}, args...))
		timedOut, code, err, skip := h.timeout, h.launchCode, h.launchErr, h.noPidFile
		h.timeout = false
		h.mu.Unlock()
		if timedOut {
			return "", -1, errors.New("fake launch: timed out")
		}
		if !skip {
			content := fmt.Sprintf("%d 5000\n", h.pidOf(sid))
			if werr := os.WriteFile(args[5], []byte(content), 0o600); werr != nil {
				h.t.Errorf("fake launch: pid file: %v", werr)
			}
		}
		return "", code, err
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

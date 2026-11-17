package nodeserve

import (
	"context"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/nodeapi"
)

const (
	procSession  = "7b1e4c2a-9d3f-4a6b-8c5e-2f0a1d9b7e43"
	otherSession = "e2d4f6a8-1b3c-4d5e-9f7a-6c8b0a2e4d61"
)

func stopPath(id string) string { return nodeapi.Prefix + "/workspaces/" + id + "/stop" }
func killPath(id string) string { return nodeapi.Prefix + "/workspaces/" + id + "/kill" }

// procRunner is the adapter's runner for process commands: answer gives the output and exit
// code of every call, and it records each call as one space-joined line.
type procRunner struct {
	mu     sync.Mutex
	calls  []string
	answer func(line string) (string, int)
}

func (p *procRunner) run(_ context.Context, _ time.Duration, name string, args ...string) (string, int, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	p.mu.Lock()
	p.calls = append(p.calls, line)
	p.mu.Unlock()
	out, code := p.answer(line)
	return out, code, nil
}

func (p *procRunner) lines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.calls)
}

// useProcRunner puts p in as the runner of the node's adapter, with a sleep that does not wait.
// resolve keeps running git for real through the fixture's own runner.
func useProcRunner(f fixture, p *procRunner) {
	f.h.adapter.Run = p.run
	f.h.adapter.Sleep = func(time.Duration) {}
}

// pgrepSeq answers pgrep for sid with the exit codes in seq, one per call (the last repeats),
// and 0 to everything else.
func pgrepSeq(sid string, seq ...int) func(string) (string, int) {
	n := 0
	return func(line string) (string, int) {
		if strings.HasSuffix(line, "pgrep -f "+sid) {
			code := seq[min(n, len(seq)-1)]
			n++
			return "", code
		}
		return "", 0
	}
}

func TestKillOnSoftwareProfileKillsInsideContainer(t *testing.T) {
	ws := gitRepo(t)
	f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)
	f.h.d.Profiles["software"] = true
	inContainer := pgrepSeq(procSession, 0, 1)
	p := &procRunner{answer: func(line string) (string, int) {
		if strings.HasPrefix(line, "podman ps ") {
			return "c0ffee\n", 0
		}
		return inContainer(line)
	}}
	useProcRunner(f, p)

	rec := send(f.h, http.MethodPost, killPath("ws-1"), nodeapi.StopRequest{Profile: "software", SessionIDs: []string{procSession}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	want := []string{
		"podman ps --filter label=devcontainer.local_folder --format {{.ID}}",
		"podman exec c0ffee pgrep -f " + procSession,
		"podman exec c0ffee pkill -f " + procSession,
		"podman exec c0ffee pgrep -f " + procSession,
	}
	if got := p.lines(); !slices.Equal(got, want) {
		t.Errorf("calls = %q, want %q (nothing on the host)", got, want)
	}
	if f.h.adapter.Profile != "" {
		t.Errorf("shared adapter profile = %q, want it untouched", f.h.adapter.Profile)
	}
}

func TestKillDefaultProfileKillsOnHost(t *testing.T) {
	ws := gitRepo(t)
	f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)
	p := &procRunner{answer: pgrepSeq(procSession, 0, 1)}
	useProcRunner(f, p)

	rec := send(f.h, http.MethodPost, killPath("ws-1"), nodeapi.StopRequest{Profile: "default", SessionIDs: []string{procSession}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	want := []string{"pgrep -f " + procSession, "pkill -f " + procSession, "pgrep -f " + procSession}
	if got := p.lines(); !slices.Equal(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
}

func TestKillEmptySessionIDIs400BeforeAnyCall(t *testing.T) {
	ws := gitRepo(t)
	f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)
	p := &procRunner{answer: func(string) (string, int) { return "", 0 }}
	useProcRunner(f, p)
	cases := map[string][]string{
		"empty":         {procSession, ""},
		"dot":           {"a.b"},
		"space":         {"a b"},
		"option":        {"-x y"},
		"leading dash":  {"-v"},
		"empty in list": {""},
	}
	for _, path := range []string{killPath("ws-1"), stopPath("ws-1")} {
		for name, ids := range cases {
			rec := send(f.h, http.MethodPost, path, nodeapi.StopRequest{Profile: "default", SessionIDs: ids})
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s %s: status = %d, want 400: %s", path, name, rec.Code, rec.Body.String())
			}
		}
	}
	if n := f.forge.hits.Load(); n != 0 {
		t.Errorf("forge hits = %d, want none", n)
	}
	if n := f.runner.calls.Load(); n != 0 {
		t.Errorf("git calls = %d, want none", n)
	}
	if got := p.lines(); len(got) != 0 {
		t.Errorf("process calls = %q, want none", got)
	}
}

func TestKillUnknownProfileIs400(t *testing.T) {
	ws := gitRepo(t)
	f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)
	p := &procRunner{answer: func(string) (string, int) { return "", 0 }}
	useProcRunner(f, p)
	for _, path := range []string{killPath("ws-1"), stopPath("ws-1")} {
		for _, profile := range []string{"", "software", "Default"} {
			rec := send(f.h, http.MethodPost, path, nodeapi.StopRequest{Profile: profile, SessionIDs: []string{procSession}})
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s profile %q: status = %d, want 400: %s", path, profile, rec.Code, rec.Body.String())
			}
		}
	}
	if n := f.forge.hits.Load(); n != 0 {
		t.Errorf("forge hits = %d, want none before the profile check", n)
	}
	if got := p.lines(); len(got) != 0 {
		t.Errorf("process calls = %q, want none", got)
	}
}

func TestKillUnsafeWorkspaceIDIs400(t *testing.T) {
	f := newFixture(t, nil, nodeToken)
	p := &procRunner{answer: func(string) (string, int) { return "", 0 }}
	useProcRunner(f, p)
	for _, path := range []string{killPath("local-1"), stopPath("_x")} {
		rec := send(f.h, http.MethodPost, path, nodeapi.StopRequest{Profile: "default", SessionIDs: []string{procSession}})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", path, rec.Code, rec.Body.String())
		}
	}
	if n := f.forge.hits.Load(); n != 0 {
		t.Errorf("forge hits = %d, want none", n)
	}
}

func TestKillGoneWorkspaceIs404WorkspaceGone(t *testing.T) {
	f := newFixture(t, nil, nodeToken)
	p := &procRunner{answer: func(string) (string, int) { return "", 0 }}
	useProcRunner(f, p)
	rec := send(f.h, http.MethodPost, killPath("ws-9"), nodeapi.StopRequest{Profile: "default", SessionIDs: []string{procSession}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if p := problem(t, rec); p.Code != nodeapi.CodeWorkspaceGone {
		t.Errorf("code = %q, want %q", p.Code, nodeapi.CodeWorkspaceGone)
	}
	if got := p.lines(); len(got) != 0 {
		t.Errorf("process calls = %q, want none", got)
	}
}

// Without unit_prefix the node has no systemd unit: stop kills by session id, per session the
// same pgrep, pkill and up to five confirming pgreps as Kill.
func TestStopWithoutUnitPrefixFallsBackToKill(t *testing.T) {
	ws := gitRepo(t)
	f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)
	p := &procRunner{answer: pgrepSeq(procSession, 0, 0, 0, 1)}
	useProcRunner(f, p)

	rec := send(f.h, http.MethodPost, stopPath("ws-1"), nodeapi.StopRequest{Profile: "default", SessionIDs: []string{procSession}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	want := []string{
		"pgrep -f " + procSession,
		"pkill -f " + procSession,
		"pgrep -f " + procSession,
		"pgrep -f " + procSession,
		"pgrep -f " + procSession,
	}
	if got := p.lines(); !slices.Equal(got, want) {
		t.Errorf("calls = %q, want %q (no systemctl)", got, want)
	}
}

func TestStopWithoutUnitPrefixStillRunningIs500(t *testing.T) {
	ws := gitRepo(t)
	f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)
	p := &procRunner{answer: pgrepSeq(procSession, 0)} // never ends
	useProcRunner(f, p)

	rec := send(f.h, http.MethodPost, stopPath("ws-1"), nodeapi.StopRequest{Profile: "default", SessionIDs: []string{procSession}})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	pgreps := 0
	for _, l := range p.lines() {
		if l == "pgrep -f "+procSession {
			pgreps++
		}
	}
	if pgreps != 6 {
		t.Errorf("pgrep calls = %d, want 6 (one, then five to confirm)", pgreps)
	}
}

func TestStopWithUnitPrefixUsesSystemctl(t *testing.T) {
	ws := gitRepo(t)
	f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)
	f.h.d.Forge.UnitPrefix = "lh-agent-"
	f.h.adapter.UnitPrefix = "lh-agent-"
	unit := "lh-agent-default-" + filepath.Base(ws)
	p := &procRunner{answer: func(line string) (string, int) {
		if line == "systemctl --user is-active "+unit {
			return "inactive\n", 3
		}
		return "", 0
	}}
	useProcRunner(f, p)

	rec := send(f.h, http.MethodPost, stopPath("ws-1"), nodeapi.StopRequest{Profile: "default", SessionIDs: []string{procSession, otherSession}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	want := []string{"systemctl --user stop " + unit, "systemctl --user is-active " + unit}
	if got := p.lines(); !slices.Equal(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
}

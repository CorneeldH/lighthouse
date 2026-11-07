// Package workplacetest is the contract every workplace.Workplace adapter keeps. Each adapter
// runs the same cases against a Fixture of its own; only tests import this package.
package workplacetest

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Fixture puts one adapter into a state and hands it out. Each method arranges the world
// before the call under test; the suite never looks inside the adapter.
type Fixture interface {
	Workplace() workplace.Workplace
	Workspace(t *testing.T, branch string) workplace.Workspace // prepared, clean
	Spec(t *testing.T) workplace.LaunchSpec                    // a valid start; the suite sets SessionID
	MakeDirty(t *testing.T, ws workplace.Workspace)
	Forget(t *testing.T, ws workplace.Workspace) // the workplace no longer knows it
	Live(t *testing.T, sid string)               // a process of sid runs: its session is working; a spawned sid without Live is done
	Survivor(t *testing.T, sid string)           // ... and outlives every signal
	Unmeasurable(t *testing.T)                   // processes cannot be measured
	SpawnTimesOut(t *testing.T)                  // the next launch has an unknown outcome
	Launches() int                               // launches sent so far
	ProcCalls() int                              // process measurements and signals so far
	Signals() int                                // signals sent so far
}

// Case is one contract case.
type Case struct {
	Name string
	Run  func(t *testing.T, f Fixture)
}

// Cases are the contract cases, in the order of the spec.
var Cases = []Case{
	{"PrepareTwiceSameWorkspace", prepareTwiceSameWorkspace},
	{"LookupKnownAndForgotten", lookupKnownAndForgotten},
	{"SessionsOfForgottenIsGone", sessionsOfForgottenIsGone},
	{"SpawnInvalidNotAccepted", spawnInvalidNotAccepted},
	{"SpawnAcceptedShowsSession", spawnAcceptedShowsSession},
	{"SpawnUncertainNeverTwice", spawnUncertainNeverTwice},
	{"EmptySessionIDRejected", emptySessionIDRejected},
	{"KillWithoutProcess", killWithoutProcess},
	{"KillSurvivorIsError", killSurvivorIsError},
	{"KillUnmeasurableIsError", killUnmeasurableIsError},
	{"KillLeavesOtherSession", killLeavesOtherSession},
	{"RemoveDirtyKeeps", removeDirtyKeeps},
	{"RemoveForgottenAndTwice", removeForgottenAndTwice},
	{"DiscardIdempotent", discardIdempotent},
	{"EndedSessionIsDone", endedSessionIsDone},
}

// Run runs every case as a subtest, each with a fresh fixture.
func Run(t *testing.T, newFixture func(t *testing.T) Fixture) {
	for _, c := range Cases {
		t.Run(c.Name, func(t *testing.T) { c.Run(t, newFixture(t)) })
	}
}

func prepareTwiceSameWorkspace(t *testing.T, f Fixture) {
	w := f.Workplace()
	first, err := w.Prepare(t.Context(), "issue-1-x")
	if err != nil {
		t.Fatalf("first Prepare: %v", err)
	}
	second, err := w.Prepare(t.Context(), "issue-1-x")
	if err != nil {
		t.Fatalf("second Prepare: %v", err)
	}
	if first.ID == "" || second.ID != first.ID {
		t.Errorf("IDs = %q, %q; want the same non-empty ID", first.ID, second.ID)
	}
	if !filepath.IsAbs(second.Path) {
		t.Errorf("Path = %q, want an absolute path", second.Path)
	}
}

func lookupKnownAndForgotten(t *testing.T, f Fixture) {
	ws := f.Workspace(t, "issue-2-x")
	got, err := f.Workplace().Lookup(t.Context(), ws.ID)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.ID != ws.ID || got.Path != ws.Path || got.Branch != "issue-2-x" {
		t.Errorf("Lookup = %+v, want ID %q, Path %q and Branch issue-2-x", got, ws.ID, ws.Path)
	}
	f.Forget(t, ws)
	if _, err := f.Workplace().Lookup(t.Context(), ws.ID); !errors.Is(err, workplace.ErrGone) {
		t.Errorf("Lookup after Forget: err = %v, want ErrGone", err)
	}
}

func sessionsOfForgottenIsGone(t *testing.T, f Fixture) {
	ws := f.Workspace(t, "issue-3-x")
	f.Forget(t, ws)
	if _, err := f.Workplace().Sessions(t.Context(), ws.ID); !errors.Is(err, workplace.ErrGone) {
		t.Errorf("Sessions: err = %v, want ErrGone", err)
	}
}

func spawnInvalidNotAccepted(t *testing.T, f Fixture) {
	ws := f.Workspace(t, "issue-4-x")
	invalid := map[string]func(l *workplace.LaunchSpec){
		"SessionIDNotUUID":     func(l *workplace.LaunchSpec) { l.SessionID = "not-a-uuid" },
		"PromptStartsWithDash": func(l *workplace.LaunchSpec) { l.Prompt = "-x" },
	}
	for name, spoil := range invalid {
		t.Run(name, func(t *testing.T) {
			l := f.Spec(t)
			l.SessionID = runs.NewSessionID()
			spoil(&l)
			if _, err := f.Workplace().Spawn(t.Context(), ws, l); !errors.Is(err, workplace.ErrNotAccepted) {
				t.Errorf("Spawn: err = %v, want ErrNotAccepted", err)
			}
			ss, err := f.Workplace().Sessions(t.Context(), ws.ID)
			if err != nil {
				t.Fatalf("Sessions: %v", err)
			}
			if len(ss) != 0 {
				t.Errorf("Sessions = %+v, want none", ss)
			}
			if n := f.Launches(); n != 0 {
				t.Errorf("launches = %d, want 0", n)
			}
		})
	}
}

func spawnAcceptedShowsSession(t *testing.T, f Fixture) {
	ws := f.Workspace(t, "issue-5-x")
	sid := runs.NewSessionID()
	s := spawn(t, f, ws, sid)
	if s.Key == "" || s.SessionID != sid || s.WorkspaceID != ws.ID {
		t.Errorf("Spawn = %+v, want a key, SessionID %s and WorkspaceID %s", s, sid, ws.ID)
	}
	ss, err := f.Workplace().Sessions(t.Context(), ws.ID)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	found := false
	for _, e := range ss {
		found = found || e.Key == s.Key && e.SessionID == sid
	}
	if !found {
		t.Errorf("Sessions = %+v, want key %q with session id %s", ss, s.Key, sid)
	}
	if n := f.Launches(); n != 1 {
		t.Errorf("launches = %d, want 1", n)
	}
}

func spawnUncertainNeverTwice(t *testing.T, f Fixture) {
	ws := f.Workspace(t, "issue-6-x")
	f.SpawnTimesOut(t)
	l := f.Spec(t)
	l.SessionID = runs.NewSessionID()
	if _, err := f.Workplace().Spawn(t.Context(), ws, l); !errors.Is(err, workplace.ErrUncertain) {
		t.Errorf("Spawn: err = %v, want ErrUncertain", err)
	}
	if n := f.Launches(); n != 1 {
		t.Errorf("launches = %d, want 1, never a second", n)
	}
}

// An empty id would make a match on the session id hit every process, foreign ones included.
func emptySessionIDRejected(t *testing.T, f Fixture) {
	ws := f.Workspace(t, "issue-7-x")
	sids := []string{runs.NewSessionID(), ""}
	if err := f.Workplace().Kill(t.Context(), ws.ID, sids); err == nil {
		t.Error("Kill: err = nil, want an error for an empty session id")
	}
	if err := f.Workplace().Stop(t.Context(), ws.ID, sids); err == nil {
		t.Error("Stop: err = nil, want an error for an empty session id")
	}
	if n := f.ProcCalls(); n != 0 {
		t.Errorf("process calls = %d, want 0", n)
	}
	if n := f.Signals(); n != 0 {
		t.Errorf("signals = %d, want 0", n)
	}
}

func killWithoutProcess(t *testing.T, f Fixture) {
	ws := f.Workspace(t, "issue-8-x")
	if err := f.Workplace().Kill(t.Context(), ws.ID, []string{runs.NewSessionID()}); err != nil {
		t.Errorf("Kill: err = %v, want nil", err)
	}
	if n := f.Signals(); n != 0 {
		t.Errorf("signals = %d, want 0", n)
	}
}

func killSurvivorIsError(t *testing.T, f Fixture) {
	ws := f.Workspace(t, "issue-9-x")
	sid := runs.NewSessionID()
	f.Live(t, sid)
	f.Survivor(t, sid)
	if err := f.Workplace().Kill(t.Context(), ws.ID, []string{sid}); err == nil {
		t.Error("Kill: err = nil, want an error for a process that outlives every signal")
	}
}

func killUnmeasurableIsError(t *testing.T, f Fixture) {
	ws := f.Workspace(t, "issue-10-x")
	sid := runs.NewSessionID()
	f.Live(t, sid)
	f.Unmeasurable(t)
	if err := f.Workplace().Kill(t.Context(), ws.ID, []string{sid}); err == nil {
		t.Error("Kill: err = nil, want an error when processes cannot be measured")
	}
	if n := f.Signals(); n != 0 {
		t.Errorf("signals = %d, want 0", n)
	}
}

func killLeavesOtherSession(t *testing.T, f Fixture) {
	ws := f.Workspace(t, "issue-11-x")
	a, b := runs.NewSessionID(), runs.NewSessionID()
	spawn(t, f, ws, a)
	spawn(t, f, ws, b)
	f.Live(t, a)
	f.Live(t, b)
	if err := f.Workplace().Kill(t.Context(), ws.ID, []string{a}); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	stands := standsBySessionID(t, f, ws)
	if got, ok := stands[b]; !ok || got != workplace.Working {
		t.Errorf("stand of the other session = %v (present %v), want working", got, ok)
	}
	if got, ok := stands[a]; !ok || got != workplace.Done {
		t.Errorf("stand of the killed session = %v (present %v), want done", got, ok)
	}
}

func removeDirtyKeeps(t *testing.T, f Fixture) {
	ws := f.Workspace(t, "issue-12-x")
	f.MakeDirty(t, ws)
	if err := f.Workplace().Remove(t.Context(), ws.ID); !errors.Is(err, workplace.ErrDirty) {
		t.Errorf("Remove: err = %v, want ErrDirty", err)
	}
	got, err := f.Workplace().Lookup(t.Context(), ws.ID)
	if err != nil {
		t.Fatalf("Lookup after a refused Remove: %v", err)
	}
	if got.Path != ws.Path {
		t.Errorf("Lookup Path = %q, want %q", got.Path, ws.Path)
	}
}

func removeForgottenAndTwice(t *testing.T, f Fixture) {
	forgotten := f.Workspace(t, "issue-13-a")
	f.Forget(t, forgotten)
	if err := f.Workplace().Remove(t.Context(), forgotten.ID); err != nil {
		t.Errorf("Remove of a forgotten workspace: err = %v, want nil", err)
	}
	ws := f.Workspace(t, "issue-13-b")
	for i := 1; i <= 2; i++ {
		if err := f.Workplace().Remove(t.Context(), ws.ID); err != nil {
			t.Errorf("Remove %d: err = %v, want nil", i, err)
		}
	}
	if _, err := f.Workplace().Lookup(t.Context(), ws.ID); !errors.Is(err, workplace.ErrGone) {
		t.Errorf("Lookup after Remove: err = %v, want ErrGone", err)
	}
}

func discardIdempotent(t *testing.T, f Fixture) {
	ws := f.Workspace(t, "issue-14-x")
	for i := 1; i <= 2; i++ {
		if err := f.Workplace().Discard(t.Context(), ws.ID); err != nil {
			t.Errorf("Discard %d: err = %v, want nil", i, err)
		}
	}
}

func endedSessionIsDone(t *testing.T, f Fixture) {
	ws := f.Workspace(t, "issue-15-x")
	a, b := runs.NewSessionID(), runs.NewSessionID()
	spawn(t, f, ws, a)
	spawn(t, f, ws, b)
	f.Live(t, b)
	stands := standsBySessionID(t, f, ws)
	if got, ok := stands[a]; !ok || got != workplace.Done {
		t.Errorf("stand of the ended session = %v (present %v), want done", got, ok)
	}
	if got, ok := stands[b]; !ok || got == workplace.Done {
		t.Errorf("stand of the live session = %v (present %v), want not done", got, ok)
	}
}

// spawn starts sid in ws with the fixture's spec and fails the test when it is not accepted.
func spawn(t *testing.T, f Fixture, ws workplace.Workspace, sid string) workplace.Session {
	t.Helper()
	l := f.Spec(t)
	l.SessionID = sid
	s, err := f.Workplace().Spawn(t.Context(), ws, l)
	if err != nil {
		t.Fatalf("Spawn %s: %v", sid, err)
	}
	return s
}

func standsBySessionID(t *testing.T, f Fixture, ws workplace.Workspace) map[string]workplace.Stand {
	t.Helper()
	ss, err := f.Workplace().Sessions(t.Context(), ws.ID)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	stands := map[string]workplace.Stand{}
	for _, s := range ss {
		stands[s.SessionID] = s.Stand()
	}
	return stands
}

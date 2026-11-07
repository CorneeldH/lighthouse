//go:build linux

package local

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

const unknownTranscript = `{"type":"mode","mode":"normal","sessionId":"` + testSID + `"}
{"type":"system","subtype":"informational","content":"Unknown command: /superpowers:subagent-driven-development","timestamp":"2026-11-06T17:20:36.129Z"}
`

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// adopted is a prepared workspace on a worktree the adapter did not make.
func adopted(t *testing.T) (*Adapter, *fakeProcs, store, workplace.Workspace, string) {
	t.Helper()
	repo, _ := newRepo(t)
	mine := filepath.Join(filepath.Dir(repo), "mine")
	git(t, repo, "worktree", "add", mine, "-b", "issue-1-x")
	a, procs, _ := newAdapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	s, err := a.store()
	if err != nil {
		t.Fatal(err)
	}
	return a, procs, s, ws, repo
}

func addSession(t *testing.T, s store, id, sid, key string) {
	t.Helper()
	if err := s.putSession(id, sessRecord{Key: key, SessionID: sid}); err != nil {
		t.Fatal(err)
	}
	if err := s.ensureLogs(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.logPath(key), []byte("log"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveDeletesWorktreeKeepsBranch(t *testing.T) {
	a, _, _, ws, s := spawnSetup(t)
	sess, err := a.Spawn(t.Context(), ws, testSpec(testSID))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.logPath(sess.Key), []byte("log"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Remove(t.Context(), ws.ID); err != nil {
		t.Fatalf("Remove = %v, want nil", err)
	}
	if exists(ws.Path) {
		t.Errorf("worktree %s still exists", ws.Path)
	}
	if git(t, a.RepoDir, "branch", "--list", "issue-1-x") == "" {
		t.Error("branch issue-1-x is gone, want kept")
	}
	for _, p := range []string{s.workspacePath(ws.ID), s.sessionDir(ws.ID), s.logPath(sess.Key)} {
		if exists(p) {
			t.Errorf("%s still exists", p)
		}
	}
}

func TestRemoveThenPrepareFreshID(t *testing.T) {
	repo, _ := newRepo(t)
	a, _, _ := newAdapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	if err := a.Remove(t.Context(), ws.ID); err != nil {
		t.Fatal(err)
	}
	again := prepare(t, a, "issue-1-x")
	if again.ID == ws.ID {
		t.Errorf("ID %q reused after Remove", again.ID)
	}
}

func TestRemoveAdoptedLeavesWorktree(t *testing.T) {
	a, _, _, ws, _ := adopted(t)
	if err := a.Remove(t.Context(), ws.ID); err != nil {
		t.Fatalf("Remove = %v, want nil", err)
	}
	if !exists(ws.Path) {
		t.Error("adopted worktree deleted, want kept")
	}
	if _, err := a.Lookup(t.Context(), ws.ID); !errors.Is(err, workplace.ErrGone) {
		t.Errorf("Lookup = %v, want ErrGone", err)
	}
}

func TestRemoveAdoptedKillsMarkedFirst(t *testing.T) {
	a, procs, s, ws, _ := adopted(t)
	addSession(t, s, ws.ID, "s1", "s1.aa")
	procs.marked["s1"] = []int{1001}
	if err := a.Remove(t.Context(), ws.ID); err != nil {
		t.Fatalf("Remove = %v, want nil", err)
	}
	if len(procs.sent) == 0 || procs.sent[0] != (sentSignal{1001, syscall.SIGTERM}) {
		t.Errorf("sent = %v, want SIGTERM to 1001 first", procs.sent)
	}
	if exists(s.sessionDir(ws.ID)) || exists(s.logPath("s1.aa")) || exists(s.workspacePath(ws.ID)) {
		t.Error("records remain after a successful Remove")
	}
}

func TestRemoveAdoptedSurvivorKeepsRecords(t *testing.T) {
	a, procs, s, ws, _ := adopted(t)
	addSession(t, s, ws.ID, "s1", "s1.aa")
	procs.marked["s1"] = []int{1001}
	procs.survivors[1001] = true
	if err := a.Remove(t.Context(), ws.ID); err == nil {
		t.Fatal("Remove = nil, want an error")
	}
	if _, err := a.Lookup(t.Context(), ws.ID); err != nil {
		t.Errorf("Lookup after a failed Remove = %v, want found", err)
	}
	if !exists(s.logPath("s1.aa")) {
		t.Error("log dropped by a failed Remove")
	}
}

func TestRemoveVanishedDropsRecords(t *testing.T) {
	a, _, _, ws, s := spawnSetup(t)
	addSession(t, s, ws.ID, "s1", "s1.aa")
	if err := os.RemoveAll(ws.Path); err != nil {
		t.Fatal(err)
	}
	if err := a.Remove(t.Context(), ws.ID); err != nil {
		t.Fatalf("Remove = %v, want nil", err)
	}
	if exists(s.workspacePath(ws.ID)) || exists(s.sessionDir(ws.ID)) || exists(s.logPath("s1.aa")) {
		t.Error("records remain")
	}
}

// A vanished worktree whose run still has processes: they end first, then the records go.
func TestRemoveVanishedWithProcessesEndsThem(t *testing.T) {
	a, h, _, ws, s := spawnSetup(t)
	addSession(t, s, ws.ID, "s1", "s1.aa")
	h.procs.marked["s1"] = []int{1001}
	if err := os.RemoveAll(ws.Path); err != nil {
		t.Fatal(err)
	}
	if err := a.Remove(t.Context(), ws.ID); err != nil {
		t.Fatalf("Remove = %v, want nil", err)
	}
	if len(h.procs.sent) == 0 || h.procs.sent[0].pid != 1001 {
		t.Errorf("sent = %v, want a signal to 1001", h.procs.sent)
	}
	if exists(s.workspacePath(ws.ID)) || exists(s.sessionDir(ws.ID)) {
		t.Error("records remain")
	}
}

func TestRemoveVanishedSurvivorKeepsRecords(t *testing.T) {
	a, h, _, ws, s := spawnSetup(t)
	addSession(t, s, ws.ID, "s1", "s1.aa")
	h.procs.marked["s1"] = []int{1001}
	h.procs.survivors[1001] = true
	if err := os.RemoveAll(ws.Path); err != nil {
		t.Fatal(err)
	}
	if err := a.Remove(t.Context(), ws.ID); err == nil {
		t.Fatal("Remove = nil, want an error")
	}
	if !exists(s.workspacePath(ws.ID)) || !exists(s.logPath("s1.aa")) {
		t.Error("records dropped by a failed Remove")
	}
}

func TestRemoveRefusedIsNotDirty(t *testing.T) {
	repo, _ := newRepo(t)
	a, _, _ := newAdapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	git(t, repo, "worktree", "lock", ws.Path)
	err := a.Remove(t.Context(), ws.ID)
	if err == nil || errors.Is(err, workplace.ErrDirty) || !strings.Contains(err.Error(), "exit code") {
		t.Errorf("Remove = %v, want a plain error with the exit code", err)
	}
	if _, err := a.Lookup(t.Context(), ws.ID); err != nil {
		t.Errorf("Lookup after a refused Remove = %v, want found", err)
	}
}

func TestRemoveDirtyIsErrDirty(t *testing.T) {
	repo, _ := newRepo(t)
	a, _, _ := newAdapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	if err := os.WriteFile(filepath.Join(ws.Path, "x.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Remove(t.Context(), ws.ID); !errors.Is(err, workplace.ErrDirty) {
		t.Errorf("Remove = %v, want ErrDirty", err)
	}
	if !exists(ws.Path) {
		t.Error("dirty worktree removed")
	}
}

func TestRemoveNoRecordIsNil(t *testing.T) {
	repo, _ := newRepo(t)
	a, _, _ := newAdapter(t, repo)
	if err := a.Remove(t.Context(), "local-nope-abcdef"); err != nil {
		t.Errorf("Remove = %v, want nil", err)
	}
}

func TestDiscardDropsOnlyPidlessSessions(t *testing.T) {
	a, _, _, ws, s := spawnSetup(t)
	addSession(t, s, ws.ID, "s1", "s1.aa")
	addSession(t, s, ws.ID, "s2", "s2.bb")
	if err := os.WriteFile(s.pidPath(ws.ID, "s2.bb"), []byte("20001 5000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Discard(t.Context(), ws.ID); err != nil {
		t.Fatalf("Discard = %v, want nil", err)
	}
	recs, err := s.sessions(ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Key != "s2.bb" {
		t.Errorf("sessions = %v, want only s2.bb", recs)
	}
	if exists(s.logPath("s1.aa")) {
		t.Error("log of the discarded launch remains")
	}
}

func TestDiscardKeepsVanishedButLive(t *testing.T) {
	a, h, _, ws, s := spawnSetup(t)
	addSession(t, s, ws.ID, "s1", "s1.aa")
	h.procs.marked["s1"] = []int{1001}
	if err := os.RemoveAll(ws.Path); err != nil {
		t.Fatal(err)
	}
	if err := a.Discard(t.Context(), ws.ID); err != nil {
		t.Fatalf("Discard = %v, want nil", err)
	}
	if recs, _ := s.sessions(ws.ID); len(recs) != 1 {
		t.Fatalf("sessions = %v, want the record kept", recs)
	}
	if err := a.Kill(t.Context(), ws.ID, []string{"s1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Lookup(t.Context(), ws.ID); !errors.Is(err, workplace.ErrGone) {
		t.Fatalf("Lookup = %v, want ErrGone", err)
	}
	if err := a.Discard(t.Context(), ws.ID); err != nil {
		t.Fatalf("Discard = %v, want nil", err)
	}
	if exists(s.workspacePath(ws.ID)) || exists(s.sessionDir(ws.ID)) {
		t.Error("records remain after Discard of a gone workspace")
	}
}

func TestTranscriptReadsHostProjectDir(t *testing.T) {
	repo, _ := newRepo(t)
	a, _, _ := newAdapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	if _, err := a.Transcript(t.Context(), ws.ID, testSID); !errors.Is(err, workplace.ErrNotMeasurable) {
		t.Errorf("Transcript without a project dir = %v, want ErrNotMeasurable", err)
	}
	dir := workplace.ProjectDir(a.Home, ws.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, testSID+".jsonl"), []byte(unknownTranscript), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := a.Transcript(t.Context(), ws.ID, testSID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UnknownCommand != "/superpowers:subagent-driven-development" {
		t.Errorf("UnknownCommand = %q", got.UnknownCommand)
	}
	if want := time.Date(2026, 11, 6, 17, 20, 36, 129000000, time.UTC); !got.LastEvent.Equal(want) {
		t.Errorf("LastEvent = %v, want %v", got.LastEvent, want)
	}
}

func TestTranscriptUnsafeSessionID(t *testing.T) {
	repo, _ := newRepo(t)
	a, _, _ := newAdapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	// A file one level up that a path-joined "../x" would reach.
	dir := workplace.ProjectDir(a.Home, ws.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "x.jsonl"), []byte(unknownTranscript), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := a.Transcript(t.Context(), ws.ID, "../x")
	if err == nil || errors.Is(err, workplace.ErrNotMeasurable) {
		t.Errorf("Transcript = %v, want an unsafe-id error", err)
	}
}

// A claude that finished can leave a dev server with the mark: Remove ends it before the
// records that find it go.
func TestRemoveEndsMarkedProcessesFirst(t *testing.T) {
	a, h, _, ws, s := spawnSetup(t)
	addSession(t, s, ws.ID, "s1", "s1.aa")
	h.procs.marked["s1"] = []int{1001}
	if err := a.Remove(t.Context(), ws.ID); err != nil {
		t.Fatalf("Remove = %v, want nil", err)
	}
	if len(h.procs.sent) == 0 || h.procs.sent[0] != (sentSignal{1001, syscall.SIGTERM}) {
		t.Errorf("sent = %v, want SIGTERM to 1001 first", h.procs.sent)
	}
	if len(h.procs.marked["s1"]) != 0 || exists(ws.Path) || exists(s.workspacePath(ws.ID)) || exists(s.sessionDir(ws.ID)) {
		t.Error("process, worktree or records remain")
	}
}

func TestRemoveSurvivorKeepsWorktreeAndRecords(t *testing.T) {
	a, h, _, ws, s := spawnSetup(t)
	addSession(t, s, ws.ID, "s1", "s1.aa")
	h.procs.marked["s1"] = []int{1001}
	h.procs.survivors[1001] = true
	if err := a.Remove(t.Context(), ws.ID); err == nil || errors.Is(err, workplace.ErrDirty) {
		t.Fatalf("Remove = %v, want a plain error", err)
	}
	if !exists(ws.Path) || !exists(s.workspacePath(ws.ID)) || !exists(s.logPath("s1.aa")) {
		t.Error("worktree or records dropped by a failed Remove")
	}
}

// A dirty worktree is refused before any process is signalled.
func TestRemoveDirtySignalsNothing(t *testing.T) {
	a, h, _, ws, s := spawnSetup(t)
	addSession(t, s, ws.ID, "s1", "s1.aa")
	h.procs.marked["s1"] = []int{1001}
	if err := os.WriteFile(filepath.Join(ws.Path, "x.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Remove(t.Context(), ws.ID); !errors.Is(err, workplace.ErrDirty) {
		t.Fatalf("Remove = %v, want ErrDirty", err)
	}
	if len(h.procs.sent) != 0 {
		t.Errorf("sent = %v, want none", h.procs.sent)
	}
}

func TestDiscardEndsMarkedProcessesOfPidlessSession(t *testing.T) {
	a, h, _, ws, s := spawnSetup(t)
	addSession(t, s, ws.ID, "s1", "s1.aa")
	h.procs.marked["s1"] = []int{1001}
	if err := a.Discard(t.Context(), ws.ID); err != nil {
		t.Fatalf("Discard = %v, want nil", err)
	}
	if len(h.procs.sent) == 0 || h.procs.sent[0] != (sentSignal{1001, syscall.SIGTERM}) {
		t.Errorf("sent = %v, want SIGTERM to 1001", h.procs.sent)
	}
	if recs, _ := s.sessions(ws.ID); len(recs) != 0 {
		t.Errorf("sessions = %v, want none", recs)
	}
}

func TestDiscardSurvivorKeepsRecord(t *testing.T) {
	a, h, _, ws, s := spawnSetup(t)
	addSession(t, s, ws.ID, "s1", "s1.aa")
	h.procs.marked["s1"] = []int{1001}
	h.procs.survivors[1001] = true
	if err := a.Discard(t.Context(), ws.ID); err == nil {
		t.Fatal("Discard = nil, want an error")
	}
	if recs, _ := s.sessions(ws.ID); len(recs) != 1 {
		t.Errorf("sessions = %v, want the record kept", recs)
	}
}

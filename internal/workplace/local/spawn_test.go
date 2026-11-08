//go:build linux

package local

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

const testSID = "11111111-2222-4333-8444-555555555555"

func testSpec(sid string) workplace.LaunchSpec {
	return workplace.LaunchSpec{Profile: "infra", Mode: "start", Prompt: "do it", InstructionsFile: "/abs/instr.md", SessionID: sid}
}

// spawnSetup is a prepared workspace on a fake host.
func spawnSetup(t *testing.T) (*Adapter, *fakeHost, *int, workplace.Workspace, store) {
	t.Helper()
	repo, _ := newRepo(t)
	h := newFakeHost(t)
	a, slept := h.adapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	s, err := a.store()
	if err != nil {
		t.Fatal(err)
	}
	return a, h, slept, ws, s
}

func launches(h *fakeHost) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.launches
}

func TestSpawnLaunchCommand(t *testing.T) {
	a, h, _, ws, s := spawnSetup(t)
	got, err := a.Spawn(t.Context(), ws, testSpec(testSID))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^` + testSID + `\.[0-9a-f]{8}$`).MatchString(got.Key) {
		t.Errorf("Key = %q, want <sid>.<8 hex>", got.Key)
	}
	want := []string{"-c", launchScript, "sh", ws.Path,
		filepath.Join(a.StateDir, "local/o/r/logs", got.Key+".log"),
		filepath.Join(a.StateDir, "local/o/r/sessions", ws.ID, got.Key+".pid"),
		"env", "LIGHTHOUSE_CONTROLLER_RUN=" + testSID, "LIGHTHOUSE_STATE_DIR=" + a.StateDir,
		"claude", "-p", "--permission-mode", "auto", "--strict-mcp-config",
		"--allowedTools=Read(~/.claude/plugins/cache/**)", "--append-system-prompt-file", "/abs/instr.md",
		"--session-id", testSID, "do it"}
	if len(h.argvs) != 1 || !reflect.DeepEqual(h.argvs[0], want) {
		t.Errorf("launch argv = %q\nwant %q", h.argvs, want)
	}
	fi, err := os.Stat(filepath.Dir(s.logPath(got.Key)))
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("logs dir: %v, %v; want mode 0700", fi, err)
	}
}

func TestSpawnLogsDirUnmakeableNotAccepted(t *testing.T) {
	a, h, _, ws, s := spawnSetup(t)
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "logs"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := a.Spawn(t.Context(), ws, testSpec(testSID))
	if !errors.Is(err, workplace.ErrNotAccepted) || launches(h) != 0 {
		t.Errorf("err = %v, launches = %d; want ErrNotAccepted and 0", err, launches(h))
	}
	if recs, _ := s.sessions(ws.ID); len(recs) != 0 {
		t.Errorf("session records = %v, want none", recs)
	}
}

func TestSpawnNoClaudeNotAccepted(t *testing.T) {
	a, h, _, ws, s := spawnSetup(t)
	h.noClaude = true
	_, err := a.Spawn(t.Context(), ws, testSpec(testSID))
	if !errors.Is(err, workplace.ErrNotAccepted) || launches(h) != 0 {
		t.Errorf("err = %v, launches = %d; want ErrNotAccepted and 0", err, launches(h))
	}
	if recs, _ := s.sessions(ws.ID); len(recs) != 0 {
		t.Errorf("session records = %v, want none", recs)
	}
}

func TestSpawnMarkedSessionNotAccepted(t *testing.T) {
	a, h, _, ws, _ := spawnSetup(t)
	h.procs.marked[testSID] = []int{4242}
	_, err := a.Spawn(t.Context(), ws, testSpec(testSID))
	if !errors.Is(err, workplace.ErrNotAccepted) || launches(h) != 0 {
		t.Errorf("err = %v, launches = %d; want ErrNotAccepted and 0", err, launches(h))
	}
}

func TestSpawnUnwritableStateNotAccepted(t *testing.T) {
	a, h, _, ws, s := spawnSetup(t)
	dir := filepath.Join(s.dir, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := os.Mkdir(filepath.Join(dir, "probe"), 0o700); err == nil {
		t.Skip("directory permissions are not enforced (root?)")
	}
	_, err := a.Spawn(t.Context(), ws, testSpec(testSID))
	if !errors.Is(err, workplace.ErrNotAccepted) || launches(h) != 0 {
		t.Errorf("err = %v, launches = %d; want ErrNotAccepted and 0", err, launches(h))
	}
}

func TestSpawnLaunchExitNonZeroNotAccepted(t *testing.T) {
	a, h, _, ws, s := spawnSetup(t)
	h.launchCode, h.noPidFile = 97, true
	_, err := a.Spawn(t.Context(), ws, testSpec(testSID))
	if !errors.Is(err, workplace.ErrNotAccepted) || launches(h) != 1 {
		t.Errorf("err = %v, launches = %d; want ErrNotAccepted and 1", err, launches(h))
	}
	if recs, _ := s.sessions(ws.ID); len(recs) != 0 {
		t.Errorf("session records = %v, want none", recs)
	}
}

func TestSpawnRunErrorWithPidFileAccepted(t *testing.T) {
	a, h, _, ws, _ := spawnSetup(t)
	h.launchCode, h.launchErr = -1, errors.New("timed out after the child wrote its pid")
	got, err := a.Spawn(t.Context(), ws, testSpec(testSID))
	if err != nil || got.SessionID != testSID {
		t.Errorf("Spawn = %+v, %v; want accepted", got, err)
	}
	if launches(h) != 1 {
		t.Errorf("launches = %d, want 1", launches(h))
	}
}

func TestSpawnNoPidFileUncertain(t *testing.T) {
	a, h, slept, ws, s := spawnSetup(t)
	h.noPidFile = true
	_, err := a.Spawn(t.Context(), ws, testSpec(testSID))
	if !errors.Is(err, workplace.ErrUncertain) {
		t.Errorf("err = %v, want ErrUncertain", err)
	}
	if *slept != 50 || launches(h) != 1 {
		t.Errorf("sleeps = %d, launches = %d; want 50 and 1", *slept, launches(h))
	}
	if recs, _ := s.sessions(ws.ID); len(recs) != 1 {
		t.Errorf("session records = %v, want 1", recs)
	}
	if ss, err := a.Sessions(t.Context(), ws.ID); err != nil || len(ss) != 0 {
		t.Errorf("Sessions = %v, %v; want none", ss, err)
	}
}

func TestSpawnKeyFreshPerLaunch(t *testing.T) {
	a, _, _, ws, _ := spawnSetup(t)
	first, err := a.Spawn(t.Context(), ws, testSpec(testSID))
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.Spawn(t.Context(), ws, testSpec(testSID))
	if err != nil {
		t.Fatal(err)
	}
	if first.Key == second.Key {
		t.Errorf("both launches got key %q", first.Key)
	}
	if ss, err := a.Sessions(t.Context(), ws.ID); err != nil || len(ss) != 2 {
		t.Errorf("Sessions = %v, %v; want 2", ss, err)
	}
}

func TestSessionsWorkingNeedsEqualStartTime(t *testing.T) {
	a, h, _, ws, s := spawnSetup(t)
	if err := s.putSession(ws.ID, sessRecord{Key: "k1", SessionID: testSID}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.pidPath(ws.ID, "k1"), []byte("1001 5000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for start, want := range map[uint64]string{5000: "working", 6000: "done"} {
		h.procs.start[1001] = start
		ss, err := a.Sessions(t.Context(), ws.ID)
		if err != nil || len(ss) != 1 || ss[0].State != want {
			t.Errorf("start %d: Sessions = %+v, %v; want state %s", start, ss, err, want)
		}
	}
}

func TestSessionsUnmeasurableIsError(t *testing.T) {
	a, h, _, ws, s := spawnSetup(t)
	if err := s.putSession(ws.ID, sessRecord{Key: "k1", SessionID: testSID}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.pidPath(ws.ID, "k1"), []byte("1001 5000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.procs.err = errors.New("cannot read /proc")
	ss, err := a.Sessions(t.Context(), ws.ID)
	if err == nil || errors.Is(err, workplace.ErrGone) || len(ss) != 0 {
		t.Errorf("Sessions = %v, %v; want a plain error", ss, err)
	}
}

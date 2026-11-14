package bashwrites

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// preEnv points the hook at repo and a fresh XDG dir, and clears the seams.
func preEnv(t *testing.T, repo string) string {
	t.Helper()
	xdg := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("CLAUDE_PROJECT_DIR", repo)
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	t.Setenv("LIGHTHOUSE_BASH_GATES_START_NS", "")
	return xdg
}

func prePayload(t *testing.T, session, toolUse, cwd, command string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"session_id": session, "tool_use_id": toolUse, "cwd": cwd,
		"tool_input": map[string]any{"command": command},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func stateOf(t *testing.T, session string) string {
	t.Helper()
	state, err := StateDir(session)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestPreReturnsZeroWithoutAProjectDir(t *testing.T) {
	xdg := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	eq(t, "code", Pre(prePayload(t, "s", "t1", t.TempDir(), "echo hi"), time.Now), 0)
	if _, err := os.Stat(filepath.Join(xdg, "lighthouse-bash-gates", "s")); !os.IsNotExist(err) {
		t.Fatal("a session directory exists without CLAUDE_PROJECT_DIR")
	}
}

func TestPreReturnsZeroOnABrokenRepo(t *testing.T) {
	broken := newRepo(t)
	writeFile(t, filepath.Join(broken, ".git", "HEAD"), "garbage\n")
	xdg := preEnv(t, broken)
	eq(t, "code", Pre(prePayload(t, "sess-broken", "toolu_broken", broken, "echo hi > x.sh"), time.Now), 0)
	if _, err := os.Stat(filepath.Join(xdg, "lighthouse-bash-gates", "sess-broken")); !os.IsNotExist(err) {
		t.Fatal("a session directory exists for a broken project dir")
	}
}

func TestPreSwallowsEveryStrangePayload(t *testing.T) {
	repo := newRepo(t)
	preEnv(t, repo)
	for _, raw := range []string{
		"", "null", "[1]", `"x"`, "{", `{}`, `{"cwd": "/x"}`,
		`{"cwd": 3, "tool_input": {"command": "x"}}`,
		`{"cwd": "/x", "tool_input": {"command": 3}}`,
		`{"cwd": "/x", "session_id": null, "tool_input": {"command": "x"}}`,
		`{"cwd": "/x", "session_id": "../x", "tool_input": {"command": "x"}}`,
	} {
		eq(t, "code for "+raw, Pre([]byte(raw), time.Now), 0)
	}
}

func TestPreSwallowsAPanic(t *testing.T) {
	repo := newRepo(t)
	preEnv(t, repo)
	old := seed
	seed = func(string, int64, time.Time) (map[string]any, error) { panic("boom") }
	t.Cleanup(func() { seed = old })
	eq(t, "code", Pre(prePayload(t, "s", "t", repo, "true"), time.Now), 0)
}

func TestPreDoesNotOverwriteAnUnreadableLedger(t *testing.T) {
	repo := newRepo(t)
	preEnv(t, repo)
	writeFile(t, filepath.Join(repo, "dirty.sh"), "echo dirty\n")
	state := stateOf(t, "sess-corrupt")
	path := filepath.Join(state, "ledger.json")
	original := []byte("{corrupt")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	eq(t, "code", Pre(prePayload(t, "sess-corrupt", "toolu_corrupt", repo, "echo hi > x.sh"), time.Now), 0)
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, original) {
		t.Fatalf("ledger.json = %q, want it untouched", got)
	}
	_ = withLedger(state, func(l *Ledger) error {
		eq(t, "unreadable", l.Unreadable, true)
		eq(t, "roots", len(l.Roots), 0)
		return nil
	})
}

func TestPreDoesNotSeedPastAReplacedLedger(t *testing.T) {
	repo := newRepo(t)
	preEnv(t, repo)
	writeFile(t, filepath.Join(repo, "dirty.sh"), "echo dirty\n")
	state := stateOf(t, "sess-reset")
	writeFile(t, filepath.Join(state, "ledger.json"), "{}")
	writeFile(t, filepath.Join(state, "ledger.reset"), "")
	eq(t, "code", Pre(prePayload(t, "sess-reset", "toolu_reset", repo, "true"), time.Now), 0)
	_ = withLedger(state, func(l *Ledger) error {
		if _, ok := l.Roots[repo]; ok {
			t.Fatal("pre seeded a root past ledger.reset")
		}
		return nil
	})
	m, ok := readMarker(state, "toolu_reset")
	if !ok {
		t.Fatal("no marker")
	}
	eq(t, "marker roots", m.Roots(), []string{repo})
}

func TestPreSeedsAFreshRootAndWritesAMarker(t *testing.T) {
	repo := newRepo(t)
	preEnv(t, repo)
	eq(t, "code", Pre(prePayload(t, "sess-fresh", "toolu_fresh", repo, "echo hi > x.sh"), time.Now), 0)
	state := stateOf(t, "sess-fresh")
	_ = withLedger(state, func(l *Ledger) error {
		var rec struct{ Head string }
		if ok, err := l.Get(repo, &rec); err != nil || !ok || rec.Head == "" {
			t.Fatalf("root not seeded: %v %v %q", ok, err, rec.Head)
		}
		return nil
	})
	m, ok := readMarker(state, "toolu_fresh")
	if !ok {
		t.Fatal("no marker")
	}
	eq(t, "marker roots", m.Roots(), []string{repo})
	if ns, ok := m.startNS(); !ok || ns <= 0 {
		t.Fatalf("start_ns = %d, %v", ns, ok)
	}
}

func TestPreNeverOverwritesAnExistingRoot(t *testing.T) {
	repo := newRepo(t)
	preEnv(t, repo)
	writeFile(t, filepath.Join(repo, "dirty.sh"), "echo dirty\n")
	state := stateOf(t, "sess-existing")
	sentinel := map[string]any{
		"seen": map[string]any{"sentinel.sh": []int{1, 2, 3, 4}}, "head": "deadbeef",
		"pending": []string{}, "claims": map[string]any{},
	}
	if err := withLedger(state, func(l *Ledger) error { return l.Set(repo, sentinel) }); err != nil {
		t.Fatal(err)
	}
	eq(t, "code", Pre(prePayload(t, "sess-existing", "toolu_1", repo, "echo hi > x.sh"), time.Now), 0)
	_ = withLedger(state, func(l *Ledger) error {
		var got map[string]any
		_, _ = l.Get(repo, &got)
		var want map[string]any
		raw, _ := json.Marshal(sentinel)
		_ = json.Unmarshal(raw, &want)
		eq(t, "record", got, want)
		return nil
	})
}

func TestPreHonoursTheStartNSSeam(t *testing.T) {
	repo := newRepo(t)
	preEnv(t, repo)
	t.Setenv("LIGHTHOUSE_BASH_GATES_START_NS", "424242")
	eq(t, "code", Pre(prePayload(t, "sess-ns", "toolu_ns", repo, "true"), time.Now), 0)
	m, ok := readMarker(stateOf(t, "sess-ns"), "toolu_ns")
	if !ok {
		t.Fatal("no marker")
	}
	ns, _ := m.startNS()
	eq(t, "start_ns", ns, int64(424242))
}

func TestPreWritesTheMarkerBeforeSeeding(t *testing.T) {
	repo := newRepo(t)
	preEnv(t, repo)
	old := seed
	seed = func(string, int64, time.Time) (map[string]any, error) { panic("killed during seeding") }
	t.Cleanup(func() { seed = old })
	eq(t, "code", Pre(prePayload(t, "sess-utf8", "toolu_0", repo, "true"), time.Now), 0)
	m, ok := readMarker(stateOf(t, "sess-utf8"), "toolu_0")
	if !ok {
		t.Fatal("no marker")
	}
	eq(t, "marker roots", m.Roots(), []string{repo})
	if ns, ok := m.startNS(); !ok || ns <= 0 {
		t.Fatalf("start_ns = %d, %v", ns, ok)
	}
}

func TestPreStopsSeedingAtItsDeadline(t *testing.T) {
	repo := newRepo(t)
	preEnv(t, repo)
	var seeded []string
	oldSeed, oldDeadline := seed, preDeadline
	seed = func(root string, ns int64, d time.Time) (map[string]any, error) {
		seeded = append(seeded, root)
		return oldSeed(root, ns, d)
	}
	preDeadline = -time.Second
	t.Cleanup(func() { seed, preDeadline = oldSeed, oldDeadline })
	eq(t, "code", Pre(prePayload(t, "sess-utf8", "toolu_d", repo, "true"), time.Now), 0)
	eq(t, "seeded", len(seeded), 0)
	if _, ok := readMarker(stateOf(t, "sess-utf8"), "toolu_d"); !ok {
		t.Fatal("no marker")
	}
}

func TestPreFallsBackOnAMarkerKeyFromTheCommand(t *testing.T) {
	repo := newRepo(t)
	preEnv(t, repo)
	raw := []byte(`{"session_id": "sess-k", "cwd": "` + repo + `", "tool_input": {"command": "true"}}`)
	eq(t, "code", Pre(raw, time.Now), 0)
	state := stateOf(t, "sess-k")
	entries, _ := os.ReadDir(state)
	found := false
	for _, e := range entries {
		if len(e.Name()) > len("sess-k-") && e.Name()[:7] == "sess-k-" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no marker named after the command hash in %v", entries)
	}
}

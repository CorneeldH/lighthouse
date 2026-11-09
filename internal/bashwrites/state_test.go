package bashwrites

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
)

// newRepo builds a repo with one commit under a fresh temp dir and returns its realpath.
func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	fixturesetup.Build(t, root, []map[string]any{{"repo": "repo", "branch": "main"}})
	real, err := filepath.EvalSymlinks(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newState(t *testing.T) string {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(state, 0o700); err != nil {
		t.Fatal(err)
	}
	return state
}

var plainRecord = map[string]any{"seen": map[string]any{}, "head": "x", "pending": []string{}, "claims": map[string]any{}}

func TestSignatureIsNoneForAMissingPath(t *testing.T) {
	if _, ok := signatureOf(filepath.Join(t.TempDir(), "nope.sh")); ok {
		t.Fatal("a missing path has a signature")
	}
}

func TestSignatureIsFourIntsForAnExistingFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.sh")
	writeFile(t, f, "echo hi\n")
	sig, ok := signatureOf(f)
	if !ok {
		t.Fatal("no signature for an existing file")
	}
	if sig[0] <= 0 || sig[3] != int64(len("echo hi\n")) {
		t.Fatalf("signature = %v, want a positive mtime and the file size", sig)
	}
}

func TestStateDirCreatesTheDirWithMode0700(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(t.TempDir(), "xdg"))
	path, err := StateDir("abc123")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("state dir missing: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %o, want 700", info.Mode().Perm())
	}
	if !strings.HasSuffix(path, filepath.Join("lighthouse-bash-gates", "abc123")) {
		t.Fatalf("path = %s", path)
	}
}

func TestStateDirRejectsATraversalSessionID(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(t.TempDir(), "xdg"))
	for _, id := range []string{"../evil", "..", "a/b"} {
		_, err := StateDir(id)
		if err == nil || err.Error() != "invalid session_id: '"+id+"'" {
			t.Fatalf("StateDir(%q) error = %v", id, err)
		}
	}
}

func TestWriteMarkerThenReadMarkerRoundTrips(t *testing.T) {
	state := newState(t)
	if err := writeMarker(state, "toolu_x", 123, []string{"/a", "/b"}); err != nil {
		t.Fatal(err)
	}
	m, ok := readMarker(state, "toolu_x")
	if !ok {
		t.Fatal("marker not read")
	}
	eq(t, "roots", m.Roots(), []string{"/a", "/b"})
	ns, ok := m.startNS()
	eq(t, "start_ns", ns, int64(123))
	eq(t, "start_ns ok", ok, true)
}

func TestReadMarkerMissingOrBrokenIsNone(t *testing.T) {
	state := newState(t)
	if _, ok := readMarker(state, "nope"); ok {
		t.Fatal("missing marker was read")
	}
	writeFile(t, filepath.Join(state, "bad.json"), "[1]")
	if _, ok := readMarker(state, "bad"); ok {
		t.Fatal("a marker that is not an object was read")
	}
	writeFile(t, filepath.Join(state, "worse.json"), "{nope")
	if _, ok := readMarker(state, "worse"); ok {
		t.Fatal("a marker that is not JSON was read")
	}
}

func TestPruneRemovesOnlyMarkersOlderThanAnHour(t *testing.T) {
	state := newState(t)
	for _, name := range []string{"old.json", "fresh.json", "ledger.json", "ledger.lock"} {
		writeFile(t, filepath.Join(state, name), "{}")
	}
	now := time.Now()
	old := now.Add(-3700 * time.Second)
	if err := os.Chtimes(filepath.Join(state, "old.json"), old, old); err != nil {
		t.Fatal(err)
	}
	pruneMarkers(state, now)
	if _, err := os.Stat(filepath.Join(state, "old.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old marker survived")
	}
	for _, name := range []string{"fresh.json", "ledger.json", "ledger.lock"} {
		if _, err := os.Stat(filepath.Join(state, name)); err != nil {
			t.Fatalf("%s was pruned", name)
		}
	}
}

func TestPruneKeepsTheLedgerResetSentinel(t *testing.T) {
	state := newState(t)
	sentinel := filepath.Join(state, "ledger.reset")
	writeFile(t, sentinel, "")
	now := time.Now()
	old := now.Add(-3700 * time.Second)
	if err := os.Chtimes(sentinel, old, old); err != nil {
		t.Fatal(err)
	}
	pruneMarkers(state, now)
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatal("sentinel was pruned")
	}
}

func TestUnreadableLedgerIsFlaggedNotFatal(t *testing.T) {
	state := newState(t)
	writeFile(t, filepath.Join(state, "ledger.json"), "{not valid json")
	err := withLedger(state, func(l *Ledger) error {
		eq(t, "unreadable", l.Unreadable, true)
		eq(t, "roots", len(l.Roots), 0)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLedgerThatIsNotAnObjectIsUnreadable(t *testing.T) {
	for _, text := range []string{"[1]", "null", `"x"`, "3"} {
		state := newState(t)
		writeFile(t, filepath.Join(state, "ledger.json"), text)
		_ = withLedger(state, func(l *Ledger) error {
			eq(t, text+" unreadable", l.Unreadable, true)
			return nil
		})
	}
}

func TestLedgerRowThatIsNotAnObjectDropsOnlyThatRoot(t *testing.T) {
	state := newState(t)
	writeFile(t, filepath.Join(state, "ledger.json"), `{"/a": 5, "/b": {"head": "h"}}`)
	_ = withLedger(state, func(l *Ledger) error {
		eq(t, "unreadable", l.Unreadable, false)
		if _, ok := l.Roots["/a"]; ok {
			t.Fatal("a row that is not an object was kept")
		}
		if _, ok := l.Roots["/b"]; !ok {
			t.Fatal("the valid row was dropped")
		}
		return nil
	})
}

func TestMissingLedgerIsEmptyAndReadable(t *testing.T) {
	state := newState(t)
	_ = withLedger(state, func(l *Ledger) error {
		eq(t, "unreadable", l.Unreadable, false)
		eq(t, "roots", len(l.Roots), 0)
		return nil
	})
}

func TestLedgerPersistsChangesAcrossCalls(t *testing.T) {
	state := newState(t)
	if err := withLedger(state, func(l *Ledger) error { return l.Set("/r", plainRecord) }); err != nil {
		t.Fatal(err)
	}
	_ = withLedger(state, func(l *Ledger) error {
		var got map[string]any
		ok, err := l.Get("/r", &got)
		if err != nil || !ok {
			t.Fatalf("Get = %v, %v", ok, err)
		}
		want := map[string]any{"seen": map[string]any{}, "head": "x", "pending": []any{}, "claims": map[string]any{}}
		eq(t, "record", got, want)
		return nil
	})
}

func TestLedgerDoesNotWriteWhenTheBodyFails(t *testing.T) {
	state := newState(t)
	boom := errors.New("boom")
	err := withLedger(state, func(l *Ledger) error {
		_ = l.Set("/r", plainRecord)
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, "ledger.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("ledger.json was written after a failing body")
	}
}

func TestLedgerDoesNotWriteBackAnUnreadableLedger(t *testing.T) {
	state := newState(t)
	path := filepath.Join(state, "ledger.json")
	original := []byte("{not valid json")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	_ = withLedger(state, func(l *Ledger) error {
		eq(t, "unreadable", l.Unreadable, true)
		return l.Set("/r", plainRecord)
	})
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

func TestLedgerWritesWhenUnreadableIsCleared(t *testing.T) {
	state := newState(t)
	writeFile(t, filepath.Join(state, "ledger.json"), "{not valid json")
	_ = withLedger(state, func(l *Ledger) error {
		eq(t, "unreadable", l.Unreadable, true)
		l.Unreadable = false
		return l.Set("/r", plainRecord)
	})
	_ = withLedger(state, func(l *Ledger) error {
		eq(t, "unreadable", l.Unreadable, false)
		eq(t, "roots", len(l.Roots), 1)
		return nil
	})
}

func TestLedgerSurvivesParallelWriters(t *testing.T) {
	state := newState(t)
	const workers, rounds = 8, 50
	counter := func(l *Ledger) error {
		var rec struct{ Count int }
		if _, err := l.Get("counter", &rec); err != nil {
			return err
		}
		rec.Count++
		return l.Set("counter", rec)
	}
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range rounds {
				if err := withLedger(state, counter); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	_ = withLedger(state, func(l *Ledger) error {
		var rec struct{ Count int }
		if ok, err := l.Get("counter", &rec); err != nil || !ok {
			t.Fatalf("Get = %v, %v", ok, err)
		}
		eq(t, "count", rec.Count, workers*rounds)
		return nil
	})
}

func TestLedgerReadsAPythonWrittenLedger(t *testing.T) {
	state := newState(t)
	data, err := os.ReadFile("../../tests/pack/fixtures/bash_writes/ledger_python.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "ledger.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	err = withLedger(state, func(l *Ledger) error {
		eq(t, "unreadable", l.Unreadable, false)
		var rec struct {
			Seen    map[string][4]int64 `json:"seen"`
			Pending []string            `json:"pending"`
		}
		if ok, err := l.Get("/work/repo", &rec); err != nil || !ok {
			t.Fatalf("Get = %v, %v", ok, err)
		}
		eq(t, "seen café", rec.Seen["café.sh"], [4]int64{1700000000000000000, 1700000000000000001, 12345, 42})
		eq(t, "pending", rec.Pending, []string{"café.sh"})
		return l.Set("/other", plainRecord)
	})
	if err != nil {
		t.Fatal(err)
	}
	written, _ := os.ReadFile(filepath.Join(state, "ledger.json"))
	var back map[string]json.RawMessage
	if err := json.Unmarshal(written, &back); err != nil {
		t.Fatal(err)
	}
	var orig map[string]json.RawMessage
	if err := json.Unmarshal(data, &orig); err != nil {
		t.Fatal(err)
	}
	eq(t, "roots", len(back), 2)
	var a, b any
	_ = json.Unmarshal(orig["/work/repo"], &a)
	_ = json.Unmarshal(back["/work/repo"], &b)
	eq(t, "python root survives the write-back", b, a)
	var kept struct{ Seen map[string]json.RawMessage }
	if err := json.Unmarshal(back["/work/repo"], &kept); err != nil {
		t.Fatal(err)
	}
	if _, ok := kept.Seen["café.sh"]; !ok {
		t.Fatalf("café.sh lost: %s", written)
	}
}

func TestLedgerKeepsANanosecondSignatureAbove2To53(t *testing.T) {
	state := newState(t)
	const big = `9007199254740993` // 2^53 + 1: a float64 cannot hold it
	writeFile(t, filepath.Join(state, "ledger.json"),
		`{"/r": {"seen": {"a.sh": [`+big+`, 1, 2, 3]}, "head": "h", "unknown": {"x": 1}}}`)
	_ = withLedger(state, func(l *Ledger) error { return l.Set("/s", plainRecord) })
	written, _ := os.ReadFile(filepath.Join(state, "ledger.json"))
	if !strings.Contains(string(written), big) {
		t.Fatalf("the signature was mangled: %s", written)
	}
	if !strings.Contains(string(written), `"unknown"`) {
		t.Fatalf("a field this version does not know was lost: %s", written)
	}
}

func TestSeedExcludesFilesChangedAtOrAfterThePreStart(t *testing.T) {
	repo := newRepo(t)
	writeFile(t, filepath.Join(repo, "a.sh"), "echo a\n")
	t0 := time.Now().UnixNano()
	time.Sleep(30 * time.Millisecond) // the kernel stamps ctime with a coarse clock that lags t0
	writeFile(t, filepath.Join(repo, "b.sh"), "echo b\n")

	record, err := seedRoot(repo, t0, time.Now().Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	seen := record["seen"].(map[string][]int64)
	if _, ok := seen["a.sh"]; !ok {
		t.Fatal("a.sh missing from seen")
	}
	if _, ok := seen["b.sh"]; ok {
		t.Fatal("b.sh is in seen")
	}
	eq(t, "pending", record["pending"], []string{})
	eq(t, "claims", record["claims"], map[string]any{})
}

func TestSeedIgnoresExtensionsOutsideTheGates(t *testing.T) {
	repo := newRepo(t)
	writeFile(t, filepath.Join(repo, "a.sh"), "echo a\n")
	writeFile(t, filepath.Join(repo, "a.txt"), "plain\n")
	writeFile(t, filepath.Join(repo, ".sh"), "dotfile\n")
	t0 := time.Now().UnixNano()

	record, err := seedRoot(repo, t0, time.Now().Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	seen := record["seen"].(map[string][]int64)
	if _, ok := seen["a.sh"]; !ok {
		t.Fatal("a.sh missing from seen")
	}
	if _, ok := seen["a.txt"]; ok {
		t.Fatal("a.txt is in seen")
	}
	if _, ok := seen[".sh"]; ok {
		t.Fatal(".sh has no extension and is in seen")
	}
}

func TestSeedOfAnUnbornRepoHasHeadUnborn(t *testing.T) {
	repo := t.TempDir()
	fixturesetup.Git(t, repo, "init", "-q", "-b", "main")
	record, err := seedRoot(repo, time.Now().UnixNano(), time.Now().Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "record", record, map[string]any{
		"seen": map[string][]int64{}, "head": "unborn", "pending": []string{}, "claims": map[string]any{},
	})
}

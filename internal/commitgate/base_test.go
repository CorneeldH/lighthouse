package commitgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
)

// runtimeDir points XDG_RUNTIME_DIR at a fresh directory and returns it.
func runtimeDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", dir)
	return dir
}

// A post runs after every Bash call: only record makes the state dir.
func TestMovedDoesNotCreateTheStateDir(t *testing.T) {
	runtime := runtimeDir(t)
	got, err := MovedRoots(fixturePayload)
	if err != nil || got != nil {
		t.Fatalf("MovedRoots = %v, %v; want nil, nil", got, err)
	}
	if _, err := os.Stat(filepath.Join(runtime, "lighthouse-commit-test-gate")); !os.IsNotExist(err) {
		t.Errorf("state dir exists after MovedRoots: %v", err)
	}
}

func TestMovedWithAReadOnlyRuntimeDirIsEmpty(t *testing.T) {
	runtime := runtimeDir(t)
	if err := os.Chmod(runtime, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(runtime, 0o700) })
	got, err := MovedRoots(fixturePayload)
	if err != nil || got != nil {
		t.Fatalf("MovedRoots = %v, %v; want nil, nil", got, err)
	}
}

func TestOldMarkersArePruned(t *testing.T) {
	runtimeDir(t)
	root := t.TempDir()
	fixturesetup.Build(t, root, []map[string]any{{"repo": "repo", "branch": "main"}})
	state, err := StateDir("s1", true)
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(state, "old.json")
	if err := os.WriteFile(stale, []byte(`{"heads": {}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stale, time.Unix(1, 0), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := Record(fixturePayload, []string{filepath.Join(root, "repo")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale marker still there: %v", err)
	}
}

func TestStateDirIsPrivateAndRefusesPathEscape(t *testing.T) {
	runtime := runtimeDir(t)
	dir, err := StateDir("s1", true)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(runtime, "lighthouse-commit-test-gate", "s1"); dir != want {
		t.Errorf("StateDir = %q, want %q", dir, want)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("state dir mode = %v, %v; want 0700", info, err)
	}
	for _, id := range []string{"a/b", ".."} {
		_, err := StateDir(id, true)
		if want := "invalid session_id: '" + id + "'"; err == nil || err.Error() != want {
			t.Errorf("StateDir(%q) err = %v, want %q", id, err, want)
		}
	}
}

// marker_python.json is the shape json.dumps writes (é): MovedRoots reads it. Its base is not
// what the repo has now, so the root moved.
func TestMovedReadsAPythonWrittenMarker(t *testing.T) {
	runtimeDir(t)
	root := t.TempDir()
	fixturesetup.Build(t, root, []map[string]any{{"unborn": "café"}, {"file": "café/a.txt", "text": "x\n"}, {"commit": "café", "message": "m"}})
	text, err := os.ReadFile(filepath.Join(fixtureDir, "commit_base", "marker_python.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "\\u00e9") {
		t.Fatal("the fixture no longer has an escaped root")
	}
	state, err := StateDir("s1", true)
	if err != nil {
		t.Fatal(err)
	}
	marker := fixturesetup.Fill(t, string(text), root)
	if err := os.WriteFile(filepath.Join(state, "t1.json"), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := MovedRoots(fixturePayload)
	if err != nil {
		t.Fatal(err)
	}
	want := []Moved{{Root: filepath.Join(root, "café"), Base: strings.Repeat("1", 40), Head: fixturesetup.Fill(t, "{rev:café:HEAD}", root)}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("MovedRoots = %v, want %v", got, want)
	}
}

// What Record writes for a non-ASCII root is read back by MovedRoots.
func TestRecordedNonASCIIRootRoundTrips(t *testing.T) {
	runtimeDir(t)
	root := t.TempDir()
	fixturesetup.Build(t, root, []map[string]any{{"unborn": "café"}})
	repo := filepath.Join(root, "café")
	if err := Record(fixturePayload, []string{repo}); err != nil {
		t.Fatal(err)
	}
	fixturesetup.Build(t, root, []map[string]any{{"file": "café/a.txt", "text": "x\n"}, {"commit": "café", "message": "m"}})
	got, err := MovedRoots(fixturePayload)
	want := Moved{Root: repo, Base: EmptyTree, Head: fixturesetup.Fill(t, "{rev:café:HEAD}", root)}
	if err != nil || len(got) != 1 || got[0] != want {
		t.Errorf("MovedRoots = %v, %v; want %v", got, err, want)
	}
}

func TestMovedErrorsAreNamedAfterTheModule(t *testing.T) {
	for name, marker := range map[string]string{
		"not json":     "{",
		"no heads":     `{"other": {}}`,
		"head no text": `{"heads": {"/x": 5}}`,
	} {
		t.Run(name, func(t *testing.T) {
			runtimeDir(t)
			state, err := StateDir("s1", true)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(state, "t1.json"), []byte(marker), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := MovedRoots(fixturePayload)
			if err == nil || !strings.HasPrefix(err.Error(), "commit_base: ") || got != nil {
				t.Errorf("MovedRoots = %v, %v; want a commit_base: error", got, err)
			}
		})
	}
}

func TestChangedFilesGitFailureIsAnError(t *testing.T) {
	root := t.TempDir()
	fixturesetup.Build(t, root, []map[string]any{{"repo": "repo", "branch": "main"}})
	head := fixturesetup.Fill(t, "{rev:repo:HEAD}", root)
	got, err := ChangedFiles(filepath.Join(root, "repo"), strings.Repeat("0", 40), head)
	if err == nil || !strings.HasPrefix(err.Error(), "commit_base: ") || got != nil {
		t.Errorf("ChangedFiles = %v, %v; want a commit_base: error", got, err)
	}
}

func TestStateBaseFallsBackPerPlatform(t *testing.T) {
	for _, c := range []struct{ goos, xdg, want string }{
		{"linux", "/run/user/1000", "/run/user/1000"},
		{"linux", "", "/tmp"},
		{"darwin", "", "/tmp"},
		{"windows", "", "C:/Temp"},
		{"windows", "D:/rt", "D:/rt"},
	} {
		if got := stateBase(c.goos, c.xdg, "C:/Temp"); got != c.want {
			t.Errorf("stateBase(%s, %q) = %q, want %q", c.goos, c.xdg, got, c.want)
		}
	}
}

package gitinvocations

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
)

// The twins of the inline tests of tests/pack/test_git_invocations.py. That file drove the
// Python CLI (`git_invocations.py <cwd>` with the command on stdin, one tab-separated line per
// invocation); here Find is the same contract without the process.

func repoAt(t *testing.T, branch string) string {
	t.Helper()
	root := t.TempDir()
	fixturesetup.Build(t, root, []map[string]any{{"repo": "x", "branch": branch}})
	return filepath.Join(root, "x")
}

// twin of test_cli_contract
func TestFindContract(t *testing.T) {
	x := repoAt(t, "main")
	got, err := Find("git commit -m x", x, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []Invocation{{Action: "commit", Target: x, ToMain: "no", Raw: x}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Find = %+v, want %+v", got, want)
	}
	if _, err := Find("git commit -m 'x", x, Options{}); !errors.Is(err, ErrUnparseable) {
		t.Fatalf("open quote: err = %v, want ErrUnparseable (the CLI's exit 3)", err)
	}
}

// twin of test_cli_takes_the_default_branch
func TestFindTakesTheDefaultBranch(t *testing.T) {
	x := repoAt(t, "issue-1-x")
	got, err := Find("git push origin HEAD:trunk", x, Options{DefaultBranch: "trunk"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []Invocation{{Action: "push", Target: x, ToMain: "yes", Raw: x}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Find = %+v, want %+v", got, want)
	}
}

// twin of test_markers_are_english_tokens
func TestMarkersAreEnglishTokens(t *testing.T) {
	if Unresolved != "<unknown>" || NewDir != "<new>" {
		t.Fatalf("markers %q %q, want <unknown> <new>", Unresolved, NewDir)
	}
}

// twin of test_import_reads_no_stdin: the Python module once read its command from stdin at
// import. Find takes the command as an argument; with a stdin that never ends it still returns.
func TestFindReadsNoStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	defer func() { _ = r.Close() }()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	x := repoAt(t, "main")
	done := make(chan error, 1)
	go func() {
		_, err := Find("git commit -m x", x, Options{})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Find blocked on stdin")
	}
}

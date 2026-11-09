package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/review"
)

func runReviewPair(t *testing.T, deps review.PairDeps, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.AddCommand(newReviewPairCommand(deps))
	root.SetArgs(append([]string{"review-pair"}, args...))
	code = execute(root, &errOut)
	return out.String(), errOut.String(), code
}

func TestReviewPairUsageErrorsExitTwo(t *testing.T) {
	for name, args := range map[string][]string{
		"no subcommand":   {},
		"no attempt dir":  {"blind"},
		"extra arguments": {"merge", "a", "b"},
		"unknown":         {"tally", "a"},
		"unknown flag":    {"blind", "--x", "a"},
	} {
		out, errOut, code := runReviewPair(t, review.PairDeps{}, args...)
		if code != 2 || out != "" || errOut == "" {
			t.Errorf("%s: code %d, stdout %q, stderr %q; want 2, nothing, a message", name, code, out, errOut)
		}
	}
}

// A failing run is exit 4 with the literal stderr line and nothing on stdout.
func TestReviewPairFailureIsExitFour(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "attempt.json"), []byte(`{"kind":"plan","reviewed_sha":"abc"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_REPO_ROOT", t.TempDir())
	deps := review.PairDeps{
		Coin:      func() bool { return true },
		TreeState: func(string) (string, bool, error) { return "def", true, nil },
	}
	out, errOut, code := runReviewPair(t, deps, "blind", dir)
	if code != 4 || out != "" || errOut != "SHADOW-PAIR FAILED: work tree changed since start or not clean\n" {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestReviewPairIsRegistered(t *testing.T) {
	root := NewRoot(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	for _, sub := range []string{"blind", "merge"} {
		if cmd, _, err := root.Find([]string{"review-pair", sub}); err != nil || cmd.Name() != sub {
			t.Errorf("Find(review-pair %s) = %v, %v", sub, cmd, err)
		}
	}
}

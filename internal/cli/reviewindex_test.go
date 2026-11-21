package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/store"
)

// indexScenario is a scenario whose plan review wrote one record on the branch issue-12, with
// findings, under forge.repo CorneeldH/lighthouse. HOME and XDG_STATE_HOME are tempdirs, so
// store.DefaultPath never reaches the real lighthouse.db.
func indexScenario(t *testing.T) *scenario {
	t.Helper()
	s := newScenario(t, map[string]any{})
	mustWrite(t, filepath.Join(s.vars["tmp"], "lighthouse.toml"),
		"[project]\nlanguage = \"nl\"\n[forge]\nrepo = \"CorneeldH/lighthouse\"\n")
	s.git("checkout", "-q", "-b", "issue-12")
	s.runStep(step{"op": "run", "args": []any{"start", "${PLAN}"}, "bind": "a"})
	s.roundStep(step{"attempt": "a", "n": float64(1), "severities": []any{"MAJOR"}})
	return s
}

func (s *scenario) writeRecord() (stdout, stderr string) {
	s.t.Helper()
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.AddCommand(newPlanReviewRecordCommand(s.deps))
	root.SetArgs([]string{"plan-review-record", "write", s.vars["a"], "--outcome", "aborted",
		"--review-mode", "codex", "--spec", s.vars["SPEC"]})
	if code := execute(root, &errOut); code != 0 {
		s.t.Fatalf("write: exit %d\nstderr: %s", code, errOut.String())
	}
	return out.String(), errOut.String()
}

func runReviewIndex(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.AddCommand(newReviewIndexCommand())
	root.SetArgs(append([]string{"review-index"}, args...))
	code = execute(root, &errOut)
	return code, out.String(), errOut.String()
}

func countFindings(t *testing.T) int {
	t.Helper()
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.OpenShared(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	var n int
	if err := st.DB.QueryRow(`SELECT COUNT(*) FROM findings`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestReviewIndexIndexesDataRoot(t *testing.T) {
	s := indexScenario(t)
	// Write the record on a machine whose database cannot be opened, so only review-index indexes it.
	t.Setenv("XDG_STATE_HOME", filepath.Join(s.vars["tmp"], "blocked", "x"))
	mustWrite(t, filepath.Join(s.vars["tmp"], "blocked"), "a file, not a directory")
	if _, stderr := s.writeRecord(); !strings.Contains(stderr, "REVIEW-INDEX FAILED") {
		t.Fatalf("stderr = %q, want REVIEW-INDEX FAILED", stderr)
	}
	t.Setenv("XDG_STATE_HOME", filepath.Join(s.vars["tmp"], "state"))

	code, out, errOut := runReviewIndex(t)
	if code != 0 || out != "indexed: new=1 updated=0 unchanged=0 removed=0\n" {
		t.Fatalf("first run: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	code, out, errOut = runReviewIndex(t)
	if code != 0 || out != "indexed: new=0 updated=0 unchanged=1 removed=0\n" {
		t.Fatalf("second run: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestReviewIndexEmptyDataRoot(t *testing.T) {
	s := newScenario(t, map[string]any{})
	if err := os.RemoveAll(s.vars["data"]); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runReviewIndex(t)
	if code != 0 || out != "indexed: new=0 updated=0 unchanged=0 removed=0\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestReviewIndexLedgerFlag(t *testing.T) {
	s := newScenario(t, map[string]any{})
	mustWrite(t, filepath.Join(s.vars["tmp"], "lighthouse.toml"),
		"[project]\nlanguage = \"nl\"\n[forge]\nrepo = \"CorneeldH/lighthouse\"\n")
	ledger := "../review/testdata/sdd-ledger-ruling.md"

	code, out, errOut := runReviewIndex(t, "--ledger", ledger, "--issue", "376")
	if code != 0 || !strings.HasPrefix(out, "indexed: new=2 ") {
		t.Fatalf("with --issue: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if n := countFindings(t); n != 2 {
		t.Errorf("findings = %d, want 2", n)
	}

	// A ledger whose plan path names no issue and without --issue is skipped with a reason.
	plain := filepath.Join(s.vars["tmp"], "ledger.md")
	mustWrite(t, plain, "# SDD ledger — plan: docs/plans/zonder-nummer.md\nTask 1: Ruling: iets — waarom\n")
	code, out, errOut = runReviewIndex(t, "--ledger", plain)
	if code != 0 || !strings.Contains(out, "skipped: "+plain+": ") {
		t.Fatalf("without issue: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if n := countFindings(t); n != 2 {
		t.Errorf("findings after skipped ledger = %d, want 2", n)
	}

	if code, _, _ := runReviewIndex(t, "stray"); code != 2 {
		t.Errorf("positional argument: exit %d, want 2", code)
	}
	if code, _, _ := runReviewIndex(t, "--ledger", filepath.Join(s.vars["tmp"], "missing.md"), "--issue", "1"); code != 1 {
		t.Errorf("missing ledger: exit %d, want 1", code)
	}
}

func TestRecordWriteIndexes(t *testing.T) {
	s := indexScenario(t)
	if _, stderr := s.writeRecord(); stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if n := countFindings(t); n != 1 {
		t.Errorf("findings after write = %d, want 1", n)
	}
}

func TestRecordWriteIndexFailureIsWarning(t *testing.T) {
	s := indexScenario(t)
	mustWrite(t, filepath.Join(s.vars["tmp"], "blocked"), "a file, not a directory")
	t.Setenv("XDG_STATE_HOME", filepath.Join(s.vars["tmp"], "blocked", "x"))
	stdout, stderr := s.writeRecord()
	if !strings.Contains(stderr, "REVIEW-INDEX FAILED: ") {
		t.Errorf("stderr = %q, want REVIEW-INDEX FAILED", stderr)
	}
	if _, err := os.Stat(filepath.Join(strings.TrimSpace(stdout), "record.json")); err != nil {
		t.Errorf("record.json missing: %v", err)
	}
}

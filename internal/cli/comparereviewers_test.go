package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/store"
)

func runCompareReviewers(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.AddCommand(newCompareReviewersCommand())
	root.SetArgs(append([]string{"compare-reviewers"}, args...))
	code = execute(root, &errOut)
	return code, out.String(), errOut.String()
}

func writeCompareRecord(t *testing.T, root, chain, body string) {
	t.Helper()
	dir := filepath.Join(root, "plan-review", chain, "20260101T000000Z")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "record.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedCompareDB creates the migrated database at the default path with trajectories o/r#n
// (route per n from routeOf), then closes it.
func seedCompareDB(t *testing.T, n int, routeOf func(int) string) {
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
	for i := 1; i <= n; i++ {
		if _, err := st.DB.Exec(`INSERT INTO trajectories (repo, issue, created_at, route) VALUES ('o/r', ?, '2026-03-02T09:00:00Z', ?)`,
			i, routeOf(i)); err != nil {
			t.Fatal(err)
		}
	}
}

func compareEnv(t *testing.T) string {
	t.Helper()
	data := t.TempDir()
	t.Setenv("LIGHTHOUSE_DATA", data)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	return data
}

func chainRecord(issue int, decision, extra string) string {
	return fmt.Sprintf(`{"chain_key":"issue-%d-x","review_mode":"codex","paired":true,`+
		`"reviewer":{"codex_version":"codex 1"},"trajectory":{"repo":"o/r","issue":%d},%s`+
		`"findings":[{"finding_id":"F1","decision":%q,"round":1,"source":"both",`+
		`"severity_by_source":{"codex":"MAJOR","fallback":"MAJOR"}}]}`, issue, issue, extra, decision)
}

func TestCompareReviewers_PrintsDroppedAndNoConclusion(t *testing.T) {
	data := compareEnv(t)
	// 10 trajectories on route big (adopted MAJOR for both), 3 on route small (rejected only)
	seedCompareDB(t, 13, func(i int) string {
		if i <= 10 {
			return "big"
		}
		return "small"
	})
	for i := 1; i <= 13; i++ {
		d := "accept"
		if i > 10 {
			d = "reject"
		}
		writeCompareRecord(t, data, fmt.Sprintf("c%d", i), chainRecord(i, d, ""))
	}
	writeCompareRecord(t, data, "fb", `{"review_mode":"fallback","paired":true}`)
	writeCompareRecord(t, data, "nover", `{"review_mode":"codex","paired":true,"reviewer":{"codex_version":null}}`)

	code, out, errOut := runCompareReviewers(t)
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, errOut)
	}
	for _, want := range []string{
		"Geselecteerd: 13 records. Afgevallen: 2.",
		"| fallback (review_mode fallback) | 1 |",
		"| zonder reviewer.codex_version | 1 |",
		"- Alleen Codex: 10 van 10 overgenomen BLOCKER/MAJOR (100%), Wilson 95%-interval 72% tot 100%: 90% niet uitgesloten, niet bevestigd.",
		"| route | big | 10 | 10 | 10 van 10 | 0 hersteld, 0 open | gemeten |",
		"| route | small | 3 | 3 | niet gedefinieerd | 0 hersteld, 0 open | geen conclusie |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "| - | - |") {
		t.Errorf("the empty-class placeholder must not appear:\n%s", out)
	}
}

func TestCompareReviewers_NoFindingsIsNotDefined(t *testing.T) {
	compareEnv(t)
	seedCompareDB(t, 0, nil)
	code, out, errOut := runCompareReviewers(t)
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, errOut)
	}
	want := "- Alleen Codex: niet gedefinieerd (geen overgenomen BLOCKER- of MAJOR-bevindingen)."
	if !strings.Contains(out, want) || strings.Contains(out, "NaN") {
		t.Errorf("want %q and no NaN:\n%s", want, out)
	}
}

func TestCompareReviewers_NamesNotAppliedAssignment(t *testing.T) {
	data := compareEnv(t)
	seedCompareDB(t, 1, func(int) string { return "r" })
	writeCompareRecord(t, data, "c1", chainRecord(1, "accept", `"assignment":"x","assignment_applied":false,`))
	_, out, _ := runCompareReviewers(t)
	want := "- Niet-toegepaste indeling `x` (assignment_applied: false): 1."
	if !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
}

func TestCompareReviewers_MissingDatabaseIsExit1AndCreatesNothing(t *testing.T) {
	compareEnv(t)
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runCompareReviewers(t)
	if code != 1 || !strings.Contains(errOut, "no database at") {
		t.Errorf("want exit 1 with a clear message, got %d: %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("the state directory must not be created, stat err: %v", err)
	}
}

func TestCompareReviewers_RejectsArguments(t *testing.T) {
	if code, _, _ := runCompareReviewers(t, "x"); code != 2 {
		t.Errorf("want exit 2, got %d", code)
	}
}

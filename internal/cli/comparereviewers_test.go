package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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

func TestCompareReviewers_PrintsDroppedAndNoConclusion(t *testing.T) {
	data := t.TempDir()
	t.Setenv("LIGHTHOUSE_DATA", data)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	writeCompareRecord(t, data, "good", `{"chain_key":"issue-7-x","review_mode":"codex","paired":true,`+
		`"reviewer":{"codex_version":"codex 1"},"trajectory":{"repo":"o/r","issue":7},`+
		`"findings":[{"finding_id":"F1","decision":"accept","round":1,"source":"both",`+
		`"severity_by_source":{"codex":"MAJOR","fallback":"MINOR"}}]}`)
	writeCompareRecord(t, data, "fb", `{"review_mode":"fallback","paired":true}`)
	writeCompareRecord(t, data, "nover", `{"review_mode":"codex","paired":true,"reviewer":{"codex_version":null}}`)

	code, out, errOut := runCompareReviewers(t)
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, errOut)
	}
	for _, want := range []string{"Geselecteerd: 1 records. Afgevallen: 2.", "afgevallen", "| fallback (review_mode fallback) | 1 |",
		"| zonder reviewer.codex_version | 1 |", "geen conclusie", "Alleen Codex: 1 van 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestCompareReviewers_NoFindingsIsNotDefined(t *testing.T) {
	data := t.TempDir()
	t.Setenv("LIGHTHOUSE_DATA", data)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	code, out, errOut := runCompareReviewers(t)
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, errOut)
	}
	if !strings.Contains(out, "niet gedefinieerd") || strings.Contains(out, "NaN") {
		t.Errorf("want 'niet gedefinieerd' and no NaN:\n%s", out)
	}
}

func TestCompareReviewers_RejectsArguments(t *testing.T) {
	if code, _, _ := runCompareReviewers(t, "x"); code != 2 {
		t.Errorf("want exit 2, got %d", code)
	}
}

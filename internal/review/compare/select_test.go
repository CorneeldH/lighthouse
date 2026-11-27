package compare

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/review"
)

// writeRecord writes <root>/<kind>-review/<chain>/<attempt>/record.json.
func writeRecord(t *testing.T, root, kind, chain, attempt, body string) {
	t.Helper()
	dir := filepath.Join(root, kind+"-review", chain, attempt)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "record.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const trajectory = `"trajectory":{"repo":"o/r","issue":7}`

func rec(mode string, paired bool, version string, extra string) string {
	v := "null"
	if version != "" {
		v = `"` + version + `"`
	}
	p := "false"
	if paired {
		p = "true"
	}
	s := `{"schema_version":3,"chain_key":"issue-7-x","review_mode":"` + mode + `","paired":` + p +
		`,"reviewer":{"codex_version":` + v + `},` + trajectory +
		`,"findings":[{"finding_id":"F1","severity":"MAJOR","decision":"accept","round":1,"source":"both",` +
		`"severity_by_source":{"codex":"MAJOR","fallback":"MINOR"}},{"finding_id":"F2","severity":"MINOR","round":2}]`
	if extra != "" {
		s += "," + extra
	}
	return s + "}"
}

func TestSelect_KeepsOnlyPairedCodexWithVersion(t *testing.T) {
	root := t.TempDir()
	writeRecord(t, root, "plan", "good", "20260101T000000Z", rec("codex", true, "codex 1.2", `"assignment":"standard","assignment_applied":true`))
	writeRecord(t, root, "plan", "fb", "20260101T000000Z", rec("fallback", true, "codex 1.2", ""))
	writeRecord(t, root, "plan", "nover", "20260101T000000Z", rec("codex", true, "", ""))
	writeRecord(t, root, "plan", "solo", "20260101T000000Z", rec("codex", false, "codex 1.2", ""))
	// the newest attempt is only staged
	writeRecord(t, root, "spec", "staged", "20260101T000000Z", rec("codex", true, "codex 1.2", ""))
	if err := os.MkdirAll(filepath.Join(root, "spec-review", "staged", ".20260102T000000Z.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}

	kept, dropped, err := Select(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 {
		t.Fatalf("kept = %d, want 1: %+v", len(kept), kept)
	}
	want := Dropped{Fallback: 1, NoCodexVersion: 1, NotPaired: 1, NotFinished: 1}
	if dropped != want {
		t.Errorf("dropped = %+v, want %+v", dropped, want)
	}
	r := kept[0]
	if r.Kind != "plan" || r.Chain != "good" || !r.Paired || r.Assignment != "standard" ||
		r.AssignmentApplied == nil || !*r.AssignmentApplied || !r.Linked || r.Repo != "o/r" || r.Issue != 7 {
		t.Errorf("record = %+v", r)
	}
	if len(r.Findings) != 2 {
		t.Fatalf("findings = %+v", r.Findings)
	}
	f := r.Findings[0]
	if f.ID != "F1" || f.Source != "both" || f.Severity != "MAJOR" || f.SevCodex != "MAJOR" ||
		f.SevFallback != "MINOR" || f.Decision != "adopted" || f.Round != 1 {
		t.Errorf("finding = %+v", f)
	}
	if g := r.Findings[1]; g.Round != 2 || g.Source != "" || g.Decision != "" {
		t.Errorf("round 2 finding = %+v", g)
	}
}

func TestSelect_OtherModeCounted(t *testing.T) {
	root := t.TempDir()
	writeRecord(t, root, "plan", "a", "20260101T000000Z", rec("agents", true, "codex 1", ""))
	kept, dropped, err := Select(root, nil)
	if err != nil || len(kept) != 0 || dropped != (Dropped{OtherMode: 1}) {
		t.Errorf("kept=%v dropped=%+v err=%v", kept, dropped, err)
	}
}

func TestSelect_UnreadableCounted(t *testing.T) {
	root := t.TempDir()
	writeRecord(t, root, "plan", "a", "20260101T000000Z", "{not json")
	kept, dropped, err := Select(root, nil)
	if err != nil || len(kept) != 0 || dropped != (Dropped{Unreadable: 1}) {
		t.Errorf("kept=%v dropped=%+v err=%v", kept, dropped, err)
	}
}

func TestSelect_NewestAttemptPerChain(t *testing.T) {
	root := t.TempDir()
	writeRecord(t, root, "plan", "c", "20260101T000000Z", rec("codex", true, "codex 1", ""))
	writeRecord(t, root, "plan", "c", "20260102T000000Z", rec("fallback", true, "codex 1", ""))
	kept, dropped, err := Select(root, nil)
	if err != nil || len(kept) != 0 || dropped != (Dropped{Fallback: 1}) {
		t.Errorf("kept=%v dropped=%+v err=%v", kept, dropped, err)
	}
}

func gitRepo(t *testing.T) (dir, sha string) {
	t.Helper()
	dir = t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return dir, strings.TrimSpace(string(out))
}

func TestSelect_AmbiguousRepoNotLinked(t *testing.T) {
	root := t.TempDir()
	dirA, sha := gitRepo(t)
	dirB := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", dirA, dirB+"/c").CombinedOutput(); err != nil {
		t.Fatalf("clone: %v: %s", err, out)
	}
	body := strings.Replace(rec("codex", true, "codex 1", `"reviewed_sha":"`+sha+`"`), trajectory+",", "", 1)
	writeRecord(t, root, "plan", "issue-7-x", "20260101T000000Z", body)

	both := []review.ServedRepo{{Slug: "o/a", Dir: dirA}, {Slug: "o/b", Dir: dirB + "/c"}}
	kept, _, err := Select(root, both)
	if err != nil || len(kept) != 1 {
		t.Fatalf("kept=%v err=%v", kept, err)
	}
	if kept[0].Linked || kept[0].Repo != "" || len(kept[0].Findings) != 2 {
		t.Errorf("ambiguous record = %+v", kept[0])
	}

	one := []review.ServedRepo{{Slug: "o/a", Dir: dirA}, {Slug: "o/c", Dir: t.TempDir()}}
	kept, _, err = Select(root, one)
	if err != nil || len(kept) != 1 || !kept[0].Linked || kept[0].Repo != "o/a" || kept[0].Issue != 7 {
		t.Errorf("traced record = %+v err=%v", kept, err)
	}
}

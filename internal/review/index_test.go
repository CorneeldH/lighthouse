package review

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/store"
)

func openIndexDB(t *testing.T) *sql.DB {
	t.Helper()
	s, err := store.OpenShared(context.Background(), filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatalf("OpenShared: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s.DB
}

// writeChain creates <root>/<kind>-review/<chain>/<attempt>/record.json, or
// only .<attempt>.tmp/attempt.json when record is nil.
func writeChain(t *testing.T, root, kind, chain, attempt string, record map[string]any) string {
	t.Helper()
	dir := filepath.Join(root, kind+"-review", chain, attempt)
	name, body := "record.json", []byte(nil)
	if record == nil {
		dir = filepath.Join(root, kind+"-review", chain, "."+attempt+".tmp")
		name, body = "attempt.json", []byte("{}")
	} else {
		var err error
		if body, err = json.Marshal(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func rec(issue int, findings ...[3]string) map[string]any {
	fs := []any{}
	for _, f := range findings {
		var d any
		if f[2] != "" {
			d = f[2]
		}
		fs = append(fs, map[string]any{"finding_id": f[0], "severity": f[1], "decision": d})
	}
	return map[string]any{
		"schema_version": 6, "issue_spec": nil,
		"trajectory": map[string]any{"repo": "CorneeldH/lighthouse", "issue": issue},
		"findings":   fs, "written_at": "2026-11-20T10:00:00+00:00",
	}
}

func rows(t *testing.T, db *sql.DB, where string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM findings WHERE ` + where).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var fixedNow = func() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) }

func TestFindingDecision(t *testing.T) {
	for in, want := range map[string]string{
		"accept": "adopted", "spec": "adopted", "minor_yes": "adopted",
		"reject": "rejected", "minor_no": "rejected",
		"accepted_open": "left_open", "defer": "left_open", "": "",
	} {
		if got := FindingDecision(in); got != want {
			t.Errorf("FindingDecision(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIndexRecordsLinksFindings(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	path := writeChain(t, root, "plan", "c1", "A1", rec(376, [3]string{"r1-f1", "BLOCKER", "accept"}, [3]string{"r1-f2", "MINOR", "minor_no"}))
	data, _ := os.ReadFile(path)
	res, err := IndexRecords(ctx, db, root, fixedNow, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 2 || len(res.Skipped) != 0 {
		t.Fatalf("result = %+v, want New 2", res)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM findings f JOIN trajectories t ON t.id = f.trajectory_id
		WHERE t.repo = 'CorneeldH/lighthouse' AND t.issue = 376 AND f.component = 'plan_review'
		AND f.record_sum = ? AND f.record_path = ? AND f.source = 'plan-review-record'
		AND ((f.finding_key = 'r1-f1' AND f.decision = 'adopted' AND f.severity = 'BLOCKER' AND f.source_id = 'plan/c1/A1/r1-f1')
		  OR (f.finding_key = 'r1-f2' AND f.decision = 'rejected' AND f.severity = 'MINOR'))`,
		Sum(data), path).Scan(&n); err != nil || n != 2 {
		t.Errorf("matching rows = %d (%v), want 2", n, err)
	}
}

func TestIndexRecordsDetectsChangedFile(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	writeChain(t, root, "plan", "c1", "A1", rec(376, [3]string{"r1-f1", "MAJOR", "accept"}, [3]string{"r1-f2", "MINOR", "accept"}))
	if _, err := IndexRecords(ctx, db, root, fixedNow, nil); err != nil {
		t.Fatal(err)
	}
	writeChain(t, root, "plan", "c1", "A1", rec(376, [3]string{"r1-f1", "MAJOR", "reject"}, [3]string{"r1-f2", "MINOR", "reject"}))
	res, err := IndexRecords(ctx, db, root, fixedNow, nil)
	if err != nil || res.Updated != 2 || res.New != 0 || res.Unchanged != 0 {
		t.Fatalf("second = %+v, %v; want Updated 2", res, err)
	}
	if n := rows(t, db, `decision = 'rejected'`); n != 2 {
		t.Errorf("rejected rows = %d, want 2", n)
	}
	if n := rows(t, db, `1 = 1`); n != 2 {
		t.Errorf("rows = %d, want 2", n)
	}
	res, err = IndexRecords(ctx, db, root, fixedNow, nil)
	if err != nil || res.Unchanged != 2 || res.Updated != 0 {
		t.Errorf("third = %+v, %v; want Unchanged 2", res, err)
	}
}

func TestIndexRecordsNewestAttemptWins(t *testing.T) {
	two := rec(376, [3]string{"r1-f1", "MAJOR", "accept"}, [3]string{"r1-f2", "MINOR", "accept"})
	t.Run("newest attempt only staged", func(t *testing.T) {
		ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
		writeChain(t, root, "plan", "c1", "A1", two)
		if res, err := IndexRecords(ctx, db, root, fixedNow, nil); err != nil || res.New != 2 {
			t.Fatalf("first = %+v, %v", res, err)
		}
		writeChain(t, root, "plan", "c1", "A2", nil)
		res, err := IndexRecords(ctx, db, root, fixedNow, nil)
		if err != nil || res.Removed != 2 {
			t.Fatalf("second = %+v, %v; want Removed 2", res, err)
		}
		if n := rows(t, db, `1 = 1`); n != 0 {
			t.Errorf("rows = %d, want 0", n)
		}
	})
	t.Run("newest attempt has record", func(t *testing.T) {
		ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
		writeChain(t, root, "plan", "c1", "A1", two)
		if _, err := IndexRecords(ctx, db, root, fixedNow, nil); err != nil {
			t.Fatal(err)
		}
		writeChain(t, root, "plan", "c1", "A2", rec(376, [3]string{"r1-f1", "MAJOR", "accept"}))
		res, err := IndexRecords(ctx, db, root, fixedNow, nil)
		if err != nil || res.Removed != 2 || res.New != 1 {
			t.Fatalf("second = %+v, %v; want Removed 2, New 1", res, err)
		}
		if n := rows(t, db, `source_id = 'plan/c1/A2/r1-f1'`); n != 1 {
			t.Errorf("A2 rows = %d, want 1", n)
		}
		if n := rows(t, db, `1 = 1`); n != 1 {
			t.Errorf("rows = %d, want 1", n)
		}
	})
}

func TestIndexRecordsSkipsWithoutTrajectory(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	null := rec(1, [3]string{"r1-f1", "MAJOR", "accept"})
	null["trajectory"] = nil
	writeChain(t, root, "plan", "null", "A1", null)
	old := rec(1, [3]string{"r1-f1", "MAJOR", "accept"})
	old["schema_version"] = 5
	delete(old, "trajectory")
	writeChain(t, root, "spec", "old", "A1", old)
	res, err := IndexRecords(ctx, db, root, fixedNow, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := rows(t, db, `1 = 1`); n != 0 || res.New != 0 {
		t.Errorf("rows = %d, New = %d, want none", n, res.New)
	}
	if len(res.Skipped) != 2 {
		t.Fatalf("Skipped = %+v, want 2", res.Skipped)
	}
	for _, s := range res.Skipped {
		if !strings.Contains(s.Reason, "trajectory") || s.Path == "" {
			t.Errorf("skip = %+v, want a reason naming trajectory and a path", s)
		}
	}
}

func TestIndexRecordsSpecIsIssue(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	r := rec(42, [3]string{"r1-f1", "MAJOR", "accept"})
	r["issue_spec"] = map[string]any{"issue": 42, "file": "issue-42.md"}
	writeChain(t, root, "plan", "c1", "A1", r)
	if res, err := IndexRecords(ctx, db, root, fixedNow, nil); err != nil || res.New != 1 {
		t.Fatalf("result = %+v, %v", res, err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM findings f JOIN trajectories t ON t.id = f.trajectory_id WHERE t.issue = 42`).Scan(&n); err != nil || n != 1 {
		t.Errorf("linked rows = %d (%v), want 1", n, err)
	}
}

func TestIndexRecordsBadChainDoesNotStopTheRest(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	bad := rec(1, [3]string{"r1-f1", "MAJOR", "accept"})
	bad["written_at"] = "0001-01-01T00:00:00Z"
	writeChain(t, root, "plan", "a-bad", "A1", bad)
	unreadable := writeChain(t, root, "plan", "b-unreadable", "A1", rec(2, [3]string{"r1-f1", "MAJOR", "accept"}))
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	writeChain(t, root, "plan", "c-good", "A1", rec(3, [3]string{"r1-f1", "MAJOR", "accept"}))
	res, err := IndexRecords(ctx, db, root, fixedNow, nil)
	if err != nil {
		t.Fatalf("IndexRecords: %v", err)
	}
	if res.New != 1 || rows(t, db, `source_id LIKE 'plan/c-good/%'`) != 1 {
		t.Errorf("res = %+v, want the good chain indexed", res)
	}
	want := 1
	if os.Geteuid() != 0 {
		want = 2
	}
	if len(res.Skipped) != want {
		t.Errorf("Skipped = %+v, want %d", res.Skipped, want)
	}
}

func runRows(t *testing.T, db *sql.DB, where string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM review_runs WHERE ` + where).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIndexRecordsWritesRunWithoutFindings(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	r := rec(376)
	r["outcome"] = "converged"
	path := writeChain(t, root, "plan", "c1", "A1", r)
	data, _ := os.ReadFile(path)
	res, err := IndexRecords(ctx, db, root, fixedNow, nil)
	if err != nil || res.Runs != 1 || res.New != 0 || len(res.Skipped) != 0 {
		t.Fatalf("res = %+v, %v; want Runs 1", res, err)
	}
	if n := runRows(t, db, `finding_count = 0 AND outcome = 'converged' AND component = 'plan_review'
		AND source = 'plan-review-record' AND record_sum = '`+Sum(data)+`' AND trajectory_id IS NOT NULL
		AND ran_at LIKE '2026-11-20T10:00:00%'`); n != 1 {
		t.Errorf("matching run rows = %d, want 1", n)
	}
}

func TestIndexRecordsUnlinkedRecordGetsRunAndSkip(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	null := rec(1, [3]string{"r1-f1", "MAJOR", "accept"})
	null["trajectory"] = nil
	writeChain(t, root, "spec", "u", "A1", null)
	res, err := IndexRecords(ctx, db, root, fixedNow, nil)
	if err != nil || res.Runs != 1 || len(res.Skipped) != 1 {
		t.Fatalf("res = %+v, %v; want Runs 1 and one Skip", res, err)
	}
	if n := runRows(t, db, `trajectory_id IS NULL AND unlinked_reason = 'no_trajectory' AND component = 'spec_review'`); n != 1 {
		t.Errorf("unlinked run rows = %d, want 1", n)
	}
	if n := rows(t, db, `1 = 1`); n != 0 {
		t.Errorf("finding rows = %d, want 0", n)
	}
}

func TestIndexRecordsUnreadableRecordWritesNoRun(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	bad := rec(1)
	bad["written_at"] = "nonsense"
	writeChain(t, root, "plan", "bad", "A1", bad)
	path := writeChain(t, root, "plan", "json", "A1", rec(2))
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := IndexRecords(ctx, db, root, fixedNow, nil)
	if err != nil || len(res.Skipped) != 2 || res.Runs != 0 {
		t.Fatalf("res = %+v, %v; want 2 skips, no runs", res, err)
	}
	if n := runRows(t, db, `1 = 1`); n != 0 {
		t.Errorf("run rows = %d, want 0", n)
	}
}

func TestIndexRecordsSameSumChangesNothing(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	writeChain(t, root, "plan", "c1", "A1", rec(376, [3]string{"r1-f1", "MAJOR", "accept"}))
	writeChain(t, root, "plan", "c2", "A1", rec(377))
	if _, err := IndexRecords(ctx, db, root, fixedNow, nil); err != nil {
		t.Fatal(err)
	}
	res, err := IndexRecords(ctx, db, root, fixedNow, nil)
	if err != nil || res.Runs != 0 || res.New != 0 || res.Updated != 0 || res.Removed != 0 || res.Unchanged != 1 {
		t.Fatalf("res = %+v, %v; want only Unchanged 1", res, err)
	}
	if n := runRows(t, db, `1 = 1`); n != 2 {
		t.Errorf("run rows = %d, want 2", n)
	}
}

func TestIndexRecordsNewerAttemptReplacesRun(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	writeChain(t, root, "plan", "c1", "A1", rec(376, [3]string{"r1-f1", "MAJOR", "accept"}))
	if _, err := IndexRecords(ctx, db, root, fixedNow, nil); err != nil {
		t.Fatal(err)
	}
	writeChain(t, root, "plan", "c1", "A2", rec(376))
	res, err := IndexRecords(ctx, db, root, fixedNow, nil)
	if err != nil || res.Runs != 1 || res.Removed != 1 {
		t.Fatalf("res = %+v, %v; want Runs 1, Removed 1 (findings only)", res, err)
	}
	if n := runRows(t, db, `1 = 1`); n != 1 {
		t.Errorf("run rows = %d, want 1", n)
	}
	if n := runRows(t, db, `source_id LIKE 'plan/c1/A2/%' AND finding_count = 0`); n != 1 {
		t.Errorf("A2 run rows = %d, want 1", n)
	}
}

func TestIndexRecordsAbortedOutcome(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	r := rec(376, [3]string{"r1-f1", "MAJOR", ""}, [3]string{"r1-f2", "MINOR", ""})
	r["outcome"] = "aborted"
	writeChain(t, root, "plan", "c1", "A1", r)
	if _, err := IndexRecords(ctx, db, root, fixedNow, nil); err != nil {
		t.Fatal(err)
	}
	if n := runRows(t, db, `outcome = 'aborted' AND finding_count = 2`); n != 1 {
		t.Errorf("aborted run rows = %d, want 1", n)
	}
}

func TestIndexRecordsFailureKeepsOldRows(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	writeChain(t, root, "plan", "c1", "A1", rec(376, [3]string{"r1-f1", "MAJOR", "accept"}))
	if _, err := IndexRecords(ctx, db, root, fixedNow, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_finding BEFORE INSERT ON findings WHEN NEW.finding_key = 'boom'
		BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	writeChain(t, root, "plan", "c1", "A2", rec(376, [3]string{"r1-f1", "MAJOR", "accept"}, [3]string{"boom", "MAJOR", "accept"}))
	if _, err := IndexRecords(ctx, db, root, fixedNow, nil); err == nil {
		t.Fatal("IndexRecords succeeded, want the trigger failure")
	}
	if n := rows(t, db, `source_id = 'plan/c1/A1/r1-f1'`); n != 1 {
		t.Errorf("old finding rows = %d, want 1", n)
	}
	if n := runRows(t, db, `source_id LIKE 'plan/c1/A1/%'`); n != 1 {
		t.Errorf("old run rows = %d, want 1", n)
	}
	if n := runRows(t, db, `source_id LIKE 'plan/c1/A2/%'`) + rows(t, db, `source_id LIKE 'plan/c1/A2/%'`); n != 0 {
		t.Errorf("new rows = %d, want 0", n)
	}
}

func TestIndexLedgerWritesNoReviewRun(t *testing.T) {
	ctx, db := context.Background(), openIndexDB(t)
	path := filepath.Join(t.TempDir(), "ledger.md")
	body := "# SDD ledger — plan: plans/issue-5-x.md\nTask 1: done. Ruling: ok\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := &Trajectory{Repo: "CorneeldH/lighthouse", Issue: 5}
	res, err := IndexLedger(ctx, db, path, tr, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.Runs != 0 || runRows(t, db, `1 = 1`) != 0 {
		t.Errorf("res = %+v, run rows = %d, want none", res, runRows(t, db, `1 = 1`))
	}
}

// reviewersOf follows the real composition of round 1 in the record: a fallback ran Opus alone, a
// paired round 1 ran Codex and Opus, any other Codex round ran Codex alone (the agents skill,
// Opus failed, or a codex variant that skipped the Opus side).
func TestReviewersOf(t *testing.T) {
	cases := []struct {
		name, mode string
		paired     bool
		want       string
	}{
		{"paired", "codex", true, `["codex","opus"]`},
		{"same family paired", "codex_same_family", true, `["codex","opus"]`},
		{"codex only (agents skill, Opus failed, variant codex)", "codex", false, `["codex"]`},
		{"same family alone", "codex_same_family", false, `["codex"]`},
		// An Opus-only round: a Codex failure or variant:…=opus, both recorded as fallback.
		{"fallback", "fallback", false, `["opus"]`},
		{"no review_mode", "", true, `null`},
	}
	for _, c := range cases {
		got, _ := json.Marshal(reviewersOf(c.mode, c.paired))
		if string(got) != c.want {
			t.Errorf("%s: reviewersOf = %s, want %s", c.name, got, c.want)
		}
	}
}

func runCols(t *testing.T, db *sql.DB) (mode, reviewers sql.NullString, assignment string, applied sql.NullInt64) {
	t.Helper()
	err := db.QueryRow(`SELECT review_mode, reviewers, assignment, assignment_applied FROM review_runs`).
		Scan(&mode, &reviewers, &assignment, &applied)
	if err != nil {
		t.Fatalf("read review_runs: %v", err)
	}
	return
}

func TestIndexRecordsReviewRunColumns(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	r := rec(572)
	r["review_mode"] = "fallback"
	r["assignment"] = "variant:plan_review=codex"
	r["assignment_applied"] = true
	writeChain(t, root, "plan", "c1", "A1", r)
	if _, err := IndexRecords(ctx, db, root, fixedNow, nil); err != nil {
		t.Fatal(err)
	}
	mode, reviewers, assignment, applied := runCols(t, db)
	if mode.String != "fallback" || reviewers.String != `["opus"]` || assignment != "variant:plan_review=codex" ||
		!applied.Valid || applied.Int64 != 1 {
		t.Errorf("columns = %v %v %q %v; want fallback [\"opus\"] variant:plan_review=codex 1", mode, reviewers, assignment, applied)
	}
	if n := runRows(t, db, `finding_count = 0`); n != 1 {
		t.Errorf("zero-finding run rows = %d, want 1", n)
	}

	r["review_mode"] = "codex"
	r["assignment_applied"] = false
	r["paired"] = true
	writeChain(t, root, "plan", "c1", "A1", r)
	if _, err := IndexRecords(ctx, db, root, fixedNow, nil); err != nil {
		t.Fatal(err)
	}
	mode, reviewers, _, applied = runCols(t, db)
	if mode.String != "codex" || reviewers.String != `["codex","opus"]` || !applied.Valid || applied.Int64 != 0 {
		t.Errorf("after change = %v %v %v; want codex both 0", mode, reviewers, applied)
	}

	// A codex round 1 without a pair (the agents skill writes the same review_mode) is Codex alone.
	r["paired"] = false
	writeChain(t, root, "plan", "c1", "A1", r)
	if _, err := IndexRecords(ctx, db, root, fixedNow, nil); err != nil {
		t.Fatal(err)
	}
	if _, reviewers, _, _ = runCols(t, db); reviewers.String != `["codex"]` {
		t.Errorf("unpaired codex reviewers = %v, want [\"codex\"]", reviewers)
	}
}

func TestIndexRecordsWithoutAssignmentIsStandard(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	writeChain(t, root, "plan", "c1", "A1", rec(572))
	if _, err := IndexRecords(ctx, db, root, fixedNow, nil); err != nil {
		t.Fatal(err)
	}
	mode, reviewers, assignment, applied := runCols(t, db)
	if assignment != "standard" || applied.Valid {
		t.Errorf("assignment = %q, applied = %v; want standard, NULL", assignment, applied)
	}
	if mode.Valid || reviewers.Valid {
		t.Errorf("mode = %v, reviewers = %v; want NULL for a record without review_mode", mode, reviewers)
	}
}

func findingReviewerCols(t *testing.T, db *sql.DB, key string) (src, sevCodex, sevFallback sql.NullString, source, severity string) {
	t.Helper()
	err := db.QueryRow(`SELECT reviewer_source, severity_codex, severity_fallback, source, severity FROM findings
		WHERE finding_key = ?`, key).Scan(&src, &sevCodex, &sevFallback, &source, &severity)
	if err != nil {
		t.Fatalf("read finding %s: %v", key, err)
	}
	return
}

func recWithSources(issue int) map[string]any {
	r := rec(issue, [3]string{"F1", "major", ""}, [3]string{"F2", "minor", ""}, [3]string{"F3", "critical", ""})
	fs := r["findings"].([]any)
	set := func(i int, src string, by map[string]any) {
		fs[i].(map[string]any)["source"] = src
		fs[i].(map[string]any)["severity_by_source"] = by
	}
	set(0, "codex", map[string]any{"codex": "major"})
	set(1, "fallback", map[string]any{"fallback": "minor"})
	set(2, "both", map[string]any{"codex": "critical", "fallback": "major"})
	return r
}

func TestIndexFindingReviewerSource(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	writeChain(t, root, "plan", "c1", "A1", recWithSources(572))
	if _, err := IndexRecords(ctx, db, root, fixedNow, nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ key, src, codex, fallback, severity string }{
		{"F1", "codex", "major", "", "major"},
		{"F2", "fallback", "", "minor", "minor"},
		{"F3", "both", "critical", "major", "critical"},
	} {
		src, codex, fallback, source, severity := findingReviewerCols(t, db, c.key)
		if src.String != c.src || codex.String != c.codex || fallback.String != c.fallback ||
			codex.Valid != (c.codex != "") || fallback.Valid != (c.fallback != "") {
			t.Errorf("%s: reviewer_source %v codex %v fallback %v; want %s %q %q", c.key, src, codex, fallback, c.src, c.codex, c.fallback)
		}
		if source != "plan-review-record" || severity != c.severity {
			t.Errorf("%s: source %q severity %q; want plan-review-record %q", c.key, source, severity, c.severity)
		}
	}

	r := recWithSources(572)
	r["findings"].([]any)[0].(map[string]any)["source"] = "both"
	r["findings"].([]any)[0].(map[string]any)["severity_by_source"] = map[string]any{"codex": "major", "fallback": "minor"}
	r["written_at"] = "2026-11-20T11:00:00+00:00"
	writeChain(t, root, "plan", "c1", "A1", r)
	if _, err := IndexRecords(ctx, db, root, fixedNow, nil); err != nil {
		t.Fatal(err)
	}
	src, codex, fallback, _, _ := findingReviewerCols(t, db, "F1")
	if src.String != "both" || codex.String != "major" || fallback.String != "minor" {
		t.Errorf("after change: %v %v %v; want both major minor", src, codex, fallback)
	}
}

func TestIndexFindingWithoutSource(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	writeChain(t, root, "plan", "c1", "A1", rec(572, [3]string{"V1", "major", ""}))
	if _, err := IndexRecords(ctx, db, root, fixedNow, nil); err != nil {
		t.Fatal(err)
	}
	src, codex, fallback, _, _ := findingReviewerCols(t, db, "V1")
	if src.Valid || codex.Valid || fallback.Valid {
		t.Errorf("columns = %v %v %v; want all NULL", src, codex, fallback)
	}
}

// derivedRecord is a record without trajectory that carries what Trace needs.
func derivedRecord(chain string) map[string]any {
	r := rec(1, [3]string{"r1-f1", "MAJOR", "accept"})
	r["trajectory"] = nil
	r["chain_key"] = chain
	r["reviewed_sha"] = "abc"
	return r
}

func TestIndexDerivedTrajectory(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	writeChain(t, root, "plan", "issue-572-x", "A1", derivedRecord("issue-572-x"))
	repos := []ServedRepo{{Slug: "o/a", Dir: "/a"}, {Slug: "o/b", Dir: "/b"}}
	oldHas := hasCommit
	hasCommit = func(dir, sha string) bool { return dir == "/b" && sha == "abc" }
	t.Cleanup(func() { hasCommit = oldHas })
	res, err := IndexRecords(ctx, db, root, fixedNow, repos)
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 1 || len(res.Skipped) != 0 {
		t.Errorf("res = %+v, want New 1 and no Skip", res)
	}
	if n := runRows(t, db, `trajectory_id IS NOT NULL AND unlinked_reason IS NULL`); n != 1 {
		t.Errorf("linked run rows = %d, want 1", n)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM findings f JOIN trajectories t ON t.id = f.trajectory_id WHERE t.repo = 'o/b' AND t.issue = 572`).Scan(&n); err != nil || n != 1 {
		t.Errorf("finding rows = %d (%v), want 1", n, err)
	}
}

func TestIndexAmbiguousDerivationStaysUnlinked(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	writeChain(t, root, "plan", "issue-572-x", "A1", derivedRecord("issue-572-x"))
	repos := []ServedRepo{{Slug: "o/a", Dir: "/a"}, {Slug: "o/b", Dir: "/b"}}
	oldHas := hasCommit
	hasCommit = func(string, string) bool { return true }
	t.Cleanup(func() { hasCommit = oldHas })
	res, err := IndexRecords(ctx, db, root, fixedNow, repos)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, "no trajectory") {
		t.Errorf("Skipped = %+v, want the no-trajectory skip", res.Skipped)
	}
	if n := runRows(t, db, `trajectory_id IS NULL AND unlinked_reason = 'no_trajectory'`); n != 1 {
		t.Errorf("unlinked run rows = %d, want 1", n)
	}
	if n := rows(t, db, `1 = 1`); n != 0 {
		t.Errorf("finding rows = %d, want 0", n)
	}
}

// A record whose assignment the database cannot take is a skip of that chain, not an error that
// aborts the index: the other chains are still written.
func TestIndexRecordsSkipsBadAssignment(t *testing.T) {
	ctx, db, root := context.Background(), openIndexDB(t), t.TempDir()
	bad := rec(572, [3]string{"r1-f1", "MAJOR", "accept"})
	bad["assignment"] = "variant:foo"
	bad["assignment_applied"] = true
	writeChain(t, root, "plan", "bad", "A1", bad)
	writeChain(t, root, "plan", "good", "A1", rec(573))
	res, err := IndexRecords(ctx, db, root, fixedNow, nil)
	if err != nil {
		t.Fatalf("IndexRecords: %v, want the bad record skipped", err)
	}
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, "assignment") {
		t.Errorf("Skipped = %+v, want one skip naming the assignment", res.Skipped)
	}
	if n := runRows(t, db, `1 = 1`); n != 1 || rows(t, db, `1 = 1`) != 0 {
		t.Errorf("run rows = %d, finding rows = %d; want only the good chain's run", n, rows(t, db, `1 = 1`))
	}
}

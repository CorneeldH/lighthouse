package review

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeLedger(t *testing.T, extra string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "sdd-ledger-ruling.md"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ledger.md")
	if err := os.WriteFile(path, append(data, extra...), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseLedger(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "sdd-ledger-ruling.md"))
	if err != nil {
		t.Fatal(err)
	}
	plan, got := ParseLedger(data)
	if want := "docs/developer/planning/plans/2026-11-20-issue-376-x.md"; plan != want {
		t.Errorf("planPath = %q, want %q", plan, want)
	}
	want := []LedgerFinding{
		{Task: 2, Text: "de code blijft staan, het patroon is elders gelijk", Decision: "rejected"},
		{Task: 3, Text: "finding over cache genegeerd — kost niets als het fout is", Decision: "left_open"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings = %+v, want %+v", got, want)
	}
}

func TestLedgerIssue(t *testing.T) {
	if n, ok := LedgerIssue("docs/developer/planning/plans/2026-11-20-issue-376-x.md"); n != 376 || !ok {
		t.Errorf("LedgerIssue = %d, %v; want 376, true", n, ok)
	}
	if n, ok := LedgerIssue("docs/plans/other.md"); n != 0 || ok {
		t.Errorf("LedgerIssue = %d, %v; want 0, false", n, ok)
	}
}

func TestIndexLedgerRulings(t *testing.T) {
	ctx, db := context.Background(), openIndexDB(t)
	tr := &Trajectory{Repo: "CorneeldH/lighthouse", Issue: 376}
	path := writeLedger(t, "")
	res, err := IndexLedger(ctx, db, path, tr, fixedNow)
	if err != nil || res.New != 2 {
		t.Fatalf("first = %+v, %v; want New 2", res, err)
	}
	if n := rows(t, db, `component = 'task_review' AND source = 'sdd-ledger'`); n != 2 {
		t.Errorf("task_review rows = %d, want 2", n)
	}
	if n := rows(t, db, `finding_key = 'task-2-ruling-1' AND decision = 'rejected' AND severity = ''
		AND source_id = 'docs/developer/planning/plans/2026-11-20-issue-376-x.md#2#1'`); n != 1 {
		t.Errorf("task 2 row matches = %d, want 1", n)
	}
	later := func() time.Time { return fixedNow().Add(time.Hour) }
	res, err = IndexLedger(ctx, db, path, tr, later)
	if err != nil || res.Unchanged != 2 || res.New != 0 || res.Updated != 0 {
		t.Fatalf("second = %+v, %v; want Unchanged 2", res, err)
	}
	if n := rows(t, db, `1 = 1`); n != 2 {
		t.Errorf("rows = %d, want 2", n)
	}
	path = writeLedger(t, "Task 4: Ruling: nog een — waarom\n")
	res, err = IndexLedger(ctx, db, path, tr, fixedNow)
	if err != nil || res.New != 1 || res.Updated != 2 {
		t.Fatalf("third = %+v, %v; want New 1, Updated 2", res, err)
	}
	if n := rows(t, db, `1 = 1`); n != 3 {
		t.Errorf("rows = %d, want 3", n)
	}
}

func TestIndexLedgerWithoutTrajectorySkips(t *testing.T) {
	ctx, db := context.Background(), openIndexDB(t)
	path := writeLedger(t, "")
	res, err := IndexLedger(ctx, db, path, nil, fixedNow)
	if err != nil || res.New != 0 || len(res.Skipped) != 1 || res.Skipped[0].Reason == "" || res.Skipped[0].Path != path {
		t.Fatalf("result = %+v, %v; want one Skip with reason", res, err)
	}
	if n := rows(t, db, `1 = 1`); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

func TestIndexLedgerWithoutPlanHeaderIsSkipped(t *testing.T) {
	ctx, db := context.Background(), openIndexDB(t)
	dir := t.TempDir()
	var paths []string
	for _, name := range []string{"a.md", "b.md"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("no header here\nTask 1: Ruling: iets — waarom\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	for i, p := range paths {
		tr := &Trajectory{Repo: "CorneeldH/lighthouse", Issue: 100 * (i + 1)}
		res, err := IndexLedger(ctx, db, p, tr, fixedNow)
		if err != nil {
			t.Fatal(err)
		}
		if res.New != 0 || len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, "plan") {
			t.Errorf("res = %+v, want one skip naming the plan header", res)
		}
	}
	if n := rows(t, db, `1 = 1`); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

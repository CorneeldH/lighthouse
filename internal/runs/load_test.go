package runs

import (
	"context"
	"testing"
	"time"
)

// Classification: behaviour tests of the host-wide reads; they fail if OpenAll
// filters by repo or includes closed runs, if LastOfSession picks the wrong row
// or ignores other repos, or if Resumable's rule changes.

func TestOpenAllAcrossRepos(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	a, err := Take(ctx, db, claim("o/a", 1, "start:1"))
	if err != nil {
		t.Fatal(err)
	}
	closed, err := Take(ctx, db, claim("o/a", 2, "start:2"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Release(ctx, db, closed.ID, Done, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	b, err := Take(ctx, db, claim("o/b", 3, "start:3"))
	if err != nil {
		t.Fatal(err)
	}

	got, err := OpenAll(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("OpenAll = %d rows, want 2: %+v", len(got), got)
	}
	if got[0].ID != a.ID || got[0].Repo != "o/a" || got[0].Issue != 1 ||
		got[1].ID != b.ID || got[1].Repo != "o/b" || got[1].Issue != 3 {
		t.Errorf("OpenAll = %+v", got)
	}
}

func TestLastOfSession(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	first := claim("o/a", 1, "start:1")
	first.SessionID = "s1"
	r1, err := Take(ctx, db, first)
	if err != nil {
		t.Fatal(err)
	}
	if err := Release(ctx, db, r1.ID, Parked, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	resume := claim("o/a", 1, "usage:s1@x:y")
	resume.SessionID, resume.Mode, resume.Trigger = "s1", "resume", "usage"
	r2, err := Take(ctx, db, resume)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Take(ctx, db, claim("o/b", 5, "start:5")); err != nil {
		t.Fatal(err)
	}

	repo, row, ok, err := LastOfSession(ctx, db, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || repo != "o/a" || row.ID != r2.ID || row.Mode != "resume" || !row.Open {
		t.Errorf("LastOfSession = %q, %+v, %v", repo, row, ok)
	}
	if _, _, ok, err := LastOfSession(ctx, db, "nope"); err != nil || ok {
		t.Errorf("unknown session: ok = %v, err = %v; want false, nil", ok, err)
	}
}

func TestResumable(t *testing.T) {
	const key = "usage:s1@2026-11-01T11:00:00Z:2026-11-01T12:00:00Z"
	const other = "usage:s1@2026-11-01T10:00:00Z:2026-11-01T12:00:00Z"
	closedRow := func(o Outcome) Row { return Row{Outcome: o} }
	usageRow := func(k string, o Outcome, attempt int) Row {
		return Row{Trigger: "usage", TriggerKey: k, Outcome: o, Attempt: attempt}
	}
	tests := []struct {
		name string
		row  Row
		want bool
	}{
		{"closed parked", closedRow(Parked), true},
		{"open row", Row{Open: true}, false},
		{"open parked", Row{Open: true, Outcome: Parked}, false},
		{"handoff", closedRow(Handoff), false},
		{"question", closedRow(Question), false},
		{"done", closedRow(Done), false},
		{"usage start_failed attempt 1", usageRow(key, StartFailed, 1), true},
		{"usage start_failed attempt 2", usageRow(key, StartFailed, 2), false},
		{"usage start_failed other key", usageRow(other, StartFailed, 1), false},
		{"usage open start_failed", Row{Open: true, Trigger: "usage", TriggerKey: key, Outcome: StartFailed, Attempt: 1}, false},
		{"non-usage start_failed same key", Row{Trigger: "handoff", TriggerKey: key, Outcome: StartFailed, Attempt: 1}, false},
		{"usage done", usageRow(key, Done, 1), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Resumable(tt.row, key); got != tt.want {
				t.Errorf("Resumable(%+v) = %v, want %v", tt.row, got, tt.want)
			}
		})
	}
}

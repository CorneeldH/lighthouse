package runs

import (
	"context"
	"testing"
	"time"
)

func TestHistoryOpenAndClosed(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	first := claim("owner/a", 7, "start:7")
	first.Branch = "issue-7-x"
	first.Command, first.Prompt, first.Profile = "cmd", "prompt", "software"
	r1, err := Take(ctx, db, first)
	if err != nil {
		t.Fatal(err)
	}
	if err := Release(ctx, db, r1.ID, StartFailed, "x", time.Now()); err != nil {
		t.Fatal(err)
	}
	r2, err := Take(ctx, db, claim("owner/a", 7, "start:7"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Take(ctx, db, claim("owner/b", 3, "start:3")); err != nil {
		t.Fatal(err)
	}

	got, err := History(ctx, db, "owner/a")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("History = %d rows, want 2: %+v", len(got), got)
	}
	a, b := got[0], got[1]
	if a.ID != r1.ID || a.Open || a.Outcome != StartFailed || a.Reason != "x" {
		t.Errorf("first row: %+v", a)
	}
	if a.Issue != 7 || a.SessionID != first.SessionID || a.Mode != "start" || a.Trigger != "handoff" ||
		a.TriggerKey != "start:7" || a.Attempt != 1 || a.Command != "cmd" || a.Prompt != "prompt" ||
		a.Branch != "issue-7-x" || a.Profile != "software" || !a.StartedAt.Equal(first.Now) {
		t.Errorf("first row fields: %+v", a)
	}
	if b.ID != r2.ID || !b.Open || b.Attempt != 2 || b.Outcome != "" {
		t.Errorf("second row: %+v", b)
	}
}

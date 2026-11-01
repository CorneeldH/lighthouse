package runs

import (
	"context"
	"testing"
	"time"
)

func TestOpenBriefs(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	c := claim("o/a", 3, "start:3")
	c.Command, c.Profile = "/lighthouse-start-issue 3", "software"
	first, err := Take(ctx, db, c)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if err := SetWorkplace(ctx, db, first.ID, "ws-3", "issue-3-x"); err != nil {
		t.Fatalf("SetWorkplace: %v", err)
	}
	second, err := Take(ctx, db, claim("o/a", 4, "start:4"))
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if _, err := Take(ctx, db, claim("o/b", 5, "start:5")); err != nil {
		t.Fatalf("Take: %v", err)
	}

	got, err := OpenBriefs(ctx, db, "o/a")
	if err != nil {
		t.Fatalf("OpenBriefs: %v", err)
	}
	want := []Brief{
		{ID: first.ID, Repo: "o/a", Issue: 3, Command: "/lighthouse-start-issue 3", Trigger: "handoff",
			Profile: "software", WorkspaceID: "ws-3", StartedAt: c.Now},
		{ID: second.ID, Repo: "o/a", Issue: 4, Trigger: "handoff", StartedAt: c.Now},
	}
	if len(got) != len(want) {
		t.Fatalf("OpenBriefs = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("OpenBriefs[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestBriefsByID(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	closed, err := Take(ctx, db, claim("o/a", 1, "start:1"))
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if err := Release(ctx, db, closed.ID, Parked, "", time.Now()); err != nil {
		t.Fatalf("Release: %v", err)
	}
	open, err := Take(ctx, db, claim("o/a", 2, "start:2"))
	if err != nil {
		t.Fatalf("Take: %v", err)
	}

	got, err := BriefsByID(ctx, db, []int64{open.ID, closed.ID})
	if err != nil {
		t.Fatalf("BriefsByID: %v", err)
	}
	if len(got) != 2 || got[0].ID != open.ID || got[1].ID != closed.ID {
		t.Fatalf("BriefsByID = %+v, want ids [%d %d] in that order", got, open.ID, closed.ID)
	}

	got, err = BriefsByID(ctx, db, []int64{closed.ID, 9999})
	if err != nil {
		t.Fatalf("BriefsByID unknown: %v", err)
	}
	if len(got) != 1 || got[0].ID != closed.ID || got[0].Issue != 1 {
		t.Fatalf("BriefsByID with unknown id = %+v, want only the closed run", got)
	}
}

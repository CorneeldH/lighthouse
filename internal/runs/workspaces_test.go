package runs

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"
)

// closedRun claims and closes a run of issue with workspace id and branch, and returns its id.
func closedRun(t *testing.T, db *sql.DB, repo string, issue int, key, workspaceID, branch string) int64 {
	t.Helper()
	ctx := context.Background()
	r, err := Take(ctx, db, claim(repo, issue, key))
	if err != nil {
		t.Fatalf("Take %s: %v", key, err)
	}
	if err := SetWorkplace(ctx, db, r.ID, workspaceID, branch); err != nil {
		t.Fatalf("SetWorkplace %s: %v", key, err)
	}
	if err := Release(ctx, db, r.ID, Done, "", time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("Release %s: %v", key, err)
	}
	return r.ID
}

func workspaces(t *testing.T, db *sql.DB, repo string) []WorkspaceRef {
	t.Helper()
	got, err := Workspaces(context.Background(), db, repo)
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	return got
}

// #945: four runs shared one workspace; it is one candidate.
func TestWorkspacesOnePerWorkspace(t *testing.T) {
	db := openDB(t)
	for _, key := range []string{"start:945", "answer:1", "answer:2", "handoff:3"} {
		closedRun(t, db, "o/r", 945, key, "d7c0b0ea0de54804", "work-issue-945-immich-smart-search-191b7750")
	}
	closedRun(t, db, "o/r", 12, "start:12", "ws-12", "issue-12")

	want := []WorkspaceRef{
		{Issue: 12, WorkspaceID: "ws-12", Branch: "issue-12"},
		{Issue: 945, WorkspaceID: "d7c0b0ea0de54804", Branch: "work-issue-945-immich-smart-search-191b7750"},
	}
	if got := workspaces(t, db, "o/r"); !reflect.DeepEqual(got, want) {
		t.Errorf("Workspaces = %+v, want %+v", got, want)
	}
}

func TestWorkspacesSkipsEmptyAndRemoved(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	closedRun(t, db, "o/r", 1, "start:1", "", "")
	closedRun(t, db, "o/r", 2, "start:2", "ws-2", "issue-2")
	closedRun(t, db, "o/other", 3, "start:3", "ws-3", "issue-3")
	if err := MarkWorkspaceRemoved(ctx, db, "o/r", "", "ws-2", time.Now()); err != nil {
		t.Fatalf("MarkWorkspaceRemoved: %v", err)
	}
	if got := workspaces(t, db, "o/r"); got == nil || len(got) != 0 {
		t.Errorf("Workspaces = %#v, want an empty, non-nil list", got)
	}
}

func TestWorkspacesLastRowWins(t *testing.T) {
	db := openDB(t)
	closedRun(t, db, "o/r", 7, "start:7", "ws-7", "old-branch")
	closedRun(t, db, "o/r", 7, "answer:1", "ws-7", "new-branch")

	want := []WorkspaceRef{{Issue: 7, WorkspaceID: "ws-7", Branch: "new-branch"}}
	if got := workspaces(t, db, "o/r"); !reflect.DeepEqual(got, want) {
		t.Errorf("Workspaces = %+v, want %+v", got, want)
	}
}

func TestMarkWorkspaceRemovedOnlyThatWorkspace(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	a1 := closedRun(t, db, "o/r", 7, "start:7", "ws-7", "b")
	a2 := closedRun(t, db, "o/r", 7, "answer:1", "ws-7", "b")
	other := closedRun(t, db, "o/r", 8, "start:8", "ws-8", "b")
	otherRepo := closedRun(t, db, "o/x", 7, "start:7", "ws-7", "b")

	if err := MarkWorkspaceRemoved(ctx, db, "o/r", "", "ws-7", time.Date(2026, 11, 1, 10, 0, 0, 0, time.FixedZone("CET", 3600))); err != nil {
		t.Fatalf("MarkWorkspaceRemoved: %v", err)
	}
	want := map[int64]string{a1: "2026-11-01T09:00:00Z", a2: "2026-11-01T09:00:00Z", other: "", otherRepo: ""}
	got := map[int64]string{}
	rows, err := db.Query(`SELECT id, coalesce(workspace_removed_at, '') FROM runs`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var at string
		if err := rows.Scan(&id, &at); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[id] = at
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("workspace_removed_at per row = %v, want %v", got, want)
	}
}

// The same workspace id on two nodes is two workspaces; removing one leaves the other.
func TestWorkspacesPerNode(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	closedRun(t, db, "o/r", 7, "start:7", "w1", "issue-7")
	c := claim("o/r", 8, "start:8")
	c.Node = "node-a"
	c.Profile = "prof-b"
	r, err := Take(ctx, db, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetWorkplace(ctx, db, r.ID, "w1", "issue-8"); err != nil {
		t.Fatal(err)
	}
	if err := Release(ctx, db, r.ID, Done, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	want := []WorkspaceRef{
		{Issue: 7, WorkspaceID: "w1", Branch: "issue-7"},
		{Issue: 8, Node: "node-a", WorkspaceID: "w1", Branch: "issue-8", Profile: "prof-b"},
	}
	if got := workspaces(t, db, "o/r"); !reflect.DeepEqual(got, want) {
		t.Fatalf("Workspaces = %+v, want %+v", got, want)
	}
	if err := MarkWorkspaceRemoved(ctx, db, "o/r", "node-a", "w1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := workspaces(t, db, "o/r"); !reflect.DeepEqual(got, want[:1]) {
		t.Errorf("after remove on node-a: %+v, want %+v", got, want[:1])
	}
}

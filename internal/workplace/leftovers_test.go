package workplace

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/store"
)

func openLeftoverDB(t *testing.T) *sql.DB {
	t.Helper()
	s, err := store.OpenShared(context.Background(), filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatalf("OpenShared: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s.DB
}

var leftoverNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func TestLeftoversRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openLeftoverDB(t)
	in := []Leftover{
		{Repo: "o/b", Issue: 2, WorkspaceID: "w9", Path: "/p/9", Reason: "dirty"},
		{Repo: "o/a", Issue: 10, WorkspaceID: "w1", Path: "/p/1", Reason: "unpushed"},
		{Repo: "o/a", Issue: 3, WorkspaceID: "w2", Path: "/p/2", Reason: "busy"},
		{Repo: "o/a", Issue: 3, WorkspaceID: "w0", Path: "/p/0", Reason: "dirty"},
	}
	for _, l := range in {
		if err := RecordLeftover(ctx, db, l, leftoverNow); err != nil {
			t.Fatalf("RecordLeftover: %v", err)
		}
	}
	got, err := Leftovers(ctx, db)
	if err != nil {
		t.Fatalf("Leftovers: %v", err)
	}
	want := []Leftover{
		{Repo: "o/a", Issue: 3, WorkspaceID: "w0", Path: "/p/0", Reason: "dirty"},
		{Repo: "o/a", Issue: 3, WorkspaceID: "w2", Path: "/p/2", Reason: "busy"},
		{Repo: "o/a", Issue: 10, WorkspaceID: "w1", Path: "/p/1", Reason: "unpushed"},
		{Repo: "o/b", Issue: 2, WorkspaceID: "w9", Path: "/p/9", Reason: "dirty"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Leftovers = %+v, want %+v", got, want)
	}
}

func TestRecordLeftoverUpserts(t *testing.T) {
	ctx := context.Background()
	db := openLeftoverDB(t)
	first := Leftover{Repo: "o/a", Issue: 3, WorkspaceID: "w1", Path: "/p/1", Reason: "dirty"}
	if err := RecordLeftover(ctx, db, first, leftoverNow); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Reason = "unpushed"
	if err := RecordLeftover(ctx, db, second, leftoverNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := Leftovers(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Leftover{second}; !reflect.DeepEqual(got, want) {
		t.Errorf("Leftovers = %+v, want %+v", got, want)
	}
}

func TestClearLeftover(t *testing.T) {
	ctx := context.Background()
	db := openLeftoverDB(t)
	a := Leftover{Repo: "o/a", Issue: 1, WorkspaceID: "w1", Path: "/p/1", Reason: "dirty"}
	sameID := Leftover{Repo: "o/b", Issue: 1, WorkspaceID: "w1", Path: "/q/1", Reason: "dirty"}
	other := Leftover{Repo: "o/a", Issue: 2, WorkspaceID: "w2", Path: "/p/2", Reason: "dirty"}
	for _, l := range []Leftover{a, sameID, other} {
		if err := RecordLeftover(ctx, db, l, leftoverNow); err != nil {
			t.Fatal(err)
		}
	}
	if err := ClearLeftover(ctx, db, "o/a", "w1"); err != nil {
		t.Fatalf("ClearLeftover: %v", err)
	}
	if err := ClearLeftover(ctx, db, "o/a", "missing"); err != nil {
		t.Fatalf("ClearLeftover of an absent row: %v", err)
	}
	got, err := Leftovers(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Leftover{other, sameID}; !reflect.DeepEqual(got, want) {
		t.Errorf("Leftovers = %+v, want %+v", got, want)
	}
}

func TestLeftoversIgnoresOtherKinds(t *testing.T) {
	ctx := context.Background()
	db := openLeftoverDB(t)
	if _, err := db.Exec(`INSERT INTO tick_state(repo, kind, key, value, updated_at) VALUES ('o/a', 'deferred', 'x', '{"issue":1}', 't')`); err != nil {
		t.Fatal(err)
	}
	got, err := Leftovers(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("Leftovers = %+v, want none", got)
	}
}

func TestLeftoversEmptyNotNil(t *testing.T) {
	got, err := Leftovers(context.Background(), openLeftoverDB(t))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("Leftovers = %#v, want empty non-nil slice", got)
	}
}

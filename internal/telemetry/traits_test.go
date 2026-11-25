package telemetry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/protocol"
)

func triageComment(gate string, at time.Time) protocol.Comment {
	return protocol.Comment{
		Body:      protocol.RenderTriageMarker(protocol.TriageMarker{Gate: gate}) + "\nroute",
		CreatedAt: at.UTC().Format(time.RFC3339),
	}
}

func TestRouteFromCommentsLastMarkerBeforeReview(t *testing.T) {
	review := t0.Add(2 * time.Hour)
	comments := []protocol.Comment{
		triageComment("2", t0),
		{Body: "no marker", CreatedAt: t0.Add(time.Hour).Format(time.RFC3339)},
		triageComment("4a", t0.Add(time.Hour)),
		triageComment("4c", t0.Add(3*time.Hour)),
	}
	got, ok := RouteFromComments(comments, review)
	if !ok || got != "4a" {
		t.Fatalf("RouteFromComments = %q, %v; want 4a", got, ok)
	}
	got, ok = RouteFromComments(comments, time.Time{})
	if !ok || got != "4c" {
		t.Fatalf("without bound = %q, %v; want 4c", got, ok)
	}
}

func TestRouteFromCommentsInvariantIsZero(t *testing.T) {
	got, ok := RouteFromComments([]protocol.Comment{triageComment("invariant", t0)}, time.Time{})
	if !ok || got != "0" {
		t.Fatalf("got %q, %v; want 0", got, ok)
	}
}

func TestRouteFromCommentsNoMarker(t *testing.T) {
	if got, ok := RouteFromComments([]protocol.Comment{{Body: "hi", CreatedAt: t0.Format(time.RFC3339)}}, time.Time{}); ok {
		t.Fatalf("got %q, true; want false", got)
	}
	if _, ok := RouteFromComments(nil, time.Time{}); ok {
		t.Fatal("nil comments must give false")
	}
}

func TestAreaFromLabelsZeroOneTwo(t *testing.T) {
	cases := []struct {
		labels []string
		want   string
		ok     bool
	}{
		{[]string{"bug", "area:web"}, "web", true},
		{[]string{"bug"}, "", false},
		{[]string{"area:web", "area:cli"}, "", false},
	}
	for _, c := range cases {
		got, ok := AreaFromLabels(c.labels, "area:")
		if got != c.want || ok != c.ok {
			t.Errorf("AreaFromLabels(%v) = %q, %v; want %q, %v", c.labels, got, ok, c.want, c.ok)
		}
	}
}

func TestAreaFromLabelsNoPrefix(t *testing.T) {
	if got, ok := AreaFromLabels([]string{"area:web"}, ""); ok {
		t.Fatalf("empty prefix gave %q, true", got)
	}
}

func TestGitSourceCodeLines(t *testing.T) {
	r := newFakeRepo(t)
	lines := strings.Repeat("x := 1\n", 150)
	writeFile(t, r.dir, "code.go", lines)
	writeFile(t, r.dir, "notes.md", "a\nb\nc\n")
	runGit(t, r.dir, nil, "add", ".")
	runGit(t, r.dir, nil, "-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-q", "-m", "feat(c): code (#12)")
	src := GitSource{Dir: r.dir, Branch: "main"}
	got, err := src.CodeLines(context.Background(), 12)
	if err != nil || got != 150 {
		t.Fatalf("CodeLines = %d, %v; want 150", got, err)
	}
	if _, err := src.CodeLines(context.Background(), 99); err == nil || !strings.Contains(err.Error(), ErrNoMergeCommit.Error()) {
		t.Fatalf("unknown PR err = %v; want ErrNoMergeCommit", err)
	}
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func strp(s string) *string { return &s }

func TestRecordTraitsKeepsRecoveryUntouched(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	id, _, err := RecordRecovery(ctx, db, recoveryRec(RecoveryRecovered, item("fix", "abc", time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	before := readRecovery(t, db, id)
	err = RecordTraits(ctx, db, Traits{Repo: "o/r", Issue: 3, Route: strp("2"), Area: strp("web"), CodeLines: map[int]int{7: 42}})
	if err != nil {
		t.Fatal(err)
	}
	if after := readRecovery(t, db, id); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Errorf("recovery changed: %+v -> %+v", before, after)
	}
	if n := count(t, db, `SELECT count(*) FROM trajectories WHERE route = '2' AND area = 'web'`); n != 1 {
		t.Errorf("trajectory traits rows = %d; want 1", n)
	}
	if n := count(t, db, `SELECT count(*) FROM recoveries WHERE pr = 7 AND code_lines = 42`); n != 1 {
		t.Errorf("code_lines rows = %d; want 1", n)
	}
}

func TestRecordTraitsDoesNotOverwriteCodeLines(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	if _, _, err := RecordRecovery(ctx, db, recoveryRec(RecoveryOpen)); err != nil {
		t.Fatal(err)
	}
	tr := Traits{Repo: "o/r", Issue: 3, CodeLines: map[int]int{7: 10}}
	if err := RecordTraits(ctx, db, tr); err != nil {
		t.Fatal(err)
	}
	tr.CodeLines[7] = 99
	if err := RecordTraits(ctx, db, tr); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT count(*) FROM recoveries WHERE code_lines = 10`); n != 1 {
		t.Errorf("code_lines kept = %d; want 1", n)
	}
	if n := count(t, db, `SELECT count(*) FROM trajectories WHERE route IS NULL AND area IS NULL`); n != 1 {
		t.Errorf("nil pointers must leave route and area NULL")
	}
}

func TestTraitTargets(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	now := mergedAt.Add(24 * time.Hour)
	// issue 3: open, nothing filled; issue 4: closed and complete, old merge.
	if _, _, err := RecordRecovery(ctx, db, recoveryRec(RecoveryOpen)); err != nil {
		t.Fatal(err)
	}
	old := RecoveryRecord{
		Origin: Origin{Source: "recovery", SourceID: "o/r#8", RecordedAt: t0},
		Repo:   "o/r", Issue: 4,
		Recovery: Recovery{PR: 8, MergedAt: mergedAt.Add(-90 * 24 * time.Hour), Status: RecoveryNone},
	}
	if _, _, err := RecordRecovery(ctx, db, old); err != nil {
		t.Fatal(err)
	}
	if err := RecordTraits(ctx, db, Traits{Repo: "o/r", Issue: 4, Route: strp("1"), Area: strp("cli"), CodeLines: map[int]int{8: 5}}); err != nil {
		t.Fatal(err)
	}
	// a review run for issue 3, two of them: the smallest ran_at wins.
	for i, d := range []time.Duration{2 * time.Hour, time.Hour} {
		if _, _, err := InTransactionRun(ctx, db, reviewRun(fmt.Sprintf("plan:a:%d", i), 3, fmt.Sprint(i))); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE review_runs SET ran_at = ? WHERE source_id = ?`, stamp(t0.Add(d)), fmt.Sprintf("plan:a:%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := TraitTargets(ctx, db, "o/r", now, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("targets = %+v; want 2", got)
	}
	by := map[int]TraitTarget{}
	for _, g := range got {
		by[g.Issue] = g
	}
	a, b := by[3], by[4]
	if !a.NeedsFill || !a.InWindow || len(a.PRs) != 1 || a.PRs[0] != 7 || !a.LastMergedAt.Equal(mergedAt) {
		t.Errorf("issue 3 = %+v", a)
	}
	if a.FirstReviewAt == nil || !a.FirstReviewAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("issue 3 FirstReviewAt = %v; want %v", a.FirstReviewAt, t0.Add(time.Hour))
	}
	if b.NeedsFill || b.InWindow || b.FirstReviewAt != nil {
		t.Errorf("issue 4 = %+v", b)
	}
}

func TestTraitTargetsAreaCountsOnlyWithPrefix(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	old := RecoveryRecord{
		Origin: Origin{Source: "recovery", SourceID: "o/r#8", RecordedAt: t0},
		Repo:   "o/r", Issue: 4,
		Recovery: Recovery{PR: 8, MergedAt: mergedAt.Add(-90 * 24 * time.Hour), Status: RecoveryNone},
	}
	if _, _, err := RecordRecovery(ctx, db, old); err != nil {
		t.Fatal(err)
	}
	// Route and size are known; the area stays NULL because the repo has no area prefix.
	if err := RecordTraits(ctx, db, Traits{Repo: "o/r", Issue: 4, Route: strp("1"), CodeLines: map[int]int{8: 5}}); err != nil {
		t.Fatal(err)
	}
	now := mergedAt.Add(24 * time.Hour)
	for _, withArea := range []bool{true, false} {
		got, err := TraitTargets(ctx, db, "o/r", now, 30, withArea)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].NeedsFill != withArea {
			t.Errorf("withArea %v: targets = %+v; want NeedsFill %v", withArea, got, withArea)
		}
	}
}

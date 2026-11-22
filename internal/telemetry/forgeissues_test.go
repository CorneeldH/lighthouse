package telemetry

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

type fakeSearcher struct {
	issues []forge.Issue
	err    error
	texts  []string
}

func (f *fakeSearcher) SearchIssues(_ context.Context, text string) ([]forge.Issue, error) {
	f.texts = append(f.texts, text)
	return f.issues, f.err
}

func TestForgeIssuesSearchesOnceAndFiltersPerPR(t *testing.T) {
	created := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	f := &fakeSearcher{issues: []forge.Issue{
		{Number: 20, Title: "kapot na #7", Body: "x", Labels: []string{"bug", "p1"}, CreatedAt: created},
		{Number: 21, Title: "los", Body: "volgt uit #8", Labels: []string{"Bug"}, CreatedAt: created},
		{Number: 22, Title: "niet #77", Body: "ook niet #70"},
	}}
	since := time.Date(2026, 11, 5, 23, 30, 0, 0, time.FixedZone("x", 3600))
	get := ForgeIssues(f, since)
	ctx := context.Background()

	got7, err := get(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	want7 := []BugIssue{{Number: 20, Title: "kapot na #7", Body: "x", Labels: []string{"bug", "p1"}, CreatedAt: created}}
	if !reflect.DeepEqual(got7, want7) {
		t.Fatalf("pr 7: got %+v, want %+v", got7, want7)
	}
	got8, err := get(ctx, 8)
	if err != nil || len(got8) != 1 || got8[0].Number != 21 {
		t.Fatalf("pr 8: got %+v, %v", got8, err)
	}
	if got9, _ := get(ctx, 9); len(got9) != 0 {
		t.Fatalf("pr 9: got %+v, want none", got9)
	}
	if want := []string{"label:bug created:>=2026-11-05"}; !reflect.DeepEqual(f.texts, want) {
		t.Fatalf("searches = %q, want exactly %q (one per run, UTC date)", f.texts, want)
	}
}

func TestForgeIssuesKeepsTheError(t *testing.T) {
	boom := errors.New("rate limited")
	get := ForgeIssues(&fakeSearcher{err: boom}, time.Now())
	if _, err := get(context.Background(), 1); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

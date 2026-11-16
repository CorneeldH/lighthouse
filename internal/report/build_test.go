package report

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

type fakeSource struct {
	merged    []forge.PRSummary
	closed    []forge.Issue
	milestone []forge.MilestonedIssue
	open      []forge.PRSummary
	failing   string
	gotSince  time.Time
	gotUntil  time.Time
}

var errBoom = errors.New("boom")

func (f *fakeSource) fail(name string) error {
	if f.failing == name {
		return errBoom
	}
	return nil
}

func (f *fakeSource) MergedPRs(_ context.Context, since, until time.Time) ([]forge.PRSummary, error) {
	f.gotSince, f.gotUntil = since, until
	return f.merged, f.fail("merged")
}

func (f *fakeSource) ClosedIssues(_ context.Context, _, _ time.Time) ([]forge.Issue, error) {
	return f.closed, f.fail("closed")
}

func (f *fakeSource) MilestonedIssues(_ context.Context, _, _ time.Time) ([]forge.MilestonedIssue, error) {
	return f.milestone, f.fail("milestoned")
}

func (f *fakeSource) OpenPRsWithStatus(_ context.Context) ([]forge.PRSummary, error) {
	return f.open, f.fail("open")
}

func eq(t *testing.T, want, got any) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("want %#v, got %#v", want, got)
	}
}

func testPeriod() Period {
	return NewPeriod(KindDay, time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC))
}

func TestBuildFillsEverySection(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	m1 := forge.Milestone{Number: 1, Title: "M1"}
	src := &fakeSource{
		merged:    []forge.PRSummary{{Number: 5}},
		closed:    []forge.Issue{{Number: 6}},
		open:      []forge.PRSummary{{Number: 7}},
		milestone: []forge.MilestonedIssue{{Issue: forge.Issue{Number: 8}, Milestone: m1}},
	}
	p := testPeriod()
	r, err := Build(context.Background(), src, "o/r", p, now)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, p, r.Period)
	eq(t, "o/r", r.Repo)
	eq(t, now, r.At)
	eq(t, []forge.PRSummary{{Number: 5}}, r.Merged)
	eq(t, []forge.Issue{{Number: 6}}, r.Closed)
	eq(t, []forge.PRSummary{{Number: 7}}, r.OpenPRs)
	eq(t, []RoadmapGroup{{Milestone: m1, Issues: []forge.Issue{{Number: 8}}}}, r.Roadmap)
	eq(t, p.Since, src.gotSince)
	eq(t, p.Until, src.gotUntil)
}

func TestBuildEmptySections(t *testing.T) {
	r, err := Build(context.Background(), &fakeSource{}, "o/r", testPeriod(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	eq(t, 0, len(r.Merged))
	eq(t, 0, len(r.Closed))
	eq(t, 0, len(r.OpenPRs))
	eq(t, 0, len(r.Roadmap))
}

func TestBuildErrorFromAnySourceFailsBuild(t *testing.T) {
	for _, name := range []string{"merged", "closed", "milestoned", "open"} {
		t.Run(name, func(t *testing.T) {
			src := &fakeSource{
				failing:   name,
				merged:    []forge.PRSummary{{Number: 1}},
				closed:    []forge.Issue{{Number: 2}},
				open:      []forge.PRSummary{{Number: 3}},
				milestone: []forge.MilestonedIssue{{Issue: forge.Issue{Number: 4}}},
			}
			r, err := Build(context.Background(), src, "o/r", testPeriod(), time.Now())
			if !errors.Is(err, errBoom) || !strings.HasPrefix(err.Error(), "report: "+name+":") {
				t.Fatalf("err = %v", err)
			}
			eq(t, Report{}, r)
		})
	}
}

func TestBuildOrder(t *testing.T) {
	t0 := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	src := &fakeSource{
		merged: []forge.PRSummary{
			{Number: 1, MergedAt: t0.Add(2 * time.Hour)},
			{Number: 2, MergedAt: t0},
			{Number: 3, MergedAt: t0.Add(time.Hour)},
		},
		closed: []forge.Issue{
			{Number: 10, ClosedAt: t0.Add(time.Hour)},
			{Number: 11, ClosedAt: t0.Add(2 * time.Hour)},
			{Number: 12, ClosedAt: t0},
		},
		open: []forge.PRSummary{{Number: 9}, {Number: 4}, {Number: 7}},
		milestone: []forge.MilestonedIssue{
			{Issue: forge.Issue{Number: 30}, Milestone: forge.Milestone{Number: 2, Title: "B"}},
			{Issue: forge.Issue{Number: 21}, Milestone: forge.Milestone{Number: 1, Title: "A"}},
			{Issue: forge.Issue{Number: 20}, Milestone: forge.Milestone{Number: 1, Title: "A"}},
		},
	}
	r, err := Build(context.Background(), src, "o/r", testPeriod(), t0)
	if err != nil {
		t.Fatal(err)
	}

	var merged, closed, open []int
	for _, p := range r.Merged {
		merged = append(merged, p.Number)
	}
	for _, i := range r.Closed {
		closed = append(closed, i.Number)
	}
	for _, p := range r.OpenPRs {
		open = append(open, p.Number)
	}
	eq(t, []int{2, 3, 1}, merged)
	eq(t, []int{12, 10, 11}, closed)
	eq(t, []int{4, 7, 9}, open)
	if len(r.Roadmap) != 2 {
		t.Fatalf("roadmap = %v", r.Roadmap)
	}
	eq(t, "A", r.Roadmap[0].Milestone.Title)
	eq(t, "B", r.Roadmap[1].Milestone.Title)
	eq(t, 20, r.Roadmap[0].Issues[0].Number)
	eq(t, 21, r.Roadmap[0].Issues[1].Number)
}

func TestBuildGroupsRoadmapByMilestone(t *testing.T) {
	a := forge.Milestone{Number: 1, Title: "A"}
	b := forge.Milestone{Number: 2, Title: "B"}
	src := &fakeSource{milestone: []forge.MilestonedIssue{
		{Issue: forge.Issue{Number: 1}, Milestone: a},
		{Issue: forge.Issue{Number: 2}, Milestone: b},
		{Issue: forge.Issue{Number: 3}, Milestone: a},
	}}
	r, err := Build(context.Background(), src, "o/r", testPeriod(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	want := []RoadmapGroup{
		{Milestone: a, Issues: []forge.Issue{{Number: 1}, {Number: 3}}},
		{Milestone: b, Issues: []forge.Issue{{Number: 2}}},
	}
	eq(t, want, r.Roadmap)
}

func TestPRStatusFirstHitWins(t *testing.T) {
	tests := []struct {
		name string
		pr   forge.PRSummary
		want Status
	}{
		{"draft beats conflict", forge.PRSummary{Draft: true, Mergeable: "CONFLICTING"}, StatusDraft},
		{"conflict beats red", forge.PRSummary{Mergeable: "CONFLICTING", Checks: "FAILURE"}, StatusConflict},
		{"failure", forge.PRSummary{Mergeable: "MERGEABLE", Checks: "FAILURE"}, StatusRed},
		{"error", forge.PRSummary{Checks: "ERROR"}, StatusRed},
		{"pending", forge.PRSummary{Checks: "PENDING"}, StatusBusy},
		{"expected", forge.PRSummary{Checks: "EXPECTED"}, StatusBusy},
		{"success", forge.PRSummary{Mergeable: "MERGEABLE", Checks: "SUCCESS"}, StatusGreen},
		{"no checks", forge.PRSummary{Mergeable: "MERGEABLE"}, StatusNoChecks},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eq(t, tt.want, PRStatus(tt.pr))
		})
	}
}

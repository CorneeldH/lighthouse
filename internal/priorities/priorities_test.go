package priorities

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/overview"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// fakeForge serves one Planning; the embedded nil Forge panics on any other call, and
// BoardItems must never be called.
type fakeForge struct {
	forge.Forge
	t        *testing.T
	planning forge.Planning
	err      error
}

func (f *fakeForge) Planning(context.Context) (forge.Planning, error) {
	return f.planning, f.err
}

func (f *fakeForge) BoardItems(context.Context) ([]forge.BoardItem, error) {
	f.t.Fatal("Build must not read the board")
	return nil, nil
}

func TestBuildJoinsStatusInMemory(t *testing.T) {
	src := &fakeForge{t: t, planning: forge.Planning{
		Issues: []forge.PlanIssue{
			{Number: 1, Title: "one", URL: "u1", Milestone: &forge.Milestone{Number: 7, Title: "M", State: "open"}},
			{Number: 2, Title: "two", URL: "u2"},
		},
		Milestones: []forge.MilestoneCount{{Milestone: forge.Milestone{Number: 7, Title: "M", Description: "d"}, Open: 3, Done: 4}},
	}}
	statuses := []overview.IssueStatus{{Repo: "o/a", Issue: 1, Status: "Controller"}, {Repo: "o/b", Issue: 2, Status: "Other"}}

	got, err := Build(context.Background(), []Source{{Slug: "o/a", Src: src}}, overview.Overview{Statuses: statuses})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "o/a" || len(got[0].Issues) != 2 {
		t.Fatalf("Build = %+v", got)
	}
	i1, i2 := got[0].Issues[0], got[0].Issues[1]
	if i1.Status != "Controller" {
		t.Errorf("issue 1 status = %q, want Controller", i1.Status)
	}
	if i2.Status != "" {
		t.Errorf("issue 2 (status only in another repo) = %q, want empty", i2.Status)
	}
	if i1.Milestone == nil || *i1.Milestone != (MilestoneRef{Number: 7, Title: "M", State: "open"}) || i2.Milestone != nil {
		t.Errorf("milestones = %+v, %+v", i1.Milestone, i2.Milestone)
	}
	if want := (Milestone{Number: 7, Title: "M", Description: "d", Open: 3, Done: 4, Closed: []ClosedIssue{}}); len(got[0].Milestones) != 1 || !reflect.DeepEqual(got[0].Milestones[0], want) {
		t.Errorf("milestones = %+v, want %+v", got[0].Milestones, want)
	}
}

func TestBuildNeverCallsBoardItems(t *testing.T) {
	src := &fakeForge{t: t}
	if _, err := Build(context.Background(), []Source{{Slug: "o/a", Src: src}}, overview.Overview{}); err != nil {
		t.Fatalf("Build: %v", err)
	}
}

func TestBuildFailsNamingTheRepoWhenOneRepoFails(t *testing.T) {
	boom := errors.New("boom")
	ok := &fakeForge{t: t}
	bad := &fakeForge{t: t, err: boom}
	got, err := Build(context.Background(), []Source{{Slug: "o/ok", Src: ok}, {Slug: "o/bad", Src: bad}}, overview.Overview{})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "o/bad") {
		t.Errorf("err = %v, want boom naming o/bad", err)
	}
	if got != nil {
		t.Errorf("partial result = %+v, want none", got)
	}
}

func TestBuildNoSourcesIsErrNotConfigured(t *testing.T) {
	if _, err := Build(context.Background(), nil, overview.Overview{}); !errors.Is(err, forge.ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}

func TestBuildOpenBlockerInOtherRepoKeepsItsRepo(t *testing.T) {
	src := &fakeForge{t: t, planning: forge.Planning{Issues: []forge.PlanIssue{
		{Number: 1, BlockedBy: []forge.Blocker{{Repo: "o/other", Issue: 9, State: "OPEN"}}},
	}}}
	got, err := Build(context.Background(), []Source{{Slug: "o/a", Src: src}}, overview.Overview{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if b := got[0].Issues[0].BlockedBy; len(b) != 1 || b[0] != (Blocker{Repo: "o/other", Issue: 9}) {
		t.Errorf("BlockedBy = %+v, want o/other#9", b)
	}
}

func TestBuildLabelKeyOpenQuestionAndClosed(t *testing.T) {
	labels := repoconfig.Labels{NeedsShaping: "needs-shaping", NeedsDecision: "needs-decision", BlockedIssue: "blocked"}
	src := &fakeForge{t: t, planning: forge.Planning{
		Issues: []forge.PlanIssue{
			{Number: 1, Labels: []string{"needs-shaping"}},
			{Number: 2, Labels: []string{"blocked", "needs-decision"}}, // the rank of labels.md: needs_decision wins
			{Number: 3, Labels: []string{"unrelated"}},
			{Number: 4, Labels: []string{""}},
			{Number: 5},
		},
		Milestones: []forge.MilestoneCount{{Milestone: forge.Milestone{Number: 7, Title: "M"}, Done: 1,
			Closed: []forge.ClosedIssue{{Number: 9, Title: "nine", URL: "u9"}}}},
	}}
	ov := overview.Overview{Questions: []overview.Question{{Repo: "o/a", Number: 5}, {Repo: "o/other", Number: 1}}}

	got, err := Build(context.Background(), []Source{{Slug: "o/a", Src: src, Labels: labels}}, ov)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var keys []string
	var questions []bool
	for _, i := range got[0].Issues {
		keys = append(keys, i.Label)
		questions = append(questions, i.OpenQuestion)
	}
	if want := []string{"needs_shaping", "needs_decision", "", "", ""}; !reflect.DeepEqual(keys, want) {
		t.Errorf("labels = %q, want %q", keys, want)
	}
	if want := []bool{false, false, false, false, true}; !reflect.DeepEqual(questions, want) {
		t.Errorf("open questions = %v, want %v (only o/a#5; o/other#1 is another repo)", questions, want)
	}
	if want := []ClosedIssue{{Number: 9, Title: "nine", URL: "u9"}}; !reflect.DeepEqual(got[0].Milestones[0].Closed, want) {
		t.Errorf("Closed = %+v, want %+v", got[0].Milestones[0].Closed, want)
	}
}

func TestBuildEmptyLabelNameNeverMatches(t *testing.T) {
	src := &fakeForge{t: t, planning: forge.Planning{Issues: []forge.PlanIssue{{Number: 1, Labels: []string{""}}}}}
	got, err := Build(context.Background(), []Source{{Slug: "o/a", Src: src}}, overview.Overview{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if l := got[0].Issues[0].Label; l != "" {
		t.Errorf("label = %q, want empty: an empty configured name matches nothing", l)
	}
}

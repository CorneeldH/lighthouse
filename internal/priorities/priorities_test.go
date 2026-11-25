package priorities

import (
	"context"
	"encoding/json"
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
			{Number: 1, Title: "one", URL: "u1", Milestone: &forge.Milestone{Number: 7, Title: "M", State: "open", URL: "<fake-url>"}},
			{Number: 2, Title: "two", URL: "u2"},
			{Number: 3, Title: "three", URL: "u3", Milestone: &forge.Milestone{Number: 8, Title: "Old", State: "closed"}},
		},
		Milestones: []forge.MilestoneCount{{Milestone: forge.Milestone{Number: 7, Title: "M", Description: "d", URL: "<fake-url>"}, Open: 3, Done: 4}},
	}}
	statuses := []overview.IssueStatus{{Repo: "o/a", Issue: 1, Status: "Controller"}, {Repo: "o/b", Issue: 2, Status: "Other"}}

	got, err := Build(context.Background(), []Source{{Slug: "o/a", Src: src}}, overview.Overview{Statuses: statuses})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "o/a" || len(got[0].Issues) != 3 {
		t.Fatalf("Build = %+v", got)
	}
	i1, i2 := got[0].Issues[0], got[0].Issues[1]
	if i1.Status != "Controller" {
		t.Errorf("issue 1 status = %q, want Controller", i1.Status)
	}
	if i2.Status != "" {
		t.Errorf("issue 2 (status only in another repo) = %q, want empty", i2.Status)
	}
	if i1.Milestone == nil || *i1.Milestone != (MilestoneRef{Number: 7, Title: "M", State: "open", URL: "<fake-url>"}) || i2.Milestone != nil {
		t.Errorf("milestones = %+v, %+v", i1.Milestone, i2.Milestone)
	}
	if i3 := got[0].Issues[2]; i3.Milestone == nil || i3.Milestone.URL != "" {
		t.Errorf("issue 3 milestone without URL = %+v, want URL empty", i3.Milestone)
	}
	if want := (Milestone{Number: 7, Title: "M", Description: "d", URL: "<fake-url>", Open: 3, Done: 4, Closed: []ClosedIssue{}}); len(got[0].Milestones) != 1 || !reflect.DeepEqual(got[0].Milestones[0], want) {
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
	labels := repoconfig.Labels{NeedsMind: "needs-mind", BlockedIssue: "blocked"}
	src := &fakeForge{t: t, planning: forge.Planning{
		Issues: []forge.PlanIssue{
			{Number: 1, Labels: []string{"needs-mind"}},
			{Number: 2, Labels: []string{"blocked", "needs-mind"}}, // Keys order: needs_mind first
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
	var keys [][]string
	var questions []bool
	for _, i := range got[0].Issues {
		keys = append(keys, i.Labels)
		questions = append(questions, i.OpenQuestion)
	}
	// Never nil, so the JSON holds [] and not null.
	if want := [][]string{{"needs_mind"}, {"needs_mind", "blocked_issue"}, {}, {}, {}}; !reflect.DeepEqual(keys, want) {
		t.Errorf("labels = %#v, want %#v", keys, want)
	}
	if want := []bool{false, false, false, false, true}; !reflect.DeepEqual(questions, want) {
		t.Errorf("open questions = %v, want %v (only o/a#5; o/other#1 is another repo)", questions, want)
	}
	if want := []ClosedIssue{{Number: 9, Title: "nine", URL: "u9"}}; !reflect.DeepEqual(got[0].Milestones[0].Closed, want) {
		t.Errorf("Closed = %+v, want %+v", got[0].Milestones[0].Closed, want)
	}
}

// Every axis shows: a blockade next to needs_mind does not hide the needs_mind key.
func TestBuildLabelsNeedsMindBesideBlockedExternal(t *testing.T) {
	labels := repoconfig.Labels{NeedsMind: "needs-mind", BlockedExternal: "blocked-external"}
	src := &fakeForge{t: t, planning: forge.Planning{Issues: []forge.PlanIssue{
		{Number: 1, Labels: []string{"needs-mind", "blocked-external"}},
	}}}
	got, err := Build(context.Background(), []Source{{Slug: "o/a", Src: src, Labels: labels}}, overview.Overview{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if want := []string{"needs_mind", "blocked_external"}; !reflect.DeepEqual(got[0].Issues[0].Labels, want) {
		t.Errorf("labels = %v, want %v", got[0].Issues[0].Labels, want)
	}
}

func TestBuildEmptyLabelNameNeverMatches(t *testing.T) {
	src := &fakeForge{t: t, planning: forge.Planning{Issues: []forge.PlanIssue{{Number: 1, Labels: []string{""}}}}}
	got, err := Build(context.Background(), []Source{{Slug: "o/a", Src: src}}, overview.Overview{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if l := got[0].Issues[0].Labels; l == nil || len(l) != 0 {
		t.Errorf("labels = %#v, want empty non-nil: an empty configured name matches nothing", l)
	}
}

// A closed issue (Review Focus 5) reaches the API only as a ClosedIssue of a milestone, which
// carries no labels and no blocked_by, whatever labels the closed issue had.
func TestBuildClosedIssueCarriesNoLabels(t *testing.T) {
	src := &fakeForge{t: t, planning: forge.Planning{
		Milestones: []forge.MilestoneCount{{Milestone: forge.Milestone{Number: 7, Title: "M"}, Done: 1,
			Closed: []forge.ClosedIssue{{Number: 9, Title: "nine", URL: "u9"}}}},
	}}
	labels := repoconfig.Labels{BlockedIssue: "blocked"}
	got, err := Build(context.Background(), []Source{{Slug: "o/a", Src: src, Labels: labels}}, overview.Overview{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	raw, err := json.Marshal(got[0].Milestones[0].Closed)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "labels") || strings.Contains(string(raw), "blocked_by") {
		t.Errorf("closed = %s, want no labels and no blocked_by", raw)
	}
}

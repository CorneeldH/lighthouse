package labelmigrate

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

var newLabels = repoconfig.Labels{
	BlockedIssue: "blocked-issue", BlockedExternal: "blocked-external",
	NeedsMind: "3-needs-mind", NeedsHands: "3-needs-hands",
}

// fakeForge is an in-memory forge: issue labels, board status and the label list.
type fakeForge struct {
	forge.Forge
	issues   []forge.PlanIssue
	labels   []string
	status   map[int]string
	relabels int
	// failRelabel is the 1-based relabel call that applies its add and then fails; 0 is never.
	failRelabel int
}

func (f *fakeForge) Planning(context.Context) (forge.Planning, error) {
	out := forge.Planning{}
	for _, is := range f.issues {
		is.Labels = slices.Clone(is.Labels)
		out.Issues = append(out.Issues, is)
	}
	return out, nil
}

func (f *fakeForge) Labels(context.Context) ([]string, error) { return slices.Clone(f.labels), nil }

func (f *fakeForge) CreateLabel(_ context.Context, name, _ string) error {
	if !slices.Contains(f.labels, name) {
		f.labels = append(f.labels, name)
	}
	return nil
}

func (f *fakeForge) DeleteLabel(_ context.Context, name string) error {
	f.labels = slices.DeleteFunc(f.labels, func(s string) bool { return s == name })
	for i := range f.issues {
		f.issues[i].Labels = slices.DeleteFunc(f.issues[i].Labels, func(s string) bool { return s == name })
	}
	return nil
}

func (f *fakeForge) EditIssue(_ context.Context, n int, e forge.IssueEdit) (forge.Issue, error) {
	f.relabels++
	for i := range f.issues {
		if f.issues[i].Number != n {
			continue
		}
		for _, a := range e.AddLabels {
			if !slices.Contains(f.issues[i].Labels, a) {
				f.issues[i].Labels = append(f.issues[i].Labels, a)
			}
		}
		if f.failRelabel == f.relabels {
			return forge.Issue{}, errors.New("boom")
		}
		f.issues[i].Labels = slices.DeleteFunc(f.issues[i].Labels, func(s string) bool { return slices.Contains(e.RemoveLabels, s) })
	}
	return forge.Issue{Number: n}, nil
}

func (f *fakeForge) IssueItem(_ context.Context, n int) (forge.BoardItem, bool, error) {
	s, ok := f.status[n]
	return forge.BoardItem{Issue: n, Status: s}, ok, nil
}

func (f *fakeForge) SetStatus(_ context.Context, it forge.BoardItem, status string) error {
	f.status[it.Issue] = status
	return nil
}

func (f *fakeForge) state() string {
	var parts []string
	for _, is := range f.issues {
		l := slices.Clone(is.Labels)
		sort.Strings(l)
		parts = append(parts, strings.Join(l, ","))
	}
	l := slices.Clone(f.labels)
	sort.Strings(l)
	return strings.Join(parts, "|") + "#" + strings.Join(l, ",")
}

func issue(n int, labels ...string) forge.PlanIssue {
	return forge.PlanIssue{Number: n, Labels: labels}
}

func newFake(issues ...forge.PlanIssue) *fakeForge {
	return &fakeForge{issues: issues, status: map[int]string{},
		labels: []string{"needs-shaping", "needs-decision", "needs-testing", "bug"}}
}

func build(t *testing.T, f *fakeForge, runs RunLookup) []Step {
	t.Helper()
	steps, err := BuildPlan(t.Context(), f, OldDefaults, newLabels, runs)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	return steps
}

func kinds(steps []Step) []string {
	var k []string
	for _, s := range steps {
		k = append(k, s.Kind)
	}
	return k
}

func find(steps []Step, kind string) []Step {
	var out []Step
	for _, s := range steps {
		if s.Kind == kind {
			out = append(out, s)
		}
	}
	return out
}

func TestBuildPlanMergesShapingAndDecision(t *testing.T) {
	f := newFake(issue(1, "needs-shaping", "needs-decision", "bug"))
	rel := find(build(t, f, nil), "relabel")
	if len(rel) != 1 || !slices.Equal(rel[0].Add, []string{"3-needs-mind"}) ||
		!slices.Equal(rel[0].Remove, []string{"needs-shaping", "needs-decision"}) {
		t.Fatalf("relabel = %+v, want one with Add=[3-needs-mind] and both old names removed", rel)
	}
}

func TestBuildPlanOldDefaultsWithoutLabelsTable(t *testing.T) {
	f := newFake(issue(1, "needs-decision"))
	if rel := find(build(t, f, nil), "relabel"); len(rel) != 1 || rel[0].Add[0] != "3-needs-mind" {
		t.Fatalf("relabel = %+v", rel)
	}
}

func TestBuildPlanTestingToHands(t *testing.T) {
	f := newFake(issue(1, "needs-testing"), issue(2, "needs-testing", "3-needs-hands"))
	rel := find(build(t, f, nil), "relabel")
	if len(rel) != 2 {
		t.Fatalf("relabel = %+v", rel)
	}
	if !slices.Equal(rel[0].Add, []string{"3-needs-hands"}) || !slices.Equal(rel[0].Remove, []string{"needs-testing"}) {
		t.Errorf("issue 1 = %+v", rel[0])
	}
	if len(rel[1].Add) != 0 || !slices.Equal(rel[1].Remove, []string{"needs-testing"}) {
		t.Errorf("issue 2 already has hands, got %+v", rel[1])
	}
}

func TestBuildPlanBlocksMindWithOpenBlocker(t *testing.T) {
	blocked := issue(1, "needs-shaping")
	blocked.BlockedBy = []forge.Blocker{{Issue: 9, State: "OPEN"}}
	already := issue(2, "3-needs-mind", "blocked-issue")
	already.BlockedBy = blocked.BlockedBy
	free := issue(3, "needs-shaping")
	f := newFake(blocked, already, free)
	blocks := find(build(t, f, nil), "block")
	if len(blocks) != 1 || blocks[0].Issue != 1 || !slices.Equal(blocks[0].Add, []string{"blocked-issue"}) {
		t.Fatalf("block = %+v, want only issue 1", blocks)
	}
}

func TestBuildPlanInProgressBlockedNoRun(t *testing.T) {
	mk := func(status string) *fakeForge {
		is := issue(1, "blocked-external")
		f := newFake(is)
		f.status[1] = status
		return f
	}
	lookup := func(state string) RunLookup { return func(int) (string, error) { return state, nil } }

	st := find(build(t, mk("In progress"), lookup("done")), "status")
	if len(st) != 1 || st[0].Issue != 1 || st[0].Status != "To do" {
		t.Errorf("status = %+v, want To do for issue 1", st)
	}
	for _, s := range []string{"open", "parked"} {
		if st := find(build(t, mk("In progress"), lookup(s)), "status"); len(st) != 0 {
			t.Errorf("run %s: status = %+v, want none", s, st)
		}
	}
	if st := find(build(t, mk("Controller"), lookup("done")), "status"); len(st) != 0 {
		t.Errorf("Controller: status = %+v, want none", st)
	}
	if _, err := BuildPlan(t.Context(), mk("In progress"), OldDefaults, newLabels, nil); err == nil ||
		!strings.Contains(err.Error(), "stap 3 vraagt de runs-database") {
		t.Errorf("nil RunLookup err = %v", err)
	}
}

func TestBuildPlanInProgressWithoutBlockIsLeft(t *testing.T) {
	f := newFake(issue(1))
	f.status[1] = "In progress"
	steps := build(t, f, func(int) (string, error) { return "done", nil })
	if len(find(steps, "status")) != 0 {
		t.Errorf("unblocked issue got a status step: %+v", steps)
	}
}

func TestBuildPlanCreatesMindLabelOnce(t *testing.T) {
	f := newFake()
	cr := find(build(t, f, nil), "create-label")
	if len(cr) != 1 || cr[0].Add[0] != "3-needs-mind" {
		t.Fatalf("create-label = %+v", cr)
	}
	f.labels = append(f.labels, "3-needs-mind")
	if cr := find(build(t, f, nil), "create-label"); len(cr) != 0 {
		t.Errorf("label exists, got %+v", cr)
	}
}

func TestBuildPlanDeletesLabelsLast(t *testing.T) {
	f := newFake(issue(1, "needs-shaping"))
	f.labels = []string{"needs-shaping", "needs-testing"} // needs-decision is not on the forge
	steps := build(t, f, nil)
	var del []string
	seenDelete := false
	for _, s := range steps {
		if s.Kind == "delete-label" {
			seenDelete = true
			del = append(del, s.Remove[0])
		} else if seenDelete {
			t.Fatalf("step %q after a delete-label: %v", s.Kind, kinds(steps))
		}
	}
	if !slices.Equal(del, []string{"needs-shaping", "needs-testing"}) {
		t.Errorf("deleted = %v", del)
	}
}

func TestExecuteResumesAfterFailure(t *testing.T) {
	mk := func() *fakeForge {
		return newFake(issue(1, "needs-shaping"), issue(2, "needs-decision", "needs-shaping"), issue(3, "needs-testing"))
	}
	run := func(f *fakeForge) error {
		steps, err := BuildPlan(t.Context(), f, OldDefaults, newLabels, nil)
		if err != nil {
			t.Fatal(err)
		}
		return Execute(t.Context(), f, steps, &bytes.Buffer{})
	}
	clean := mk()
	if err := run(clean); err != nil {
		t.Fatal(err)
	}

	f := mk()
	f.failRelabel = 2
	if err := run(f); err == nil {
		t.Fatal("want the second relabel to fail")
	}
	if got := f.issues[1].Labels; !slices.Contains(got, "3-needs-mind") || !slices.Contains(got, "needs-decision") {
		t.Fatalf("half-way state = %v, want new label with old still on", got)
	}
	if err := run(f); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if f.state() != clean.state() {
		t.Errorf("state after resume = %s, clean run = %s", f.state(), clean.state())
	}
	if steps := build(t, f, nil); len(steps) != 0 {
		t.Errorf("third plan = %+v, want empty", steps)
	}
}

func TestExecuteOrderAndOutput(t *testing.T) {
	f := newFake()
	steps := []Step{
		{Kind: "delete-label", Remove: []string{"needs-testing"}},
		{Kind: "status", Issue: 1, Status: "To do"},
		{Kind: "block", Issue: 1, Add: []string{"blocked-issue"}},
		{Kind: "relabel", Issue: 1, Add: []string{"3-needs-mind"}, Remove: []string{"needs-shaping"}},
		{Kind: "create-label", Add: []string{"3-needs-mind"}},
	}
	f.issues = []forge.PlanIssue{issue(1)}
	f.status[1] = "In progress"
	var out bytes.Buffer
	if err := Execute(t.Context(), f, steps, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("output = %q", out.String())
	}
	for i, k := range []string{"create-label", "relabel", "block", "status", "delete-label"} {
		if !strings.Contains(lines[i], k) {
			t.Errorf("line %d = %q, want %s", i, lines[i], k)
		}
	}
	if f.status[1] != "To do" {
		t.Errorf("status = %q", f.status[1])
	}
}

func TestBuildPlanClosedBlockerDoesNotBlock(t *testing.T) {
	mind := issue(1, "needs-shaping")
	mind.BlockedBy = []forge.Blocker{{Issue: 9, State: "CLOSED"}}
	prog := issue(2)
	prog.BlockedBy = []forge.Blocker{{Issue: 9, State: "CLOSED"}}
	f := newFake(mind, prog)
	f.status[2] = "In progress"
	steps := build(t, f, func(int) (string, error) { return "done", nil })
	if b := find(steps, "block"); len(b) != 0 {
		t.Errorf("closed blocker produced block steps: %+v", b)
	}
	if s := find(steps, "status"); len(s) != 0 {
		t.Errorf("closed blocker produced status steps: %+v", s)
	}
}

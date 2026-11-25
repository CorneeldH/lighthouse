package report

import (
	"fmt"
	"testing"
)

func blockOf(t *testing.T, r ComponentsReport, name string) Block {
	t.Helper()
	for _, b := range r.Blocks {
		if b.Name == name {
			return b
		}
	}
	t.Fatalf("no block %q", name)
	return Block{}
}

func groupOf(t *testing.T, b Block, name string) Group {
	t.Helper()
	for _, g := range b.Groups {
		if g.Name == name {
			return g
		}
	}
	t.Fatalf("block %q has no group %q (groups %+v)", b.Name, name, b.Groups)
	return Group{}
}

func wantValue(t *testing.T, what string, m Measure, value float64, n int) {
	t.Helper()
	if m.Value == nil || *m.Value != value || m.N != n {
		t.Errorf("%s = %s; want %v with n=%d", what, fmtMeasure(m), value, n)
	}
}

func wantNull(t *testing.T, what string, m Measure, n int) {
	t.Helper()
	if m.Value != nil || m.N != n || m.Reason == "" {
		t.Errorf("%s = %s; want null with n=%d and a reason", what, fmtMeasure(m), n)
	}
}

func fmtMeasure(m Measure) string {
	v := "nil"
	if m.Value != nil {
		v = fmt.Sprint(*m.Value)
	}
	return fmt.Sprintf("{value %s, n %d, reason %q}", v, m.N, m.Reason)
}

func TestComponentsBlockOrder(t *testing.T) {
	db, _ := newFixture(t)
	r := Components(load(t, db), Filter{})
	want := []string{"plan_review", "spec_review", "lane", "model", "harness",
		"task_review", "final_review", "pr_review", "test_gate"}
	if len(r.Blocks) != len(want) {
		t.Fatalf("blocks = %d; want %d", len(r.Blocks), len(want))
	}
	for i, b := range r.Blocks {
		if b.Name != want[i] {
			t.Errorf("block %d = %q; want %q", i, b.Name, want[i])
		}
	}
}

func TestComponentsPlanReviewSplitsWithAndWithout(t *testing.T) {
	db, f := newFixture(t)
	small := f.addTrajectory("o/r", 1, withPlan(2))
	f.addReview(small, "plan_review", at(0), 1)
	f.addTrajectory("o/r", 2, withPlan(3), createdAt(at(5)))
	large := f.addTrajectory("o/r", 3, withPlan(12))
	f.addReview(large, "plan_review", at(1), 0)
	f.addTrajectory("o/r", 4, withPlan(10), createdAt(at(5)))
	f.addTrajectory("o/r", 5, withPlan(11), createdAt(at(6)))
	f.addTrajectory("o/r", 6) // no plan: outside the population

	d := load(t, db)
	b := blockOf(t, Components(d, Filter{}), "plan_review")
	if b.Kind != "toggle" {
		t.Errorf("Kind = %q; want toggle", b.Kind)
	}
	if g := groupOf(t, b, "with"); g.Trajectories != 2 || g.Reviews != 2 {
		t.Errorf("all with: trajectories %d, reviews %d; want 2, 2", g.Trajectories, g.Reviews)
	}
	if g := groupOf(t, b, "without"); g.Trajectories != 3 {
		t.Errorf("all without: trajectories %d; want 3", g.Trajectories)
	}

	b = blockOf(t, Components(d, Filter{Size: SizeSmall}), "plan_review")
	if w, wo := groupOf(t, b, "with"), groupOf(t, b, "without"); w.Trajectories != 1 || wo.Trajectories != 1 {
		t.Errorf("small: with %d, without %d; want 1, 1", w.Trajectories, wo.Trajectories)
	}
	b = blockOf(t, Components(d, Filter{Size: SizeLarge}), "plan_review")
	if w, wo := groupOf(t, b, "with"), groupOf(t, b, "without"); w.Trajectories != 1 || wo.Trajectories != 2 {
		t.Errorf("large: with %d, without %d; want 1, 2", w.Trajectories, wo.Trajectories)
	}
}

func TestComponentsReviewWithoutFindingsCountsInDenominator(t *testing.T) {
	db, f := newFixture(t)
	a := f.addTrajectory("o/r", 1, withPlan(2))
	_, path := f.addReview(a, "plan_review", at(0), 2)
	f.addFinding(a, "plan_review", "k1", "high", "adopted", path)
	f.addFinding(a, "plan_review", "k2", "low", "rejected", path)
	b := f.addTrajectory("o/r", 2, withPlan(2))
	f.addReview(b, "plan_review", at(1), 0)

	g := groupOf(t, blockOf(t, Components(load(t, db), Filter{}), "plan_review"), "with")
	wantValue(t, "Found", g.Found, 0.5, 2)
	if g.Findings != 2 || g.BySeverity["high"] != 1 || g.BySeverity["low"] != 1 {
		t.Errorf("Findings %d, BySeverity %v; want 2, high 1 low 1", g.Findings, g.BySeverity)
	}
}

func TestComponentsAbortedReviewCounts(t *testing.T) {
	db, f := newFixture(t)
	a := f.addTrajectory("o/r", 1, withPlan(2))
	f.addReview(a, "plan_review", at(0), 1)
	b := f.addTrajectory("o/r", 2, withPlan(2))
	id, _ := f.addReview(b, "plan_review", at(1), 0)
	f.exec(`UPDATE review_runs SET outcome = 'aborted' WHERE id = ?`, id)

	g := groupOf(t, blockOf(t, Components(load(t, db), Filter{}), "plan_review"), "with")
	if g.Reviews != 2 {
		t.Errorf("Reviews = %d; want 2", g.Reviews)
	}
	wantValue(t, "Found", g.Found, 0.5, 2)
}

func TestComponentsAdoptedRatioKeepsLeftOpenSeparate(t *testing.T) {
	db, f := newFixture(t)
	a := f.addTrajectory("o/r", 1, withPlan(2))
	_, path := f.addReview(a, "plan_review", at(0), 5)
	f.addFinding(a, "plan_review", "k1", "high", "adopted", path)
	f.addFinding(a, "plan_review", "k2", "high", "adopted", path)
	f.addFinding(a, "plan_review", "k3", "high", "rejected", path)
	f.addFinding(a, "plan_review", "k4", "high", "left_open", path)
	f.addFinding(a, "plan_review", "k5", "high", "", path)
	f.addFinding(a, "spec_review", "k6", "high", "rejected", path) // other component

	g := groupOf(t, blockOf(t, Components(load(t, db), Filter{}), "plan_review"), "with")
	wantValue(t, "Adopted", g.Adopted, 2.0/3.0, 3)
	if g.LeftOpen != 1 || g.Undecided != 1 || g.Findings != 5 {
		t.Errorf("LeftOpen %d, Undecided %d, Findings %d; want 1, 1, 5", g.LeftOpen, g.Undecided, g.Findings)
	}
}

func TestComponentsCohortExcludesEarlierPlans(t *testing.T) {
	db, f := newFixture(t)
	f.addTrajectory("o/r", 1, withPlan(2), createdAt(at(-10))) // before the cohort
	r := f.addTrajectory("o/r", 2, withPlan(2))
	f.addReview(r, "plan_review", at(0), 0)
	f.addTrajectory("o/r", 3, withPlan(2), createdAt(at(10)))

	b := blockOf(t, Components(load(t, db), Filter{}), "plan_review")
	if g := groupOf(t, b, "without"); g.Trajectories != 1 {
		t.Errorf("without = %d; want 1 (only the plan after the cohort)", g.Trajectories)
	}
	if b.Outside.BeforeCohort != 1 {
		t.Errorf("BeforeCohort = %d; want 1", b.Outside.BeforeCohort)
	}
	// spec_review has no linked review in this repo: every plan is before its cohort.
	sb := blockOf(t, Components(load(t, db), Filter{}), "spec_review")
	if sb.Outside.BeforeCohort != 3 || groupOf(t, sb, "without").Trajectories != 0 {
		t.Errorf("spec_review: BeforeCohort %d, without %d; want 3, 0",
			sb.Outside.BeforeCohort, groupOf(t, sb, "without").Trajectories)
	}
}

func TestComponentsCostNilWhenUnpriced(t *testing.T) {
	db, f := newFixture(t)
	for i, cost := range []*float64{fp(1), fp(3), nil} {
		tid := f.addTrajectory("o/r", i+1)
		run := f.addRun(tid, "standard", at(0), at(10))
		f.addSetup(ofRun(run), "sdd", "", "")
		f.addUsage(ofRun(run), "opus", 10, cost, "estimate")
		f.addRecovery(tid, 10+i, "none", at(60), nil)
	}
	tid := f.addTrajectory("o/r", 9)
	run := f.addRun(tid, "standard", at(0), at(10))
	f.addSetup(ofRun(run), "native", "", "")
	f.addUsage(ofRun(run), "opus", 10, nil, "")
	f.addRecovery(tid, 19, "none", at(60), nil)

	b := blockOf(t, Components(load(t, db), Filter{}), "lane")
	sdd := groupOf(t, b, "sdd")
	wantValue(t, "sdd Cost", sdd.Cost, 2, 2)
	if sdd.CostKind != "estimate" {
		t.Errorf("sdd CostKind = %q; want estimate", sdd.CostKind)
	}
	native := groupOf(t, b, "native")
	wantNull(t, "native Cost", native.Cost, 0)
	if native.CostKind != "" {
		t.Errorf("native CostKind = %q; want empty", native.CostKind)
	}
}

func TestComponentsPerMeasureN(t *testing.T) {
	db, f := newFixture(t)
	withRuns := f.addTrajectory("o/r", 1, withPlan(2))
	f.addReview(withRuns, "plan_review", at(0), 0)
	run := f.addRun(withRuns, "standard", at(0), at(30))
	f.addRecovery(withRuns, 10, "recovered", at(60), nil, "fix", "revert")
	f.addQuestion(withRuns, ofRun(run), at(5))
	noRuns := f.addTrajectory("o/r", 2, withPlan(2))
	f.addReview(noRuns, "plan_review", at(1), 0)
	f.addRecovery(noRuns, 11, "none", at(60), nil)

	g := groupOf(t, blockOf(t, Components(load(t, db), Filter{}), "plan_review"), "with")
	wantValue(t, "Recovered", g.Recovered, 0.5, 2)
	wantValue(t, "RecoveryItems", g.RecoveryItems, 1, 2)
	// The trajectory without a run or session has no source for questions: it is absent, not 0.
	wantValue(t, "Questions", g.Questions, 1, 1)
	if g.Active.N != 1 || g.Active.Value == nil || g.HumanWait.N != 1 {
		t.Errorf("Active %s, HumanWait %s; want n=1 each", fmtMeasure(g.Active), fmtMeasure(g.HumanWait))
	}
}

func TestComponentsLabelCorrelationAndAssigned(t *testing.T) {
	db, f := newFixture(t)
	a := f.addTrajectory("o/r", 1)
	f.addSetup(ofRun(f.addRun(a, "variant:lane=sdd", at(0), at(10))), "sdd", "", "")
	b := f.addTrajectory("o/r", 2)
	f.addSetup(ofRun(f.addRun(b, "standard", at(0), at(10))), "native", "", "")

	pa := f.addTrajectory("o/r", 4, withPlan(2))
	f.addRun(pa, "variant:plan_review=on", at(0), at(10))
	f.addReview(pa, "plan_review", at(0), 0)
	pb := f.addTrajectory("o/r", 5, withPlan(2))
	f.addRun(pb, "standard", at(0), at(10))

	r := Components(load(t, db), Filter{})
	if pr := blockOf(t, r, "plan_review"); pr.Label != LabelAssigned || pr.LabelNote != LabelNote(LabelAssigned) {
		t.Errorf("plan_review: Label %q, LabelNote %q; want assigned", pr.Label, pr.LabelNote)
	}
	lane := blockOf(t, r, "lane")
	if lane.Label != LabelAssigned || lane.LabelNote != LabelNote(LabelAssigned) {
		t.Errorf("pure: Label %q, LabelNote %q; want assigned", lane.Label, lane.LabelNote)
	}

	c := f.addTrajectory("o/r", 3)
	f.addSetup(ofRun(f.addRun(c, "variant:lane=sdd", at(0), at(10))), "native", "", "")
	f.addRun(c, "standard", at(20), at(30)) // c is mixed
	lane = blockOf(t, Components(load(t, db), Filter{}), "lane")
	if lane.Label != LabelCorrelation || lane.LabelNote != LabelNote(LabelCorrelation) {
		t.Errorf("mixed: Label %q, LabelNote %q; want correlation", lane.Label, lane.LabelNote)
	}
}

func TestComponentsFilterAppliesToEveryBlock(t *testing.T) {
	db, f := newFixture(t)
	for i, repo := range []string{"o/a", "o/b"} {
		tid := f.addTrajectory(repo, i+1, withPlan(2))
		f.addReview(tid, "plan_review", at(0), 0)
		f.addReview(tid, "spec_review", at(0), 0)
		run := f.addRun(tid, "standard", at(0), at(10))
		f.addSetup(ofRun(run), "sdd", "opus", "claude-code")
	}
	r := Components(load(t, db), Filter{Repo: "o/a"})
	for _, name := range []string{"plan_review", "spec_review", "lane", "model", "harness"} {
		total := 0
		for _, g := range blockOf(t, r, name).Groups {
			total += g.Trajectories
		}
		if total != 1 {
			t.Errorf("block %s: trajectories = %d; want 1 under the repo filter", name, total)
		}
	}
	if got := r.Options.Repos; len(got) != 2 || got[0] != "o/a" || got[1] != "o/b" {
		t.Errorf("Options.Repos = %v; want both repos regardless of the filter", got)
	}
}

func TestComponentsUnmeasuredBlocksHaveReason(t *testing.T) {
	db, _ := newFixture(t)
	r := Components(load(t, db), Filter{})
	for _, name := range []string{"task_review", "final_review", "pr_review", "test_gate"} {
		b := blockOf(t, r, name)
		if b.Kind != "unmeasured" || b.Reason == "" || len(b.Groups) != 0 {
			t.Errorf("block %s: Kind %q, Reason %q, groups %d; want unmeasured with reason, no groups",
				name, b.Kind, b.Reason, len(b.Groups))
		}
	}
	if got := blockOf(t, r, "final_review").Reason; got != "no source in this milestone" {
		t.Errorf("final_review reason = %q", got)
	}
	for _, name := range []string{"plan_review", "spec_review"} {
		if got := blockOf(t, r, name).Reason; got != "not measured — review costs are not recorded" {
			t.Errorf("%s reason = %q", name, got)
		}
	}
}

func TestComponentsOpenWindowIsOutside(t *testing.T) {
	db, f := newFixture(t)
	open := f.addTrajectory("o/r", 1, withPlan(2))
	f.addReview(open, "plan_review", at(0), 0)
	f.addRecovery(open, 10, "recovered", at(10), nil, "fix")
	f.addRecovery(open, 11, "open", at(20), nil)
	none := f.addTrajectory("o/r", 2, withPlan(2))
	f.addReview(none, "plan_review", at(1), 0)
	done := f.addTrajectory("o/r", 3, withPlan(2))
	f.addReview(done, "plan_review", at(2), 0)
	f.addRecovery(done, 12, "none", at(10), nil)

	b := blockOf(t, Components(load(t, db), Filter{}), "plan_review")
	if b.Outside.Running != 1 || b.Outside.NoRecovery != 1 {
		t.Errorf("Outside = %+v; want running 1, no recovery 1", b.Outside)
	}
	g := groupOf(t, b, "with")
	if g.Trajectories != 3 {
		t.Errorf("Trajectories = %d; want 3", g.Trajectories)
	}
	wantValue(t, "Recovered", g.Recovered, 0, 1)
}

func TestComponentsLaneOutsideCountsMixedAndUnknown(t *testing.T) {
	db, f := newFixture(t)
	mixed := f.addTrajectory("o/r", 1)
	f.addSetup(ofRun(f.addRun(mixed, "standard", at(0), at(10))), "sdd", "", "")
	f.addSetup(ofRun(f.addRun(mixed, "standard", at(20), at(30))), "native", "", "")
	f.addTrajectory("o/r", 2) // no lane

	b := blockOf(t, Components(load(t, db), Filter{}), "lane")
	if b.Outside.MixedLane != 1 || b.Outside.NoLane != 1 {
		t.Errorf("Outside = %+v; want mixed 1, no lane 1", b.Outside)
	}
}

func TestComponentsEmptyGroupHasNullNotZero(t *testing.T) {
	db, f := newFixture(t)
	a := f.addTrajectory("o/r", 1)
	f.addSetup(ofRun(f.addRun(a, "standard", at(0), at(10))), "sdd", "opus", "claude-code")

	r := Components(load(t, db), Filter{})
	native := groupOf(t, blockOf(t, r, "lane"), "native")
	if native.Trajectories != 0 {
		t.Fatalf("native trajectories = %d; want 0", native.Trajectories)
	}
	for what, m := range map[string]Measure{"Found": native.Found, "Adopted": native.Adopted,
		"Cost": native.Cost, "Recovered": native.Recovered, "RecoveryItems": native.RecoveryItems,
		"Questions": native.Questions, "Active": native.Active, "HumanWait": native.HumanWait} {
		wantNull(t, "native "+what, m, 0)
	}
	if native.Found.Reason != "not applicable" {
		t.Errorf("lane Found reason = %q; want not applicable", native.Found.Reason)
	}
	without := groupOf(t, blockOf(t, r, "plan_review"), "without")
	wantNull(t, "plan_review without Found", without.Found, 0)
	if without.Found.Reason != "not applicable" {
		t.Errorf("without Found reason = %q; want not applicable", without.Found.Reason)
	}
	with := groupOf(t, blockOf(t, r, "plan_review"), "with")
	wantNull(t, "plan_review with Found", with.Found, 0)
	if with.Found.Reason == "not applicable" {
		t.Errorf("empty with group: Found reason %q; want a missing-data reason", with.Found.Reason)
	}
	if g := groupOf(t, blockOf(t, r, "model"), "opus"); g.Trajectories != 1 {
		t.Errorf("model opus trajectories = %d; want 1", g.Trajectories)
	}
	if g := groupOf(t, blockOf(t, r, "harness"), "claude-code"); g.Trajectories != 1 {
		t.Errorf("harness claude-code trajectories = %d; want 1", g.Trajectories)
	}
}

func TestFilterMatch(t *testing.T) {
	route, area := "2", "ui"
	tr := &Trajectory{Repo: "o/r", Route: &route, Area: &area, PlanTasks: intp(5), PRSize: PRBelow,
		Model: "opus", Harness: "claude-code"}
	for _, c := range []struct {
		f    Filter
		want bool
	}{
		{Filter{}, true},
		{Filter{Repo: "o/r", Route: "2", Size: SizeMedium, PRSize: PRBelow, Area: "ui", Model: "opus",
			Harness: "claude-code", Component: "plan_review"}, true},
		{Filter{Repo: "o/x"}, false},
		{Filter{Route: "1"}, false},
		{Filter{Size: SizeSmall}, false},
		{Filter{PRSize: PRAbove}, false},
		{Filter{Area: "cli"}, false},
		{Filter{Model: "sonnet"}, false},
		{Filter{Harness: "codex"}, false},
	} {
		if got := c.f.Match(tr); got != c.want {
			t.Errorf("%+v.Match = %v; want %v", c.f, got, c.want)
		}
	}
	if (Filter{Route: "2"}).Match(&Trajectory{}) || (Filter{Size: SizeSmall}).Match(&Trajectory{}) {
		t.Error("a filter on an unknown trait must not match")
	}
}

func TestMedian(t *testing.T) {
	if _, ok := median(nil); ok {
		t.Error("median(nil) ok; want false")
	}
	if v, _ := median([]float64{3, 1, 2}); v != 2 {
		t.Errorf("odd median = %v; want 2", v)
	}
	if v, _ := median([]float64{4, 1, 3, 2}); v != 2.5 {
		t.Errorf("even median = %v; want 2.5", v)
	}
}

func TestComponentsCostKindMixedAcrossTrajectories(t *testing.T) {
	db, f := newFixture(t)
	for i, kind := range []string{"estimate", "actual"} {
		tid := f.addTrajectory("o/r", i+1)
		run := f.addRun(tid, "standard", at(0), at(10))
		f.addSetup(ofRun(run), "sdd", "", "")
		f.addUsage(ofRun(run), "opus", 10, fp(1), kind)
		f.addRecovery(tid, 10+i, "none", at(60), nil)
	}
	g := groupOf(t, blockOf(t, Components(load(t, db), Filter{}), "lane"), "sdd")
	if g.CostKind != "mixed" {
		t.Errorf("CostKind = %q; want mixed", g.CostKind)
	}
}

func TestComponentsQuestionsSkipTrajectoriesWithoutRunOrSession(t *testing.T) {
	db, f := newFixture(t)
	tid := f.addTrajectory("o/r", 1, withPlan(2))
	f.addReview(tid, "plan_review", at(0), 0)
	f.addRecovery(tid, 10, "none", at(60), nil)

	g := groupOf(t, blockOf(t, Components(load(t, db), Filter{}), "plan_review"), "with")
	wantNull(t, "Questions", g.Questions, 0)
	wantValue(t, "Recovered", g.Recovered, 0, 1)
}

func TestComponentsCostOnlyOverClosedTrajectories(t *testing.T) {
	db, f := newFixture(t)
	add := func(issue int, cost float64) int64 {
		tid := f.addTrajectory("o/r", issue)
		run := f.addRun(tid, "standard", at(0), at(10))
		f.addSetup(ofRun(run), "sdd", "", "")
		f.addUsage(ofRun(run), "opus", 10, fp(cost), "estimate")
		return tid
	}
	closed := add(1, 2)
	f.addRecovery(closed, 10, "none", at(60), nil)
	running := add(2, 10)
	f.addRecovery(running, 11, "open", at(60), nil)
	add(3, 20) // no recovery measurement

	b := blockOf(t, Components(load(t, db), Filter{}), "lane")
	sdd := groupOf(t, b, "sdd")
	wantValue(t, "sdd Cost", sdd.Cost, 2, 1)
	if b.Outside.Running != 1 || b.Outside.NoRecovery != 1 {
		t.Errorf("Outside = %+v; want running 1, no recovery 1", b.Outside)
	}

	only := f.addTrajectory("o/r", 4)
	f.addSetup(ofRun(f.addRun(only, "standard", at(0), at(10))), "native", "", "")
	f.addRecovery(only, 12, "none", at(60), nil)
	native := groupOf(t, blockOf(t, Components(load(t, db), Filter{}), "lane"), "native")
	wantNull(t, "native Cost", native.Cost, 0)
	if native.Cost.Reason != ReasonNoCost {
		t.Errorf("native Cost reason = %q; want %q", native.Cost.Reason, ReasonNoCost)
	}
}

func TestComponentsOptionsOrderAndShownValues(t *testing.T) {
	db, f := newFixture(t)
	big := 10000
	for i, route := range []string{"4c", "2", "0", "4a"} {
		tid := f.addTrajectory("o/r", i+1, withRoute(route), withPlan([]int{12, 2, 5, 1}[i]))
		lines := 1
		if i == 0 {
			lines = big
		}
		f.addRecovery(tid, 10+i, "none", at(60), &lines)
	}
	mixed := f.addTrajectory("o/r", 9)
	f.addSession(mixed, "claude-code", "standard", at(0), at(10))
	f.addSession(mixed, "codex", "standard", at(0), at(10))
	one := f.addTrajectory("o/r", 10)
	f.addSession(one, "claude-code", "standard", at(0), at(10))

	o := Components(load(t, db), Filter{}).Options
	eq := func(what string, got, want []string) {
		t.Helper()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s = %v; want %v", what, got, want)
		}
	}
	eq("Sizes", o.Sizes, []string{SizeSmall, SizeMedium, SizeLarge})
	eq("PRSizes", o.PRSizes, []string{PRBelow, PRAbove})
	eq("Routes", o.Routes, []string{"0", "2", "4a", "4c"})
	eq("Harnesses", o.Harnesses, []string{"claude-code"})
	eq("Models", o.Models, []string{})
}

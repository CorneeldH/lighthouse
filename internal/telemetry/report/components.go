package report

// This file aggregates a Dataset per component: one Block per component in a
// fixed order, each with its groups and the four answers of spec §4. A
// missing measurement is a nil Value with a reason, never 0; the minimum n is
// the UI's concern, the Go side only reports n.

import (
	"sort"

	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

// Filter narrows the trajectories of every block. An empty field does not
// narrow. Component only selects in the trajectory list; Match ignores it.
type Filter struct {
	Repo      string `json:"repo"`
	Route     string `json:"route"`
	Size      string `json:"size"`
	PRSize    string `json:"pr_size"`
	Area      string `json:"area"`
	Model     string `json:"model"`
	Harness   string `json:"harness"`
	Component string `json:"component"`
}

// Match reports whether t passes every set field of f. A trajectory whose
// trait is unknown does not match a filter on that trait.
func (f Filter) Match(t *Trajectory) bool {
	size, _ := SizeClass(t.PlanTasks)
	checks := []struct{ want, have string }{
		{f.Repo, t.Repo},
		{f.Route, deref(t.Route)},
		{f.Size, size},
		{f.PRSize, t.PRSize},
		{f.Area, deref(t.Area)},
		{f.Model, t.Model},
		{f.Harness, t.Harness},
	}
	for _, c := range checks {
		if c.want != "" && c.want != c.have {
			return false
		}
	}
	return true
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Measure is one cell: a nil Value is not measured and carries a Reason.
type Measure struct {
	Value  *float64 `json:"value"`
	N      int      `json:"n"`
	Reason string   `json:"reason,omitempty"`
}

// Group is one side of a comparison. Active and HumanWait are in seconds;
// Cost, Questions, Active and HumanWait are medians. Cost and the outcome
// measures cover only the closed trajectories of the group.
type Group struct {
	Name          string         `json:"name"`
	Trajectories  int            `json:"trajectories"`
	Reviews       int            `json:"reviews"`
	Found         Measure        `json:"found"`
	Findings      int            `json:"findings"`
	BySeverity    map[string]int `json:"by_severity"`
	Adopted       Measure        `json:"adopted"`
	LeftOpen      int            `json:"left_open"`
	Undecided     int            `json:"undecided"`
	Cost          Measure        `json:"cost"`
	CostKind      string         `json:"cost_kind"`
	Recovered     Measure        `json:"recovered"`
	RecoveryItems Measure        `json:"recovery_items"`
	Questions     Measure        `json:"questions"`
	Active        Measure        `json:"active"`
	HumanWait     Measure        `json:"human_wait"`
}

// Kinds of a block.
const (
	KindToggle     = "toggle"
	KindValues     = "values"
	KindUnmeasured = "unmeasured"
)

// Block is the comparison of one component.
type Block struct {
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	Label     string  `json:"label"`
	LabelNote string  `json:"label_note"`
	Reason    string  `json:"reason,omitempty"`
	Groups    []Group `json:"groups"`
	Outside   Outside `json:"outside"`
}

// Outside counts the trajectories of a block that fall outside a comparison:
// Running and NoRecovery outside the outcome measures, NoLane and MixedLane
// outside the lane comparison, BeforeCohort outside a review comparison.
type Outside struct {
	Running      int `json:"running"`
	NoRecovery   int `json:"no_recovery"`
	NoLane       int `json:"no_lane"`
	MixedLane    int `json:"mixed_lane"`
	BeforeCohort int `json:"before_cohort"`
}

// ComponentsReport is every block plus the filter values in the data.
type ComponentsReport struct {
	Blocks  []Block `json:"blocks"`
	Options Options `json:"options"`
}

// Options are the values of each filter field that occur in the data, sorted.
type Options struct {
	Repos     []string `json:"repos"`
	Routes    []string `json:"routes"`
	Sizes     []string `json:"sizes"`
	PRSizes   []string `json:"pr_sizes"`
	Areas     []string `json:"areas"`
	Models    []string `json:"models"`
	Harnesses []string `json:"harnesses"`
}

// Reasons of a block or a measure.
const (
	ReasonReviewCosts   = "not measured — review costs are not recorded"
	ReasonNoSource      = "no source in this milestone"
	ReasonTaskReview    = "not measured — a ledger reaches the database only through a manual review-index --ledger, is deleted at the end of the SDD run, has no review per task as a unit and no adopted decision; a comparison would measure who indexed the ledger"
	ReasonNotApplicable = "not applicable"
	ReasonNoReviews     = "no reviews"
	ReasonNoDecisions   = "no findings with a decision"
	ReasonNoCost        = "no closed trajectories with a priced cost"
	ReasonNoClosed      = "no closed trajectories"
	ReasonNoTimeline    = "no closed trajectories with a run or session"
)

// Components aggregates d under f into the blocks plan_review, spec_review,
// lane, model, harness, task_review, final_review, pr_review and test_gate.
func Components(d *Dataset, f Filter) ComponentsReport {
	var in []*Trajectory
	for i := range d.Trajectories {
		if f.Match(&d.Trajectories[i]) {
			in = append(in, &d.Trajectories[i])
		}
	}
	cohorts := d.Cohorts()
	r := ComponentsReport{Options: options(d)}
	for _, c := range []string{"plan_review", "spec_review"} {
		r.Blocks = append(r.Blocks, reviewBlock(c, in, cohorts))
	}
	r.Blocks = append(r.Blocks, laneBlock(in), valuesBlock("model", in, func(t *Trajectory) string { return t.Model }),
		valuesBlock("harness", in, func(t *Trajectory) string {
			if t.Harness == "mixed" {
				return ""
			}
			return t.Harness
		}))
	r.Blocks = append(r.Blocks, unmeasured("task_review", ReasonTaskReview))
	for _, c := range []string{"final_review", "pr_review", "test_gate"} {
		r.Blocks = append(r.Blocks, unmeasured(c, ReasonNoSource))
	}
	return r
}

func unmeasured(name, reason string) Block {
	return Block{Name: name, Kind: KindUnmeasured, Reason: reason, Groups: []Group{}}
}

// reviewBlock compares the plans with and without a review of component,
// within the cohort of their repo.
func reviewBlock(component string, in []*Trajectory, cohorts Cohorts) Block {
	b := Block{Name: component, Kind: KindToggle, Reason: ReasonReviewCosts}
	var with, without []*Trajectory
	for _, t := range in {
		if t.PlanTasks == nil {
			continue
		}
		switch cohorts.Place(t, component) {
		case PlaceWith:
			with = append(with, t)
		case PlaceWithout:
			without = append(without, t)
		default:
			b.Outside.BeforeCohort++
		}
	}
	b.Groups = []Group{
		group(PlaceWith, with, component),
		group(PlaceWithout, without, ""),
	}
	finish(&b, with, without)
	return b
}

// laneBlock compares sdd with native; mixed and unknown lanes are outside.
func laneBlock(in []*Trajectory) Block {
	b := Block{Name: "lane", Kind: KindValues}
	var sdd, native []*Trajectory
	for _, t := range in {
		switch t.Lane {
		case "sdd":
			sdd = append(sdd, t)
		case "native":
			native = append(native, t)
		case "mixed":
			b.Outside.MixedLane++
		default:
			b.Outside.NoLane++
		}
	}
	b.Groups = []Group{group("sdd", sdd, ""), group("native", native, "")}
	finish(&b, sdd, native)
	return b
}

// valuesBlock has one group per known value of key, sorted by name.
func valuesBlock(name string, in []*Trajectory, key func(*Trajectory) string) Block {
	b := Block{Name: name, Kind: KindValues, Groups: []Group{}}
	byValue := map[string][]*Trajectory{}
	for _, t := range in {
		if v := key(t); v != "" {
			byValue[v] = append(byValue[v], t)
		}
	}
	values := make([]string, 0, len(byValue))
	for v := range byValue {
		values = append(values, v)
	}
	sort.Strings(values)
	groups := make([][]*Trajectory, 0, len(values))
	for _, v := range values {
		b.Groups = append(b.Groups, group(v, byValue[v], ""))
		groups = append(groups, byValue[v])
	}
	finish(&b, groups...)
	return b
}

// finish sets the label of b over the assignments of its groups and counts
// the trajectories of the groups that fall outside the outcome measures.
func finish(b *Block, groups ...[]*Trajectory) {
	assignments := make([][]string, len(groups))
	for i, g := range groups {
		assignments[i] = []string{}
		for _, t := range g {
			assignments[i] = append(assignments[i], t.Assignment)
			switch {
			case t.Closed:
			case len(t.Recoveries) == 0:
				b.Outside.NoRecovery++
			default:
				b.Outside.Running++
			}
		}
	}
	b.Label = ComparisonLabel(assignments)
	b.LabelNote = LabelNote(b.Label)
}

// group aggregates ts. A non-empty component makes the review measures
// apply to that component; otherwise they are not applicable.
func group(name string, ts []*Trajectory, component string) Group {
	g := Group{Name: name, Trajectories: len(ts), BySeverity: map[string]int{}}
	g.Found = missing(0, ReasonNotApplicable)
	g.Adopted = missing(0, ReasonNotApplicable)
	if component != "" {
		reviewMeasures(&g, ts, component)
	}

	var costs []float64
	kinds := map[string]bool{}
	var recovered, items, questions, active, human []float64
	for _, t := range ts {
		if !t.Closed {
			continue
		}
		if t.Cost != nil {
			costs = append(costs, *t.Cost)
			kinds[t.CostKind] = true
		}
		rec, n := 0.0, 0
		for _, r := range t.Recoveries {
			if r.Status == string(telemetry.RecoveryRecovered) {
				rec = 1
			}
			n += len(r.Items)
		}
		recovered = append(recovered, rec)
		items = append(items, float64(n))
		questions = append(questions, float64(t.Questions.Dispatcher+t.Questions.Transcript))
		if t.Active != nil && t.HumanWait != nil {
			active = append(active, t.Active.Seconds())
			human = append(human, t.HumanWait.Seconds())
		}
	}
	g.Cost = medianMeasure(costs, ReasonNoCost)
	switch len(kinds) {
	case 0:
	case 1:
		for k := range kinds {
			g.CostKind = k
		}
	default:
		g.CostKind = "mixed"
	}
	g.Recovered = meanMeasure(recovered, ReasonNoClosed)
	g.RecoveryItems = meanMeasure(items, ReasonNoClosed)
	g.Questions = medianMeasure(questions, ReasonNoClosed)
	g.Active = medianMeasure(active, ReasonNoTimeline)
	g.HumanWait = medianMeasure(human, ReasonNoTimeline)
	return g
}

// reviewMeasures fills Reviews, Found, Findings, BySeverity, Adopted,
// LeftOpen and Undecided from the reviews and findings of component.
// Aborted reviews count.
func reviewMeasures(g *Group, ts []*Trajectory, component string) {
	found, adopted, decided := 0, 0, 0
	for _, t := range ts {
		for _, r := range t.Reviews {
			if r.Component != component {
				continue
			}
			g.Reviews++
			if r.FindingCount > 0 {
				found++
			}
		}
		for _, f := range t.Findings {
			if f.Component != component {
				continue
			}
			g.Findings++
			g.BySeverity[f.Severity]++
			switch f.Decision {
			case "":
				g.Undecided++
			case "left_open":
				g.LeftOpen++
			case "adopted":
				adopted++
				decided++
			default:
				decided++
			}
		}
	}
	g.Found = ratio(found, g.Reviews, ReasonNoReviews)
	g.Adopted = ratio(adopted, decided, ReasonNoDecisions)
}

func missing(n int, reason string) Measure { return Measure{N: n, Reason: reason} }

func value(v float64, n int) Measure { return Measure{Value: &v, N: n} }

func ratio(part, whole int, reason string) Measure {
	if whole == 0 {
		return missing(0, reason)
	}
	return value(float64(part)/float64(whole), whole)
}

func meanMeasure(xs []float64, reason string) Measure {
	if len(xs) == 0 {
		return missing(0, reason)
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return value(sum/float64(len(xs)), len(xs))
}

func medianMeasure(xs []float64, reason string) Measure {
	m, ok := median(xs)
	if !ok {
		return missing(0, reason)
	}
	return value(m, len(xs))
}

// median returns the median of xs, the mean of the middle two for an even
// count; false for an empty slice. xs is not changed.
func median(xs []float64) (float64, bool) {
	if len(xs) == 0 {
		return 0, false
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid], true
	}
	return (s[mid-1] + s[mid]) / 2, true
}

// Fixed orders of the classed options; other values sort after them.
var (
	routeOrder  = []string{"0", "1", "2", "4a", "4b", "4c"}
	sizeOrder   = []string{SizeSmall, SizeMedium, SizeLarge}
	prSizeOrder = []string{PRBelow, PRAbove}
)

// options collects the filter values that occur in d, unfiltered. A model or
// harness the blocks never show (empty, "mixed") is left out.
func options(d *Dataset) Options {
	sets := make([]map[string]bool, 7)
	for i := range sets {
		sets[i] = map[string]bool{}
	}
	for i := range d.Trajectories {
		t := &d.Trajectories[i]
		size, _ := SizeClass(t.PlanTasks)
		harness := t.Harness
		if harness == "mixed" {
			harness = ""
		}
		for j, v := range []string{t.Repo, deref(t.Route), size, t.PRSize, deref(t.Area), t.Model, harness} {
			if v != "" {
				sets[j][v] = true
			}
		}
	}
	return Options{Repos: ordered(sets[0], nil), Routes: ordered(sets[1], routeOrder),
		Sizes: ordered(sets[2], sizeOrder), PRSizes: ordered(sets[3], prSizeOrder),
		Areas: ordered(sets[4], nil), Models: ordered(sets[5], nil), Harnesses: ordered(sets[6], nil)}
}

// ordered lists set: first the values of order that occur, then the rest
// alphabetically.
func ordered(set map[string]bool, order []string) []string {
	out := make([]string, 0, len(set))
	known := map[string]bool{}
	for _, v := range order {
		known[v] = true
		if set[v] {
			out = append(out, v)
		}
	}
	rest := make([]string, 0, len(set))
	for v := range set {
		if !known[v] {
			rest = append(rest, v)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

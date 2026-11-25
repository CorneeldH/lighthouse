package labelmigrate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// Step kinds, in the order Execute runs them.
const (
	KindCreateLabel = "create-label"
	KindRelabel     = "relabel"
	KindBlock       = "block"
	KindStatus      = "status"
	KindDeleteLabel = "delete-label"
)

var kindOrder = []string{KindCreateLabel, KindRelabel, KindBlock, KindStatus, KindDeleteLabel}

const (
	statusInProgress = "In progress"
	statusTodo       = "To do"
)

// Step is one change on the forge. create-label puts the label name in Add (colour MindColor),
// delete-label in Remove; relabel and block edit issue Issue; status moves it to Status.
type Step struct {
	Kind        string
	Issue       int
	Add, Remove []string
	Status      string
}

// RunLookup returns the run state ("open", "parked", ...) of an issue; nil means there is no
// runs database.
type RunLookup func(issue int) (state string, err error)

// BuildPlan reads the forge and returns the steps still needed to move it from the old labels
// to l, in execution order. A step whose result is already on the forge is not in the plan, so
// a second plan after a failed run only holds what is left. Planning lists open issues only;
// closed issues lose their old labels by the delete-label steps.
func BuildPlan(ctx context.Context, f forge.Forge, old OldNames, l repoconfig.Labels, runs RunLookup) ([]Step, error) {
	existing, err := f.Labels(ctx)
	if err != nil {
		return nil, fmt.Errorf("labelmigrate: labels: %w", err)
	}
	pl, err := f.Planning(ctx)
	if err != nil {
		return nil, fmt.Errorf("labelmigrate: planning: %w", err)
	}
	oldNames := uniq(old.Shaping, old.Decision, old.Testing)
	var creates, relabels, blocks, statuses, deletes []Step

	if !slices.Contains(existing, l.NeedsMind) {
		creates = append(creates, Step{Kind: KindCreateLabel, Add: []string{l.NeedsMind}})
	}
	for _, is := range pl.Issues {
		labels := slices.Clone(is.Labels)
		var remove []string
		for _, n := range oldNames {
			if slices.Contains(labels, n) {
				remove = append(remove, n)
			}
		}
		if len(remove) > 0 {
			var add []string
			if (slices.Contains(labels, old.Shaping) || slices.Contains(labels, old.Decision)) && !slices.Contains(labels, l.NeedsMind) {
				add = append(add, l.NeedsMind)
			}
			if slices.Contains(labels, old.Testing) && !slices.Contains(labels, l.NeedsHands) {
				add = append(add, l.NeedsHands)
			}
			relabels = append(relabels, Step{Kind: KindRelabel, Issue: is.Number, Add: add, Remove: remove})
			labels = slices.DeleteFunc(labels, func(s string) bool { return slices.Contains(remove, s) })
			labels = append(labels, add...)
		}
		open := hasOpenBlocker(is.BlockedBy)
		if slices.Contains(labels, l.NeedsMind) && open && !slices.Contains(labels, l.BlockedIssue) {
			// blocked_issue beats blocked_external, as in issuestate.SetLabel.
			var rm []string
			if l.BlockedExternal != "" && l.BlockedExternal != l.BlockedIssue && slices.Contains(labels, l.BlockedExternal) {
				rm = []string{l.BlockedExternal}
			}
			blocks = append(blocks, Step{Kind: KindBlock, Issue: is.Number, Add: []string{l.BlockedIssue}, Remove: rm})
			labels = append(slices.DeleteFunc(labels, func(s string) bool { return slices.Contains(rm, s) }), l.BlockedIssue)
		}
		blocked := open || slices.Contains(labels, l.BlockedIssue) || slices.Contains(labels, l.BlockedExternal)
		if !blocked {
			continue
		}
		st, err := stuckInProgress(ctx, f, is.Number, runs)
		if err != nil {
			return nil, err
		}
		if st {
			statuses = append(statuses, Step{Kind: KindStatus, Issue: is.Number, Status: statusTodo})
		}
	}
	for _, n := range oldNames {
		if slices.Contains(existing, n) && n != l.NeedsMind && n != l.NeedsHands {
			deletes = append(deletes, Step{Kind: KindDeleteLabel, Remove: []string{n}})
		}
	}
	var out []Step
	for _, g := range [][]Step{creates, relabels, blocks, statuses, deletes} {
		out = append(out, g...)
	}
	return out, nil
}

// stuckInProgress is true for a blocked issue on the board with Status In progress and no
// open or parked run. Controller never matches.
func stuckInProgress(ctx context.Context, f forge.Forge, n int, runs RunLookup) (bool, error) {
	item, ok, err := f.IssueItem(ctx, n)
	if err != nil {
		return false, fmt.Errorf("labelmigrate: board item of #%d: %w", n, err)
	}
	if !ok || item.Status != statusInProgress {
		return false, nil
	}
	if runs == nil {
		return false, errors.New("step 3 needs the runs database")
	}
	state, err := runs(n)
	if err != nil {
		return false, fmt.Errorf("labelmigrate: run of #%d: %w", n, err)
	}
	return state != "open" && state != "parked", nil
}

func uniq(names ...string) []string {
	var out []string
	for _, n := range names {
		if n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// Execute runs steps in the order create-label, relabel, block, status, delete-label and stops
// at the first error. It writes one line per step to out.
func Execute(ctx context.Context, f forge.Forge, steps []Step, out io.Writer) error {
	for _, kind := range kindOrder {
		for _, s := range steps {
			if s.Kind != kind {
				continue
			}
			if err := run(ctx, f, s); err != nil {
				return fmt.Errorf("labelmigrate: %s: %w", describe(s), err)
			}
			if _, err := fmt.Fprintln(out, describe(s)); err != nil {
				return err
			}
		}
	}
	return nil
}

func run(ctx context.Context, f forge.Forge, s Step) error {
	switch s.Kind {
	case KindCreateLabel:
		return f.CreateLabel(ctx, s.Add[0], MindColor)
	case KindRelabel, KindBlock:
		_, err := f.EditIssue(ctx, s.Issue, forge.IssueEdit{AddLabels: s.Add, RemoveLabels: s.Remove})
		return err
	case KindStatus:
		item, ok, err := f.IssueItem(ctx, s.Issue)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("#%d is not on the board", s.Issue)
		}
		return f.SetStatus(ctx, item, s.Status)
	case KindDeleteLabel:
		return f.DeleteLabel(ctx, s.Remove[0])
	}
	return fmt.Errorf("unknown step kind %q", s.Kind)
}

// String is the one-line form of a step, as Execute prints it.
func (s Step) String() string { return describe(s) }

func describe(s Step) string {
	switch s.Kind {
	case KindCreateLabel:
		return fmt.Sprintf("create-label %s", s.Add[0])
	case KindDeleteLabel:
		return fmt.Sprintf("delete-label %s", s.Remove[0])
	case KindStatus:
		return fmt.Sprintf("status #%d -> %s", s.Issue, s.Status)
	}
	return fmt.Sprintf("%s #%d +%v -%v", s.Kind, s.Issue, s.Add, s.Remove)
}

// hasOpenBlocker reports whether one of bs is still open; a closed blocker blocks nothing.
func hasOpenBlocker(bs []forge.Blocker) bool {
	return slices.ContainsFunc(bs, func(b forge.Blocker) bool { return b.State == "OPEN" })
}

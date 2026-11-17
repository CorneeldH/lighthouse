package dispatch

import (
	"context"
	"slices"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

// scheduledBranchPRs reads the PRs of the branch of each open scheduled run in open, by branch,
// sorted by number; nil when no open scheduled run has a branch.
func scheduledBranchPRs(ctx context.Context, f forge.Forge, open []runs.OpenRun) (map[string][]forge.PR, error) {
	var out map[string][]forge.PR
	for _, r := range open {
		if r.Schedule == "" || r.Branch == "" {
			continue
		}
		prs, err := f.BranchPRs(ctx, r.Branch)
		if err != nil {
			return nil, err
		}
		prs = slices.Clone(prs)
		slices.SortFunc(prs, func(x, y forge.PR) int { return x.Number - y.Number })
		if out == nil {
			out = map[string][]forge.PR{}
		}
		out[r.Branch] = prs
	}
	return out, nil
}

// withRoborev is a copy of prs in which every merge-ready PR has its roborev state from its
// comments, as Complete measures the PRs of an item with an open run; prs stays untouched.
func withRoborev(ctx context.Context, f forge.Forge, prs map[string][]forge.PR, trust *Trust) (map[string][]forge.PR, error) {
	if prs == nil {
		return nil, nil
	}
	out := make(map[string][]forge.PR, len(prs))
	for branch, list := range prs {
		list = slices.Clone(list)
		for i, pr := range list {
			if !mergeCandidate(pr) {
				continue
			}
			comments, err := f.Comments(ctx, pr.Number)
			if err != nil {
				return nil, err
			}
			if list[i].Roborev, err = roborevState(ctx, comments, trust); err != nil {
				return nil, err
			}
		}
		out[branch] = list
	}
	return out, nil
}

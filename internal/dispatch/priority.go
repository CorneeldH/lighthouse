package dispatch

import (
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/priorities"
	"github.com/CorneeldH/lighthouse/internal/prioweight"
)

// PriorityBand is the band of a board Priority, the manual override on the prio weight: 0 for
// P0, 1 for P1, no or an unknown priority, 2 for P2. The tick and triage order by band first,
// then by prio weight.
func PriorityBand(priority string) int {
	switch priority {
	case "P0":
		return 0
	case "P2":
		return 2
	}
	return 1
}

// Weights is the prio weight of #445 of every open issue in in: the factor of its milestone,
// from the `Priority: N` rank in the descriptions of the open milestones, plus the factors of
// the issues that wait on it. blockers maps an issue to the issues of the same repo it waits on;
// a blocker that is not an open issue of in counts for nothing.
func Weights(in forge.PrioInputs, blockers map[int][]int) map[int]int {
	ranks := make(map[int]int, len(in.Milestones))
	for _, m := range in.Milestones {
		if rank, ok := priorities.ParseRank(m.Description); ok {
			ranks[m.Number] = rank
		}
	}
	issues := make([]prioweight.Issue, 0, len(in.Issues))
	for n, ms := range in.Issues {
		issues = append(issues, prioweight.Issue{Number: n, Milestone: ms, BlockedBy: blockers[n]})
	}
	return prioweight.Compute(issues, prioweight.Factors(ranks))
}

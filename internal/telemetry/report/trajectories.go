package report

// This file builds the trajectory list of spec §7: one row per trajectory
// with its plan size, the components that ran, its cost and its recovery
// state. The UI sorts further; the list comes sorted on repo, issue.

import (
	"slices"
	"sort"

	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

// Recovery states of a list row.
const (
	RecoveryNoneMeasured = "none_measured"
	RecoveryOpen         = "open"
	RecoveryRecovered    = "recovered"
	RecoveryNone         = "none"
)

// ListRow is one trajectory in the list. Cost nil: not measured.
type ListRow struct {
	ID         int64    `json:"id"`
	Repo       string   `json:"repo"`
	Issue      int      `json:"issue"`
	Size       string   `json:"size"`
	Components []string `json:"components"`
	Cost       *float64 `json:"cost"`
	CostKind   string   `json:"cost_kind"`
	Recovery   string   `json:"recovery"`
	Closed     bool     `json:"closed"`
}

// listComponents are the components a list row can show, in block order.
var listComponents = []string{"plan_review", "spec_review", "lane", "model", "harness"}

// ranComponents lists the components that ran in t: a review component with
// a linked review_runs row, lane, model and harness with a known value.
func ranComponents(t *Trajectory) []string {
	out := []string{}
	for _, c := range listComponents {
		var ran bool
		switch c {
		case "lane":
			ran = t.Lane != ""
		case "model":
			ran = t.Model != ""
		case "harness":
			ran = t.Harness != ""
		default:
			ran = t.HasReview(c)
		}
		if ran {
			out = append(out, c)
		}
	}
	return out
}

// recoveryState is none_measured without a recovery row, open with an open
// window, recovered when a PR recovered, otherwise none.
func recoveryState(t *Trajectory) string {
	if len(t.Recoveries) == 0 {
		return RecoveryNoneMeasured
	}
	for _, r := range t.Recoveries {
		if r.Status == string(telemetry.RecoveryOpen) {
			return RecoveryOpen
		}
	}
	if hasRecovered(t) {
		return RecoveryRecovered
	}
	return RecoveryNone
}

// Trajectories lists the trajectories of d that pass f. f.Component, when
// set, keeps only the trajectories in which that component ran.
func Trajectories(d *Dataset, f Filter) []ListRow {
	rows := []ListRow{}
	for i := range d.Trajectories {
		t := &d.Trajectories[i]
		if !f.Match(t) {
			continue
		}
		ran := ranComponents(t)
		if f.Component != "" && !slices.Contains(ran, f.Component) {
			continue
		}
		size, _ := SizeClass(t.PlanTasks)
		rows = append(rows, ListRow{ID: t.ID, Repo: t.Repo, Issue: t.Issue, Size: size, Components: ran,
			Cost: t.Cost, CostKind: t.CostKind, Recovery: recoveryState(t), Closed: t.Closed})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Repo != rows[j].Repo {
			return rows[i].Repo < rows[j].Repo
		}
		return rows[i].Issue < rows[j].Issue
	})
	return rows
}

// Package report holds the classification and labelling rules of the
// telemetry UI and loads its dataset. It opens no database: Load reads
// through the connection it is given.
package report

import (
	"sort"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/roborev"
)

// Size classes of a trajectory, by the number of plan tasks.
const (
	SizeSmall  = "small"
	SizeMedium = "medium"
	SizeLarge  = "large"
)

// SizeClass classes a trajectory by its plan tasks: 0-3 small, 4-8 medium,
// 9 and up large. A nil count is unknown: ("", false).
func SizeClass(planTasks *int) (string, bool) {
	if planTasks == nil {
		return "", false
	}
	switch n := *planTasks; {
	case n <= 3:
		return SizeSmall, true
	case n <= 8:
		return SizeMedium, true
	default:
		return SizeLarge, true
	}
}

// PR size classes, against the review threshold.
const (
	PRBelow = "below"
	PRAbove = "above"
)

// PRSizeClass classes a PR by its code lines against roborev.Threshold.
func PRSizeClass(codeLines int) string {
	if codeLines < roborev.Threshold {
		return PRBelow
	}
	return PRAbove
}

// LaneOf folds the lanes of a trajectory: "sdd" for sdd only, "native" for
// native only, "mixed" for both, "" without a lane. Empty strings do not count.
func LaneOf(lanes []string) string {
	var sdd, native bool
	for _, l := range lanes {
		switch l {
		case "sdd":
			sdd = true
		case "native":
			native = true
		}
	}
	switch {
	case sdd && native:
		return "mixed"
	case sdd:
		return "sdd"
	case native:
		return "native"
	}
	return ""
}

// AssignmentOf folds assignments: "variant:<x>" when all carry the same
// variant, "standard" when none is a variant (shadow counts as standard, an
// empty list too), otherwise "mixed". An input "mixed" stays "mixed".
func AssignmentOf(assignments []string) string {
	variant := ""
	other := false
	for _, a := range assignments {
		if a == "mixed" {
			return "mixed"
		}
		if !strings.HasPrefix(a, "variant:") {
			other = true
			continue
		}
		if variant != "" && variant != a {
			return "mixed"
		}
		variant = a
	}
	switch {
	case variant == "":
		return "standard"
	case other:
		return "mixed"
	}
	return variant
}

// ModelUse is the use of one model in a trajectory.
type ModelUse struct {
	Cost   *float64
	Tokens int64
}

// DominantModel returns the model with the highest cost; when no model has a
// cost, the one with the most tokens. A tie goes to the alphabetically first.
func DominantModel(byModel map[string]ModelUse) string {
	names := make([]string, 0, len(byModel))
	anyCost := false
	for n, u := range byModel {
		names = append(names, n)
		if u.Cost != nil {
			anyCost = true
		}
	}
	sort.Strings(names)
	best := ""
	var bestCost float64
	var bestTokens int64
	for _, n := range names {
		u := byModel[n]
		if anyCost {
			if u.Cost == nil {
				continue
			}
			if best == "" || *u.Cost > bestCost {
				best, bestCost = n, *u.Cost
			}
			continue
		}
		if best == "" || u.Tokens > bestTokens {
			best, bestTokens = n, u.Tokens
		}
	}
	return best
}

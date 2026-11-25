package report

import "strings"

// Labels of a comparison.
const (
	LabelCorrelation = "correlation"
	LabelAssigned    = "assigned"
)

// ComparisonLabel is "assigned" only when every group is pure (one unique
// assignment, not "mixed") and at least one group carries a variant;
// otherwise "correlation".
func ComparisonLabel(groups [][]string) string {
	variant := false
	for _, g := range groups {
		a := AssignmentOf(g)
		if a == "mixed" {
			return LabelCorrelation
		}
		if strings.HasPrefix(a, "variant:") {
			variant = true
		}
	}
	if variant {
		return LabelAssigned
	}
	return LabelCorrelation
}

// LabelNote is the sentence shown with a label; unknown labels give "".
func LabelNote(label string) string {
	switch label {
	case LabelCorrelation:
		return "Observed, not caused: reviews run more often on hard issues."
	case LabelAssigned:
		return "Assigned by lighthouse"
	}
	return ""
}

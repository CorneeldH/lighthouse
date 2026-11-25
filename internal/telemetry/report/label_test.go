package report

import "testing"

func TestComparisonLabel(t *testing.T) {
	tests := []struct {
		name   string
		groups [][]string
		want   string
	}{
		{"all standard", [][]string{{"standard"}, {"standard"}}, LabelCorrelation},
		{"variant vs standard", [][]string{{"variant:a=1"}, {"standard"}}, LabelAssigned},
		{"mixed group", [][]string{{"variant:a=1", "standard"}, {"standard"}}, LabelCorrelation},
		{"two impure groups", [][]string{{"variant:a=1", "standard"}, {"variant:a=1", "variant:b=2"}}, LabelCorrelation},
		{"empty list", nil, LabelCorrelation},
		{"single pure variant group", [][]string{{"variant:a=1", "variant:a=1"}}, LabelAssigned},
		{"shadow counts standard", [][]string{{"shadow:x"}, {"standard"}}, LabelCorrelation},
		{"literal mixed vs variant", [][]string{{"mixed"}, {"variant:a=1"}}, LabelCorrelation},
		{"literal mixed alone", [][]string{{"mixed"}}, LabelCorrelation},
		{"empty group", [][]string{{}, {"variant:a=1"}}, LabelAssigned},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComparisonLabel(tt.groups); got != tt.want {
				t.Fatalf("ComparisonLabel(%v) = %q; want %q", tt.groups, got, tt.want)
			}
		})
	}
}

func TestLabelNote(t *testing.T) {
	tests := []struct{ label, want string }{
		{LabelCorrelation, "Observed, not caused: reviews run more often on hard issues."},
		{LabelAssigned, "Assigned by lighthouse"},
		{"other", ""},
	}
	for _, tt := range tests {
		if got := LabelNote(tt.label); got != tt.want {
			t.Errorf("LabelNote(%q) = %q; want %q", tt.label, got, tt.want)
		}
	}
}

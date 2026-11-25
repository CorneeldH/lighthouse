package report

import "testing"

func intp(i int) *int { return &i }

func fp(f float64) *float64 { return &f }

func TestSizeClass(t *testing.T) {
	tests := []struct {
		name  string
		in    *int
		want  string
		known bool
	}{
		{"nil", nil, "", false},
		{"zero", intp(0), SizeSmall, true},
		{"three", intp(3), SizeSmall, true},
		{"four", intp(4), SizeMedium, true},
		{"eight", intp(8), SizeMedium, true},
		{"nine", intp(9), SizeLarge, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := SizeClass(tt.in)
			if got != tt.want || ok != tt.known {
				t.Fatalf("SizeClass = %q, %v; want %q, %v", got, ok, tt.want, tt.known)
			}
		})
	}
}

func TestPRSizeClass(t *testing.T) {
	tests := []struct {
		lines int
		want  string
	}{{0, PRBelow}, {99, PRBelow}, {100, PRAbove}, {500, PRAbove}}
	for _, tt := range tests {
		if got := PRSizeClass(tt.lines); got != tt.want {
			t.Errorf("PRSizeClass(%d) = %q; want %q", tt.lines, got, tt.want)
		}
	}
}

func TestLaneOf(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"sdd", []string{"sdd"}, "sdd"},
		{"native", []string{"native"}, "native"},
		{"both", []string{"sdd", "native"}, "mixed"},
		{"empty", []string{}, ""},
		{"nil", nil, ""},
		{"blank ignored", []string{"", "native"}, "native"},
		{"only blank", []string{""}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LaneOf(tt.in); got != tt.want {
				t.Fatalf("LaneOf(%v) = %q; want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestAssignmentOf(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"empty", nil, "standard"},
		{"standard", []string{"standard"}, "standard"},
		{"shadow is standard", []string{"shadow:x"}, "standard"},
		{"same variant", []string{"variant:a=1", "variant:a=1"}, "variant:a=1"},
		{"two variants", []string{"variant:a=1", "variant:b=2"}, "mixed"},
		{"variant and standard", []string{"variant:a=1", "standard"}, "mixed"},
		{"variant and shadow", []string{"variant:a=1", "shadow:x"}, "mixed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AssignmentOf(tt.in); got != tt.want {
				t.Fatalf("AssignmentOf(%v) = %q; want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDominantModel(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]ModelUse
		want string
	}{
		{"empty", nil, ""},
		{"cost wins over tokens", map[string]ModelUse{
			"a": {Cost: fp(1), Tokens: 1000}, "b": {Cost: fp(2), Tokens: 1}}, "b"},
		{"costed beats uncosted with more tokens", map[string]ModelUse{
			"a": {Tokens: 9999}, "b": {Cost: fp(0.5), Tokens: 1}}, "b"},
		{"tokens only", map[string]ModelUse{
			"a": {Tokens: 5}, "b": {Tokens: 7}}, "b"},
		{"cost tie alphabetical", map[string]ModelUse{
			"b": {Cost: fp(1)}, "a": {Cost: fp(1)}}, "a"},
		{"token tie alphabetical", map[string]ModelUse{
			"b": {Tokens: 3}, "a": {Tokens: 3}}, "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DominantModel(tt.in); got != tt.want {
				t.Fatalf("DominantModel = %q; want %q", got, tt.want)
			}
		})
	}
}

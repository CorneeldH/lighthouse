package prioweight

import (
	"reflect"
	"testing"
)

func TestFactors(t *testing.T) {
	tests := []struct {
		name  string
		ranks map[int]int
		want  map[int]int
	}{
		{"empty", map[int]int{}, map[int]int{}},
		{"contiguous", map[int]int{10: 1, 11: 2, 12: 3}, map[int]int{10: 3, 11: 2, 12: 1}},
		{"gap in the ranks", map[int]int{10: 1, 11: 3, 12: 5}, map[int]int{10: 3, 11: 2, 12: 1}},
		{"equal ranks", map[int]int{10: 1, 11: 1, 12: 2}, map[int]int{10: 3, 11: 3, 12: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Factors(tt.ranks); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Factors = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputeExamplesFrom445(t *testing.T) {
	factors := map[int]int{1: 3, 2: 7, 3: 0, 4: 5, 5: 2}
	issues := []Issue{
		{Number: 372, Milestone: 1},
		{Number: 500, Milestone: 2, BlockedBy: []int{372}},
		{Number: 439, Milestone: 3, BlockedBy: []int{372}},
		{Number: 501, Milestone: 3, BlockedBy: []int{372}},
		{Number: 19, Milestone: 3},
		{Number: 255, Milestone: 4, BlockedBy: []int{19}},
		{Number: 437, Milestone: 5},
		{Number: 438, Milestone: 5, BlockedBy: []int{437}},
		{Number: 446, Milestone: 5, BlockedBy: []int{437}},
		{Number: 445, Milestone: 5, BlockedBy: []int{438}},
	}
	got := Compute(issues, factors)
	for nr, want := range map[int]int{372: 10, 19: 5, 437: 10} {
		if got[nr] != want {
			t.Errorf("weight #%d = %d, want %d", nr, got[nr], want)
		}
	}
	if len(got) != len(issues) {
		t.Errorf("%d weights for %d issues", len(got), len(issues))
	}
}

func TestComputeLongestDistance(t *testing.T) {
	factors := map[int]int{1: 1}
	issues := []Issue{
		{Number: 1, Milestone: 1},
		{Number: 2, Milestone: 1, BlockedBy: []int{1}},
		{Number: 3, Milestone: 1, BlockedBy: []int{2, 1}},
	}
	if got := Compute(issues, factors)[1]; got != 4 {
		t.Errorf("weight A = %d, want 4", got)
	}
}

func TestComputeCycle(t *testing.T) {
	factors := map[int]int{1: 2}
	tests := []struct {
		name   string
		issues []Issue
		want   map[int]int
	}{
		{"two issues", []Issue{
			{Number: 1, Milestone: 1, BlockedBy: []int{2}},
			{Number: 2, Milestone: 1, BlockedBy: []int{1}},
		}, map[int]int{1: 4, 2: 4}},
		{"self reference", []Issue{
			{Number: 1, Milestone: 1, BlockedBy: []int{1}},
		}, map[int]int{1: 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compute(tt.issues, factors); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Compute = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputeUnknownBlocker(t *testing.T) {
	factors := map[int]int{1: 3}
	issues := []Issue{{Number: 1, Milestone: 1, BlockedBy: []int{99}}}
	got := Compute(issues, factors)
	if got[1] != 3 || len(got) != 1 {
		t.Errorf("Compute = %v, want {1: 3}", got)
	}
}

func TestComputeMilestoneWithoutRank(t *testing.T) {
	issues := []Issue{{Number: 1}, {Number: 2, Milestone: 9, BlockedBy: []int{1}}}
	got := Compute(issues, map[int]int{})
	if got[1] != 0 || got[2] != 0 {
		t.Errorf("Compute = %v, want all 0", got)
	}
}

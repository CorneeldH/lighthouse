package main

import (
	"reflect"
	"testing"
)

func ip(n int) *int { return &n }

func chipOf(id, status string, issue *int) chip { return chip{ID: id, Status: status, Issue: issue} }

func TestPlanned(t *testing.T) {
	chain := Chain{
		Chips: []chip{chipOf("a", "works", ip(1)), chipOf("b", "planned", ip(7)), chipOf("c", "idea", nil)},
		Handoffs: []handoff{
			{From: "x", To: "y", Status: "planned"},
			{From: "p", To: "q", Status: "works"},
		},
	}
	want := []Item{{"b", ip(7)}, {"c", nil}, {"x->y", nil}}
	if got := Planned(chain); !reflect.DeepEqual(got, want) {
		t.Errorf("Planned = %+v, want %+v", got, want)
	}
}

func TestPlannedDuplicateNameKeepsPositionAndLastValue(t *testing.T) {
	chain := Chain{Chips: []chip{chipOf("a", "planned", ip(1)), chipOf("b", "planned", ip(2)), chipOf("a", "planned", ip(3))}}
	want := []Item{{"a", ip(3)}, {"b", ip(2)}}
	if got := Planned(chain); !reflect.DeepEqual(got, want) {
		t.Errorf("Planned = %+v, want %+v", got, want)
	}
}

func TestProblems(t *testing.T) {
	const hint = "(see docs/developer/labels.md)"
	tests := []struct {
		name  string
		chain Chain
		open  map[int]string
		want  []string
	}{
		{"matching map and issues", Chain{Chips: []chip{chipOf("a", "works", nil), chipOf("b", "planned", ip(7))}}, map[int]string{7: ""}, nil},
		{"planned without issue", Chain{Handoffs: []handoff{{From: "a", To: "b", Status: "planned"}}}, map[int]string{},
			[]string{"a->b: planned without an issue"}},
		{"closed or unlabelled issue", Chain{Chips: []chip{chipOf("b", "idea", ip(7))}}, map[int]string{},
			[]string{"b: #7 is not an open issue labelled loop " + hint}},
		{"milestoned issue missing from the map is left to the job", Chain{Chips: []chip{chipOf("b", "planned", ip(7))}}, map[int]string{7: "M2", 9: "M2 later"}, nil},
		{"issue without milestone missing from the map", Chain{Chips: []chip{chipOf("a", "works", nil)}}, map[int]string{9: ""}, nil},
		{"milestoned issue planned on the map", Chain{Handoffs: []handoff{{From: "a", To: "b", Status: "planned", Issue: ip(9)}}}, map[int]string{9: "M2"}, nil},
		{"issue only, no milestone: clean", Chain{Chips: []chip{chipOf("x", "planned", ip(7))}}, map[int]string{7: ""}, nil},
		{"milestone only, no issue: flagged", Chain{Chips: []chip{chipOf("x", "planned", nil)}}, map[int]string{7: "M9"},
			[]string{"x: planned without an issue"}},
		{"planned problems only, milestoned issues off the map are not flagged",
			Chain{Chips: []chip{chipOf("x", "planned", nil), chipOf("y", "planned", ip(5))}}, map[int]string{20: "M", 10: "M"},
			[]string{
				"x: planned without an issue",
				"y: #5 is not an open issue labelled loop " + hint,
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Problems(tt.chain, tt.open); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Problems = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPRProblems(t *testing.T) {
	const hint = "(see docs/developer/labels.md)"
	mapSet := map[string]bool{"docs/site/data/loop.json": true}
	tests := []struct {
		name    string
		chain   Chain
		closed  map[int]bool
		touched map[string]bool
		want    []string
	}{
		{"closing a planned item without update", Chain{Chips: []chip{chipOf("b", "planned", ip(7))}}, map[int]bool{7: true}, mapSet,
			[]string{"b: still planned, but this PR closes #7; set status works and drop issue " + hint}},
		{"closing without touching the map", Chain{Chips: []chip{chipOf("b", "works", nil)}}, map[int]bool{7: true}, map[string]bool{"ci/x.py": true},
			[]string{"#7: this PR closes a loop issue but leaves docs/site/data/loop.json untouched " + hint}},
		{"only the issue left planned is flagged", Chain{Chips: []chip{chipOf("a", "works", nil), chipOf("b", "planned", ip(8))}},
			map[int]bool{7: true, 8: true}, mapSet,
			[]string{"b: still planned, but this PR closes #8; set status works and drop issue " + hint}},
		{"new step added as works is clean", Chain{Chips: []chip{chipOf("new", "works", nil)}}, map[int]bool{7: true}, mapSet, nil},
		{"closing nothing is clean", Chain{Chips: []chip{chipOf("b", "planned", ip(7))}}, map[int]bool{}, map[string]bool{}, nil},
		{"planned item still flagged and untouched map lists nothing twice",
			Chain{Chips: []chip{chipOf("b", "planned", ip(7))}}, map[int]bool{7: true}, map[string]bool{"x": true},
			[]string{"b: still planned, but this PR closes #7; set status works and drop issue " + hint}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PRProblems(tt.chain, tt.closed, tt.touched); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PRProblems = %q, want %q", got, tt.want)
			}
		})
	}
}

package priorities

import "testing"

func TestParseRank(t *testing.T) {
	tests := []struct {
		name string
		in   string
		rank int
		ok   bool
	}{
		{"empty", "", 0, false},
		{"plain", "Priority: 3", 3, true},
		{"lowercase and spaces", "  priority:7  ", 7, true},
		{"uppercase, wide gap", "PRIORITY:  12", 12, true},
		{"word", "Priority: high", 0, false},
		{"zero", "Priority: 0", 0, false},
		{"leading zero", "Priority: 03", 0, false},
		{"trailing text", "Priority: 2 extra", 0, false},
		{"list item", "- Priority: 2", 0, false},
		{"first wins", "a\nPriority: 2\nPriority: 5", 2, true},
		{"invalid line skipped", "Priority: high\nPriority: 4", 4, true},
		{"crlf", "a\r\nPriority: 6\r\nb", 6, true},
		{"overflow", "Priority: 99999999999999999999", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rank, ok := ParseRank(tt.in)
			if rank != tt.rank || ok != tt.ok {
				t.Errorf("ParseRank(%q) = (%d, %v), want (%d, %v)", tt.in, rank, ok, tt.rank, tt.ok)
			}
		})
	}
}

func TestSetRank(t *testing.T) {
	tests := []struct {
		name string
		in   string
		rank int
		want string
	}{
		{"empty", "", 1, "Priority: 1"},
		{"append", "Pitch", 2, "Pitch\n\nPriority: 2"},
		{"replace", "Pitch\nPriority: 2\nrest", 1, "Pitch\nPriority: 1\nrest"},
		{"crlf", "a\r\nPriority: 2\r\nb", 9, "a\r\nPriority: 9\r\nb"},
		{"first valid line", "Priority: high\nPriority: 4", 1, "Priority: high\nPriority: 1"},
		{"keeps indent and trailing", "  Priority: 2  \n", 1, "  Priority: 1  \n"},
		{"trailing newline appended literally", "Pitch\n", 2, "Pitch\n\n\nPriority: 2"},
		{"blank description", "  ", 1, "  \n\nPriority: 1"},
		{"trailing crlf appended literally", "Pitch\r\n", 2, "Pitch\r\n\n\nPriority: 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SetRank(tt.in, tt.rank); got != tt.want {
				t.Errorf("SetRank(%q, %d) = %q, want %q", tt.in, tt.rank, got, tt.want)
			}
		})
	}
}

func TestWithDescription(t *testing.T) {
	in := []Repo{
		{Slug: "a/x", Milestones: []Milestone{{Number: 1, Description: "old"}, {Number: 2, Description: "two"}}},
		{Slug: "a/y", Milestones: []Milestone{{Number: 1, Description: "other"}}},
	}
	got := WithDescription(in, "a/x", 1, "new")
	if d := got[0].Milestones[0].Description; d != "new" {
		t.Errorf("changed description = %q, want new", d)
	}
	if d := got[0].Milestones[1].Description; d != "two" {
		t.Errorf("sibling milestone = %q, want two", d)
	}
	if d := got[1].Milestones[0].Description; d != "other" {
		t.Errorf("other repo = %q, want other", d)
	}
	if d := in[0].Milestones[0].Description; d != "old" {
		t.Errorf("input mutated: %q", d)
	}
}

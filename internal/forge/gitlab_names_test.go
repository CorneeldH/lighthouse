package forge

import "testing"

// Two names on one GitLab status or label are refused by the config readers; should they
// still arrive, the name read back is the first in sorted order, every time, not a random one.
func TestGitLabNamesAreDeterministic(t *testing.T) {
	status := map[string]string{"On hold": "Waiting", "Controller": "waiting", "Done": "Done", "B": "x", "A": "y"}
	labels := map[string]string{"P2": "priority::high", "P1": "priority::high", "P3": "priority::low"}
	for range 100 { // Go randomises map order: one lucky pass proves nothing
		if got := statusName(status, "WAITING"); got != "Controller" {
			t.Fatalf("statusName(WAITING) = %q, want Controller", got)
		}
		if got := labelPriority(labels, "priority::high"); got != "P1" {
			t.Fatalf("labelPriority(priority::high) = %q, want P1", got)
		}
	}
	if got := statusName(status, "Gone"); got != UnknownStatusPrefix+"Gone" {
		t.Errorf("statusName(Gone) = %q, want %q", got, UnknownStatusPrefix+"Gone")
	}
	if got := labelPriority(labels, "priority::none"); got != "" {
		t.Errorf("labelPriority(priority::none) = %q, want empty", got)
	}
}

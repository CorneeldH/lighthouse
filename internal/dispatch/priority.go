package dispatch

// PriorityRank orders P0, P1, P2, then no or an unknown priority; triage sorts candidates by it.
func PriorityRank(priority string) int { return priorityRank(priority) }

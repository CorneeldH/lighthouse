// Package triage is the pure logic of lighthouse triage (spec 2026-11-01-triage-design).
package triage

import (
	"slices"
	"sort"
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// Comment is one issue comment.
type Comment struct {
	ID                      int64
	Author, Body, CreatedAt string
	Trusted                 bool // the author is the token login, or its role is in answer_permissions
}

// Ref is another issue of the milestone, as the judgment sees it.
type Ref struct {
	Number int
	Title  string
}

// Facts is everything the round knows about one issue.
type Facts struct {
	Number                         int
	Title, Body, Author            string
	AuthorTrusted                  bool
	State                          string // "open" or "closed"
	Labels                         []string
	UpdatedAt                      time.Time
	Milestone                      *forge.Milestone
	ItemID                         string          // "" when not on the board
	Archived                       bool            // the board item is archived
	Status, Priority               string          // option names; "" when unset or not on the board, Status "unknown:<id>" for an unlisted option
	Blockers                       []forge.Blocker // blocked_by, open and closed: BoardItem.BlockedBy, or forge.BlockedBy off the board
	Comments                       []Comment       // oldest first
	Work                           []string        // running work found, e.g. "PR #12", "branch origin/issue-7-x", "worktree /p", "run 31"
	GitLog                         string
	GitShas                        []string // full shas of the commits in GitLog
	MilestoneOpen, MilestoneClosed []Ref
}

// Skip is the reason a candidate filter rule rejects an issue; SkipNone means it is a candidate.
type Skip string

// The candidate rules of the spec ("Kandidaten"), in order.
const (
	SkipNone      Skip = ""
	SkipMilestone Skip = "milestone"
	SkipStatus    Skip = "status"
	SkipArchived  Skip = "archived"
	SkipMarker    Skip = "marker"
	SkipChain     Skip = "chain"
	SkipWork      Skip = "running work"
)

// Candidate applies the candidate rules of the spec in order; the first that fails is the Skip.
func Candidate(f Facts, cfg repoconfig.Triage, l repoconfig.Labels) Skip {
	switch {
	case f.State != "open":
		return SkipMilestone
	case cfg.MilestonesOnly && (f.Milestone == nil || f.Milestone.State != "open"):
		// With milestones_only = false every open issue counts, also one in a closed milestone.
		return SkipMilestone
	case cfg.MilestoneTitleExclude != nil && f.Milestone != nil && cfg.MilestoneTitleExclude.MatchString(f.Milestone.Title):
		// Also with milestones_only = false: a title that matches is out, whatever the milestone state.
		return SkipMilestone
	case f.Archived:
		// An archived item is on the board, out of sight of the tick: a write there helps no one.
		return SkipArchived
	case f.Status != "" && f.Status != "To do":
		return SkipStatus
	}
	if m, ok := LastMarker(f.Comments); ok && MarkerHolds(m, f, l) {
		return SkipMarker
	}
	for _, c := range f.Comments {
		if _, ok := protocol.ParseHeader(c.Body); ok && c.Trusted {
			return SkipChain
		}
	}
	if len(f.Work) > 0 {
		return SkipWork
	}
	return SkipNone
}

// LastMarker returns the marker of the newest trusted comment that carries one. Only the newest
// marker counts, and a marker of an untrusted author never does.
func LastMarker(cs []Comment) (protocol.TriageMarker, bool) {
	for _, c := range slices.Backward(cs) {
		if !c.Trusted {
			continue
		}
		if m, ok := protocol.ParseTriageMarker(c.Body); ok {
			return m, true
		}
	}
	return protocol.TriageMarker{}, false
}

// MarkerHolds reports whether the marker still keeps the issue out of triage. A marker without
// a stop never lapses by itself; one with a stop lapses when the stop is gone.
func MarkerHolds(m protocol.TriageMarker, f Facts, l repoconfig.Labels) bool {
	if m.Label != "" && (m.Label == l.NeedsHands || m.Label == l.NeedsTesting) {
		return false
	}
	if !m.Stop {
		return true
	}
	if m.Gate == "invariant" {
		return len(Invariant(f.Labels, l)) > 0
	}
	for _, n := range m.Blockers {
		for _, b := range f.Blockers {
			if b.Issue == n && b.State == "OPEN" {
				return true
			}
		}
	}
	return m.Label != "" && slices.Contains(f.Labels, m.Label)
}

// Changed reports whether the issue differs between two reads in a way the recheck refuses to
// write over (spec "Hercontrole"): state, status, an archived item, labels, comments, update time
// or running work.
func Changed(before, after Facts) bool {
	return before.State != after.State ||
		before.Status != after.Status ||
		before.Archived != after.Archived ||
		!before.UpdatedAt.Equal(after.UpdatedAt) ||
		!sameSorted(before.Labels, after.Labels) ||
		!sameSorted(before.Work, after.Work) ||
		!slices.Equal(commentIDs(before.Comments), commentIDs(after.Comments))
}

func sameSorted(a, b []string) bool {
	a, b = slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b))
	return slices.Equal(a, b)
}

func commentIDs(cs []Comment) []int64 {
	ids := make([]int64, len(cs))
	for i, c := range cs {
		ids[i] = c.ID
	}
	return ids
}

// Sort orders candidates by priority, then by issue number.
func Sort(fs []Facts) {
	sort.SliceStable(fs, func(i, j int) bool {
		ri, rj := dispatch.PriorityRank(fs[i].Priority), dispatch.PriorityRank(fs[j].Priority)
		if ri != rj {
			return ri < rj
		}
		return fs[i].Number < fs[j].Number
	})
}

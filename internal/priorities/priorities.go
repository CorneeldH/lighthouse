// Package priorities builds the data of the prioritisation view: per repository the open
// issues with milestone, open blockers and board Status, and the open milestones with counts.
package priorities

import (
	"context"
	"fmt"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/overview"
)

// Source is one repository to read: its slug and its forge.
type Source struct {
	Slug string
	Src  forge.Forge
}

// MilestoneRef is the milestone an issue belongs to. State is "open" or "closed".
type MilestoneRef struct {
	Number       int
	Title, State string
}

// Blocker is an open issue that blocks another; Repo is the repo of the blocker, which can
// differ from the repo of the blocked issue.
type Blocker struct {
	Repo  string
	Issue int
}

// Issue is one open issue. Status is its board Status, "" when it has none.
type Issue struct {
	Number    int
	Title     string
	URL       string
	Milestone *MilestoneRef
	BlockedBy []Blocker
	Status    string
}

// Milestone is an open milestone with the size of its work.
type Milestone struct {
	Number      int
	Title       string
	Description string
	Open, Done  int
}

// Repo is the planning of one repository; neither list is ever nil.
type Repo struct {
	Slug       string
	Issues     []Issue
	Milestones []Milestone
}

// Run is a dispatch run as the view shows it next to an issue.
type Run struct {
	Repo         string
	Issue        int
	State, Phase string
}

// Result is what the prioritisation endpoint returns; neither list is ever nil.
type Result struct {
	Repos []Repo
	Runs  []Run
}

// Build reads Planning of every source, one after the other, and joins the board Status from
// statuses in memory: it never reads a board. Without sources it returns forge.ErrNotConfigured.
// A repo that fails fails the whole build, naming the repo; there is no partial result.
func Build(ctx context.Context, srcs []Source, statuses []overview.IssueStatus) ([]Repo, error) {
	if len(srcs) == 0 {
		return nil, forge.ErrNotConfigured
	}
	status := make(map[string]string, len(statuses))
	for _, s := range statuses {
		status[key(s.Repo, s.Issue)] = s.Status
	}
	repos := make([]Repo, 0, len(srcs))
	for _, s := range srcs {
		p, err := s.Src.Planning(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.Slug, err)
		}
		r := Repo{Slug: s.Slug, Issues: make([]Issue, 0, len(p.Issues)), Milestones: make([]Milestone, 0, len(p.Milestones))}
		for _, pi := range p.Issues {
			i := Issue{Number: pi.Number, Title: pi.Title, URL: pi.URL, BlockedBy: make([]Blocker, 0, len(pi.BlockedBy)),
				Status: status[key(s.Slug, pi.Number)]}
			if m := pi.Milestone; m != nil {
				i.Milestone = &MilestoneRef{Number: m.Number, Title: m.Title, State: m.State}
			}
			for _, b := range pi.BlockedBy {
				i.BlockedBy = append(i.BlockedBy, Blocker{Repo: b.Repo, Issue: b.Issue})
			}
			r.Issues = append(r.Issues, i)
		}
		for _, m := range p.Milestones {
			r.Milestones = append(r.Milestones, Milestone{Number: m.Number, Title: m.Title, Description: m.Description, Open: m.Open, Done: m.Done})
		}
		repos = append(repos, r)
	}
	return repos, nil
}

func key(repo string, issue int) string { return fmt.Sprintf("%s#%d", repo, issue) }

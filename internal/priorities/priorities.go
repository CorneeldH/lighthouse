// Package priorities builds the data of the prioritisation view: per repository the open
// issues with milestone, open blockers and board Status, and the open milestones with counts.
package priorities

import (
	"context"
	"fmt"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/issuestate"
	"github.com/CorneeldH/lighthouse/internal/overview"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// Source is one repository to read: its slug, its forge and its blocking label names.
type Source struct {
	Slug   string
	Src    forge.Forge
	Labels repoconfig.Labels
}

// MilestoneRef is the milestone an issue belongs to. State is "open" or "closed".
type MilestoneRef struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
}

// Blocker is an open issue that blocks another; Repo is the repo of the blocker, which can
// differ from the repo of the blocked issue.
type Blocker struct {
	Repo  string `json:"repo"`
	Issue int    `json:"issue"`
}

// Issue is one open issue. Status is its board Status, "" when it has none. Label is the
// `[labels]` key of its blocking label with the lowest rank (for example "needs_shaping"), ""
// without one. OpenQuestion is true when the issue has an open question for the human.
type Issue struct {
	Number    int           `json:"number"`
	Title     string        `json:"title"`
	URL       string        `json:"url"`
	Milestone *MilestoneRef `json:"milestone,omitempty"`
	BlockedBy []Blocker     `json:"blocked_by"`
	Status    string        `json:"status"`
	Label     string        `json:"label"`
	// OpenQuestion is true when the overview holds a question for this issue.
	OpenQuestion bool `json:"open_question"`
}

// ClosedIssue is an issue of a milestone that counts as done.
type ClosedIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

// Milestone is an open milestone with the size of its work.
type Milestone struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Open        int    `json:"open"`
	Done        int    `json:"done"`
	// Closed are the issues behind Done; never nil.
	Closed []ClosedIssue `json:"closed"`
}

// Repo is the planning of one repository; neither list is ever nil.
type Repo struct {
	Slug       string      `json:"slug"`
	Issues     []Issue     `json:"issues"`
	Milestones []Milestone `json:"milestones"`
}

// Run is a dispatch run as the view shows it next to an issue.
type Run struct {
	Repo  string `json:"repo"`
	Issue int    `json:"issue"`
	State string `json:"state"`
	Phase string `json:"phase"`
}

// Result is what the prioritisation endpoint returns; neither list is ever nil.
type Result struct {
	Repos []Repo `json:"repos"`
	Runs  []Run  `json:"runs"`
}

// Build reads Planning of every source, one after the other, and joins the board Status and the
// open questions from ov in memory: it never reads a board. Without sources it returns forge.ErrNotConfigured.
// A repo that fails fails the whole build, naming the repo; there is no partial result.
func Build(ctx context.Context, srcs []Source, ov overview.Overview) ([]Repo, error) {
	if len(srcs) == 0 {
		return nil, forge.ErrNotConfigured
	}
	status := make(map[string]string, len(ov.Statuses))
	for _, s := range ov.Statuses {
		status[key(s.Repo, s.Issue)] = s.Status
	}
	asked := make(map[string]bool, len(ov.Questions))
	for _, q := range ov.Questions {
		asked[key(q.Repo, q.Number)] = true
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
				Status: status[key(s.Slug, pi.Number)], Label: issuestate.BlockingKey(pi.Labels, s.Labels),
				OpenQuestion: asked[key(s.Slug, pi.Number)]}
			if m := pi.Milestone; m != nil {
				i.Milestone = &MilestoneRef{Number: m.Number, Title: m.Title, State: m.State}
			}
			for _, b := range pi.BlockedBy {
				i.BlockedBy = append(i.BlockedBy, Blocker{Repo: b.Repo, Issue: b.Issue})
			}
			r.Issues = append(r.Issues, i)
		}
		for _, m := range p.Milestones {
			closed := make([]ClosedIssue, 0, len(m.Closed))
			for _, c := range m.Closed {
				closed = append(closed, ClosedIssue{Number: c.Number, Title: c.Title, URL: c.URL})
			}
			r.Milestones = append(r.Milestones, Milestone{Number: m.Number, Title: m.Title, Description: m.Description,
				Open: m.Open, Done: m.Done, Closed: closed})
		}
		repos = append(repos, r)
	}
	return repos, nil
}

func key(repo string, issue int) string { return fmt.Sprintf("%s#%d", repo, issue) }

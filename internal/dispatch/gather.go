package dispatch

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

// dispatchStatuses are the board statuses whose items the dispatcher looks at.
var dispatchStatuses = map[string]bool{"Controller": true, "On hold": true}

// Pre is the first half of a gather: the board fields and PRs of the items, without comments.
type Pre struct {
	Repo     repoconfig.Repo
	Items    []Item // board fields and PRs; no comments
	OpenRuns []runs.OpenRun
}

// Prepare reads the board of repo and keeps the issues of repo.Slug whose Status is
// Controller or On hold, sorted by issue. An item with an open run on a branch also gets the
// PRs of that branch, merged with its closing PRs.
func Prepare(ctx context.Context, c *forge.Client, repo repoconfig.Repo, open []runs.OpenRun) (Pre, error) {
	b := repo.Board
	if b == nil {
		return Pre{}, fmt.Errorf("dispatch: %s: missing [forge.board]", repo.Slug)
	}
	board, err := c.BoardItems(ctx, b.ProjectID)
	if err != nil {
		return Pre{}, err
	}
	statusName := invert(b.StatusOptions)
	priorityName := invert(b.PriorityOptions)
	branch := map[int]string{}
	for _, r := range open {
		if r.Branch != "" {
			branch[r.Issue] = r.Branch
		}
	}
	var items []Item
	for _, bi := range board {
		status := statusName[bi.SingleSelect[b.StatusFieldID]]
		if bi.Repo != repo.Slug || bi.Issue <= 0 || !dispatchStatuses[status] {
			continue
		}
		it := Item{
			Issue: bi.Issue, Title: bi.Title, State: bi.IssueState, ItemID: bi.ItemID,
			Status: status, Priority: priorityName[bi.SingleSelect[b.PriorityFieldID]],
			ItemUpdatedAt: bi.ItemUpdatedAt, IssueUpdatedAt: bi.IssueUpdatedAt,
			BlockedBy: bi.BlockedBy, PRs: bi.ClosingPRs,
		}
		if br, ok := branch[bi.Issue]; ok {
			onBranch, err := c.BranchPRs(ctx, br)
			if err != nil {
				return Pre{}, err
			}
			it.PRs = mergePRs(bi.ClosingPRs, onBranch)
		}
		slices.SortFunc(it.PRs, func(x, y forge.PR) int { return x.Number - y.Number })
		items = append(items, it)
	}
	slices.SortFunc(items, func(x, y Item) int { return x.Issue - y.Issue })
	return Pre{Repo: repo, Items: items, OpenRuns: open}, nil
}

// invert maps option ID → option name.
func invert(options map[string]string) map[string]string {
	out := make(map[string]string, len(options))
	for name, id := range options {
		out[id] = name
	}
	return out
}

// mergePRs joins the closing PRs and the PRs of a branch, one per number. A PR in both keeps
// the closing version, so Closes stays true.
func mergePRs(closing, onBranch []forge.PR) []forge.PR {
	out := slices.Clone(closing)
	for _, p := range onBranch {
		if !slices.ContainsFunc(out, func(q forge.PR) bool { return q.Number == p.Number }) {
			out = append(out, p)
		}
	}
	return out
}

// Complete reads the comments of every prepared item and returns the snapshot. Comments of an
// untrusted author drop out; a dropped 🤖-comment gets one line in log. A comment's Rocket is
// set only when tokenLogin put a 🚀 on it. Any failed request fails the whole gather. Every
// list in the snapshot is non-nil, so its JSON has [] where a list is empty, never null.
func Complete(ctx context.Context, c *forge.Client, pre Pre, trust *Trust, tokenLogin string, log io.Writer, now time.Time) (Snapshot, error) {
	items := make([]Item, 0, len(pre.Items))
	for _, it := range pre.Items {
		all, err := c.Comments(ctx, it.Issue)
		if err != nil {
			return Snapshot{}, err
		}
		it.BlockedBy = nonNil(it.BlockedBy)
		it.PRs = nonNil(it.PRs)
		it.Comments = []Comment{}
		for _, pc := range all {
			ok, err := trust.Trusted(ctx, pc.Author)
			if err != nil {
				return Snapshot{}, err
			}
			if !ok {
				if strings.HasPrefix(strings.TrimSpace(pc.Body), protocol.Robot) {
					_, _ = fmt.Fprintf(log, "lighthouse: %s#%d: ignored 🤖-comment %s by %s: role not in answer_permissions\n",
						pre.Repo.Slug, it.Issue, pc.URL, pc.Author)
				}
				continue
			}
			rocket, err := tokenRocket(ctx, c, pc, tokenLogin)
			if err != nil {
				return Snapshot{}, err
			}
			it.Comments = append(it.Comments, Comment{ID: pc.ID, Author: pc.Author, Body: pc.Body,
				CreatedAt: pc.CreatedAt, URL: pc.URL, Rocket: rocket})
		}
		items = append(items, it)
	}
	return Snapshot{
		Version:    SnapshotVersion,
		Repo:       pre.Repo.Slug,
		TokenLogin: tokenLogin,
		GatheredAt: now.UTC(),
		Items:      items,
		OpenRuns:   nonNil(pre.OpenRuns),
	}, nil
}

// nonNil returns s, or an empty slice when s is nil.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// tokenRocket reports whether tokenLogin put a 🚀 on the comment. A comment without any 🚀
// costs no request.
func tokenRocket(ctx context.Context, c *forge.Client, pc protocol.Comment, tokenLogin string) (bool, error) {
	if pc.Rockets == 0 {
		return false, nil
	}
	logins, err := c.RocketLogins(ctx, pc.ID)
	if err != nil {
		return false, err
	}
	return slices.Contains(logins, tokenLogin), nil
}

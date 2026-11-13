package workplace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/CorneeldH/lighthouse/internal/runs"
)

// SweepInput is what Sweep needs of one repo besides the database and the workplace.
type SweepInput struct {
	Repo string
	// InController reports whether the controller still holds the issue: board Status
	// Controller or On hold.
	InController func(issue int) bool
	// MergedHead is the head SHA of the last merged PR of branch, or "" when none.
	MergedHead func(ctx context.Context, branch string) (string, error)
}

// Sweep runs Cleanup for every workspace of in.Repo that runs.Workspaces returns. removed
// lists the workspace ids it removed, in the order of runs.Workspaces, as a Leftover without
// Path or Reason. err joins the errors of the candidates; one candidate's error never stops
// the others.
//
// A workspace of an issue the controller still holds, or that has an open run, is left
// alone and loses its leftover record. A removed workspace is marked in the runs, so a
// later sweep no longer asks the forge about it.
func Sweep(ctx context.Context, db *sql.DB, w Workplace, run Runner, in SweepInput, now time.Time) (removed []Leftover, err error) {
	refs, err := runs.Workspaces(ctx, db, in.Repo)
	if err != nil {
		return nil, err
	}
	open, err := runs.Open(ctx, db, in.Repo)
	if err != nil {
		return nil, err
	}
	running := map[int]bool{}
	for _, r := range open {
		running[r.Issue] = true
	}
	var errs []error
	for _, ref := range refs {
		fail := func(err error) {
			errs = append(errs, fmt.Errorf("workspace %s of #%d: %w", ref.WorkspaceID, ref.Issue, err))
		}
		if in.InController(ref.Issue) || running[ref.Issue] {
			if err := ClearLeftover(ctx, db, in.Repo, ref.WorkspaceID); err != nil {
				fail(err)
			}
			continue
		}
		var head string
		if ref.Branch != "" {
			h, err := in.MergedHead(ctx, ref.Branch)
			if err != nil {
				l := Leftover{Repo: in.Repo, Issue: ref.Issue, WorkspaceID: ref.WorkspaceID, Reason: "not measurable: merged PR: " + err.Error()}
				if err := RecordLeftover(ctx, db, l, now); err != nil {
					fail(err)
				}
				continue
			}
			head = h
		}
		c := Candidate{
			Repo: in.Repo, Issue: ref.Issue, MergedHead: head,
			Workspace: Workspace{ID: ref.WorkspaceID, Branch: ref.Branch},
		}
		gone, err := Cleanup(ctx, db, w, run, c, now)
		if err != nil {
			fail(err)
		}
		if !gone {
			continue
		}
		// Also after an error: clearing the record can fail once the workspace is gone.
		if err := runs.MarkWorkspaceRemoved(ctx, db, in.Repo, ref.Node, ref.WorkspaceID, now); err != nil {
			fail(err)
		}
		removed = append(removed, Leftover{Repo: in.Repo, Issue: ref.Issue, WorkspaceID: ref.WorkspaceID})
	}
	return removed, errors.Join(errs...)
}

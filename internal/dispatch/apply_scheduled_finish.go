package dispatch

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
)

// scheduledBranchPrefix is the prefix of every scheduled branch (ScheduledBranch); only such a
// branch is ever deleted.
const scheduledBranchPrefix = "scheduled/"

// finishScheduled ends the run's agent, checks and commits its result and opens its PR (spec §4,
// row 1). In order:
//
//   - Kill the run's claude: nothing may change the work tree between the check and the commit.
//     A failed kill is an error and nothing else runs; the next tick tries again.
//   - Compare the workplace's GitState with the one recorded at the start. A change means the
//     agent wrote git config that `git add`, a checkout or the push would run: the run fails and
//     no other git command runs in the workplace.
//   - scheduled.Postcheck with the Remote of the repo: an explicit, authenticated URL, never the
//     workplace's origin. Its error is transient and only its redacted text goes on; a Failure
//     fails the run; nothing changed is done without a PR.
//   - Record the commit on the branch (Record.Pushed): its PR merges only while its head is
//     that commit. A PR the run already has (a.PR) is taken only when its head (a.HeadSHA) is
//     that commit; any other is foreign and fails the run.
//   - CreatePR with body "No issue: geplande run <name> (<slot>)", or the taken PR, then pr_at
//     at once: from then on only pr_timeout counts.
//
// A failed run closes its PR and deletes its branch (endScheduled). It posts nothing.
func (ap applier) finishScheduled(ctx context.Context, a Action) error {
	env := ap.env
	r, ok, err := ap.openScheduled(ctx, a)
	if err != nil || !ok {
		return err
	}
	if env.ScheduledJob == nil || env.Remote == nil {
		return errors.New("finish: no scheduled jobs in this process")
	}
	w := env.Workplace(a.Repo, r.Node, r.Profile)
	if r.SessionID != "" {
		if err := w.Kill(ctx, r.WorkspaceID, []string{r.SessionID}); err != nil {
			return fmt.Errorf("end claude of run %d before the check: %w", r.ID, err)
		}
	}
	ws, err := w.Lookup(ctx, r.WorkspaceID)
	if err != nil {
		return fmt.Errorf("workspace of run %d: %w", r.ID, err)
	}
	job, jenv, err := env.ScheduledJob(a.Repo, a.Schedule)
	if err != nil {
		return fmt.Errorf("job: %w", err)
	}
	rec, err := scheduled.DecodeRecord(r.Verdict)
	if err != nil || rec.GitState == "" {
		return ap.endScheduled(ctx, a, r, runs.Failed, "no start state recorded", a.PR)
	}
	state, err := scheduled.GitState(ctx, jenv.Run, ws.Path)
	if err != nil {
		return fmt.Errorf("git state: %s", oneLine(withoutPath(err)))
	}
	if state != rec.GitState {
		return ap.endScheduled(ctx, a, r, runs.Failed, "git state of the workplace changed since the start", a.PR)
	}
	remote, err := env.Remote(a.Repo)
	if err != nil {
		return fmt.Errorf("remote: %w", err)
	}
	jenv.Dir, jenv.Now = ws.Path, env.Now()
	res, err := scheduled.Postcheck(ctx, jenv.Run, job, jenv, rec.Verdict, r.StartSHA, r.Branch, remote)
	if err != nil {
		// The text only: Postcheck's error is redacted, but it still wraps its raw cause.
		return errors.New("postcheck: " + oneLine(err.Error()))
	}
	switch {
	case res.Failure != "":
		return ap.endScheduled(ctx, a, r, runs.Failed, "postcheck: "+res.Failure, a.PR)
	case !res.Changed:
		return ap.endScheduled(ctx, a, r, runs.Done, "nothing changed", a.PR)
	case a.PR > 0 && a.HeadSHA != res.Commit:
		// A PR on the run's branch whose head is not the checked commit: someone else's.
		return ap.endScheduled(ctx, a, r, runs.Failed, fmt.Sprintf("PR #%d is not on the checked commit", a.PR), a.PR)
	}
	// The merge is bound to the commit on the branch now; recorded before the PR is opened.
	rec.Pushed = res.Commit
	if err := runs.SetVerdict(ctx, env.DB, r.ID, rec.Encode()); err != nil {
		return fmt.Errorf("record pushed commit of run %d: %w", r.ID, err)
	}
	pr := a.PR
	if pr == 0 {
		title := fmt.Sprintf("chore: geplande run %s (%s)", a.Schedule, a.Slot)
		body := fmt.Sprintf("No issue: geplande run %s (%s)", a.Schedule, a.Slot)
		p, err := env.Forge(a.Repo).CreatePR(ctx, r.Branch, title, body)
		if err != nil {
			return fmt.Errorf("create PR: %w", err)
		}
		pr = p.Number
	}
	if err := runs.SetPRAt(ctx, env.DB, r.ID, env.Now()); err != nil {
		return fmt.Errorf("record pr_at of run %d: %w", r.ID, err)
	}
	ap.logf("%s: run %d: PR #%d", subject(a), r.ID, pr)
	return nil
}

// mergeScheduled squash-merges the run's PR on the measured head, which must be the commit the
// check pushed (else the run fails, "branch changed after push"), deletes its branch and ends the
// run as done (row 2): the merge itself deletes no branch. A moved head only defers, so the next
// tick measures again (row 4); a merge the forge refuses closes the PR, deletes the branch and
// fails the run with the forge's reason (row 3).
func (ap applier) mergeScheduled(ctx context.Context, a Action, res *Result) error {
	r, ok, err := ap.openScheduled(ctx, a)
	if err != nil || !ok {
		return err
	}
	// Decide merges only the commit the check pushed; this holds apart from it. MergePR then
	// refuses a head that moved since.
	if rec, err := scheduled.DecodeRecord(r.Verdict); err != nil || rec.Pushed == "" || rec.Pushed != a.HeadSHA {
		return ap.endScheduled(ctx, a, r, runs.Failed, "branch changed after push", a.PR)
	}
	err = ap.env.Forge(a.Repo).MergePR(ctx, a.PR, a.HeadSHA)
	var refused *forge.MergeRefused
	switch {
	case errors.Is(err, forge.ErrHeadMoved):
		res.Deferred = append(res.Deferred, deferral(a, fmt.Sprintf("merge of PR #%d skipped: head moved", a.PR)))
		return nil
	case errors.As(err, &refused):
		return ap.endScheduled(ctx, a, r, runs.Failed, "merge refused: "+oneLine(refused.Message), a.PR)
	case err != nil:
		return fmt.Errorf("merge: %w", err)
	}
	return ap.endScheduled(ctx, a, r, runs.Done, fmt.Sprintf("merged PR #%d", a.PR), 0)
}

// closeScheduled closes the run with a.Outcome and a.ReasonArg (rows 5-7, and the retry rules):
// first it forgets the launch (a.Discard), then it ends the run's claude (a.End), both only when
// the run has a workplace; a failure of either keeps the run open, so a second attempt never runs
// beside a live agent. Then endScheduled.
func (ap applier) closeScheduled(ctx context.Context, a Action) error {
	r, ok, err := ap.openScheduled(ctx, a)
	if err != nil || !ok {
		return err
	}
	if r.WorkspaceID != "" {
		w := ap.env.Workplace(a.Repo, r.Node, r.Profile)
		if a.Discard {
			if err := w.Discard(ctx, r.WorkspaceID); err != nil {
				return fmt.Errorf("discard run %d: %w", r.ID, err)
			}
		}
		if a.End && r.SessionID != "" {
			if err := w.Kill(ctx, r.WorkspaceID, []string{r.SessionID}); err != nil {
				return fmt.Errorf("end claude of run %d: %w", r.ID, err)
			}
		}
	}
	return ap.endScheduled(ctx, a, r, a.Outcome, a.ReasonArg, a.PR)
}

// endScheduled closes PR pr (0: none) and deletes the run's branch, then closes the run with o
// and reason and logs one line. A failed step keeps the run open and is an error; a run that
// ends failed is none.
func (ap applier) endScheduled(ctx context.Context, a Action, r runs.OpenRun, o runs.Outcome, reason string, pr int) error {
	f := ap.env.Forge(a.Repo)
	if pr > 0 {
		if err := f.ClosePR(ctx, pr); err != nil {
			return fmt.Errorf("close PR #%d: %w", pr, err)
		}
	}
	if strings.HasPrefix(r.Branch, scheduledBranchPrefix) {
		if err := f.DeleteBranch(ctx, r.Branch); err != nil {
			return fmt.Errorf("delete branch of run %d: %w", r.ID, err)
		}
	}
	if err := ap.closeRun(ctx, r.ID, o, reason); err != nil {
		return err
	}
	line := fmt.Sprintf("%s: run %d %s", subject(a), r.ID, o)
	if reason != "" {
		line += ": " + oneLine(reason)
	}
	ap.logf("%s", line)
	return nil
}

// openScheduled is the open scheduled run a.RunID. A run no longer open is no error: another
// process closed it, and ok is false.
func (ap applier) openScheduled(ctx context.Context, a Action) (runs.OpenRun, bool, error) {
	open, err := runs.Open(ctx, ap.env.DB, a.Repo)
	if err != nil {
		return runs.OpenRun{}, false, err
	}
	for _, r := range open {
		if r.ID == a.RunID && r.Schedule != "" {
			return r, true, nil
		}
	}
	ap.logf("run %d: already closed", a.RunID)
	return runs.OpenRun{}, false, nil
}

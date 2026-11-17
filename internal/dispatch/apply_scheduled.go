package dispatch

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// startScheduled starts a scheduled run: resolve its job, claim the run row (the only write
// that sets the slot in motion), prepare the workplace on this host (node "") and record it,
// record the start state (start sha, and the git state in the verdict record) for the check
// after the run, stage the job, write the job's instructions, and spawn the agent with the job's
// tools and no MCP config. It posts nothing: a scheduled run has no issue.
//
// It does not go through workplace.Launch, which closes a failed start as start_failed: a step
// that fails after the claim closes the run as retry with "<step>: <cause>", so the slot stays
// open for its second attempt, and returns that error. A claim another process holds (an open
// run of the schedule, also one a dead tick left behind) or a spent trigger is no error: nothing
// is prepared or spawned. An uncertain spawn may have started an agent: its launch is forgotten
// and then its claude ended before the run closes as retry; if either fails the run stays open, as an
// uncertain issue start does, and the error is returned.
func (ap applier) startScheduled(ctx context.Context, a Action, res *Result) error {
	env := ap.env
	if env.ScheduledJob == nil {
		return errors.New("job: no scheduled jobs in this process")
	}
	job, jenv, err := env.ScheduledJob(a.Repo, a.Schedule)
	if err != nil {
		return fmt.Errorf("job: %w", err)
	}
	rec, err := scheduled.DecodeRecord(a.Verdict)
	if err != nil {
		return err
	}
	now := env.Now()
	session := env.NewSession()
	run, err := runs.Take(ctx, env.DB, runs.Claim{
		Repo: a.Repo, Schedule: a.Schedule, SessionID: session, Mode: a.Mode, Trigger: runs.TriggerScheduled,
		TriggerKey: a.TriggerKey, Command: a.Command, Prompt: a.Prompt, Profile: a.Profile,
		Branch: a.Branch, Verdict: a.Verdict, Now: now,
	})
	if errors.Is(err, runs.ErrClaimed) || errors.Is(err, runs.ErrTriggerSpent) {
		ap.logf("%s: not claimed: %v", subject(a), err)
		return nil
	}
	if err != nil {
		return err
	}
	retry := func(step string, cause error) error { return ap.retryScheduled(ctx, run, step, cause) }
	// The branch follows the attempt Take counted; it differs from Decide's only when another
	// process closed a run of the slot in between.
	branch := a.Branch
	if run.Attempt != a.Attempt {
		branch = ScheduledBranch(a.Schedule, a.Slot, run.Attempt)
	}

	w := env.Workplace(a.Repo, "", a.Profile)
	ws, err := w.Prepare(ctx, branch)
	if err != nil {
		return retry("prepare", err)
	}
	if err := runs.SetWorkplace(ctx, env.DB, run.ID, ws.ID, branch); err != nil {
		return retry("record workplace", err)
	}
	sha, gitState, err := scheduled.StartState(ctx, jenv.Run, ws.Path)
	if err != nil {
		return retry("start state", err)
	}
	rec.GitState = gitState
	if err := runs.SetStartSHA(ctx, env.DB, run.ID, sha); err != nil {
		return retry("record start state", err)
	}
	if err := runs.SetVerdict(ctx, env.DB, run.ID, rec.Encode()); err != nil {
		return retry("record start state", err)
	}
	jenv.Dir, jenv.Now = ws.Path, now
	if err := job.Stage(ctx, jenv, rec.Verdict); err != nil {
		return retry("stage", err)
	}
	file, err := writeRunInstructions(env.StateDir, session, []byte(a.Instructions))
	if err != nil {
		return retry("instructions", err)
	}

	spec := workplace.LaunchSpec{
		Repo: a.Repo, SessionID: session, Mode: a.Mode, Profile: a.Profile, Prompt: a.Prompt,
		InstructionsFile: file, Tools: a.Tools,
		// No MCP server, whatever the profile has (spec §4): an MCP tool acts outside the
		// workplace, where the check after the run does not look.
		MCPConfig: "",
	}
	s, err := w.Spawn(ctx, ws, spec)
	switch {
	case errors.Is(err, workplace.ErrNotAccepted):
		return retry("launch not accepted", err)
	case err != nil:
		// Forget the launch first, so a target that has not started yet finds nothing to start
		// (kenn-forge removes the carrier); then end whatever already started.
		if derr := w.Discard(ctx, ws.ID); derr != nil {
			return fmt.Errorf("launch uncertain: %w; discard run %d: %w", err, run.ID, derr)
		}
		if kerr := w.Kill(ctx, ws.ID, []string{session}); kerr != nil {
			return fmt.Errorf("launch uncertain: %w; end claude of run %d: %w", err, run.ID, kerr)
		}
		return retry("launch uncertain", err)
	}
	res.Started++
	if err := runs.SetSession(ctx, env.DB, run.ID, s.Key); err != nil {
		// The agent runs; AdoptSessions records its key in a later tick.
		return fmt.Errorf("record session of run %d: %w", run.ID, err)
	}
	ap.logf("%s: started run %d (attempt %d) on %s", subject(a), run.ID, run.Attempt, branch)
	return nil
}

// retryScheduled closes a claimed scheduled run whose start failed with reason "<step>: <cause>"
// (retryOutcome) and returns that as an error. The release ignores a cancelled ctx: the cancel
// may be what made the step fail.
func (ap applier) retryScheduled(ctx context.Context, run runs.Run, step string, cause error) error {
	err := fmt.Errorf("%s: %w", step, cause)
	o := retryOutcome(run.Attempt)
	if rerr := runs.Release(context.WithoutCancel(ctx), ap.env.DB, run.ID, o, oneLine(withoutPath(err)), ap.env.Now()); rerr != nil {
		return errors.Join(err, fmt.Errorf("release run %d: %w", run.ID, rerr))
	}
	return err
}

// withoutPath is the text of err with the path of a file error in it cut to its base name: a run
// reason must not carry a raw path.
func withoutPath(err error) string {
	text := err.Error()
	var pe *fs.PathError
	if errors.As(err, &pe) && pe.Path != "" {
		text = strings.ReplaceAll(text, pe.Path, filepath.Base(pe.Path))
	}
	return text
}

// retryOutcome is how a scheduled run that failed for a passing reason ends: retry, which
// leaves its slot open, or on the last attempt failed: after two attempts the slot is failed
// (spec §2). Either way two runs settle the slot.
func retryOutcome(attempt int) runs.Outcome {
	if attempt >= maxScheduledAttempts {
		return runs.Failed
	}
	return runs.Retry
}

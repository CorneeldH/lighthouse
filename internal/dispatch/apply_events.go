package dispatch

// This file stores the question, answer and done moments of the dispatcher as trajectory
// events (telemetry.RecordEvent), the source the human-work and wait-time timeline reads. A
// failed write never fails the tick: it logs `event not recorded` and the timeline falls back
// to an estimate for that moment. Nothing is retried.

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

// eventSource is the source of every trajectory event the dispatcher writes.
const eventSource = "dispatch"

// recordEvent stores an event of kind on run runID of the trajectory of a.Repo#a.Issue. A
// failure is logged with `event not recorded` and returned to no one: the tick goes on.
func (ap applier) recordEvent(ctx context.Context, a Action, kind telemetry.EventKind, runID int64, sourceID, ref string, at time.Time) {
	_, _, err := telemetry.RecordEvent(ctx, ap.env.DB, telemetry.EventRecord{
		Origin: telemetry.Origin{Source: eventSource, SourceID: sourceID, RecordedAt: ap.env.Now()},
		Repo:   a.Repo, Issue: a.Issue, RunID: runID, Kind: kind, Ref: ref, At: at,
	})
	if err != nil {
		ap.logf("%s#%d: %s event not recorded: %s", a.Repo, a.Issue, kind, oneLine(err.Error()))
	}
}

// recordQuestion stores the question event of a question close a, after its run closed.
func (ap applier) recordQuestion(ctx context.Context, a Action) {
	if a.Outcome != runs.Question || a.EventRef == "" {
		return
	}
	ap.recordEvent(ctx, a, telemetry.EventQuestion, a.RunID, "question:"+a.EventRef, a.EventRef, a.EventAt)
}

// recordAnswer stores the answer event of a, whose run runID was just claimed, when a picks up
// the answer to a human question: an answer: or pushanswer: key with the Vraag in EventRef. A
// repair: key answers no human and carries no EventRef.
func (ap applier) recordAnswer(ctx context.Context, a Action, runID int64) {
	if a.EventRef == "" || a.CommentID <= 0 ||
		!strings.HasPrefix(a.TriggerKey, triggerAnswer+":") && !strings.HasPrefix(a.TriggerKey, triggerPushAnswer+":") {
		return
	}
	ap.recordEvent(ctx, a, telemetry.EventAnswer, runID, "answer:"+strconv.FormatInt(a.CommentID, 10), a.EventRef, a.EventAt)
}

// noteDone stores the done event of run a.RunID at this tick's now, once: the marker file
// <StateDir>/events/done-<run-id>.tried is made before the attempt, and a marker that exists
// skips it, so a failed write is not tried again either. Apply builds its applier per call, so
// only a file outlives the tick.
func (ap applier) noteDone(ctx context.Context, a Action) {
	id := strconv.FormatInt(a.RunID, 10)
	if ap.env.StateDir == "" {
		ap.logf("%s#%d: done event not recorded: no state dir", a.Repo, a.Issue)
		return
	}
	dir := filepath.Join(ap.env.StateDir, "events")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		ap.logf("%s#%d: done event not recorded: %s", a.Repo, a.Issue, oneLine(err.Error()))
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "done-"+id+".tried"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return
	}
	if err != nil {
		ap.logf("%s#%d: done event not recorded: %s", a.Repo, a.Issue, oneLine(err.Error()))
		return
	}
	_ = f.Close()
	ap.recordEvent(ctx, a, telemetry.EventDone, a.RunID, "done:"+id, id, ap.env.Now())
}

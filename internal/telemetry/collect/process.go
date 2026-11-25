package collect

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/CorneeldH/lighthouse/internal/telemetry"
	"github.com/CorneeldH/lighthouse/internal/telemetry/transcript"
)

const (
	sourceName   = "transcript"
	harnessName  = "claude-code"
	closeAfter   = time.Hour
	outcomeRun   = "run"
	outcomeSess  = "session"
	outcomeSkips = "skipped"
)

// Source is one session's transcript as the collector hands it over.
type Source struct {
	SessionID, TranscriptPath string
	Files                     []transcript.File
	// MTime is the newest mtime of the main and subagent files; zero for raw bytes without an
	// mtime (the rule for those is in Process).
	MTime time.Time
	// Recorded is the moment before the files were read; it becomes recorded_at.
	Recorded time.Time
}

// Options are the repo facts and switches of one collection.
type Options struct {
	Repo, DefaultBranch, PlansDir, RepoDir, StateDir string
	Now                                              func() time.Time
	SinceLast                                        bool
	// DryRun parses and assigns but writes nothing: no transaction and no plan update. The
	// outcome carries the runs the transcript would go to; SessionID stays 0.
	DryRun bool
}

// Outcome says what Process did with a transcript: Kind is "run", "session" or "skipped".
type Outcome struct {
	Kind      string
	Reason    string
	SessionID int64
	RunIDs    []int64
}

// runInfo is a runs row with the trajectory it belongs to, when it has one.
type runInfo struct {
	RunRow
	repo  string
	issue int
	// setupAt is the recorded_at of the setups row of the run; zero when there is none.
	setupAt time.Time
}

// Process writes one transcript to the database in one transaction: to the runs that share its
// session id, else to an interactive session. See the design of #375.
func Process(ctx context.Context, db *sql.DB, src Source, opt Options) (Outcome, error) {
	now := time.Now()
	if opt.Now != nil {
		now = opt.Now()
	}
	if src.Recorded.IsZero() {
		src.Recorded = now
	}
	sid := src.SessionID
	runRows, err := runsOf(ctx, db, sid)
	if err != nil {
		return Outcome{}, err
	}
	if reason := skipReason(ctx, db, src, opt, now, runRows); reason != "" {
		return Outcome{Kind: outcomeSkips, Reason: reason}, nil
	}
	reading, err := transcript.Parse(src.Files)
	if err != nil {
		return Outcome{}, fmt.Errorf("parse transcript %s: %w", sid, err)
	}
	started, ended := reading.Bounds()
	if started.IsZero() {
		return Outcome{Kind: outcomeSkips, Reason: "transcript has no lines"}, nil
	}
	if len(runRows) > 0 {
		return writeRun(ctx, db, src, opt, reading, runRows)
	}
	return writeSession(ctx, db, src, opt, reading, now, ended)
}

// runsOf returns the runs that carry the session id, in start order.
func runsOf(ctx context.Context, db *sql.DB, sid string) ([]runInfo, error) {
	if sid == "" {
		return nil, errors.New("transcript has no session id")
	}
	rows, err := db.QueryContext(ctx, `SELECT r.id, r.started_at, r.ended_at, t.repo, t.issue,
			(SELECT recorded_at FROM setups WHERE run_id = r.id)
		FROM runs r LEFT JOIN trajectories t ON t.id = r.trajectory_id
		WHERE r.session_id = ? ORDER BY r.started_at, r.id`, sid)
	if err != nil {
		return nil, fmt.Errorf("runs of session %s: %w", sid, err)
	}
	defer func() { _ = rows.Close() }()
	var out []runInfo
	for rows.Next() {
		var ri runInfo
		var started string
		var ended, repo, setup sql.NullString
		var issue sql.NullInt64
		if err := rows.Scan(&ri.ID, &started, &ended, &repo, &issue, &setup); err != nil {
			return nil, fmt.Errorf("runs of session %s: %w", sid, err)
		}
		if ri.Started, err = time.Parse(time.RFC3339Nano, started); err != nil {
			return nil, fmt.Errorf("run %d started_at: %w", ri.ID, err)
		}
		if ended.Valid {
			t, err := time.Parse(time.RFC3339Nano, ended.String)
			if err != nil {
				return nil, fmt.Errorf("run %d ended_at: %w", ri.ID, err)
			}
			ri.Ended = &t
		}
		if setup.Valid {
			if ri.setupAt, err = time.Parse(time.RFC3339Nano, setup.String); err != nil {
				return nil, fmt.Errorf("run %d setup recorded_at: %w", ri.ID, err)
			}
		}
		ri.repo, ri.issue = repo.String, int(issue.Int64)
		out = append(out, ri)
	}
	return out, rows.Err()
}

// skipReason is the reason --since-last leaves the transcript alone, or "" to process it.
func skipReason(ctx context.Context, db *sql.DB, src Source, opt Options, now time.Time, runRows []runInfo) string {
	if !opt.SinceLast {
		return ""
	}
	if len(runRows) > 0 {
		return skipRuns(src, runRows)
	}
	if src.MTime.IsZero() {
		return ""
	}
	var recorded string
	var ended sql.NullString
	err := db.QueryRowContext(ctx, `SELECT recorded_at, ended_at FROM sessions WHERE harness = ? AND session_key = ?`,
		harnessName, src.SessionID).Scan(&recorded, &ended)
	if err != nil {
		return "" // no row yet, or a read error: process, the write reports a real failure
	}
	at, err := time.Parse(time.RFC3339Nano, recorded)
	if err != nil || at.Before(src.MTime) {
		return ""
	}
	if !ended.Valid && now.Sub(src.MTime) >= closeAfter {
		return "" // an open session is revisited once it has been still for an hour
	}
	return "unchanged since the last pass"
}

// skipRuns decides --since-last for the runs of a session. With an mtime, every run needs a
// setups row at or after it. Raw bytes carry no mtime: a run is then settled only when it has
// ended and its setups row was recorded at or after that end.
func skipRuns(src Source, runRows []runInfo) string {
	for _, r := range runRows {
		switch {
		case r.setupAt.IsZero():
			return ""
		case src.MTime.IsZero() && (r.Ended == nil || r.setupAt.Before(*r.Ended)):
			return ""
		case !src.MTime.IsZero() && r.setupAt.Before(src.MTime):
			return ""
		}
	}
	return "unchanged since the last pass"
}

// segment is the stretch of the transcript that one run gets, and the run's own window.
type segment struct {
	run      runInfo
	window   RunWindow
	from, to time.Time // lines with from <= at < to belong to the run; zero is open
}

// assignRuns splits the transcript over the runs that started before its last line. The first
// run takes everything before the second one starts, a later run everything up to the next start;
// a line in a gap, or after an ended run's end, stays with the run that started before it.
func assignRuns(runRows []runInfo, readingEnd time.Time) []segment {
	rows := make([]RunRow, len(runRows))
	byID := map[int64]runInfo{}
	for i, r := range runRows {
		rows[i] = r.RunRow
		byID[r.ID] = r
	}
	ws := Windows(rows, readingEnd)
	segs := make([]segment, len(ws))
	for i, w := range ws {
		s := segment{run: byID[w.RunID], window: w}
		if i > 0 {
			s.from = w.From
		}
		if i+1 < len(ws) {
			s.to = ws[i+1].From
		}
		segs[i] = s
	}
	return segs
}

func writeRun(ctx context.Context, db *sql.DB, src Source, opt Options, reading transcript.Reading, runRows []runInfo) (Outcome, error) {
	_, ended := reading.Bounds()
	segs := assignRuns(runRows, ended)
	if len(segs) == 0 {
		return Outcome{Kind: outcomeSkips, Reason: "every run started after the last line of the transcript"}, nil
	}
	obs, err := ReadSidecar(opt.StateDir, src.SessionID)
	if err != nil {
		return Outcome{}, err
	}
	plans, err := plansFor(ctx, db, opt, reading, runTrajectories(runRows))
	if err != nil {
		return Outcome{}, err
	}
	out := Outcome{Kind: outcomeRun}
	if opt.DryRun {
		for _, s := range segs {
			out.RunIDs = append(out.RunIDs, s.run.ID)
		}
		return out, nil
	}
	err = telemetry.InTransaction(ctx, db, func(tx *telemetry.Tx) error {
		out.RunIDs = nil
		for _, s := range segs {
			suffix := ""
			if len(runRows) > 1 {
				suffix = fmt.Sprintf(":%d", s.run.ID)
			}
			// Ordering assumption: runs are claimed before the transcript size is measured, so a
			// later pass never moves a model's usage wholly to a later run; a usage row therefore
			// never goes stale in the earlier run.
			w := writing{
				sid: src.SessionID, suffix: suffix, recorded: src.Recorded, runID: s.run.ID,
				setup: reading.SetupBetween(s.from, s.to), usage: reading.UsageBetween(s.from, s.to),
				obs: PickObservation(obs, s.window),
			}
			if err := writeSetupAndUsage(ctx, tx, w); err != nil {
				return err
			}
			out.RunIDs = append(out.RunIDs, s.run.ID)
		}
		return setPlans(ctx, tx, opt.Repo, plans)
	})
	return out, err
}

func writeSession(ctx context.Context, db *sql.DB, src Source, opt Options, reading transcript.Reading, now, readingEnd time.Time) (Outcome, error) {
	started, _ := reading.Bounds()
	var endedAt *time.Time
	if !src.MTime.IsZero() && now.Sub(src.MTime) >= closeAfter {
		endedAt = &readingEnd
	}
	obs, err := ReadSidecar(opt.StateDir, src.SessionID)
	if err != nil {
		return Outcome{}, err
	}
	lines := reading.Lines()
	var plans []plan
	link := telemetry.Link(opt.DefaultBranch, lines)
	if link.Issue > 0 {
		plans, err = plansFor(ctx, db, opt, reading, []trajectory{{repo: opt.Repo, issue: link.Issue}})
		if err != nil {
			return Outcome{}, err
		}
	}
	out := Outcome{Kind: outcomeSess}
	if opt.DryRun {
		return out, nil
	}
	err = telemetry.InTransaction(ctx, db, func(tx *telemetry.Tx) error {
		id, _, err := tx.RecordSession(ctx, telemetry.SessionRecord{
			Origin: telemetry.Origin{Source: sourceName, SourceID: "transcript:" + src.SessionID, RecordedAt: src.Recorded},
			Repo:   opt.Repo, Harness: harnessName, SessionKey: src.SessionID, TranscriptPath: src.TranscriptPath,
			DefaultBranch: opt.DefaultBranch, Lines: lines, StartedAt: started, EndedAt: endedAt,
		})
		if err != nil {
			return err
		}
		out.SessionID = id
		w := writing{
			sid: src.SessionID, recorded: src.Recorded, sessionID: id,
			setup: reading.SetupBetween(time.Time{}, time.Time{}), usage: reading.UsageBetween(time.Time{}, time.Time{}),
			obs: PickObservation(obs, RunWindow{}),
		}
		if err := writeSetupAndUsage(ctx, tx, w); err != nil {
			return err
		}
		if endedAt != nil {
			if err := writeQuestions(ctx, tx, src, opt, reading, id, link.Issue); err != nil {
				return err
			}
		}
		return setPlans(ctx, tx, opt.Repo, plans)
	})
	if errors.Is(err, telemetry.ErrSessionHasFindings) || errors.Is(err, telemetry.ErrSessionHasEvents) {
		// The session keeps its link; the transaction rolled back, so nothing of this pass is stored.
		return Outcome{Kind: outcomeSkips, Reason: "session keeps its trajectory: " + err.Error()}, nil
	}
	return out, err
}

// writeQuestions records the questions and answers of a closed session as trajectory events.
// A session without a trajectory (issue 0) has nothing to attach them to. The ref carries the
// session id because transcripts reuse tool-use ids across sessions.
func writeQuestions(ctx context.Context, tx *telemetry.Tx, src Source, opt Options, reading transcript.Reading, sessionRowID int64, issue int) error {
	if issue == 0 {
		return nil
	}
	for _, q := range reading.Questions() {
		ref := src.SessionID + ":" + q.ToolUseID
		ev := telemetry.EventRecord{
			Origin: telemetry.Origin{Source: sourceName, SourceID: "transcript:" + ref + ":question", RecordedAt: src.Recorded},
			Repo:   opt.Repo, Issue: issue, SessionID: sessionRowID, Kind: telemetry.EventQuestion, Ref: ref, At: q.Asked,
		}
		if _, _, err := tx.RecordEvent(ctx, ev); err != nil {
			return err
		}
		if q.Answered == nil {
			continue
		}
		ev.SourceID, ev.Kind, ev.At = "transcript:"+ref+":answer", telemetry.EventAnswer, *q.Answered
		if _, _, err := tx.RecordEvent(ctx, ev); err != nil {
			return err
		}
	}
	return nil
}

// writing is what one run or session gets written: its setup, usage per model and the
// observation of the versions. Exactly one of runID and sessionID is set.
type writing struct {
	sid       string
	suffix    string // ":<run id>" when the transcript is divided over runs
	recorded  time.Time
	runID     int64
	sessionID int64
	setup     transcript.Setup
	usage     map[string]transcript.ModelUsage
	obs       *Observation
}

// writeSetupAndUsage is the part the run path and the session path share.
func writeSetupAndUsage(ctx context.Context, tx *telemetry.Tx, w writing) error {
	rec := telemetry.SetupRecord{
		Origin: telemetry.Origin{Source: sourceName, SourceID: "transcript:" + w.sid + w.suffix, RecordedAt: w.recorded},
		RunID:  w.runID, SessionID: w.sessionID,
		Model: w.setup.Model, Effort: w.setup.Effort, Harness: w.setup.Harness, HarnessVersion: w.setup.HarnessVersion,
		SuperpowersVersion: w.setup.SuperpowersVersion, SkillsHash: w.setup.SkillsHash, Lane: w.setup.Lane,
	}
	if w.obs != nil {
		rec.LighthouseVersion, rec.PackVersion = nonEmpty(w.obs.LighthouseVersion), nonEmpty(w.obs.PackVersion)
	}
	if _, _, err := tx.RecordSetup(ctx, rec); err != nil {
		return err
	}
	models := make([]string, 0, len(w.usage))
	for m := range w.usage {
		models = append(models, m)
	}
	sort.Strings(models)
	for _, m := range models {
		u := w.usage[m]
		ur := telemetry.UsageRecord{
			Origin: telemetry.Origin{Source: sourceName, SourceID: "transcript:" + w.sid + ":" + m + w.suffix, RecordedAt: w.recorded},
			RunID:  w.runID, SessionID: w.sessionID, Model: m,
			InputTokens: u.Tokens.Input, OutputTokens: u.Tokens.Output,
			CacheReadTokens: u.Tokens.CacheRead, CacheWriteTokens: u.Tokens.CacheWrite(),
			Cost: u.Cost,
		}
		if u.Cost != nil {
			ur.CostKind = "estimate"
		}
		if _, _, err := tx.RecordUsage(ctx, ur); err != nil {
			return err
		}
	}
	return nil
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// trajectory names the issue trajectory a plan belongs to.
type trajectory struct {
	repo  string
	issue int
}

// plan is the plan found for one trajectory; file is nil when there is none.
type plan struct {
	trajectory
	file *planFile
}

// runTrajectories lists the distinct trajectories of the runs; a run without one has no plan.
func runTrajectories(runRows []runInfo) []trajectory {
	var out []trajectory
	seen := map[trajectory]bool{}
	for _, r := range runRows {
		t := trajectory{r.repo, r.issue}
		if r.issue == 0 || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// plansFor finds the plan of each trajectory (planFor of the design), before the transaction
// opens. A plan the transcript itself wrote always counts. The name fallback only applies to a
// trajectory without a plan_path: a plan another transcript of the trajectory wrote stays, so the
// result does not depend on the order the transcripts are collected in. A trajectory that has a
// plan and for which this transcript found none is left alone.
func plansFor(ctx context.Context, db *sql.DB, opt Options, reading transcript.Reading, ts []trajectory) ([]plan, error) {
	var out []plan
	written := reading.WrittenFiles()
	for _, t := range ts {
		f, err := findWrittenPlan(ctx, opt, written)
		if err != nil {
			return nil, err
		}
		if f == nil {
			has, err := hasPlan(ctx, db, t)
			if err != nil {
				return nil, err
			}
			if has {
				continue
			}
			if f, err = findNamedPlan(ctx, opt, t.issue); err != nil {
				return nil, err
			}
		}
		out = append(out, plan{trajectory: t, file: f})
	}
	return out, nil
}

// hasPlan reports whether the trajectory already has a plan_path. A trajectory that does not
// exist yet has none.
func hasPlan(ctx context.Context, db *sql.DB, t trajectory) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT count(*) FROM trajectories WHERE repo = ? COLLATE NOCASE AND issue = ?
		AND plan_path IS NOT NULL`, t.repo, t.issue).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("plan of %s#%d: %w", t.repo, t.issue, err)
	}
	return n > 0, nil
}

func setPlans(ctx context.Context, tx *telemetry.Tx, repo string, plans []plan) error {
	for _, p := range plans {
		var tasks *int
		path := ""
		if p.file != nil {
			n := CountPlanTasks(p.file.Text)
			tasks, path = &n, p.file.Rel
		}
		if err := tx.SetPlan(ctx, p.repo, p.issue, tasks, path); err != nil {
			return err
		}
	}
	return nil
}

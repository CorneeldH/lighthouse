package telemetry

// This file reads one trajectory from lighthouse.db into a TimelineInput and
// lays it out with Timeline. Nothing is stored for the timeline itself: runs,
// sessions and events are read as they are, and the times are parsed as
// RFC 3339.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// FindTrajectoryID returns the id of the trajectory (repo, issue), comparing
// repo without case, and false when there is none.
func FindTrajectoryID(ctx context.Context, db *sql.DB, repo string, issue int) (int64, bool, error) {
	var id int64
	err := db.QueryRowContext(ctx, `SELECT id FROM trajectories WHERE repo = ? COLLATE NOCASE AND issue = ?
		ORDER BY repo = ? DESC, id LIMIT 1`, repo, issue, repo).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("telemetry: find trajectory: %w", err)
	}
	return id, true, nil
}

// LoadTimeline reads the runs, sessions and events of one trajectory and
// returns its timeline at now. closedAt zero means the issue is not closed.
func LoadTimeline(ctx context.Context, db *sql.DB, trajectoryID int64, closedAt, now time.Time) (TrajectoryTimeline, error) {
	in := TimelineInput{ClosedAt: closedAt}
	var err error
	if in.Runs, err = loadRuns(ctx, db, trajectoryID); err != nil {
		return TrajectoryTimeline{}, err
	}
	if in.Sessions, err = loadSessions(ctx, db, trajectoryID); err != nil {
		return TrajectoryTimeline{}, err
	}
	if in.Events, err = loadEvents(ctx, db, trajectoryID); err != nil {
		return TrajectoryTimeline{}, err
	}
	return Timeline(in, now), nil
}

func parseStamp(what, s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("telemetry: parse %s %q: %w", what, s, err)
	}
	return t, nil
}

func parseEnd(what string, s sql.NullString) (*time.Time, error) {
	if !s.Valid {
		return nil, nil
	}
	t, err := parseStamp(what, s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func loadRuns(ctx context.Context, db *sql.DB, tid int64) ([]TimelineRun, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, trigger, trigger_key, command, outcome, reason, started_at, ended_at
		FROM runs WHERE trajectory_id = ? ORDER BY started_at, id`, tid)
	if err != nil {
		return nil, fmt.Errorf("telemetry: read runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []TimelineRun
	for rows.Next() {
		var r TimelineRun
		var outcome, ended sql.NullString
		var started string
		if err := rows.Scan(&r.ID, &r.Trigger, &r.TriggerKey, &r.Command, &outcome, &r.Reason, &started, &ended); err != nil {
			return nil, fmt.Errorf("telemetry: scan run: %w", err)
		}
		r.Outcome = outcome.String
		if r.StartedAt, err = parseStamp("run start", started); err != nil {
			return nil, err
		}
		if r.EndedAt, err = parseEnd("run end", ended); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func loadSessions(ctx context.Context, db *sql.DB, tid int64) ([]TimelineSession, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, started_at, ended_at FROM sessions
		WHERE trajectory_id = ? ORDER BY started_at, id`, tid)
	if err != nil {
		return nil, fmt.Errorf("telemetry: read sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []TimelineSession
	for rows.Next() {
		var s TimelineSession
		var started string
		var ended sql.NullString
		if err := rows.Scan(&s.ID, &started, &ended); err != nil {
			return nil, fmt.Errorf("telemetry: scan session: %w", err)
		}
		if s.StartedAt, err = parseStamp("session start", started); err != nil {
			return nil, err
		}
		if s.EndedAt, err = parseEnd("session end", ended); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func loadEvents(ctx context.Context, db *sql.DB, tid int64) ([]TimelineEvent, error) {
	rows, err := db.QueryContext(ctx, `SELECT kind, run_id, session_id, ref, at FROM trajectory_events
		WHERE trajectory_id = ? ORDER BY at, id`, tid)
	if err != nil {
		return nil, fmt.Errorf("telemetry: read events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []TimelineEvent
	for rows.Next() {
		var e TimelineEvent
		var kind, at string
		var run, sess sql.NullInt64
		if err := rows.Scan(&kind, &run, &sess, &e.Ref, &at); err != nil {
			return nil, fmt.Errorf("telemetry: scan event: %w", err)
		}
		e.Kind, e.RunID, e.SessionID = EventKind(kind), run.Int64, sess.Int64
		if e.Kind == EventAnswer {
			e.AnswerTo = e.Ref
		}
		if e.At, err = parseStamp("event time", at); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

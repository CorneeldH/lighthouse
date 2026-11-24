package telemetry

// This file holds the writer for trajectory events: the question, answer and
// done moments that human work and wait time are measured from. Only a kind, a
// reference and a time are stored, never the text of a question or answer.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// EventKind is the kind of a trajectory event.
type EventKind string

const (
	EventQuestion EventKind = "question"
	EventAnswer   EventKind = "answer"
	EventDone     EventKind = "done"
)

// EventRecord is one event on the existing trajectory (Repo, Issue), owned by
// exactly one of a run and a session. Done belongs to a run only. Ref ties an
// answer to its question.
type EventRecord struct {
	Origin
	Repo             string
	Issue            int
	RunID, SessionID int64
	Kind             EventKind
	Ref              string
	At               time.Time
}

func (e EventRecord) validate() error {
	if err := e.Origin.validate(); err != nil {
		return err
	}
	if e.Repo == "" || e.Issue < 1 {
		return fmt.Errorf("telemetry: event needs a repo and an issue above 0; got %q, %d", e.Repo, e.Issue)
	}
	if err := oneOwner(e.RunID, e.SessionID); err != nil {
		return err
	}
	switch e.Kind {
	case EventQuestion, EventAnswer:
	case EventDone:
		if e.RunID == 0 {
			return errors.New("telemetry: a done event belongs to a run, not a session")
		}
	default:
		return fmt.Errorf("telemetry: unknown event kind %q", e.Kind)
	}
	if e.Ref == "" {
		return errors.New("telemetry: event needs a ref")
	}
	if e.At.IsZero() {
		return errors.New("telemetry: event needs an at time")
	}
	return nil
}

// RecordEvent records an event on the existing trajectory (Repo, Issue); it
// creates no trajectory. The run or session must point at that trajectory,
// else ErrTrajectoryMismatch. A second offering of the same origin, or of
// the same (trajectory, kind, ref), leaves the first row and reports not new:
// the first answer to a question wins.
func RecordEvent(ctx context.Context, db *sql.DB, e EventRecord) (int64, bool, error) {
	if err := e.validate(); err != nil {
		return 0, false, err
	}
	return inTx(ctx, db, func(tx *sql.Tx) (int64, bool, error) {
		repo, err := resolveRepo(ctx, tx, e.Repo)
		if err != nil {
			return 0, false, err
		}
		tid, err := eventTrajectory(ctx, tx, e, repo)
		if err != nil {
			return 0, false, err
		}
		return insertOrFind(ctx, tx, "trajectory_events", `INSERT INTO trajectory_events (source, source_id,
			recorded_at, trajectory_id, run_id, session_id, kind, ref, at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			[]any{e.Source, e.SourceID, stamp(e.RecordedAt), tid, nullID(e.RunID), nullID(e.SessionID),
				string(e.Kind), e.Ref, stamp(e.At)},
			bySource(e.Origin),
			lookup{"trajectory_id = ? AND kind = ? AND ref = ?", []any{tid, string(e.Kind), e.Ref}})
	})
}

// eventTrajectory returns the trajectory (repo, issue) of an event after
// checking that its run or session points at it. It creates nothing.
func eventTrajectory(ctx context.Context, tx *sql.Tx, e EventRecord, repo string) (int64, error) {
	table, owner := "runs", e.RunID
	if e.SessionID != 0 {
		table, owner = "sessions", e.SessionID
	}
	var got sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT trajectory_id FROM `+table+` WHERE id = ?`, owner).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("telemetry: event names %s %d, which does not exist", table, owner)
	}
	if err != nil {
		return 0, fmt.Errorf("telemetry: read %s %d: %w", table, owner, err)
	}
	want, ok, err := findTrajectory(ctx, tx, repo, e.Issue)
	if err != nil {
		return 0, err
	}
	if !got.Valid || !ok || got.Int64 != want {
		return 0, fmt.Errorf("%w: %s %d is not on %s#%d", ErrTrajectoryMismatch, table, owner, repo, e.Issue)
	}
	return want, nil
}

package dispatch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

// FullGatherAfter is the longest a tick may go without a full gather.
const FullGatherAfter = 30 * time.Minute

// TickState is what the precheck remembers of a repo between ticks.
type TickState struct {
	Fingerprint string
	FullAt      time.Time // last full gather
}

// LoadTickState reads the state of repo; a repo without rows gives the zero value.
func LoadTickState(ctx context.Context, db *sql.DB, repo string) (TickState, error) {
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM tick_state WHERE repo = ? AND kind = 'fingerprint'`, repo)
	if err != nil {
		return TickState{}, fmt.Errorf("dispatch: load tick state %s: %w", repo, err)
	}
	defer func() { _ = rows.Close() }()
	var s TickState
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return TickState{}, fmt.Errorf("dispatch: load tick state %s: %w", repo, err)
		}
		switch k {
		case "value":
			s.Fingerprint = v
		case "full_at":
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return TickState{}, fmt.Errorf("dispatch: tick state %s: full_at %q: %w", repo, v, err)
			}
			s.FullAt = t
		}
	}
	if err := rows.Err(); err != nil {
		return TickState{}, fmt.Errorf("dispatch: load tick state %s: %w", repo, err)
	}
	return s, nil
}

// SaveTickState upserts the state of repo in one transaction. Times are stored in UTC.
func SaveTickState(ctx context.Context, db *sql.DB, repo string, s TickState, now time.Time) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("dispatch: save tick state %s: %w", repo, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	stamp := now.UTC().Format(time.RFC3339)
	for _, kv := range [][2]string{{"value", s.Fingerprint}, {"full_at", s.FullAt.UTC().Format(time.RFC3339)}} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO tick_state (repo, kind, key, value, updated_at) VALUES (?, 'fingerprint', ?, ?, ?)
			ON CONFLICT (repo, kind, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			repo, kv[0], kv[1], stamp); err != nil {
			return fmt.Errorf("dispatch: save tick state %s: %w", repo, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("dispatch: save tick state %s: %w", repo, err)
	}
	return nil
}

// Boundaries are the moments a tick must look again even when nothing on the board changed:
// per open run the end of MaxRuntime and MaxWait, and the start of the night window today
// and yesterday, in the zone of now.
func Boundaries(d hostconfig.Dispatch, open []runs.OpenRun, now time.Time) []time.Time {
	var out []time.Time
	for _, r := range open {
		out = append(out, r.StartedAt.Add(d.MaxRuntime), r.StartedAt.Add(d.MaxWait))
	}
	if start, err := nightStart(d.Night); err == nil {
		y, m, day := now.Date()
		for _, back := range []int{0, 1} {
			out = append(out, time.Date(y, m, day-back, start.h, start.m, 0, 0, now.Location()))
		}
	}
	return out
}

type clock struct{ h, m int }

// nightStart parses the HH:MM before the dash of an "HH:MM-HH:MM" window.
func nightStart(night string) (clock, error) {
	var c clock
	var eh, em int
	if n, err := fmt.Sscanf(night, "%d:%d-%d:%d", &c.h, &c.m, &eh, &em); err != nil || n != 4 {
		return clock{}, errors.New("dispatch: night window " + night)
	}
	return c, nil
}

// Due says whether the tick must do a full gather, and why.
func Due(prev TickState, fp string, now time.Time, boundaries []time.Time) (bool, string) {
	switch {
	case prev.Fingerprint == "":
		return true, "first"
	case fp != prev.Fingerprint:
		return true, "changed"
	case now.Sub(prev.FullAt) >= FullGatherAfter:
		return true, "stale"
	}
	for _, b := range boundaries {
		if prev.FullAt.Before(b) && !b.After(now) {
			return true, "boundary"
		}
	}
	return false, "unchanged"
}

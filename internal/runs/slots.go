package runs

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SlotSettled reports whether slot of schedule is handled according to history, the runs of
// one repo: a run of its trigger key ended done or failed, or the key has two runs (attempts).
// The trigger key names trigger, schedule and slot, so it alone picks the runs.
func SlotSettled(history []Row, schedule, slot string) bool {
	key := TriggerScheduled + ":" + schedule + ":" + slot
	n := 0
	for _, r := range history {
		if r.TriggerKey != key {
			continue
		}
		if r.Outcome == Done || r.Outcome == Failed {
			return true
		}
		n++
	}
	return n >= 2
}

// SlotSkipped reports whether the gate of schedule skipped slot in repo.
func SlotSkipped(ctx context.Context, db *sql.DB, repo, schedule, slot string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT count(*) FROM schedule_slots
		WHERE repo = ? AND schedule = ? AND slot = ? AND outcome = 'skipped'`, repo, schedule, slot).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("runs: slot %s of %s skipped: %w", slot, schedule, err)
	}
	return n > 0, nil
}

// SkipSlot records that the gate of schedule skipped slot in repo, with reason. A slot that is
// already skipped keeps its first reason and time.
func SkipSlot(ctx context.Context, db *sql.DB, repo, schedule, slot, reason string, now time.Time) error {
	if _, err := db.ExecContext(ctx, `INSERT INTO schedule_slots (repo, schedule, slot, outcome, reason, at)
		VALUES (?, ?, ?, 'skipped', ?, ?) ON CONFLICT (repo, schedule, slot) DO NOTHING`,
		repo, schedule, slot, reason, stamp(now)); err != nil {
		return fmt.Errorf("runs: skip slot %s of %s: %w", slot, schedule, err)
	}
	return nil
}

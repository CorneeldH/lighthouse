// This file carries the traits of a trajectory that the UI groups by: the
// route, the area and the size of each merged PR.
package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/protocol"
)

// RouteFromComments is the gate of the last triage marker created before
// `before`; the invariant gate counts as "0". A zero before sets no upper bound.
func RouteFromComments(comments []protocol.Comment, before time.Time) (string, bool) {
	var route string
	var found bool
	var latest time.Time
	for _, c := range comments {
		m, ok := protocol.ParseTriageMarker(c.Body)
		if !ok {
			continue
		}
		at, err := time.Parse(time.RFC3339, c.CreatedAt)
		if err != nil || (!before.IsZero() && !at.Before(before)) {
			continue
		}
		if found && at.Before(latest) {
			continue
		}
		route, latest, found = m.Gate, at, true
		if route == "invariant" {
			route = "0"
		}
	}
	return route, found
}

// AreaFromLabels is the rest of the one label that starts with prefix. No such
// label, more than one, or an empty prefix gives false.
func AreaFromLabels(labels []string, prefix string) (string, bool) {
	if prefix == "" {
		return "", false
	}
	var area string
	n := 0
	for _, l := range labels {
		if rest, ok := strings.CutPrefix(l, prefix); ok {
			area = rest
			n++
		}
	}
	if n != 1 {
		return "", false
	}
	return area, true
}

// Traits are the traits of one trajectory. A nil Route or Area is left as it
// is; CodeLines maps a PR to its size.
type Traits struct {
	Repo      string
	Issue     int
	Route     *string
	Area      *string
	CodeLines map[int]int
}

// RecordTraits writes route and area of the trajectory when given, and the
// code lines of each recovery row that has none yet. It never touches status,
// items or merge time.
func RecordTraits(ctx context.Context, db *sql.DB, t Traits) error {
	_, _, err := inTx(ctx, db, func(tx *sql.Tx) (int64, bool, error) {
		repo, err := resolveRepo(ctx, tx, t.Repo)
		if err != nil {
			return 0, false, err
		}
		var tid int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM trajectories WHERE repo = ? AND issue = ?`, repo, t.Issue).Scan(&tid); err != nil {
			return 0, false, fmt.Errorf("telemetry: trajectory %s#%d: %w", t.Repo, t.Issue, err)
		}
		if t.Route != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE trajectories SET route = ? WHERE id = ?`, *t.Route, tid); err != nil {
				return 0, false, fmt.Errorf("telemetry: write route: %w", err)
			}
		}
		if t.Area != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE trajectories SET area = ? WHERE id = ?`, *t.Area, tid); err != nil {
				return 0, false, fmt.Errorf("telemetry: write area: %w", err)
			}
		}
		for pr, n := range t.CodeLines {
			if _, err := tx.ExecContext(ctx, `UPDATE recoveries SET code_lines = ? WHERE trajectory_id = ? AND pr = ? AND code_lines IS NULL`, n, tid, pr); err != nil {
				return 0, false, fmt.Errorf("telemetry: write code lines of PR %d: %w", pr, err)
			}
		}
		return tid, false, nil
	})
	return err
}

// TraitTarget is a trajectory with at least one recovery row, and what is
// still to fill in for it.
type TraitTarget struct {
	Repo          string
	Issue         int
	PRs           []int
	NeedsFill     bool       // route, area or the code lines of a PR are NULL
	InWindow      bool       // a PR merged within the window
	FirstReviewAt *time.Time // the smallest review run time of the trajectory
	LastMergedAt  time.Time
}

// TraitTargets lists the trajectories of repo with a recovery row; InWindow
// looks `days` back from now.
func TraitTargets(ctx context.Context, db *sql.DB, repo string, now time.Time, days int) ([]TraitTarget, error) {
	rows, err := db.QueryContext(ctx, `SELECT t.issue, r.pr, r.merged_at, r.code_lines IS NULL,
			t.route IS NULL OR t.area IS NULL,
			(SELECT MIN(ran_at) FROM review_runs WHERE trajectory_id = t.id)
		FROM trajectories t JOIN recoveries r ON r.trajectory_id = t.id
		WHERE t.repo = ? ORDER BY t.issue, r.pr`, repo)
	if err != nil {
		return nil, fmt.Errorf("telemetry: trait targets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	from := now.AddDate(0, 0, -days)
	var out []TraitTarget
	for rows.Next() {
		var issue, pr int
		var merged string
		var noLines, noTraits bool
		var first sql.NullString
		if err := rows.Scan(&issue, &pr, &merged, &noLines, &noTraits, &first); err != nil {
			return nil, fmt.Errorf("telemetry: scan trait target: %w", err)
		}
		at, err := time.Parse(time.RFC3339, merged)
		if err != nil {
			return nil, fmt.Errorf("telemetry: merge time %q: %w", merged, err)
		}
		if len(out) == 0 || out[len(out)-1].Issue != issue {
			tgt := TraitTarget{Repo: repo, Issue: issue}
			if first.Valid {
				f, err := time.Parse(time.RFC3339, first.String)
				if err != nil {
					return nil, fmt.Errorf("telemetry: review time %q: %w", first.String, err)
				}
				tgt.FirstReviewAt = &f
			}
			out = append(out, tgt)
		}
		tgt := &out[len(out)-1]
		tgt.PRs = append(tgt.PRs, pr)
		tgt.NeedsFill = tgt.NeedsFill || noLines || noTraits
		tgt.InWindow = tgt.InWindow || !at.Before(from)
		if at.After(tgt.LastMergedAt) {
			tgt.LastMergedAt = at
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("telemetry: trait targets: %w", err)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Issue < out[j].Issue })
	return out, nil
}

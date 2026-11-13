package dispatch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// RunView is the tick_state row of one open or parked run, as the UI reads it.
type RunView struct {
	Repo        string    `json:"repo"`
	Issue       int       `json:"issue"`
	Title       string    `json:"title"`
	State       string    `json:"state"` // "open" or "parked"
	Command     string    `json:"command"`
	Phase       string    `json:"phase"` // see Phase; "" for an empty command
	Task        int       `json:"task"`  // current plan task of a subagent-driven run; 0 without progress
	Tasks       int       `json:"tasks"` // number of tasks in that plan; 0 without progress
	Step        string    `json:"step"`  // "build", "review", "final-review"; "" without progress
	Trigger     string    `json:"trigger"`
	Profile     string    `json:"profile"`
	WorkspaceID string    `json:"workspace_id"`
	StartedAt   time.Time `json:"started_at"`
	Stand       string    `json:"stand"` // "working", "waiting", "done"; "" not measured
	At          time.Time `json:"at"`    // the tick that wrote it
}

// DeferredView is the tick_state row of one deferred start, as the UI reads it.
type DeferredView struct {
	Repo   string    `json:"repo"`
	Issue  int       `json:"issue"`
	Title  string    `json:"title"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// RunViews turns the open and parked runs of one repo into views, sorted by issue. An issue in
// both lists is shown once, as open. The phase comes from the command and the task progress from
// the plan and ledger of an sdd run, left zero when they cannot be read. The stand comes from pre.Stands and stays "" when the run
// was not measured; a done with work after the Stop in pre.Runs is working, as runCtx.done
// counts it. At stays zero for SaveRunView to set.
func RunViews(pre Pre, open, parked []runs.Brief) []RunView {
	titles := make(map[int]string, len(pre.Items))
	for _, it := range pre.Items {
		titles[it.Issue] = it.Title
	}
	out := make([]RunView, 0, len(open)+len(parked))
	seen := make(map[int]bool, len(open))
	add := func(b runs.Brief, state string) {
		v := RunView{
			Repo: b.Repo, Issue: b.Issue, Title: titles[b.Issue], State: state,
			Command: b.Command, Trigger: b.Trigger, Profile: b.Profile,
			WorkspaceID: b.WorkspaceID, StartedAt: b.StartedAt, Phase: Phase(b.Command),
		}
		if p, ok := ReadProgress(b.Command); ok {
			v.Task, v.Tasks, v.Step = p.Task, p.Tasks, p.Step
		}
		if stand, ok := pre.Stands[b.ID]; ok {
			if stand == workplace.Done && workAfterStop(pre.Runs[b.ID].Transcript) {
				stand = workplace.Working
			}
			v.Stand = stand.String()
		}
		out = append(out, v)
	}
	for _, b := range open {
		seen[b.Issue] = true
		add(b, "open")
	}
	for _, b := range parked {
		if !seen[b.Issue] {
			add(b, "parked")
		}
	}
	slices.SortStableFunc(out, func(a, b RunView) int { return a.Issue - b.Issue })
	return out
}

// SaveRunView replaces the run rows of repo in tick_state with views, in one transaction: key is
// the issue number, value is the view as JSON with At set to now in UTC.
func SaveRunView(ctx context.Context, db *sql.DB, repo string, views []RunView, now time.Time) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("dispatch: save run view %s: %w", repo, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, `DELETE FROM tick_state WHERE repo = ? AND kind = 'run'`, repo); err != nil {
		return fmt.Errorf("dispatch: save run view %s: %w", repo, err)
	}
	at := now.UTC()
	stamp := at.Format(time.RFC3339)
	for _, v := range views {
		v.Repo, v.At = repo, at
		value, merr := json.Marshal(v)
		if merr != nil {
			return fmt.Errorf("dispatch: save run view %s: %w", repo, merr)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tick_state (repo, kind, key, value, updated_at) VALUES (?, 'run', ?, ?, ?)
			ON CONFLICT (repo, kind, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			repo, strconv.Itoa(v.Issue), string(value), stamp); err != nil {
			return fmt.Errorf("dispatch: save run view %s: %w", repo, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("dispatch: save run view %s: %w", repo, err)
	}
	return nil
}

// LoadView reads the run and deferred rows of repos, in the order of repos and then by issue
// number. Both slices are non-nil. A value that does not parse gives an error naming repo and key.
func LoadView(ctx context.Context, db *sql.DB, repos []string) ([]RunView, []DeferredView, error) {
	runViews := []RunView{}
	deferred := []DeferredView{}
	for _, repo := range repos {
		rows, err := db.QueryContext(ctx, `SELECT kind, key, value FROM tick_state WHERE repo = ? AND kind IN ('run', 'deferred')`, repo)
		if err != nil {
			return nil, nil, fmt.Errorf("dispatch: load view %s: %w", repo, err)
		}
		var rv []RunView
		var dv []DeferredView
		for rows.Next() {
			var kind, key, value string
			if err := rows.Scan(&kind, &key, &value); err != nil {
				_ = rows.Close()
				return nil, nil, fmt.Errorf("dispatch: load view %s: %w", repo, err)
			}
			dec := json.NewDecoder(strings.NewReader(value))
			var derr error
			if kind == "run" {
				var v RunView
				if derr = dec.Decode(&v); derr == nil {
					v.Repo = repo
					rv = append(rv, v)
				}
			} else {
				var v DeferredView
				if derr = dec.Decode(&v); derr == nil {
					v.Repo = repo
					dv = append(dv, v)
				}
			}
			if derr != nil {
				_ = rows.Close()
				return nil, nil, fmt.Errorf("dispatch: load view %s: %s row %q: %w", repo, kind, key, derr)
			}
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("dispatch: load view %s: %w", repo, err)
		}
		slices.SortFunc(rv, func(a, b RunView) int { return a.Issue - b.Issue })
		slices.SortFunc(dv, func(a, b DeferredView) int { return a.Issue - b.Issue })
		runViews = append(runViews, rv...)
		deferred = append(deferred, dv...)
	}
	return runViews, deferred, nil
}

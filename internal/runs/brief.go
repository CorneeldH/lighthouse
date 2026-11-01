package runs

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Brief is the short row of a run: what the run view needs, without the
// prompt, session or branch columns.
type Brief struct {
	ID          int64
	Repo        string
	Issue       int
	Command     string
	Trigger     string
	Profile     string
	WorkspaceID string
	StartedAt   time.Time
}

// briefColumns are the runs columns scanBrief reads, in order.
const briefColumns = `id, repo, issue, command, trigger, profile, workspace_id, started_at`

func scanBrief(rows interface{ Scan(...any) error }) (Brief, error) {
	var b Brief
	var started string
	if err := rows.Scan(&b.ID, &b.Repo, &b.Issue, &b.Command, &b.Trigger, &b.Profile, &b.WorkspaceID, &started); err != nil {
		return Brief{}, fmt.Errorf("runs: scan brief: %w", err)
	}
	var err error
	b.StartedAt, err = time.Parse(time.RFC3339, started)
	if err != nil {
		return Brief{}, fmt.Errorf("runs: run %d started_at %q: %w", b.ID, started, err)
	}
	return b, nil
}

func queryBriefs(ctx context.Context, db *sql.DB, query string, args ...any) ([]Brief, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("runs: briefs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Brief
	for rows.Next() {
		b, err := scanBrief(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runs: briefs: %w", err)
	}
	return out, nil
}

// OpenBriefs returns the open runs of repo as short rows, ordered by id.
func OpenBriefs(ctx context.Context, db *sql.DB, repo string) ([]Brief, error) {
	return queryBriefs(ctx, db, `SELECT `+briefColumns+`
		FROM runs WHERE repo = ? AND ended_at IS NULL ORDER BY id`, repo)
}

// BriefsByID returns the runs with the given ids, open or closed, in the
// order of ids. An unknown id is skipped.
func BriefsByID(ctx context.Context, db *sql.DB, ids []int64) ([]Brief, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	found, err := queryBriefs(ctx, db, `SELECT `+briefColumns+` FROM runs WHERE id IN (`+marks+`)`, args...)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]Brief, len(found))
	for _, b := range found {
		byID[b.ID] = b
	}
	out := make([]Brief, 0, len(found))
	for _, id := range ids {
		if b, ok := byID[id]; ok {
			out = append(out, b)
		}
	}
	return out, nil
}

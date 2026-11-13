package runs

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// WorkspaceRef is a workspace a run of repo used and that is not marked removed.
type WorkspaceRef struct {
	Issue       int
	Node        string // "" is self
	WorkspaceID string
	Branch      string
}

// Workspaces returns one ref per (node, workspace id) of repo that no row marks removed. Issue and Branch
// come from the row with the highest id. Ordered by issue, then workspace id; never nil.
func Workspaces(ctx context.Context, db *sql.DB, repo string) ([]WorkspaceRef, error) {
	rows, err := db.QueryContext(ctx, `SELECT r.issue, r.node, r.workspace_id, r.branch FROM runs r
		JOIN (SELECT node, workspace_id, MAX(id) AS id FROM runs
			WHERE repo = ? AND workspace_id != ''
			GROUP BY node, workspace_id
			HAVING count(workspace_removed_at) = 0) last ON last.id = r.id
		ORDER BY r.issue, r.node, r.workspace_id`, repo)
	if err != nil {
		return nil, fmt.Errorf("runs: workspaces: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []WorkspaceRef{}
	for rows.Next() {
		var w WorkspaceRef
		if err := rows.Scan(&w.Issue, &w.Node, &w.WorkspaceID, &w.Branch); err != nil {
			return nil, fmt.Errorf("runs: workspaces: %w", err)
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runs: workspaces: %w", err)
	}
	return out, nil
}

// MarkWorkspaceRemoved marks every row of repo on node with workspaceID as removed at now; a row that
// already has a mark keeps it.
func MarkWorkspaceRemoved(ctx context.Context, db *sql.DB, repo, node, workspaceID string, now time.Time) error {
	if _, err := db.ExecContext(ctx, `UPDATE runs SET workspace_removed_at = ?
		WHERE repo = ? AND node = ? AND workspace_id = ? AND workspace_removed_at IS NULL`, stamp(now), repo, node, workspaceID); err != nil {
		return fmt.Errorf("runs: mark workspace %s removed: %w", workspaceID, err)
	}
	return nil
}

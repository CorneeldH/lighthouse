package workplace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

const leftoverKind = "cleanup"

// Leftover is a workspace that cleanup could not remove. It lives in
// tick_state as kind 'cleanup', keyed by (repo, workspace id).
type Leftover struct {
	Repo        string `json:"repo"`
	Issue       int    `json:"issue"`
	WorkspaceID string `json:"workspace_id"`
	Path        string `json:"path"`
	Reason      string `json:"reason"`
}

type leftoverValue struct {
	Issue  int    `json:"issue"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// RecordLeftover stores l, replacing an earlier record of the same workspace.
func RecordLeftover(ctx context.Context, db *sql.DB, l Leftover, now time.Time) error {
	value, err := json.Marshal(leftoverValue{Issue: l.Issue, Path: l.Path, Reason: l.Reason})
	if err != nil {
		return fmt.Errorf("encode leftover %s: %w", l.WorkspaceID, err)
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO tick_state (repo, kind, key, value, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (repo, kind, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		l.Repo, leftoverKind, l.WorkspaceID, string(value), now.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("record leftover %s: %w", l.WorkspaceID, err)
	}
	return nil
}

// ClearLeftover removes the record of a workspace; an absent record is not an error.
func ClearLeftover(ctx context.Context, db *sql.DB, repo, workspaceID string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM tick_state WHERE repo = ? AND kind = ? AND key = ?`,
		repo, leftoverKind, workspaceID)
	if err != nil {
		return fmt.Errorf("clear leftover %s: %w", workspaceID, err)
	}
	return nil
}

// Leftovers lists the records of all repos, sorted by repo, issue and
// workspace id. The slice is never nil.
func Leftovers(ctx context.Context, db *sql.DB) ([]Leftover, error) {
	rows, err := db.QueryContext(ctx, `SELECT repo, key, value FROM tick_state WHERE kind = ?`, leftoverKind)
	if err != nil {
		return nil, fmt.Errorf("list leftovers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	list := []Leftover{}
	for rows.Next() {
		var l Leftover
		var raw string
		if err := rows.Scan(&l.Repo, &l.WorkspaceID, &raw); err != nil {
			return nil, fmt.Errorf("scan leftover: %w", err)
		}
		var v leftoverValue
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, fmt.Errorf("decode leftover %s: %w", l.WorkspaceID, err)
		}
		l.Issue, l.Path, l.Reason = v.Issue, v.Path, v.Reason
		list = append(list, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list leftovers: %w", err)
	}
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		if a.Issue != b.Issue {
			return a.Issue < b.Issue
		}
		return a.WorkspaceID < b.WorkspaceID
	})
	return list, nil
}

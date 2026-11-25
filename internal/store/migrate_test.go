package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

type schemaRow struct{ Type, Name, SQL string }

func snapshot(t *testing.T, db *sql.DB) ([]schemaRow, int) {
	t.Helper()
	rows, err := db.Query(`SELECT type, name, coalesce(sql, '') FROM sqlite_master ORDER BY name`)
	if err != nil {
		t.Fatalf("read sqlite_master: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []schemaRow
	for rows.Next() {
		var r schemaRow
		if err := rows.Scan(&r.Type, &r.Name, &r.SQL); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out, userVersion(t, db)
}

func userVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	return v
}

func bareDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dsn(filepath.Join(t.TempDir(), "bare.db")))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrateTwice(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "lighthouse.db")

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	schema1, v1 := snapshot(t, s.DB)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	schema2, v2 := snapshot(t, s.DB)

	if !reflect.DeepEqual(schema1, schema2) || v1 != v2 {
		t.Errorf("schema changed on second open:\n%v (v%d)\n%v (v%d)", schema1, v1, schema2, v2)
	}
	if v2 != 14 {
		t.Errorf("user_version = %d, want 14", v2)
	}
	rows, err := s.DB.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		tables = append(tables, name)
	}
	_ = rows.Close()
	if want := []string{"action_failures", "findings", "meta", "recoveries", "recovery_items", "review_runs", "runs", "schedule_slots", "sessions", "setups", "tick_state", "trajectories", "trajectory_events", "usage"}; !reflect.DeepEqual(tables, want) {
		t.Errorf("tables = %v, want %v", tables, want)
	}
	// column -> primary key position (0 = not part of the key)
	wantCols := map[string]map[string]int{
		"runs":            {"repo": 0, "issue": 0, "trigger_key": 0, "attempt": 0, "ended_at": 0, "outcome": 0},
		"action_failures": {"action_key": 1},
		"tick_state":      {"repo": 1, "kind": 2, "key": 3},
		"schedule_slots":  {"repo": 1, "schedule": 2, "slot": 3, "outcome": 0, "reason": 0, "at": 0},
	}
	for table, want := range wantCols {
		cols := map[string]int{}
		info, err := s.DB.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
		if err != nil {
			t.Fatalf("table_info %s: %v", table, err)
		}
		for info.Next() {
			var cid, notnull, pk int
			var name, typ string
			var dflt sql.NullString
			if err := info.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
				t.Fatalf("scan table_info: %v", err)
			}
			cols[name] = pk
		}
		_ = info.Close()
		for col, pk := range want {
			if got, ok := cols[col]; !ok || got != pk {
				t.Errorf("%s.%s: present = %v, pk = %d; want pk %d", table, col, ok, got, pk)
			}
		}
	}
	var n int
	if err := s.DB.QueryRow(`SELECT count(*) FROM meta WHERE key='created_at'`).Scan(&n); err != nil {
		t.Fatalf("count created_at: %v", err)
	}
	if n != 1 {
		t.Errorf("created_at rows = %d, want 1", n)
	}
}

func TestMigrationNumbering(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		wantErr string // empty: success
		wantVer int
	}{
		{"consecutive", []string{"0001_a.sql", "0002_b.sql"}, "", 2},
		{"gap", []string{"0001_a.sql", "0003_c.sql"}, "0002", 0},
		{"duplicate", []string{"0001_a.sql", "0001_b.sql"}, "0001", 0},
		{"short number", []string{"1_a.sql"}, "1_a.sql", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{}
			for _, f := range tc.files {
				fsys[f] = &fstest.MapFile{Data: []byte("SELECT 1;")}
			}
			db := bareDB(t)
			err := migrate(context.Background(), db, fsys)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("migrate: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("migrate error = %v, want it to contain %q", err, tc.wantErr)
			}
			if v := userVersion(t, db); v != tc.wantVer {
				t.Errorf("user_version = %d, want %d", v, tc.wantVer)
			}
		})
	}
}

func TestMigrationFailsAtomically(t *testing.T) {
	db := bareDB(t)
	fsys := fstest.MapFS{"0001_a.sql": {Data: []byte("CREATE TABLE a(x); CREATE TABLE a(x);")}}
	if err := migrate(context.Background(), db, fsys); err == nil {
		t.Fatal("migrate succeeded on a failing file")
	}
	if v := userVersion(t, db); v != 0 {
		t.Errorf("user_version = %d, want 0", v)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='a'`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 0 {
		t.Errorf("table a exists after failed migration")
	}
}

func TestSchemaTooNew(t *testing.T) {
	db := bareDB(t)
	if _, err := db.Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	err := migrate(context.Background(), db, migrationFS)
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("migrate error = %v, want ErrSchemaTooNew", err)
	}
	if v := userVersion(t, db); v != 99 {
		t.Errorf("user_version = %d, want 99", v)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 0 {
		t.Errorf("database touched: %d schema objects", n)
	}
}

func TestOpenSharedSchemaTooNew(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l.db")
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	s, err := OpenShared(context.Background(), path)
	if err == nil {
		_ = s.Close()
	}
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("OpenShared error = %v, want ErrSchemaTooNew", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 0 {
		t.Errorf("database touched: %d schema objects", n)
	}
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode == "wal" {
		t.Errorf("journal_mode = wal: a refused database was changed")
	}
}

// TestApplyRereadsUserVersion covers the state another process leaves
// between migrate's first read and apply's transaction.
func TestApplyRereadsUserVersion(t *testing.T) {
	m := migration{number: 1, file: "0001_a.sql"}
	fsys := fstest.MapFS{m.file: {Data: []byte("CREATE TABLE a(x);")}}
	tests := []struct {
		name    string
		version int
		tooNew  bool
	}{
		{"already migrated", 1, false},
		{"newer than known", 2, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := bareDB(t)
			if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", tc.version)); err != nil {
				t.Fatalf("set user_version: %v", err)
			}
			err := apply(context.Background(), db, fsys, m, 1)
			if tc.tooNew != errors.Is(err, ErrSchemaTooNew) || (!tc.tooNew && err != nil) {
				t.Fatalf("apply error = %v, want ErrSchemaTooNew: %v", err, tc.tooNew)
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='a'`).Scan(&n); err != nil {
				t.Fatalf("query: %v", err)
			}
			if n != 0 {
				t.Errorf("migration ran although user_version was %d", tc.version)
			}
			if v := userVersion(t, db); v != tc.version {
				t.Errorf("user_version = %d, want %d", v, tc.version)
			}
		})
	}
}

func TestEmbeddedMigrationsNumbered(t *testing.T) {
	if _, err := loadMigrations(migrationFS); err != nil {
		t.Fatalf("embedded migrations: %v", err)
	}
}

func TestNegativeUserVersion(t *testing.T) {
	db := bareDB(t)
	if _, err := db.Exec(`PRAGMA user_version = -1`); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	err := migrate(context.Background(), db, migrationFS)
	if err == nil || !strings.Contains(err.Error(), "-1") {
		t.Fatalf("migrate error = %v, want an error naming -1", err)
	}
	if v := userVersion(t, db); v != -1 {
		t.Errorf("user_version = %d, want -1", v)
	}
}

// embeddedUpTo is the embedded migration set up to and including file.
func embeddedUpTo(t *testing.T, files ...string) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	for _, f := range files {
		body, err := fs.ReadFile(migrationFS, f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		out[f] = &fstest.MapFile{Data: body}
	}
	return out
}

func TestMigrate0003KeepsTickState(t *testing.T) {
	ctx := context.Background()
	db := bareDB(t)
	if err := migrate(ctx, db, embeddedUpTo(t, "0001_meta.sql", "0002_runs.sql")); err != nil {
		t.Fatalf("migrate to 2: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO tick_state(repo, kind, key, value, updated_at) VALUES ('o/r', 'fingerprint', 'k', 'v', 't')`); err != nil {
		t.Fatalf("insert at v2: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO tick_state(repo, kind, key, value, updated_at) VALUES ('o/r', 'cleanup', 'w', '{}', 't')`); err == nil {
		t.Fatal("version 2 accepted kind cleanup")
	}
	if err := migrate(ctx, db, embeddedUpTo(t, "0001_meta.sql", "0002_runs.sql", "0003_cleanup.sql")); err != nil {
		t.Fatalf("migrate to 3: %v", err)
	}
	if v := userVersion(t, db); v != 3 {
		t.Errorf("user_version = %d, want 3", v)
	}
	var value string
	if err := db.QueryRow(`SELECT value FROM tick_state WHERE repo = 'o/r' AND kind = 'fingerprint' AND key = 'k'`).Scan(&value); err != nil || value != "v" {
		t.Errorf("fingerprint row after migration = %q, %v; want \"v\"", value, err)
	}
	if _, err := db.Exec(`INSERT INTO tick_state(repo, kind, key, value, updated_at) VALUES ('o/r', 'cleanup', 'w', '{}', 't')`); err != nil {
		t.Errorf("cleanup rejected after migration: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO tick_state(repo, kind, key, value, updated_at) VALUES ('o/r', 'bogus', 'w', '{}', 't')`); err == nil {
		t.Error("unknown kind accepted after migration")
	}
}

// 0004 rebuilds runs to accept trigger fixpr: every row from before keeps its id and columns,
// the checks and the open-run index still hold.
func TestMigrate0004KeepsRuns(t *testing.T) {
	ctx := context.Background()
	db := bareDB(t)
	if err := migrate(ctx, db, embeddedUpTo(t, "0001_meta.sql", "0002_runs.sql", "0003_cleanup.sql")); err != nil {
		t.Fatalf("migrate to 3: %v", err)
	}
	insert := `INSERT INTO runs (id, repo, issue, session_id, mode, trigger, trigger_key, attempt, command,
		branch, workspace_id, workplace_session, started_at, ended_at, outcome, reason)
		VALUES (?, 'o/r', ?, 's', 'start', ?, ?, 1, 'c', 'b', 'w', 'k', 't0', ?, ?, ?)`
	if _, err := db.Exec(insert, 41, 7, "handoff", "start:7", "t1", "done", "why"); err != nil {
		t.Fatalf("insert closed row at v3: %v", err)
	}
	if _, err := db.Exec(insert, 42, 7, "answer", "answer:9", nil, nil, ""); err != nil {
		t.Fatalf("insert open row at v3: %v", err)
	}
	if _, err := db.Exec(insert, 43, 8, "fixpr", "fixpr:12:abc", nil, nil, ""); err == nil {
		t.Fatal("version 3 accepted trigger fixpr")
	}
	if err := migrate(ctx, db, embeddedUpTo(t, "0001_meta.sql", "0002_runs.sql", "0003_cleanup.sql", "0004_runs_fixpr.sql")); err != nil {
		t.Fatalf("migrate to 4: %v", err)
	}
	if v := userVersion(t, db); v != 4 {
		t.Errorf("user_version = %d, want 4", v)
	}
	type row struct {
		ID                                                   int
		Trigger, Key, Workspace, Session, Ended, Out, Reason string
	}
	rows, err := db.Query(`SELECT id, trigger, trigger_key, workspace_id, workplace_session,
		coalesce(ended_at, ''), coalesce(outcome, ''), reason FROM runs ORDER BY id`)
	if err != nil {
		t.Fatalf("query runs: %v", err)
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.Trigger, &r.Key, &r.Workspace, &r.Session, &r.Ended, &r.Out, &r.Reason); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	_ = rows.Close()
	want := []row{
		{41, "handoff", "start:7", "w", "k", "t1", "done", "why"},
		{42, "answer", "answer:9", "w", "k", "", "", ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runs after migration = %+v, want %+v", got, want)
	}
	if _, err := db.Exec(insert, 43, 8, "fixpr", "fixpr:12:abc", nil, nil, ""); err != nil {
		t.Errorf("trigger fixpr rejected after migration: %v", err)
	}
	if _, err := db.Exec(insert, 44, 9, "bogus", "x:1", nil, nil, ""); err == nil {
		t.Error("unknown trigger accepted after migration")
	}
	if _, err := db.Exec(insert, 45, 7, "handoff", "handoff:3", nil, nil, ""); err == nil {
		t.Error("second open run of issue 7 accepted: runs_open_issue is gone")
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'runs_trigger'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("runs_trigger index count = %d, %v; want 1", n, err)
	}
}

// 0005 adds workspace_removed_at to runs: every row from before keeps its columns and has no mark.
func TestMigrate0005KeepsRuns(t *testing.T) {
	ctx := context.Background()
	db := bareDB(t)
	if err := migrate(ctx, db, embeddedUpTo(t, "0001_meta.sql", "0002_runs.sql", "0003_cleanup.sql", "0004_runs_fixpr.sql")); err != nil {
		t.Fatalf("migrate to 4: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO runs (id, repo, issue, session_id, mode, trigger, trigger_key, attempt, command,
		branch, workspace_id, workplace_session, started_at, ended_at, outcome, reason)
		VALUES (41, 'o/r', 7, 's', 'start', 'fixpr', 'fixpr:7:abc', 1, 'c', 'b', 'w', 'k', 't0', 't1', 'done', 'why')`); err != nil {
		t.Fatalf("insert row at v4: %v", err)
	}
	if err := migrate(ctx, db, embeddedUpTo(t, "0001_meta.sql", "0002_runs.sql", "0003_cleanup.sql", "0004_runs_fixpr.sql", "0005_workspace_removed.sql")); err != nil {
		t.Fatalf("migrate to 5: %v", err)
	}
	if v := userVersion(t, db); v != 5 {
		t.Errorf("user_version = %d, want 5", v)
	}
	var trigger, key, branch, workspace, session, ended, outcome, reason string
	var removed sql.NullString
	err := db.QueryRow(`SELECT trigger, trigger_key, branch, workspace_id, workplace_session, ended_at, outcome, reason,
		workspace_removed_at FROM runs WHERE id = 41`).Scan(&trigger, &key, &branch, &workspace, &session, &ended, &outcome, &reason, &removed)
	if err != nil {
		t.Fatalf("read row after migration: %v", err)
	}
	got := []string{trigger, key, branch, workspace, session, ended, outcome, reason}
	if want := []string{"fixpr", "fixpr:7:abc", "b", "w", "k", "t1", "done", "why"}; !reflect.DeepEqual(got, want) {
		t.Errorf("row after migration = %q, want %q", got, want)
	}
	if removed.Valid {
		t.Errorf("workspace_removed_at = %q, want NULL", removed.String)
	}
}

// 0006 adds merge_sha and merge_error to runs: every row from before keeps its columns and has empty notes.
func TestMigrate0006KeepsRuns(t *testing.T) {
	ctx := context.Background()
	db := bareDB(t)
	upTo5 := []string{"0001_meta.sql", "0002_runs.sql", "0003_cleanup.sql", "0004_runs_fixpr.sql", "0005_workspace_removed.sql"}
	if err := migrate(ctx, db, embeddedUpTo(t, upTo5...)); err != nil {
		t.Fatalf("migrate to 5: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO runs (id, repo, issue, session_id, mode, trigger, trigger_key, attempt, command,
		branch, workspace_id, workplace_session, started_at, ended_at, outcome, reason)
		VALUES (41, 'o/r', 7, 's', 'start', 'fixpr', 'fixpr:7:abc', 1, 'c', 'b', 'w', 'k', 't0', 't1', 'done', 'why')`); err != nil {
		t.Fatalf("insert row at v5: %v", err)
	}
	if err := migrate(ctx, db, embeddedUpTo(t, append(upTo5, "0006_runs_merge.sql")...)); err != nil {
		t.Fatalf("migrate to 6: %v", err)
	}
	if v := userVersion(t, db); v != 6 {
		t.Errorf("user_version = %d, want 6", v)
	}
	var trigger, key, branch, workspace, session, ended, outcome, reason, mergeSHA, mergeError string
	err := db.QueryRow(`SELECT trigger, trigger_key, branch, workspace_id, workplace_session, ended_at, outcome, reason,
		merge_sha, merge_error FROM runs WHERE id = 41`).Scan(&trigger, &key, &branch, &workspace, &session, &ended, &outcome, &reason, &mergeSHA, &mergeError)
	if err != nil {
		t.Fatalf("read row after migration: %v", err)
	}
	got := []string{trigger, key, branch, workspace, session, ended, outcome, reason, mergeSHA, mergeError}
	if want := []string{"fixpr", "fixpr:7:abc", "b", "w", "k", "t1", "done", "why", "", ""}; !reflect.DeepEqual(got, want) {
		t.Errorf("row after migration = %q, want %q", got, want)
	}
}

// A run from before 0007 reads node "": that is self.
func TestMigrationNodeDefault(t *testing.T) {
	ctx := context.Background()
	db := bareDB(t)
	upTo6 := []string{"0001_meta.sql", "0002_runs.sql", "0003_cleanup.sql", "0004_runs_fixpr.sql", "0005_workspace_removed.sql", "0006_runs_merge.sql"}
	if err := migrate(ctx, db, embeddedUpTo(t, upTo6...)); err != nil {
		t.Fatalf("migrate to 6: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO runs (repo, issue, session_id, mode, trigger, trigger_key, attempt, command, started_at)
		VALUES ('o/r', 7, 's', 'start', 'handoff', 'start:7', 1, 'c', 't0')`); err != nil {
		t.Fatalf("insert at v6: %v", err)
	}
	if err := migrate(ctx, db, embeddedUpTo(t, append(upTo6, "0007_runs_node.sql")...)); err != nil {
		t.Fatalf("migrate to 7: %v", err)
	}
	var node string
	if err := db.QueryRow(`SELECT node FROM runs WHERE issue = 7`).Scan(&node); err != nil {
		t.Fatalf("read node: %v", err)
	}
	if node != "" {
		t.Errorf("node = %q, want the empty string (self)", node)
	}
}

// upTo7 is the embedded migration set before 0008.
var upTo7 = []string{"0001_meta.sql", "0002_runs.sql", "0003_cleanup.sql", "0004_runs_fixpr.sql",
	"0005_workspace_removed.sql", "0006_runs_merge.sql", "0007_runs_node.sql"}

// 0008 rebuilds runs for scheduled runs: every row from before keeps its id and all its columns
// (those of 0005-0007 included), has no schedule and empty scheduled columns, and the open
// issue run still holds its claim.
func TestMigrate0008KeepsRuns(t *testing.T) {
	ctx := context.Background()
	db := bareDB(t)
	if err := migrate(ctx, db, embeddedUpTo(t, upTo7...)); err != nil {
		t.Fatalf("migrate to 7: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO runs (id, repo, issue, session_id, mode, trigger, trigger_key, attempt, command, prompt,
		branch, workspace_id, workplace_session, profile, started_at, ended_at, outcome, reason,
		workspace_removed_at, merge_sha, merge_error, node)
		VALUES (41, 'o/r', 7, 's1', 'start', 'handoff', 'start:7', 2, 'c', 'p', 'b', 'w', 'k', 'software',
		't0', 't1', 'done', 'why', 't2', 'abc', 'refused', 'n1')`); err != nil {
		t.Fatalf("insert closed row at v7: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO runs (id, repo, issue, session_id, mode, trigger, trigger_key, attempt, command, started_at)
		VALUES (42, 'o/r', 8, 's2', 'resume', 'fixpr', 'fixpr:8:def', 1, 'c2', 't3')`); err != nil {
		t.Fatalf("insert open row at v7: %v", err)
	}
	if err := migrate(ctx, db, embeddedUpTo(t, append(upTo7, "0008_runs_schedule.sql")...)); err != nil {
		t.Fatalf("migrate to 8: %v", err)
	}
	if v := userVersion(t, db); v != 8 {
		t.Errorf("user_version = %d, want 8", v)
	}
	rows, err := db.Query(`SELECT id, repo, issue, session_id, mode, trigger, trigger_key, attempt, command, prompt,
		branch, workspace_id, workplace_session, profile, started_at, coalesce(ended_at, '-'), coalesce(outcome, '-'), reason,
		coalesce(workspace_removed_at, '-'), merge_sha, merge_error, node,
		coalesce(schedule, '-'), start_sha, verdict, coalesce(pr_at, '-') FROM runs ORDER BY id`)
	if err != nil {
		t.Fatalf("query runs: %v", err)
	}
	var got [][]string
	for rows.Next() {
		r := make([]string, 26)
		dest := make([]any, len(r))
		for i := range r {
			dest[i] = &r[i]
		}
		if err := rows.Scan(dest...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	_ = rows.Close()
	want := [][]string{
		{"41", "o/r", "7", "s1", "start", "handoff", "start:7", "2", "c", "p", "b", "w", "k", "software",
			"t0", "t1", "done", "why", "t2", "abc", "refused", "n1", "-", "", "", "-"},
		{"42", "o/r", "8", "s2", "resume", "fixpr", "fixpr:8:def", "1", "c2", "", "", "", "", "",
			"t3", "-", "-", "", "-", "", "", "", "-", "", "", "-"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runs after migration =\n%q\nwant\n%q", got, want)
	}
	if _, err := db.Exec(`INSERT INTO runs (repo, issue, session_id, mode, trigger, trigger_key, attempt, command, started_at)
		VALUES ('o/r', 8, 's3', 'start', 'handoff', 'handoff:1', 1, 'c', 't4')`); err == nil {
		t.Error("second open run of issue 8 accepted: runs_open_issue is gone")
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'runs_trigger'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("runs_trigger index count = %d, %v; want 1", n, err)
	}
}

// After 0008, a run names exactly one of issue and schedule, a schedule has at most one open run,
// and only the new trigger and outcomes are added.
func TestMigrate0008Checks(t *testing.T) {
	ctx := context.Background()
	db := bareDB(t)
	if err := migrate(ctx, db, embeddedUpTo(t, append(upTo7, "0008_runs_schedule.sql")...)); err != nil {
		t.Fatalf("migrate to 8: %v", err)
	}
	insert := `INSERT INTO runs (repo, issue, schedule, session_id, mode, trigger, trigger_key, attempt, command, started_at,
		ended_at, outcome) VALUES ('o/r', ?, ?, 's', 'start', ?, ?, 1, 'c', 't0', ?, ?)`
	tests := []struct {
		name     string
		issue    any
		schedule any
		trigger  string
		key      string
		ended    any
		outcome  any
		ok       bool
	}{
		{"scheduled open", nil, "reflect", "scheduled", "scheduled:reflect:a", nil, nil, true},
		{"second open of the schedule", nil, "reflect", "scheduled", "scheduled:reflect:b", nil, nil, false},
		{"open of another schedule", nil, "map-refresh", "scheduled", "scheduled:map-refresh:a", nil, nil, true},
		{"closed of the schedule", nil, "reflect", "scheduled", "scheduled:reflect:c", "t1", "retry", true},
		{"outcome failed", nil, "reflect", "scheduled", "scheduled:reflect:d", "t1", "failed", true},
		{"neither issue nor schedule", nil, nil, "scheduled", "scheduled:x:a", "t1", "done", false},
		{"both issue and schedule", 7, "reflect", "scheduled", "scheduled:reflect:e", "t1", "done", false},
		{"issue run", 7, nil, "handoff", "start:7", nil, nil, true},
		{"unknown outcome", nil, "reflect", "scheduled", "scheduled:reflect:f", "t1", "bogus", false},
		{"unknown trigger", 9, nil, "bogus", "x:9", nil, nil, false},
	}
	for _, tc := range tests {
		_, err := db.Exec(insert, tc.issue, tc.schedule, tc.trigger, tc.key, tc.ended, tc.outcome)
		if (err == nil) != tc.ok {
			t.Errorf("%s: insert error = %v, want accepted: %v", tc.name, err, tc.ok)
		}
	}
	slot := `INSERT INTO schedule_slots (repo, schedule, slot, outcome, reason, at) VALUES ('o/r', 'reflect', ?, ?, 'r', 't0')`
	if _, err := db.Exec(slot, "20261116T0300", "skipped"); err != nil {
		t.Errorf("skipped slot rejected: %v", err)
	}
	if _, err := db.Exec(slot, "20261116T0300", "skipped"); err == nil {
		t.Error("same slot accepted twice: no primary key")
	}
	if _, err := db.Exec(slot, "20261117T0300", "done"); err == nil {
		t.Error("slot outcome done accepted")
	}
}

// A run from before 0009 reads pushed_sha "": the push phase of no run is over.
func TestMigrationPushedDefault(t *testing.T) {
	ctx := context.Background()
	db := bareDB(t)
	upTo8 := append(append([]string{}, upTo7...), "0008_runs_schedule.sql")
	if err := migrate(ctx, db, embeddedUpTo(t, upTo8...)); err != nil {
		t.Fatalf("migrate to 8: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO runs (repo, issue, session_id, mode, trigger, trigger_key, attempt, command, started_at)
		VALUES ('o/r', 7, 's', 'start', 'handoff', 'start:7', 1, 'c', 't0')`); err != nil {
		t.Fatalf("insert at v8: %v", err)
	}
	if err := migrate(ctx, db, embeddedUpTo(t, append(upTo8, "0009_runs_pushed.sql")...)); err != nil {
		t.Fatalf("migrate to 9: %v", err)
	}
	if v := userVersion(t, db); v != 9 {
		t.Fatalf("user_version = %d, want 9", v)
	}
	var pushed string
	if err := db.QueryRow(`SELECT pushed_sha FROM runs WHERE issue = 7`).Scan(&pushed); err != nil {
		t.Fatalf("read pushed_sha: %v", err)
	}
	if pushed != "" {
		t.Errorf("pushed_sha = %q, want the empty string", pushed)
	}
}

var upTo9 = append(append([]string{}, upTo7...), "0008_runs_schedule.sql", "0009_runs_pushed.sql")

// 0010 links every existing run with an issue to one trajectory per (repo, issue), keeps every
// run row, and leaves a scheduled run without a trajectory.
func TestMigrate0010KeepsRuns(t *testing.T) {
	ctx := context.Background()
	db := bareDB(t)
	if err := migrate(ctx, db, embeddedUpTo(t, upTo9...)); err != nil {
		t.Fatalf("migrate to 9: %v", err)
	}
	for _, q := range []string{
		`INSERT INTO runs (id, repo, issue, session_id, mode, trigger, trigger_key, attempt, command, started_at, ended_at, outcome)
			VALUES (41, 'o/r', 7, 's1', 'start', 'handoff', 'start:7', 1, 'c', '2026-01-02T00:00:00Z', '2026-01-02T01:00:00Z', 'handoff')`,
		`INSERT INTO runs (id, repo, issue, session_id, mode, trigger, trigger_key, attempt, command, started_at)
			VALUES (42, 'o/r', 7, 's2', 'resume', 'answer', 'answer:7:x', 1, 'c', '2026-01-01T00:00:00Z')`,
		`INSERT INTO runs (id, repo, schedule, session_id, mode, trigger, trigger_key, attempt, command, started_at)
			VALUES (43, 'o/r', 'nightly', 's3', 'start', 'scheduled', 'sched:1', 1, 'c', '2026-01-03T00:00:00Z')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("insert at v9: %v", err)
		}
	}
	if err := migrate(ctx, db, embeddedUpTo(t, append(append([]string{}, upTo9...), "0010_telemetry.sql")...)); err != nil {
		t.Fatalf("migrate to 10: %v", err)
	}
	rows, err := db.Query(`SELECT r.id, r.trigger_key, r.session_id, coalesce(t.repo, '-'), coalesce(t.issue, 0),
		coalesce(t.created_at, '-'), r.assignment, r.trajectory_id IS NULL FROM runs r
		LEFT JOIN trajectories t ON t.id = r.trajectory_id ORDER BY r.id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var got [][]string
	for rows.Next() {
		r := make([]string, 8)
		dest := make([]any, len(r))
		for i := range r {
			dest[i] = &r[i]
		}
		if err := rows.Scan(dest...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	want := [][]string{
		{"41", "start:7", "s1", "o/r", "7", "2026-01-01T00:00:00Z", "standard", "0"},
		{"42", "answer:7:x", "s2", "o/r", "7", "2026-01-01T00:00:00Z", "standard", "0"},
		{"43", "sched:1", "s3", "-", "0", "-", "standard", "1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runs after 0010 =\n%q\nwant\n%q", got, want)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM trajectories`).Scan(&n); err != nil || n != 1 {
		t.Errorf("trajectories = %d, %v; want 1", n, err)
	}
}

const telemetryRun = `INSERT INTO runs (id, repo, issue, session_id, mode, trigger, trigger_key, attempt, command, started_at)
	VALUES (1, 'o/r', 7, 's', 'start', 'handoff', 'start:7', 1, 'c', 't0')`

func openTelemetryDB(t *testing.T) *sql.DB {
	t.Helper()
	s, err := OpenShared(context.Background(), filepath.Join(t.TempDir(), "lighthouse.db"))
	if err != nil {
		t.Fatalf("OpenShared: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.DB.Exec(`INSERT INTO trajectories (id, repo, issue, created_at) VALUES (1, 'o/r', 7, 't0')`); err != nil {
		t.Fatalf("insert trajectory: %v", err)
	}
	if _, err := s.DB.Exec(telemetryRun); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	return s.DB
}

func TestTelemetryConstraints(t *testing.T) {
	const sess = `INSERT INTO sessions (source, source_id, recorded_at, repo, harness, session_key, transcript_path, started_at,
		trajectory_id, link_basis, unlinked_reason, assignment) VALUES ('w', 'k%d', 'r', 'o/r', 'claude-code', 'k%d', '/p', 't0', %s)`
	cases := []struct {
		name string
		sql  string
		ok   bool
	}{
		{"runs assignment foo", `UPDATE runs SET assignment = 'foo'`, false},
		{"runs assignment shadow", `UPDATE runs SET assignment = 'shadow:plan_review'`, true},
		{"runs assignment variant", `UPDATE runs SET assignment = 'variant:model=opus'`, true},
		{"runs assignment bare shadow", `UPDATE runs SET assignment = 'shadow:'`, false},
		{"sessions assignment foo", fmt.Sprintf(sess, 1, 1, `1, 'branch', NULL, 'foo'`), false},
		{"sessions assignment variant", fmt.Sprintf(sess, 2, 2, `1, 'branch', NULL, 'variant:model=opus'`), true},
		{"sessions linked", fmt.Sprintf(sess, 3, 3, `1, 'transcript', NULL, 'standard'`), true},
		{"sessions unlinked", fmt.Sprintf(sess, 4, 4, `NULL, NULL, 'main_only', 'standard'`), true},
		{"sessions trajectory and reason", fmt.Sprintf(sess, 5, 5, `1, 'branch', 'main_only', 'standard'`), false},
		{"sessions neither trajectory nor reason", fmt.Sprintf(sess, 6, 6, `NULL, NULL, NULL, 'standard'`), false},
		{"sessions trajectory without link_basis", fmt.Sprintf(sess, 7, 7, `1, NULL, NULL, 'standard'`), false},
		{"sessions link_basis without trajectory", fmt.Sprintf(sess, 8, 8, `NULL, 'branch', 'main_only', 'standard'`), false},
		{"sessions bad reason", fmt.Sprintf(sess, 9, 9, `NULL, NULL, 'bogus', 'standard'`), false},
		{"setups run only", `INSERT INTO setups (source, source_id, recorded_at, run_id) VALUES ('w', 'a', 'r', 1)`, true},
		{"setups neither", `INSERT INTO setups (source, source_id, recorded_at) VALUES ('w', 'b', 'r')`, false},
		{"usage cost without kind", `INSERT INTO usage (source, source_id, recorded_at, run_id, model, cost) VALUES ('w', 'a', 'r', 1, 'm', 1.5)`, false},
		{"usage kind without cost", `INSERT INTO usage (source, source_id, recorded_at, run_id, model, cost_kind) VALUES ('w', 'b', 'r', 1, 'm', 'actual')`, false},
		{"usage cost and kind", `INSERT INTO usage (source, source_id, recorded_at, run_id, model, cost, cost_kind) VALUES ('w', 'c', 'r', 1, 'm', 1.5, 'actual')`, true},
		{"usage neither run nor session", `INSERT INTO usage (source, source_id, recorded_at, model) VALUES ('w', 'd', 'r', 'm')`, false},
		{"findings decision maybe", `INSERT INTO findings (source, source_id, recorded_at, trajectory_id, component, finding_key, record_path, record_sum, decision)
			VALUES ('w', 'a', 'r', 1, 'plan_review', 'r1-f1', '/p', 'x', 'maybe')`, false},
		{"findings decision adopted", `INSERT INTO findings (source, source_id, recorded_at, trajectory_id, component, finding_key, record_path, record_sum, decision)
			VALUES ('w', 'b', 'r', 1, 'plan_review', 'r1-f1', '/p', 'x', 'adopted')`, true},
		{"findings no decision", `INSERT INTO findings (source, source_id, recorded_at, trajectory_id, component, finding_key, record_path, record_sum)
			VALUES ('w', 'c', 'r', 1, 'plan_review', 'r1-f1', '/p', 'x')`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openTelemetryDB(t)
			_, err := db.Exec(tc.sql)
			if tc.ok && err != nil {
				t.Errorf("rejected: %v", err)
			}
			if !tc.ok && err == nil {
				t.Error("accepted, want a constraint error")
			}
		})
	}

	t.Run("setups run and session", func(t *testing.T) {
		db := openTelemetryDB(t)
		if _, err := db.Exec(fmt.Sprintf(sess, 1, 1, `1, 'branch', NULL, 'standard'`)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO setups (source, source_id, recorded_at, run_id, session_id) VALUES ('w', 'a', 'r', 1, 1)`); err == nil {
			t.Error("setup with run_id and session_id accepted")
		}
	})
	t.Run("findings run and session", func(t *testing.T) {
		db := openTelemetryDB(t)
		if _, err := db.Exec(fmt.Sprintf(sess, 1, 1, `1, 'branch', NULL, 'standard'`)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO findings (source, source_id, recorded_at, trajectory_id, run_id, session_id, component, finding_key, record_path, record_sum)
			VALUES ('w', 'a', 'r', 1, 1, 1, 'plan_review', 'r1-f1', '/p', 'x')`); err == nil {
			t.Error("finding with run_id and session_id accepted")
		}
	})
}

func TestMigrate0010ForeignKeysIntact(t *testing.T) {
	db := openTelemetryDB(t)
	if _, err := db.Exec(`INSERT INTO setups (source, source_id, recorded_at, run_id) VALUES ('w', 'a', 'r', 1)`); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		t.Error("foreign_key_check reports a violation")
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM setups WHERE run_id = 1`).Scan(&n); err != nil || n != 1 {
		t.Errorf("setups of run 1 = %d, %v; want 1", n, err)
	}
	if _, err := db.Exec(`DELETE FROM runs WHERE id = 1`); err == nil {
		t.Error("run with a setup deleted: foreign key not enforced")
	}
}

// The recipe from the spec: rebuild runs with child rows pointing at it, in one transaction.
func TestRebuildRunsWithChildrenCommits(t *testing.T) {
	db := openTelemetryDB(t)
	for _, q := range []string{
		`INSERT INTO setups (source, source_id, recorded_at, run_id) VALUES ('w', 'a', 'r', 1)`,
		`INSERT INTO usage (source, source_id, recorded_at, run_id, model) VALUES ('w', 'a', 'r', 1, 'm')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	var create string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'runs'`).Scan(&create); err != nil {
		t.Fatal(err)
	}
	widened := strings.Replace(create, "'scheduled')", "'scheduled', 'extra')", 1)
	if widened == create {
		t.Fatal("CHECK to widen not found in the runs definition")
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range []string{
		`PRAGMA defer_foreign_keys = ON`,
		`CREATE TABLE runs_stage AS SELECT * FROM runs`,
		`DROP TABLE runs`,
		widened,
		`INSERT INTO runs SELECT * FROM runs_stage`,
		`DROP TABLE runs_stage`,
	} {
		if _, err := tx.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("COMMIT: %v", err)
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Error("foreign_key_check reports a violation after the rebuild")
	}
	_ = rows.Close()
	var runs, setups, usage int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM runs), (SELECT count(*) FROM setups), (SELECT count(*) FROM usage)`).Scan(&runs, &setups, &usage); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || setups != 1 || usage != 1 {
		t.Errorf("rows runs/setups/usage = %d/%d/%d, want 1/1/1", runs, setups, usage)
	}
	if _, err := db.Exec(`UPDATE runs SET trigger = 'extra'`); err != nil {
		t.Errorf("widened CHECK not in effect: %v", err)
	}
}

func tableColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, c)
	}
	return cols
}

// The model references records and transcripts; it never stores their content.
func TestTelemetrySchemaHasNoContentColumns(t *testing.T) {
	db := openTelemetryDB(t)
	want := map[string][]string{
		"findings": {"id", "source", "source_id", "recorded_at", "trajectory_id", "run_id", "session_id", "component",
			"finding_key", "severity", "decision", "record_path", "record_sum"},
		"sessions": {"id", "source", "source_id", "recorded_at", "repo", "harness", "session_key", "transcript_path",
			"branch", "trajectory_id", "link_basis", "unlinked_reason", "assignment", "started_at", "ended_at"},
	}
	for table, cols := range want {
		if got := tableColumns(t, db, table); !reflect.DeepEqual(got, cols) {
			t.Errorf("%s columns = %v, want %v", table, got, cols)
		}
	}
}

func TestMigration0011Recovery(t *testing.T) {
	const rec = `INSERT INTO recoveries (source, source_id, recorded_at, trajectory_id, pr, merged_at, window_end, status)
		VALUES ('w', '%s', 'r', 1, %d, 't0', 't1', '%s')`
	const item = `INSERT INTO recovery_items (recovery_id, kind, ref, at) VALUES (1, 'fix', 'abc', 't0')`

	t.Run("tables exist", func(t *testing.T) {
		db := openTelemetryDB(t)
		for _, table := range []string{"recoveries", "recovery_items"} {
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
				t.Errorf("table %s: count %d, %v; want 1", table, n, err)
			}
		}
	})
	t.Run("status maybe rejected", func(t *testing.T) {
		db := openTelemetryDB(t)
		if _, err := db.Exec(fmt.Sprintf(rec, "a", 5, "maybe")); err == nil {
			t.Error("status maybe accepted")
		}
		if _, err := db.Exec(fmt.Sprintf(rec, "a", 5, "open")); err != nil {
			t.Errorf("status open rejected: %v", err)
		}
	})
	t.Run("duplicate trajectory and pr rejected", func(t *testing.T) {
		db := openTelemetryDB(t)
		if _, err := db.Exec(fmt.Sprintf(rec, "a", 5, "open")); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(fmt.Sprintf(rec, "b", 5, "open")); err == nil {
			t.Error("second row with the same (trajectory_id, pr) accepted")
		}
	})
	t.Run("duplicate source and source_id rejected", func(t *testing.T) {
		db := openTelemetryDB(t)
		if _, err := db.Exec(fmt.Sprintf(rec, "a", 5, "open")); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(fmt.Sprintf(rec, "a", 6, "open")); err == nil {
			t.Error("second row with the same (source, source_id) accepted")
		}
	})
	t.Run("unknown item kind rejected", func(t *testing.T) {
		db := openTelemetryDB(t)
		if _, err := db.Exec(fmt.Sprintf(rec, "a", 5, "open")); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO recovery_items (recovery_id, kind, ref, at) VALUES (1, 'other', 'abc', 't0')`); err == nil {
			t.Error("kind other accepted")
		}
	})
	t.Run("delete cascades to items", func(t *testing.T) {
		db := openTelemetryDB(t)
		if _, err := db.Exec(fmt.Sprintf(rec, "a", 5, "open")); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(item); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(item); err == nil {
			t.Error("duplicate (recovery_id, kind, ref) accepted")
		}
		if _, err := db.Exec(`DELETE FROM recoveries WHERE id = 1`); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM recovery_items`).Scan(&n); err != nil || n != 0 {
			t.Errorf("items after delete = %d, %v; want 0", n, err)
		}
	})
}

// upTo12 is the embedded migration set before 0013.
var upTo12 = []string{"0001_meta.sql", "0002_runs.sql", "0003_cleanup.sql", "0004_runs_fixpr.sql",
	"0005_workspace_removed.sql", "0006_runs_merge.sql", "0007_runs_node.sql", "0008_runs_schedule.sql",
	"0009_runs_pushed.sql", "0010_telemetry.sql", "0011_recovery.sql", "0012_trajectory_events.sql"}

// 0013 rebuilds setups with nullable deployment fields: an empty string becomes NULL, a filled
// value stays, lane is added with its CHECK, trajectories gain the plan columns, and the UNIQUE
// and owner constraints of 0010 survive the rebuild.
func TestMigrate0013RebuildsSetups(t *testing.T) {
	ctx := context.Background()
	db := bareDB(t)
	if err := migrate(ctx, db, embeddedUpTo(t, upTo12...)); err != nil {
		t.Fatalf("migrate to 12: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id, source, source_id, recorded_at, repo, harness, session_key,
		transcript_path, unlinked_reason, started_at)
		VALUES (1, 'transcript', 'transcript:s1', 't0', 'o/r', 'claude', 's1', '/x', 'main_only', 't0')`); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO setups (source, source_id, recorded_at, session_id, model, skills_hash)
		VALUES ('transcript', 'transcript:s1', 't0', 1, '', 'abc')`); err != nil {
		t.Fatalf("insert setup at v12: %v", err)
	}
	var before []schemaRow
	before, _ = snapshot(t, db)
	if err := migrate(ctx, db, embeddedUpTo(t, append(upTo12, "0013_deployment.sql")...)); err != nil {
		t.Fatalf("migrate to 13: %v", err)
	}
	if v := userVersion(t, db); v != 13 {
		t.Errorf("user_version = %d, want 13", v)
	}
	var model, effort, hash, lane sql.NullString
	if err := db.QueryRow(`SELECT model, effort, skills_hash, lane FROM setups WHERE session_id = 1`).
		Scan(&model, &effort, &hash, &lane); err != nil {
		t.Fatalf("read setup: %v", err)
	}
	if model.Valid || effort.Valid || lane.Valid {
		t.Errorf("model, effort, lane = %v, %v, %v; want all NULL", model, effort, lane)
	}
	if hash.String != "abc" {
		t.Errorf("skills_hash = %q, want abc", hash.String)
	}
	has := func(table, col string) bool {
		rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatalf("table_info %s: %v", table, err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n == col {
				return true
			}
		}
		return false
	}
	for _, c := range [][2]string{{"setups", "lane"}, {"trajectories", "plan_tasks"}, {"trajectories", "plan_path"}} {
		if !has(c[0], c[1]) {
			t.Errorf("%s.%s missing", c[0], c[1])
		}
	}
	if _, err := db.Exec(`INSERT INTO sessions (id, source, source_id, recorded_at, repo, harness, session_key,
		transcript_path, unlinked_reason, started_at)
		VALUES (2, 'transcript', 'transcript:s2', 't0', 'o/r', 'claude', 's2', '/x', 'main_only', 't0')`); err != nil {
		t.Fatalf("insert session 2: %v", err)
	}
	ins := func(src, sid string, session any, run any, lane any) error {
		_, err := db.Exec(`INSERT INTO setups (source, source_id, recorded_at, run_id, session_id, lane)
			VALUES (?, ?, 't', ?, ?, ?)`, src, sid, run, session, lane)
		return err
	}
	if err := ins("transcript", "transcript:s2", 2, nil, "x"); err == nil {
		t.Error("lane = 'x' accepted")
	}
	if err := ins("transcript", "transcript:s2", 2, nil, "sdd"); err != nil {
		t.Errorf("lane = 'sdd' refused: %v", err)
	}
	if err := ins("transcript", "other", 2, nil, nil); err == nil {
		t.Error("second setup for the same session accepted: UNIQUE (session_id) is gone")
	}
	if err := ins("transcript", "transcript:s2", nil, nil, nil); err == nil {
		t.Error("setup without owner accepted: owner CHECK is gone")
	}
	if _, err := db.Exec(`INSERT INTO runs (id, repo, issue, session_id, mode, trigger, trigger_key, attempt, command, started_at)
		VALUES (9, 'o/r', 1, 's9', 'start', 'handoff', 'h:9', 1, 'c', 't')`); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if err := ins("transcript", "transcript:r9", nil, 9, nil); err != nil {
		t.Fatalf("setup for run refused: %v", err)
	}
	if err := ins("transcript", "dup", nil, 9, nil); err == nil {
		t.Error("second setup for the same run accepted: UNIQUE (run_id) is gone")
	}
	if err := ins("transcript", "transcript:r9", nil, 9, nil); err == nil {
		t.Error("duplicate (source, source_id) accepted")
	}
	after, _ := snapshot(t, db)
	idx := func(rows []schemaRow) []string {
		var out []string
		for _, r := range rows {
			if r.Type == "index" && !strings.HasPrefix(r.Name, "sqlite_autoindex") {
				out = append(out, r.Name)
			}
		}
		return out
	}
	if !reflect.DeepEqual(idx(before), idx(after)) {
		t.Errorf("named indexes changed: %v -> %v", idx(before), idx(after))
	}
}

func TestTrajectoryEventsConstraints(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "lighthouse.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	for _, q := range []string{
		`INSERT INTO trajectories (id, repo, issue, created_at) VALUES (1, 'o/r', 1, '2026-01-01T00:00:00Z')`,
		`INSERT INTO runs (id, repo, issue, session_id, mode, trigger, trigger_key, attempt, started_at)
		 VALUES (1, 'o/r', 1, 's', 'start', 'handoff', 'k', 1, '2026-01-01T00:00:00Z')`,
		`INSERT INTO sessions (id, source, source_id, recorded_at, repo, harness, session_key, transcript_path, trajectory_id, link_basis, started_at)
		 VALUES (1, 'src', 's1', '2026-01-01T00:00:00Z', 'o/r', 'claude', 'k', '/t', 1, 'branch', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := s.DB.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	const ins = `INSERT INTO trajectory_events (source, source_id, recorded_at, trajectory_id, run_id, session_id, kind, ref, at)
		VALUES (?, ?, '2026-01-01T00:00:00Z', 1, ?, ?, ?, ?, '2026-01-01T00:00:00Z')`
	// A valid row first, so the unique check below has something to collide with.
	if _, err := s.DB.Exec(ins, "src", "e1", 1, nil, "question", "c1"); err != nil {
		t.Fatalf("valid insert: %v", err)
	}
	cases := []struct {
		name             string
		source, sourceID string
		run, session     any
		kind, ref        string
	}{
		{"neither run nor session", "src", "e2", nil, nil, "question", "c2"},
		{"both run and session", "src", "e3", 1, 1, "question", "c3"},
		{"done without run", "src", "e4", nil, 1, "done", "c4"},
		{"duplicate trajectory, kind, ref", "src", "e5", 1, nil, "question", "c1"},
	}
	for _, c := range cases {
		if _, err := s.DB.Exec(ins, c.source, c.sourceID, c.run, c.session, c.kind, c.ref); err == nil {
			t.Errorf("%s: insert accepted, want constraint error", c.name)
		}
	}
}

func TestMigration0014AddsReviewRunsAndTraits(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "lighthouse.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	for _, tc := range [][2]string{{"trajectories", "route"}, {"trajectories", "area"}, {"recoveries", "code_lines"}} {
		table, col := tc[0], tc[1]
		var n int
		if err := s.DB.QueryRow(`SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`, table, col).Scan(&n); err != nil || n != 1 {
			t.Errorf("column %s.%s: count %d, err %v", table, col, n, err)
		}
	}

	if _, err := s.DB.Exec(`INSERT INTO trajectories (repo, issue, created_at) VALUES ('r', 1, 't')`); err != nil {
		t.Fatalf("insert trajectory: %v", err)
	}
	const ins = `INSERT INTO review_runs (source, source_id, recorded_at, trajectory_id, unlinked_reason, component, record_path, record_sum, ran_at, finding_count)
		VALUES (?, ?, 't', ?, ?, ?, 'p', 's', 't', 0)`
	if _, err := s.DB.Exec(ins, "a", "1", nil, nil, "plan_review"); err == nil {
		t.Error("NULL trajectory without reason: want CHECK failure")
	}
	if _, err := s.DB.Exec(ins, "a", "1", nil, "no_trajectory", "plan_review"); err != nil {
		t.Errorf("NULL trajectory with reason: %v", err)
	}
	if _, err := s.DB.Exec(ins, "a", "1", nil, "no_trajectory", "spec_review"); err == nil {
		t.Error("duplicate (source, source_id): want UNIQUE failure")
	}
	if _, err := s.DB.Exec(ins, "a", "2", nil, "no_trajectory", "task_review"); err == nil {
		t.Error("component task_review: want CHECK failure")
	}
}

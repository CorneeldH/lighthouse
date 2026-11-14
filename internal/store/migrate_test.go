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
	if v2 != 6 {
		t.Errorf("user_version = %d, want 6", v2)
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
	if want := []string{"action_failures", "meta", "runs", "tick_state"}; !reflect.DeepEqual(tables, want) {
		t.Errorf("tables = %v, want %v", tables, want)
	}
	// column -> primary key position (0 = not part of the key)
	wantCols := map[string]map[string]int{
		"runs":            {"repo": 0, "issue": 0, "trigger_key": 0, "attempt": 0, "ended_at": 0, "outcome": 0},
		"action_failures": {"action_key": 1},
		"tick_state":      {"repo": 1, "kind": 2, "key": 3},
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
	if err := migrate(ctx, db, migrationFS); err != nil {
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

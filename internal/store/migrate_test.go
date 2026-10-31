package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	if v2 != 1 {
		t.Errorf("user_version = %d, want 1", v2)
	}
	var hasMeta, hasRuns bool
	for _, r := range schema2 {
		hasMeta = hasMeta || (r.Type == "table" && r.Name == "meta")
		hasRuns = hasRuns || r.Name == "runs"
	}
	if !hasMeta || hasRuns {
		t.Errorf("meta present = %v, runs present = %v; want true, false", hasMeta, hasRuns)
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

package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
)

//go:embed migrations/*.sql
var migrationsEmbed embed.FS

// migrationFS is the embedded migration set, rooted at the migrations directory.
var migrationFS = func() fs.FS {
	sub, err := fs.Sub(migrationsEmbed, "migrations")
	if err != nil {
		panic(err) // the directory is embedded at build time
	}
	return sub
}()

// ErrSchemaTooNew means the database was written by a newer lighthouse.
var ErrSchemaTooNew = errors.New("database schema is newer than this lighthouse knows")

var migrationName = regexp.MustCompile(`^(\d{4})_[a-z0-9_]+\.sql$`)

type migration struct {
	number int
	file   string
}

// loadMigrations lists the .sql files of fsys, numbered 1..n without gap or duplicate.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var list []migration
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("migration %q: name must be NNNN_name.sql with four digits", e.Name())
		}
		n, _ := strconv.Atoi(m[1]) // four digits, cannot fail
		list = append(list, migration{number: n, file: e.Name()})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].file < list[j].file })
	for i, m := range list {
		want := i + 1
		switch {
		case m.number == want:
		case i > 0 && m.number == list[i-1].number:
			return nil, fmt.Errorf("migration number %04d is used twice (%s, %s)", m.number, list[i-1].file, m.file)
		default:
			return nil, fmt.Errorf("migration numbering has a gap: expected %04d, found %s", want, m.file)
		}
	}
	return list, nil
}

// migrate runs every migration above the database's user_version, each in
// one transaction that also sets user_version. A database newer than the
// highest known migration is refused untouched.
func migrate(ctx context.Context, db *sql.DB, fsys fs.FS) error {
	list, err := loadMigrations(fsys)
	if err != nil {
		return err
	}
	var current int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	if current > len(list) {
		return fmt.Errorf("%w: user_version %d, highest known %d", ErrSchemaTooNew, current, len(list))
	}
	for _, m := range list[current:] {
		if err := apply(ctx, db, fsys, m); err != nil {
			return err
		}
	}
	return nil
}

func apply(ctx context.Context, db *sql.DB, fsys fs.FS, m migration) error {
	body, err := fs.ReadFile(fsys, m.file)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", m.file, err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", m.file, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("run migration %s: %w", m.file, err)
	}
	// PRAGMA takes no bound parameter; m.number is an int, so formatting is safe.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.number)); err != nil {
		return fmt.Errorf("set user_version after %s: %w", m.file, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", m.file, err)
	}
	return nil
}

package store

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	helperEnv = "LIGHTHOUSE_TEST_HELPER"
	helperDB  = "LIGHTHOUSE_TEST_DB"
	helperGo  = "LIGHTHOUSE_TEST_GO"
)

// TestHelperOpenShared is the child process of TestFreshDatabaseTwoProcesses:
// the test binary runs itself. Without the env variable it is skipped.
func TestHelperOpenShared(t *testing.T) {
	if os.Getenv(helperEnv) != "open" {
		t.Skip("only runs as the child of TestFreshDatabaseTwoProcesses")
	}
	goFile := os.Getenv(helperGo)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(goFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "go file %s never appeared\n", goFile)
			os.Exit(1)
		}
		time.Sleep(time.Millisecond)
	}
	s, err := OpenShared(context.Background(), os.Getenv(helperDB))
	if err == nil {
		err = s.Close()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// TestFreshDatabaseTwoProcesses lets two processes open one fresh database
// at the same moment: both must succeed, and the migrations run once.
func TestFreshDatabaseTwoProcesses(t *testing.T) {
	list, err := loadMigrations(migrationFS)
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	for i := range 20 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "l.db")
			goFile := filepath.Join(dir, "go")

			var cmds []*exec.Cmd
			var stderrs []*bytes.Buffer
			for range 2 {
				cmd := exec.Command(os.Args[0], "-test.run=^TestHelperOpenShared$")
				// atexit_sleep_ms=0: a -race child otherwise sleeps 1s at exit.
				cmd.Env = append(os.Environ(),
					"GORACE=atexit_sleep_ms=0",
					helperEnv+"=open",
					helperDB+"="+path,
					helperGo+"="+goFile,
				)
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				if err := cmd.Start(); err != nil {
					t.Fatalf("start helper: %v", err)
				}
				cmds = append(cmds, cmd)
				stderrs = append(stderrs, &stderr)
			}
			if err := os.WriteFile(goFile, nil, 0o600); err != nil {
				t.Fatalf("write go file: %v", err)
			}
			for n, cmd := range cmds {
				if err := cmd.Wait(); err != nil {
					t.Errorf("helper %d: %v; stderr: %s", n+1, err, stderrs[n])
				}
			}
			if t.Failed() {
				return
			}

			db, err := sql.Open("sqlite", dsn(path))
			if err != nil {
				t.Fatalf("sql.Open: %v", err)
			}
			defer func() { _ = db.Close() }()
			if v := userVersion(t, db); v != len(list) {
				t.Errorf("user_version = %d, want %d", v, len(list))
			}
			var created int
			if err := db.QueryRow(`SELECT count(*) FROM meta WHERE key = 'created_at'`).Scan(&created); err != nil {
				t.Fatalf("count created_at: %v", err)
			}
			if created != 1 {
				t.Errorf("created_at rows = %d, want 1", created)
			}
		})
	}
}

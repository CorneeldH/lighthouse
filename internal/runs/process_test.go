package runs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/store"
)

const (
	helperEnv = "LIGHTHOUSE_TEST_HELPER"
	helperDB  = "LIGHTHOUSE_TEST_DB"
	helperGo  = "LIGHTHOUSE_TEST_GO"
	helperKey = "LIGHTHOUSE_TEST_KEY"
)

// TestHelperTake is the child process of TestClaimTwoProcesses: the test
// binary runs itself. Exit 0 = claim won, 3 = ErrClaimed, 1 = other error.
func TestHelperTake(t *testing.T) {
	if os.Getenv(helperEnv) != "take" {
		t.Skip("only runs as the child of TestClaimTwoProcesses")
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
	ctx := context.Background()
	s, err := store.OpenShared(ctx, os.Getenv(helperDB))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_, err = Take(ctx, s.DB, claim("owner/name", 7, os.Getenv(helperKey)))
	_ = s.Close()
	switch {
	case err == nil:
		os.Exit(0)
	case errors.Is(err, ErrClaimed):
		os.Exit(3)
	default:
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// TestClaimTwoProcesses lets two processes claim the same issue at the same
// moment: exactly one wins, the other gets ErrClaimed.
func TestClaimTwoProcesses(t *testing.T) {
	for i := range 20 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "l.db")
			goFile := filepath.Join(dir, "go")
			// Create the schema first so the race is on Take alone.
			s, err := store.OpenShared(context.Background(), path)
			if err != nil {
				t.Fatalf("OpenShared: %v", err)
			}
			defer func() { _ = s.Close() }()

			var cmds []*exec.Cmd
			var stderrs []*bytes.Buffer
			for n := range 2 {
				cmd := exec.Command(os.Args[0], "-test.run=^TestHelperTake$")
				// atexit_sleep_ms=0: a -race child otherwise sleeps 1s at exit.
				cmd.Env = append(os.Environ(),
					"GORACE=atexit_sleep_ms=0",
					helperEnv+"=take",
					helperDB+"="+path,
					helperGo+"="+goFile,
					helperKey+"="+fmt.Sprintf("key:%d", n),
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
			codes := map[int]int{}
			for n, cmd := range cmds {
				err := cmd.Wait()
				code := 0
				var ee *exec.ExitError
				if errors.As(err, &ee) {
					code = ee.ExitCode()
				} else if err != nil {
					t.Fatalf("helper %d: %v", n+1, err)
				}
				codes[code]++
				if code != 0 && code != 3 {
					t.Errorf("helper %d exit %d; stderr: %s", n+1, code, stderrs[n])
				}
			}
			if codes[0] != 1 || codes[3] != 1 {
				t.Errorf("exit codes = %v, want one 0 and one 3", codes)
			}
			var open int
			if err := s.DB.QueryRow(`SELECT count(*) FROM runs WHERE ended_at IS NULL`).Scan(&open); err != nil {
				t.Fatalf("count open: %v", err)
			}
			if open != 1 {
				t.Errorf("open rows = %d, want 1", open)
			}
		})
	}
}

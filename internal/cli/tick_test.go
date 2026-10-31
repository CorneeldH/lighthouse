package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/flock"
	"github.com/CorneeldH/lighthouse/internal/store"
)

const completeRepo = `[forge]
repo = "o/r"

[forge.board]
project_number = 3
project_id = "PVT_x"
status_field_id = "SF"
priority_field_id = "PF"

[forge.board.status_options]
"Controller" = "c1"
"On hold" = "h1"

[controller]

[usage_guard]
controller = true
`

func repoDir(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, "lighthouse.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// tickFixture returns an environment with a runtime dir, a token and a host.toml listing repos.
func tickFixture(t *testing.T, repos ...string) (tickEnv, string) {
	t.Helper()
	runtime := t.TempDir()
	host := filepath.Join(t.TempDir(), "host.toml")
	body := ""
	if len(repos) > 0 {
		quoted := make([]string, len(repos))
		for i, r := range repos {
			quoted[i] = fmt.Sprintf("%q", r)
		}
		body = "[dispatch]\nrepos = [" + strings.Join(quoted, ", ") + "]\n"
	}
	if err := os.WriteFile(host, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{"XDG_RUNTIME_DIR": runtime, "GH_TOKEN": "tok"}
	env := tickEnv{Getenv: func(k string) string { return vars[k] }, HostToml: host}
	return env, filepath.Join(t.TempDir(), "lh.db")
}

func TestTickLockBusy(t *testing.T) {
	env, db := tickFixture(t)
	dir := filepath.Join(env.Getenv("XDG_RUNTIME_DIR"), "lighthouse")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	held, err := flock.TryLock(filepath.Join(dir, "tick.lock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })

	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v, want nil", err)
	}
	if !strings.Contains(errOut.String(), "previous tick still running") {
		t.Errorf("errOut = %q", errOut.String())
	}
	if _, err := os.Stat(db); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("database exists after a busy tick: %v", err)
	}
}

func TestTickWithoutToken(t *testing.T) {
	env, db := tickFixture(t)
	env.Getenv = func(k string) string {
		if k == "XDG_RUNTIME_DIR" {
			return filepath.Dir(db)
		}
		return ""
	}
	err := tick(context.Background(), env, db, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no GitHub token") {
		t.Fatalf("err = %v", err)
	}
}

func TestTickWithoutRuntimeDir(t *testing.T) {
	for _, v := range []string{"", "relative"} {
		env, db := tickFixture(t)
		env.Getenv = func(k string) string {
			if k == "XDG_RUNTIME_DIR" {
				return v
			}
			return "tok"
		}
		err := tick(context.Background(), env, db, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "XDG_RUNTIME_DIR") {
			t.Errorf("XDG_RUNTIME_DIR=%q: err = %v", v, err)
		}
	}
}

func TestTickNoRepos(t *testing.T) {
	env, db := tickFixture(t)
	if err := tick(context.Background(), env, db, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	conn, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	var v int
	if err := conn.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != 2 {
		t.Errorf("user_version = %d, want 2", v)
	}
}

func TestTickSkipsBadRepos(t *testing.T) {
	noController := repoDir(t, strings.Replace(completeRepo, "[controller]\n\n", "", 1))
	noGuard := repoDir(t, strings.Replace(completeRepo, "controller = true", "controller = false", 1))
	noOption := repoDir(t, strings.Replace(completeRepo, "\"Controller\" = \"c1\"\n", "", 1))
	good := repoDir(t, completeRepo)
	env, db := tickFixture(t, noController, noGuard, noOption, good)

	var errOut bytes.Buffer
	err := tick(context.Background(), env, db, &errOut)
	if !errors.Is(err, errSilent) {
		t.Fatalf("err = %v, want errSilent", err)
	}
	lines := strings.Split(strings.TrimSpace(errOut.String()), "\n")
	for path, reason := range map[string]string{
		noController: "missing [controller]",
		noGuard:      "usage_guard.controller is not true",
		noOption:     "status_options.Controller",
	} {
		var found string
		for _, l := range lines {
			if strings.HasPrefix(l, "lighthouse: skip "+path+":") {
				found = l
			}
		}
		if found == "" {
			t.Errorf("no skip line for %s:\n%s", path, errOut.String())
		} else if !strings.Contains(found, reason) {
			t.Errorf("skip line %q lacks its own reason %q", found, reason)
		}
	}
	if len(lines) != 3 {
		t.Errorf("want 3 skip lines (complete repo has none), got %d:\n%s", len(lines), errOut.String())
	}
}

func TestTickSkipsMissingRepo(t *testing.T) {
	missing := repoDir(t, "")
	env, db := tickFixture(t, missing)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); !errors.Is(err, errSilent) {
		t.Fatalf("err = %v, want errSilent", err)
	}
	if !strings.Contains(errOut.String(), "lighthouse: skip "+missing) {
		t.Errorf("errOut = %q", errOut.String())
	}
}

func TestTickBesideServe(t *testing.T) {
	env, db := tickFixture(t)
	st, err := store.Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := tick(context.Background(), env, db, &bytes.Buffer{}); err != nil {
		t.Fatalf("tick beside serve = %v", err)
	}
}

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
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/flock"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
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

// fakeForge points env at a fake GitHub with an empty board and token login lh-bot.
func fakeForge(t *testing.T, env *tickEnv) *forgetest.Server {
	t.Helper()
	srv := forgetest.New(t)
	srv.Login = "lh-bot"
	env.ForgeURL = srv.URL()
	return srv
}

// boardNode is a board item in GraphQL shape: issue number of repo o/r with Status option status.
func boardNode(id, status string, content map[string]any) map[string]any {
	return map[string]any{"id": id, "updatedAt": "2026-10-30T09:00:00Z",
		"fieldValues": map[string]any{"nodes": []any{
			map[string]any{"optionId": status, "field": map[string]any{"id": "SF"}},
		}},
		"content": content}
}

// issueContent is an open issue of o/r; extra adds or replaces fields.
func issueContent(number int, title string, extra map[string]any) map[string]any {
	c := map[string]any{"number": number, "title": title, "state": "OPEN", "updatedAt": "2026-10-30T10:00:00Z",
		"repository": map[string]any{"nameWithOwner": "o/r"}}
	for k, v := range extra {
		c[k] = v
	}
	return c
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
	for v, want := range map[string]string{
		"":         "tick: XDG_RUNTIME_DIR is not set: cannot take tick.lock",
		"relative": "tick: XDG_RUNTIME_DIR is not an absolute path: cannot take tick.lock",
	} {
		env, db := tickFixture(t)
		env.Getenv = func(k string) string {
			if k == "XDG_RUNTIME_DIR" {
				return v
			}
			return "tok"
		}
		err := tick(context.Background(), env, db, &bytes.Buffer{})
		if err == nil || err.Error() != want {
			t.Errorf("XDG_RUNTIME_DIR=%q: err = %v, want %q", v, err, want)
		}
	}
}

func TestTickNoRepos(t *testing.T) {
	env, db := tickFixture(t)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatal(err)
	}
	if want := "lighthouse: no repos in [dispatch] of " + env.HostToml + "\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
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

func TestTickNoRepos_NoHostToml(t *testing.T) {
	env, db := tickFixture(t)
	env.HostToml = ""
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatal(err)
	}
	if want := "lighthouse: no host.toml: no repos to check\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
}

func TestTickSkipsBadRepos(t *testing.T) {
	noController := repoDir(t, strings.Replace(completeRepo, "[controller]\n\n", "", 1))
	noGuard := repoDir(t, strings.Replace(completeRepo, "controller = true", "controller = false", 1))
	noOption := repoDir(t, strings.Replace(completeRepo, "\"Controller\" = \"c1\"\n", "", 1))
	good := repoDir(t, completeRepo)
	env, db := tickFixture(t, noController, noGuard, noOption, good)
	fakeForge(t, &env)

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
	skips := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "lighthouse: skip ") {
			skips++
		}
	}
	if skips != 3 {
		t.Errorf("want 3 skip lines (complete repo has none), got %d:\n%s", skips, errOut.String())
	}
	if !strings.Contains(errOut.String(), "lighthouse: gathered o/r: 0 items (first)\n") {
		t.Errorf("no gathered line for the complete repo:\n%s", errOut.String())
	}
}

func TestTickGathersThenSkipsUnchanged(t *testing.T) {
	good := repoDir(t, completeRepo)
	env, db := tickFixture(t, good)
	srv := fakeForge(t, &env)
	srv.BoardNodes = []map[string]any{boardNode("I7", "c1", issueContent(7, "Issue", nil))}
	t0 := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	const comments = "/repos/o/r/issues/7/comments"

	for _, step := range []struct {
		after    time.Duration
		want     string // "" is no gathered line
		comments int    // comment requests so far: only a full gather reads comments
		boards   int    // Board requests so far: the precheck reads the board every tick
	}{
		{0, "lighthouse: gathered o/r: 1 items (first)\n", 1, 1},
		{3 * time.Minute, "", 1, 2},
		{31 * time.Minute, "lighthouse: gathered o/r: 1 items (stale)\n", 2, 3},
	} {
		env.Now = func() time.Time { return t0.Add(step.after) }
		var errOut bytes.Buffer
		if err := tick(context.Background(), env, db, &errOut); err != nil {
			t.Fatalf("tick at +%v = %v\n%s", step.after, err, errOut.String())
		}
		if errOut.String() != step.want {
			t.Errorf("tick at +%v: errOut = %q, want %q", step.after, errOut.String(), step.want)
		}
		if n := srv.Count(comments); n != step.comments {
			t.Errorf("tick at +%v: comment requests = %d, want %d", step.after, n, step.comments)
		}
		if n := srv.Count("Board"); n != step.boards {
			t.Errorf("tick at +%v: Board requests = %d, want %d", step.after, n, step.boards)
		}
	}
	if n := srv.Count("/user"); n != 2 {
		t.Errorf("/user requests = %d, want 2 (only the ticks that gathered)", n)
	}
}

func TestTickForgeErrorSkipsLoudlyAndKeepsState(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(*forgetest.Server)
		want  string
	}{
		"graphql error": {func(s *forgetest.Server) { s.GraphQLErrors = []string{"Could not resolve to a node"} },
			"Could not resolve to a node"},
		"empty token login": {func(s *forgetest.Server) { s.Login = "" }, "token has no login"},
		// after login(), inside Complete: the permission read of the comment's author fails
		"permission 500": {func(s *forgetest.Server) {
			s.BoardNodes = []map[string]any{boardNode("I7", "c1", issueContent(7, "Issue", nil))}
			s.Comments = map[int][]map[string]any{7: {{"id": 1, "body": "Hoi",
				"created_at": "2026-10-30T11:00:00Z", "html_url": "https://github.com/o/r/issues/7#issuecomment-1",
				"user": map[string]any{"login": "owner"}}}}
			s.RoleStatus = 500
		}, "/repos/o/r/collaborators/owner/permission"},
	} {
		t.Run(name, func(t *testing.T) {
			good := repoDir(t, completeRepo)
			env, db := tickFixture(t, good)
			tc.setup(fakeForge(t, &env))
			var errOut bytes.Buffer
			if err := tick(context.Background(), env, db, &errOut); !errors.Is(err, errSilent) {
				t.Fatalf("err = %v, want errSilent", err)
			}
			line := strings.TrimSpace(errOut.String())
			if !strings.HasPrefix(line, "lighthouse: skip "+good+": ") || !strings.Contains(line, tc.want) {
				t.Errorf("errOut = %q, want one skip line for %s naming %q", errOut.String(), good, tc.want)
			}
			st, err := store.OpenShared(context.Background(), db)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = st.Close() })
			state, err := dispatch.LoadTickState(context.Background(), st.DB, "o/r")
			if err != nil {
				t.Fatal(err)
			}
			if state != (dispatch.TickState{}) {
				t.Errorf("tick state = %+v after a failed gather, want none", state)
			}
		})
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

func TestTickLogsInOncePerTick(t *testing.T) {
	first := repoDir(t, completeRepo)
	second := repoDir(t, strings.Replace(completeRepo, `repo = "o/r"`, `repo = "o/s"`, 1))
	env, db := tickFixture(t, first, second)
	srv := fakeForge(t, &env)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	want := "lighthouse: gathered o/r: 0 items (first)\nlighthouse: gathered o/s: 0 items (first)\n"
	if errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
	if n := srv.Count("/user"); n != 1 {
		t.Errorf("/user requests = %d, want 1 for two gathered repos", n)
	}
}

func TestTickTrustsAnswerPermissions(t *testing.T) {
	good := repoDir(t, completeRepo)
	env, db := tickFixture(t, good)
	f, err := os.OpenFile(env.HostToml, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("answer_permissions = [\"write\"]\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	srv := fakeForge(t, &env)
	srv.Roles = map[string]string{"owner": "admin"}
	srv.BoardNodes = []map[string]any{boardNode("I7", "c1", issueContent(7, "Issue", nil))}
	srv.Comments = map[int][]map[string]any{7: {{"id": 1, "body": "🤖 Vraag\n\nWelke kant op?",
		"created_at": "2026-10-30T11:00:00Z", "html_url": "https://github.com/o/r/issues/7#issuecomment-1",
		"user": map[string]any{"login": "owner"}}}}
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	want := "lighthouse: o/r#7: ignored 🤖-comment https://github.com/o/r/issues/7#issuecomment-1 by owner: role not in answer_permissions\n" +
		"lighthouse: gathered o/r: 1 items (first)\n"
	if errOut.String() != want {
		t.Errorf("errOut = %q, want %q (admin is not in answer_permissions = [\"write\"])", errOut.String(), want)
	}
}

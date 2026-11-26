package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/flock"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/usage"
	"github.com/CorneeldH/lighthouse/internal/workplace"
	"github.com/CorneeldH/lighthouse/internal/workplace/kennforge"
	"github.com/CorneeldH/lighthouse/internal/workplace/local"
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
"To do" = "t1"

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
		// proposals_repo is the usual repo o/r, so the version step has nothing to log in the tests that
		// compare errOut exactly; it comes first so that tests can append [dispatch.*] tables.
		body = "[telemetry]\nproposals_repo = \"o/r\"\n\n[dispatch]\nrepos = [" + strings.Join(quoted, ", ") + "]\n"
	}
	if err := os.WriteFile(host, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{"XDG_RUNTIME_DIR": runtime, "GH_TOKEN": "tok",
		"XDG_DATA_HOME": t.TempDir(), "XDG_STATE_HOME": t.TempDir()}
	env := tickEnv{Getenv: func(k string) string { return vars[k] }, HostToml: host, StateDir: t.TempDir(),
		Workplace: func(r repoconfig.Repo, _, _ string) workplace.Workplace {
			t.Errorf("unexpected workplace for %s: this test starts nothing", r.Slug)
			return newFakeWorkplace(t, r.Slug)
		}}
	openBrake(t, &env)
	return env, filepath.Join(t.TempDir(), "lh.db")
}

// openBrake gives env a brake that is open: a fresh usage cache at 10% and 10%, and 32 GiB of
// MemAvailable. The meter has its own clock (t0), apart from env.Now, which tests move by hours;
// a refresh or a pgrep fails the test, so a fresh cache is what the tick relies on.
func openBrake(t *testing.T, env *tickEnv) {
	t.Helper()
	m := usage.NewMeter(usage.Paths{Dir: t.TempDir()}, usage.DefaultConfig())
	m.Now = func() time.Time { return t0 }
	m.RunUsage = func(context.Context, string) ([]byte, error) {
		t.Error("the meter refreshed: the cache should be fresh")
		return nil, errors.New("no refresh in this test")
	}
	m.ClaudeRunning = func() bool {
		t.Error("the meter looked for claude")
		return false
	}
	env.Meter = &m
	env.MemInfo = filepath.Join(t.TempDir(), "meminfo")
	if err := os.WriteFile(env.MemInfo, []byte("MemTotal: 67108864 kB\nMemAvailable:   33554432 kB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setUsage(t, *env, 10, 10)
}

// setUsage writes the usage cache of env's meter, generated ten seconds before its clock.
func setUsage(t *testing.T, env tickEnv, session, week int) {
	t.Helper()
	m := env.Meter
	s := usage.State{Schema: 1, GeneratedAt: m.Now().Add(-10 * time.Second).UTC().Format("2006-01-02T15:04:05Z"),
		Session: usage.WindowState{Pct: session}, Week: usage.WindowState{Pct: week}}
	if err := usage.WriteState(m.Paths.Cache(), s); err != nil {
		t.Fatal(err)
	}
}

// fakeForge points env at a fake GitHub with an empty board and token login lh-bot.
func fakeForge(t *testing.T, env *tickEnv) *forgetest.Server {
	t.Helper()
	srv := forgetest.New(t)
	srv.Login = "lh-bot"
	srv.ProjectID = "PVT_x"
	env.ForgeURL = srv.URL()
	return srv
}

// botForge is the forge of repo at env.ForgeURL, with token tok and token login lh-bot.
func botForge(t *testing.T, env tickEnv, repo repoconfig.Repo) repoForge {
	t.Helper()
	f, err := forge.Open(forge.Config{Repo: repo.Slug, Token: "tok", BaseURL: env.ForgeURL, Board: repo.Board})
	if err != nil {
		t.Fatal(err)
	}
	return repoForge{Forge: f, who: func() (string, error) { return "lh-bot", nil }}
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

// noProposalsRepo is the log line of the version step when host.toml has no [telemetry]
// proposals_repo, once per fresh state directory.
const noProposalsRepo = "lighthouse: version change: no [telemetry] proposals_repo in host.toml: no proposals\n"

func TestTickNoRepos(t *testing.T) {
	env, db := tickFixture(t)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatal(err)
	}
	if want := "lighthouse: no repos in [dispatch] of " + env.HostToml + "\n" + noProposalsRepo; errOut.String() != want {
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
	if v != 15 {
		t.Errorf("user_version = %d, want 15", v)
	}
}

func TestTickNoRepos_NoHostToml(t *testing.T) {
	env, db := tickFixture(t)
	env.HostToml = ""
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatal(err)
	}
	if want := "lighthouse: no host.toml: no repos to check\n" + noProposalsRepo; errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
}

func TestTickSkipsBadRepos(t *testing.T) {
	// Each bad repo has a slug of its own: two paths with one slug are both refused.
	slug := func(s, name string) string { return strings.Replace(s, `repo = "o/r"`, `repo = "o/`+name+`"`, 1) }
	noController := repoDir(t, slug(strings.Replace(completeRepo, "[controller]\n\n", "", 1), "a"))
	noGuard := repoDir(t, slug(strings.Replace(completeRepo, "controller = true", "controller = false", 1), "b"))
	noOption := repoDir(t, slug(strings.Replace(completeRepo, "\"Controller\" = \"c1\"\n", "", 1), "c"))
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
	srv.BoardNodes = []map[string]any{boardNode("I7", "h1", issueContent(7, "Issue", nil))} // On hold: nothing starts
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
		if n := srv.Count("/users/o/projectsV2/3/items"); n != step.boards {
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
		"board error": {func(s *forgetest.Server) {
			s.Fail = map[string]int{"GET /users/o/projectsV2/3/items": 502}
			s.FailMessage = map[string]string{"GET /users/o/projectsV2/3/items": "Could not resolve to a node"}
		}, "Could not resolve to a node"},
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
			if !reflect.DeepEqual(state, dispatch.TickState{}) {
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

// A repo whose token is not set is skipped; the repo with a token is still ticked.
func TestTickSkipsRepoWithoutToken(t *testing.T) {
	good := repoDir(t, completeRepo)
	noToken := repoDir(t, strings.Replace(completeRepo, `repo = "o/r"`, "repo = \"o/x\"\ntoken_env = \"MISSING\"", 1))
	env, db := tickFixture(t, good, noToken)
	fakeForge(t, &env)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); !errors.Is(err, errSilent) {
		t.Fatalf("err = %v, want errSilent\n%s", err, errOut.String())
	}
	want := "lighthouse: gathered o/r: 0 items (first)\nlighthouse: skip " + noToken + ": no GitHub token: set MISSING\n"
	if errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
}

// When no repo gets a forge, tick fails on the first token error before it opens the store.
func TestTickFailsWhenNoRepoHasToken(t *testing.T) {
	noToken := repoDir(t, strings.Replace(completeRepo, `repo = "o/r"`, "repo = \"o/x\"\ntoken_env = \"MISSING\"", 1))
	env, db := tickFixture(t, noToken)
	var errOut bytes.Buffer
	err := tick(context.Background(), env, db, &errOut)
	if want := "tick: no GitHub token: set MISSING"; err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if errOut.Len() != 0 {
		t.Errorf("errOut = %q, want nothing", errOut.String())
	}
	if _, err := os.Stat(db); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("database exists after a tick without any token: %v", err)
	}
}

// Two paths with the same slug are both skipped, each line naming both paths; neither is gathered.
func TestTickSkipsDuplicateSlug(t *testing.T) {
	first, second := repoDir(t, completeRepo), repoDir(t, completeRepo)
	env, db := tickFixture(t, first, second)
	srv := fakeForge(t, &env)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); !errors.Is(err, errSilent) {
		t.Fatalf("err = %v, want errSilent\n%s", err, errOut.String())
	}
	lines := strings.Split(strings.TrimSpace(errOut.String()), "\n")
	// The skipped repos are no repos of the tick, so proposals_repo o/r is none either.
	if want := "lighthouse: version change: proposals_repo o/r is no repo of [dispatch]: no proposals"; len(lines) != 3 || lines[2] != want {
		t.Fatalf("errOut = %q, want two skip lines and %q", errOut.String(), want)
	}
	for i, self := range []string{first, second} {
		if !strings.HasPrefix(lines[i], "lighthouse: skip "+self+": ") || !strings.Contains(lines[i], first) ||
			!strings.Contains(lines[i], second) || !strings.Contains(lines[i], "o/r") {
			t.Errorf("line %d = %q, want a skip line for %s naming both paths and o/r", i, lines[i], self)
		}
	}
	if n := srv.Count("/users/o/projectsV2/3/items"); n != 0 {
		t.Errorf("Board requests = %d, want 0: no repo with a duplicate slug is gathered", n)
	}
}

func TestTickOneLoginForSharedToken(t *testing.T) {
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
	srv.BoardNodes = []map[string]any{boardNode("I7", "h1", issueContent(7, "Issue", nil))} // On hold: nothing starts
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

// fakeWorkplace is the forge of one repo: a workspace per branch, and per spawn a session in
// state "working" until setState changes every session's state.
type fakeWorkplace struct {
	t    *testing.T
	repo string

	mu       sync.Mutex
	state    string
	spawns   []workplace.LaunchSpec
	kills    []string                       // the session ids per Kill
	sessions map[string][]workplace.Session // per workspace id
	// looked, asked, removed and discarded are the workspace ids of each Lookup, Sessions,
	// Remove and Discard, in order.
	looked, asked, removed, discarded []string
	// removeErr, when set, is what Remove answers.
	removeErr error
}

func newFakeWorkplace(t *testing.T, repo string) *fakeWorkplace {
	return &fakeWorkplace{t: t, repo: repo, sessions: map[string][]workplace.Session{}}
}

func (w *fakeWorkplace) Prepare(_ context.Context, branch string) (workplace.Workspace, error) {
	return workplace.Workspace{ID: "ws-" + branch, Path: "/work/" + branch, Branch: branch}, nil
}

func (w *fakeWorkplace) Spawn(_ context.Context, ws workplace.Workspace, l workplace.LaunchSpec) (workplace.Session, error) {
	if l.Repo != w.repo {
		w.t.Errorf("spawn for %s on the workplace of %s", l.Repo, w.repo)
	}
	if _, err := os.Stat(l.InstructionsFile); err != nil {
		w.t.Errorf("spawn without its instructions file: %v", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	s := workplace.Session{Key: "key-" + l.SessionID, WorkspaceID: ws.ID, State: "working", SessionID: l.SessionID}
	w.spawns = append(w.spawns, l)
	w.sessions[ws.ID] = append(w.sessions[ws.ID], s)
	return s, nil
}

func (w *fakeWorkplace) Sessions(_ context.Context, workspaceID string) ([]workplace.Session, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.asked = append(w.asked, workspaceID)
	out := slices.Clone(w.sessions[workspaceID])
	for i := range out {
		if w.state != "" {
			out[i].State = w.state
		}
	}
	return out, nil
}

func (w *fakeWorkplace) Stop(context.Context, string, []string) error {
	w.t.Error("unexpected Stop")
	return errors.New("unexpected Stop")
}

func (w *fakeWorkplace) Kill(_ context.Context, _ string, sessionIDs []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.kills = append(w.kills, strings.Join(sessionIDs, ","))
	return nil
}

func (w *fakeWorkplace) killed() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.kills)
}

func (w *fakeWorkplace) Remove(_ context.Context, workspaceID string, _ workplace.RemoveOptions) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.removed = append(w.removed, workspaceID)
	return w.removeErr
}

func (w *fakeWorkplace) Lookup(_ context.Context, workspaceID string) (workplace.Workspace, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.looked = append(w.looked, workspaceID)
	return workplace.Workspace{ID: workspaceID, Path: "/work/" + workspaceID}, nil
}

func (w *fakeWorkplace) Discard(_ context.Context, workspaceID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.discarded = append(w.discarded, workspaceID)
	return nil
}

// calls returns copies of looked, asked and removed.
func (w *fakeWorkplace) calls() (looked, asked, removed []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.looked), slices.Clone(w.asked), slices.Clone(w.removed)
}

func (w *fakeWorkplace) setState(state string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = state
}

func (w *fakeWorkplace) spawned() []workplace.LaunchSpec {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.spawns)
}

// useWorkplaces gives each repo slug its fake workplace; any other repo is an error.
func useWorkplaces(t *testing.T, env *tickEnv, places map[string]*fakeWorkplace) {
	env.Workplace = func(r repoconfig.Repo, _, _ string) workplace.Workplace {
		w, ok := places[r.Slug]
		if !ok {
			t.Errorf("workplace for unknown repo %s", r.Slug)
			return newFakeWorkplace(t, r.Slug)
		}
		return w
	}
}

var t0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// startable is a fake GitHub with issue 7 of o/r in Controller and nothing on it yet, trusted
// logins lh-bot (the token) and owner, and the clock at t0 for tick and server alike.
func startable(t *testing.T, env *tickEnv) (*forgetest.Server, *fakeWorkplace) {
	t.Helper()
	srv := fakeForge(t, env)
	srv.Roles = map[string]string{"lh-bot": "admin", "owner": "admin"}
	srv.BoardNodes = []map[string]any{boardNode("I7", "c1", issueContent(7, "Issue", nil))}
	// Issue 7 is open: a 🤖 Returned reads it before it labels it and moves it to To do.
	srv.Issues = map[int]map[string]any{7: {"number": 7, "title": "Issue", "state": "open", "labels": []any{}}}
	env.Now = func() time.Time { return t0 }
	srv.Now = env.Now
	w := newFakeWorkplace(t, "o/r")
	useWorkplaces(t, env, map[string]*fakeWorkplace{"o/r": w})
	return srv, w
}

func appendHostToml(t *testing.T, env tickEnv, text string) {
	t.Helper()
	f, err := os.OpenFile(env.HostToml, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func openStore(t *testing.T, db string) *sql.DB {
	t.Helper()
	st, err := store.OpenShared(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st.DB
}

func history(t *testing.T, db *sql.DB, repo string) []runs.Row {
	t.Helper()
	rows, err := runs.History(context.Background(), db, repo)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func tickState(t *testing.T, db *sql.DB, repo string) dispatch.TickState {
	t.Helper()
	s, err := dispatch.LoadTickState(context.Background(), db, repo)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// writes are the REST POST paths the server received: comments and reactions, not GraphQL reads.
func writes(srv *forgetest.Server) []string {
	var out []string
	for _, p := range srv.Posts {
		if p.Path != "/graphql" {
			out = append(out, p.Path)
		}
	}
	return out
}

// bodies are the bodies of the comments the server holds on issue n.
func bodies(srv *forgetest.Server, n int) []string {
	var out []string
	for _, c := range srv.IssueComments(n) {
		b, _ := c["body"].(string)
		out = append(out, b)
	}
	return out
}

func TestTickStartsNewIssue(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, strings.Replace(completeRepo, "[controller]\n",
		"[controller]\nprofile = \"software\"\nbij_rondegrens = \"vraag\"\n", 1)))
	appendHostToml(t, env, "\n[dispatch.profiles.software]\nmcp_config = \"/etc/lh/mcp.json\"\n")
	srv, w := startable(t, &env)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	if want := "lighthouse: gathered o/r: 1 items (first)\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
	posted := bodies(srv, 7)
	if len(posted) != 1 {
		t.Fatalf("comments on #7 = %q, want one Started", posted)
	}
	h, ok := protocol.ParseHeader(posted[0])
	if !ok || h.Kind != protocol.KindStarted || h.Fields["command"] != "/lighthouse-start-issue 7" ||
		h.Fields["branch"] != "issue-7-issue" || h.Fields["workspace"] != "ws-issue-7-issue" {
		t.Errorf("posted %q, want a Started of /lighthouse-start-issue 7 on issue-7-issue in ws-issue-7-issue", posted[0])
	}
	conn := openStore(t, db)
	rows := history(t, conn, "o/r")
	if len(rows) != 1 {
		t.Fatalf("runs = %+v, want one", rows)
	}
	if r := rows[0]; !r.Open || r.Issue != 7 || r.TriggerKey != "start:7" || r.Branch != "issue-7-issue" ||
		r.Profile != "software" || r.SessionID == "" || r.SessionID != h.Session {
		t.Errorf("run = %+v, want open start:7 on issue-7-issue in profile software with the session of the Started (%q)", r, h.Session)
	}
	spawns := w.spawned()
	if len(spawns) != 1 {
		t.Fatalf("spawns = %+v, want one", spawns)
	}
	if l := spawns[0]; l.Prompt != "/lighthouse-start-issue 7 (issue #7, controller-run)" || l.Mode != "start" ||
		l.Profile != "software" || l.MCPConfig != "/etc/lh/mcp.json" || !strings.HasPrefix(l.InstructionsFile, env.StateDir+string(filepath.Separator)) {
		t.Errorf("spawn = %+v, want a start with the controller-run prompt, profile software, its mcp_config and instructions under %s", l, env.StateDir)
	}
	if text, err := os.ReadFile(spawns[0].InstructionsFile); err != nil || !strings.Contains(string(text), "asks at the round limit") {
		t.Errorf("instructions lack the round-limit appendix of bij_rondegrens = \"vraag\" (err %v)", err)
	}
	if s := tickState(t, conn, "o/r"); s.Fingerprint == "" || !s.FullAt.Equal(t0) {
		t.Errorf("tick state = %+v, want a fingerprint gathered at %v", s, t0)
	}
}

// handoffComment is a 🤖 Handoff by owner on issue 7 with next and when.
func handoffComment(next, when string) map[string]any {
	return map[string]any{"id": 30, "created_at": "2026-10-31T11:00:00Z",
		"html_url": "https://github.com/o/r/issues/7#issuecomment-30", "user": map[string]any{"login": "owner"},
		"body": "🤖 Handoff\nissue: #7\nworktree: /work/issue-7-issue\nbranch: issue-7-issue\n" +
			"next: " + next + "\nwhen: " + when + "\n"}
}

// o/s, with nothing on its board, keeps its own fingerprint: a deferral holds back only the
// repo it is in.
func TestTickDeferredKeepsFingerprint(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo), repoDir(t, strings.Replace(completeRepo, `repo = "o/r"`, `repo = "o/s"`, 1)))
	srv, w := startable(t, &env)
	useWorkplaces(t, &env, map[string]*fakeWorkplace{"o/r": w, "o/s": newFakeWorkplace(t, "o/s")})
	srv.Comments = map[int][]map[string]any{7: {handoffComment("/superpowers:subagent-driven-development /work/issue-7-issue/plans/plan.md", "night")}}
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	if want := "lighthouse: gathered o/r: 1 items (first)\nlighthouse: gathered o/s: 0 items (first)\n" +
		"lighthouse: deferred o/r#7: night\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q (12:00 is outside the night window 21:00-08:00)", errOut.String(), want)
	}
	if p := writes(srv); len(p) != 0 || len(w.spawned()) != 0 {
		t.Errorf("writes %v, spawns %v; want none for a deferred start", p, w.spawned())
	}
	conn := openStore(t, db)
	if rows := history(t, conn, "o/r"); len(rows) != 0 {
		t.Errorf("runs = %+v, want none", rows)
	}
	if s := tickState(t, conn, "o/r"); !reflect.DeepEqual(s, dispatch.TickState{}) {
		t.Errorf("tick state = %+v after a deferral, want none", s)
	}
	if s := tickState(t, conn, "o/s"); s.Fingerprint == "" {
		t.Errorf("tick state of o/s = %+v, want its fingerprint saved", s)
	}

	// Nothing on the board changed, but without a saved fingerprint the night tick gathers
	// again, and starts.
	env.Now = func() time.Time { return time.Date(2026, 10, 31, 22, 0, 0, 0, time.UTC) }
	errOut.Reset()
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("night tick = %v\n%s", err, errOut.String())
	}
	if want := "lighthouse: gathered o/r: 1 items (first)\nlighthouse: gathered o/s: 0 items (stale)\n"; errOut.String() != want {
		t.Errorf("night tick: errOut = %q, want %q", errOut.String(), want)
	}
	if rows := history(t, conn, "o/r"); len(rows) != 1 || rows[0].TriggerKey != "handoff:30" {
		t.Errorf("runs after the night tick = %+v, want the start of handoff:30", rows)
	}
}

// The skills of the repo's main checkout decide whether a /lighthouse- command can start.
func TestTickJudgesInstalledSkills(t *testing.T) {
	for name, tc := range map[string]struct {
		installed bool
		want      string // the kind of the comment tick posts
	}{
		"installed": {true, "Started"},
		"missing":   {false, "Returned"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := repoDir(t, strings.Replace(completeRepo, "[controller]\n", "[controller]\nplan_review = true\n", 1))
			if tc.installed {
				skill := filepath.Join(dir, ".claude", "skills", "lighthouse-review-plan")
				if err := os.MkdirAll(skill, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\n---\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			env, db := tickFixture(t, dir)
			srv, _ := startable(t, &env)
			srv.Comments = map[int][]map[string]any{7: {handoffComment("/lighthouse-review-plan /work/issue-7-issue/plans/plan.md", "now")}}
			var errOut bytes.Buffer
			if err := tick(context.Background(), env, db, &errOut); err != nil {
				t.Fatalf("tick = %v\n%s", err, errOut.String())
			}
			posted := bodies(srv, 7)
			if len(posted) != 2 || !strings.HasPrefix(posted[1], "🤖 "+tc.want+"\n") {
				t.Errorf("comments on #7 = %q, want the handoff and then a %s", posted, tc.want)
			}
		})
	}
}

func TestTickSecondTickDoesNotRestart(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, w := startable(t, &env)
	for _, step := range []struct {
		after time.Duration
		want  string
	}{
		{0, "lighthouse: gathered o/r: 1 items (first)\n"},
		{3 * time.Minute, "lighthouse: gathered o/r: 1 items (changed)\n"}, // the open run changed the fingerprint
	} {
		env.Now = func() time.Time { return t0.Add(step.after) }
		var errOut bytes.Buffer
		if err := tick(context.Background(), env, db, &errOut); err != nil {
			t.Fatalf("tick at +%v = %v\n%s", step.after, err, errOut.String())
		}
		if errOut.String() != step.want {
			t.Errorf("tick at +%v: errOut = %q, want %q", step.after, errOut.String(), step.want)
		}
	}
	if posted := bodies(srv, 7); len(posted) != 1 {
		t.Errorf("comments on #7 = %q, want one Started", posted)
	}
	if rows := history(t, openStore(t, db), "o/r"); len(rows) != 1 || !rows[0].Open {
		t.Errorf("runs = %+v, want one open run", rows)
	}
	if n := len(w.spawned()); n != 1 {
		t.Errorf("spawns = %d, want 1", n)
	}
}

// A `lighthouse run` claims issue 7 while the tick reads its comments: its row is in the
// database before its Started is on the issue. The tick sees the Started and must also see
// the row, so it hands nothing back as an orphan and starts nothing.
func TestTickRunClaimsDuringGather(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, w := startable(t, &env)
	conn := openStore(t, db)
	const session = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	var once sync.Once
	srv.BeforeComments = func(n int) {
		if n != 7 {
			return
		}
		once.Do(func() {
			_, err := runs.Take(context.Background(), conn, runs.Claim{Repo: "o/r", Issue: 7, SessionID: session,
				Mode: "start", Trigger: "handoff", TriggerKey: "start:7", Command: "/lighthouse-start-issue 7",
				Prompt: "/lighthouse-start-issue 7 (issue #7, controller-run)", Branch: "issue-7-issue", Now: t0})
			if err != nil {
				t.Errorf("claim of the concurrent run: %v", err)
			}
			srv.AddComment(7, "lh-bot", protocol.RenderStarted(protocol.Started{Session: session,
				Worktree: "/work/issue-7-issue", Command: "/lighthouse-start-issue 7", Start: "2026-10-31T12:00:00Z",
				Branch: "issue-7-issue", Runner: "forge", Workspace: "ws-issue-7-issue"}, "en"))
		})
	}
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	if want := "lighthouse: gathered o/r: 1 items (first)\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
	if p := writes(srv); len(p) != 0 {
		t.Errorf("tick wrote %v, want nothing: the Started has its row", p)
	}
	if rows := history(t, conn, "o/r"); len(rows) != 1 || !rows[0].Open || rows[0].SessionID != session {
		t.Errorf("runs = %+v, want only the open run of the concurrent claim", rows)
	}
	if n := len(w.spawned()); n != 0 {
		t.Errorf("spawns = %d, want 0", n)
	}
}

func TestTickFailedRepoKeepsNoFingerprint(t *testing.T) {
	first := repoDir(t, completeRepo)
	second := repoDir(t, strings.Replace(completeRepo, `repo = "o/r"`, `repo = "o/s"`, 1))
	env, db := tickFixture(t, first, second)
	srv, wr := startable(t, &env)
	ws := newFakeWorkplace(t, "o/s")
	useWorkplaces(t, &env, map[string]*fakeWorkplace{"o/r": wr, "o/s": ws})
	srv.BoardNodes = append(srv.BoardNodes, boardNode("I8", "c1", issueContent(8, "Ander", map[string]any{
		"repository": map[string]any{"nameWithOwner": "o/s"}})))
	srv.Fail = map[string]int{"POST /repos/o/r/issues/7/comments": 500}

	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); !errors.Is(err, errSilent) {
		t.Fatalf("tick = %v, want errSilent\n%s", err, errOut.String())
	}
	if !strings.Contains(errOut.String(), "\nlighthouse: apply o/r: o/r#7 start: post: ") {
		t.Errorf("errOut = %q, want an apply line for o/r naming the failed post", errOut.String())
	}
	if strings.Contains(errOut.String(), "apply o/s") {
		t.Errorf("errOut = %q, want no apply line for o/s", errOut.String())
	}
	conn := openStore(t, db)
	if s := tickState(t, conn, "o/r"); !reflect.DeepEqual(s, dispatch.TickState{}) {
		t.Errorf("tick state of o/r = %+v after its failed post, want none", s)
	}
	if s := tickState(t, conn, "o/s"); s.Fingerprint == "" {
		t.Errorf("tick state of o/s = %+v, want its fingerprint saved", s)
	}
	if rows := history(t, conn, "o/r"); len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.StartFailed {
		t.Errorf("runs of o/r = %+v, want one closed as start_failed", rows)
	}
	if rows := history(t, conn, "o/s"); len(rows) != 1 || !rows[0].Open {
		t.Errorf("runs of o/s = %+v, want one open run", rows)
	}
	if len(wr.spawned()) != 0 || len(ws.spawned()) != 1 {
		t.Errorf("spawns o/r %d, o/s %d; want 0 and 1", len(wr.spawned()), len(ws.spawned()))
	}
}

// The stand of an open run's session is part of the fingerprint: a session that turns Done
// makes the next tick gather, so a green PR gets its Done without waiting for a stale gather.
func TestTickSessionStandRegathers(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	_, w := startable(t, &env)
	for _, step := range []struct {
		after time.Duration
		state string // "" leaves the sessions as they are
		want  string
	}{
		{0, "", "lighthouse: gathered o/r: 1 items (first)\n"},
		{3 * time.Minute, "", "lighthouse: gathered o/r: 1 items (changed)\n"},
		{6 * time.Minute, "", ""},
		{9 * time.Minute, "done", "lighthouse: gathered o/r: 1 items (changed)\n"},
	} {
		if step.state != "" {
			w.setState(step.state)
		}
		env.Now = func() time.Time { return t0.Add(step.after) }
		var errOut bytes.Buffer
		if err := tick(context.Background(), env, db, &errOut); err != nil {
			t.Fatalf("tick at +%v = %v\n%s", step.after, err, errOut.String())
		}
		if errOut.String() != step.want {
			t.Errorf("tick at +%v: errOut = %q, want %q", step.after, errOut.String(), step.want)
		}
	}
}

// measureWorkplace answers Sessions from a fixed list per workspace, or with the error of that
// workspace, and Transcript from dirs: a workspace without a dir is not measurable.
type measureWorkplace struct {
	fakeWorkplace
	sessions map[string][]workplace.Session
	errs     map[string]error
	dirs     map[string]string
}

func (w *measureWorkplace) Sessions(_ context.Context, workspaceID string) ([]workplace.Session, error) {
	return w.sessions[workspaceID], w.errs[workspaceID]
}

func (w *measureWorkplace) Transcript(_ context.Context, workspaceID, sessionID string) (workplace.Transcript, error) {
	if dir, ok := w.dirs[workspaceID]; ok {
		return workplace.ReadTranscript(dir, sessionID)
	}
	return workplace.Transcript{}, fmt.Errorf("no project dir for %s: %w", workspaceID, workplace.ErrNotMeasurable)
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const (
	sidMeasured  = "11111111-1111-4111-8111-111111111111"
	sidGone      = "22222222-2222-4222-8222-222222222222"
	sidUncertain = "33333333-3333-4333-8333-333333333333"
	sidDown      = "44444444-4444-4444-8444-444444444444"
	sidNoFile    = "55555555-5555-4555-8555-555555555555"
)

// Per open run with a workspace, measure reads the stand of its session and what lies on disk
// for it: the transcript, the permission note and the resume marker.
func TestTickMeasuresRunState(t *testing.T) {
	transcripts := t.TempDir()
	writeTestFile(t, filepath.Join(transcripts, sidMeasured+".jsonl"),
		`{"type":"user","timestamp":"2026-10-31T11:50:00Z"}`+"\n"+`{"type":"assistant","timestamp":"2026-10-31T11:55:00Z"}`+"\n")
	updated := time.Date(2026, 10, 31, 11, 40, 0, 0, time.UTC)
	w := &measureWorkplace{
		sessions: map[string][]workplace.Session{
			"ws-1": {
				{Key: "other", WorkspaceID: "ws-1", State: "done", UpdatedAt: updated.Add(time.Hour)},
				{Key: "mine", WorkspaceID: "ws-1", State: "working", SessionID: sidMeasured, UpdatedAt: updated},
			},
			"ws-6": {{Key: "k6", WorkspaceID: "ws-6", State: "idle", SessionID: sidNoFile, UpdatedAt: updated}},
		},
		errs: map[string]error{
			"ws-gone": fmt.Errorf("read sessions of ws-gone: 404: %w", workplace.ErrGone),
			"ws-down": errors.New("forge down"),
		},
		dirs: map[string]string{"ws-1": transcripts, "ws-6": transcripts},
	}
	env := tickEnv{StateDir: t.TempDir(), Workplace: func(repoconfig.Repo, string, string) workplace.Workplace { return w }}
	writeTestFile(t, filepath.Join(env.StateDir, "runs", sidMeasured, "permission.json"),
		`{"version":1,"at":"2026-10-31T11:56:00Z","tool":"Bash","path":"rm -rf build","thread":"","message":"m"}`+"\n")
	open := []runs.OpenRun{
		{ID: 1, Issue: 7, SessionID: sidMeasured, WorkspaceID: "ws-1", WorkplaceSession: "mine"},
		{ID: 2, Issue: 8, SessionID: sidGone, WorkspaceID: "ws-gone", WorkplaceSession: "k2"},
		{ID: 3, Issue: 9, SessionID: sidUncertain, WorkspaceID: "ws-3"}, // no session key yet
		{ID: 4, Issue: 10, SessionID: sidDown, WorkspaceID: "ws-down", WorkplaceSession: "k4"},
		{ID: 5, Issue: 11, SessionID: "66666666-6666-4666-8666-666666666666"}, // no workspace yet
		{ID: 6, Issue: 12, SessionID: sidNoFile, WorkspaceID: "ws-6", WorkplaceSession: "k6"},
	}
	var errOut bytes.Buffer
	gotStands, gotRuns, err := measure(context.Background(), env, hostconfig.Dispatch{}, repoconfig.Repo{Slug: "o/r"}, open, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	wantStands := map[int64]workplace.Stand{1: workplace.Working, 6: workplace.Waiting}
	if !reflect.DeepEqual(gotStands, wantStands) {
		t.Errorf("stands = %v, want %v", gotStands, wantStands)
	}
	wantRuns := map[int64]dispatch.RunState{
		1: {
			SessionAt: updated,
			Transcript: &workplace.Transcript{
				LastEvent:     time.Date(2026, 10, 31, 11, 55, 0, 0, time.UTC),
				Threads:       map[string]time.Time{"": time.Date(2026, 10, 31, 11, 55, 0, 0, time.UTC)},
				AssistantTurn: true,
			},
			Permission: &dispatch.Permission{At: time.Date(2026, 10, 31, 11, 56, 0, 0, time.UTC), Tool: "Bash", Path: "rm -rf build"},
		},
		2: {Gone: true},
		3: {TranscriptUnmeasurable: true}, // no project dir on this host: measured, but the transcript is not (#514)
		6: {SessionAt: updated},           // no transcript file yet: not measurable, no line
	}
	if !reflect.DeepEqual(gotRuns, wantRuns) {
		t.Errorf("runs =\n%+v\nwant\n%+v", gotRuns, wantRuns)
	}
	if want := "lighthouse: measure o/r: run 4: forge down\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
}

// One unreadable file leaves its run without any measurement: a half measurement would read
// as "no permission prompt".
func TestMeasureFileErrorLeavesRunUnmeasured(t *testing.T) {
	w := &measureWorkplace{sessions: map[string][]workplace.Session{
		"ws-1": {{Key: "mine", WorkspaceID: "ws-1", State: "working"}},
	}}
	env := tickEnv{StateDir: t.TempDir(), Workplace: func(repoconfig.Repo, string, string) workplace.Workplace { return w }}
	writeTestFile(t, filepath.Join(env.StateDir, "runs", sidMeasured, "permission.json"), `{"version":1,"at":`)
	open := []runs.OpenRun{{ID: 1, Issue: 7, SessionID: sidMeasured, WorkspaceID: "ws-1", WorkplaceSession: "mine"}}
	var errOut bytes.Buffer
	gotStands, gotRuns, err := measure(context.Background(), env, hostconfig.Dispatch{}, repoconfig.Repo{Slug: "o/r"}, open, &errOut)
	if err != nil {
		t.Fatalf("measure = %v, want no error: an unreadable run is not measurable", err)
	}
	if len(gotStands) != 0 || len(gotRuns) != 0 {
		t.Errorf("stands %v, runs %v; want none", gotStands, gotRuns)
	}
	if !strings.HasPrefix(errOut.String(), "lighthouse: measure o/r: run 1: ") || strings.Count(errOut.String(), "\n") != 1 {
		t.Errorf("errOut = %q, want one measure line for run 1", errOut.String())
	}
}

// A workplace that cannot list the sessions of an uncertain start costs that run its key and
// its measurement, loudly, but the repo is still gathered.
func TestTickAdoptFailureIsLoudNotFatal(t *testing.T) {
	dir := repoDir(t, completeRepo)
	env, db := tickFixture(t, dir)
	srv := fakeForge(t, &env)
	srv.Roles = map[string]string{"lh-bot": "admin"}
	env.Now = func() time.Time { return t0 }
	w := &measureWorkplace{errs: map[string]error{"ws-7": errors.New("forge down")}}
	env.Workplace = func(repoconfig.Repo, string, string) workplace.Workplace { return w }
	conn := openStore(t, db)
	ctx := context.Background()
	run, err := runs.Take(ctx, conn, runs.Claim{Repo: "o/r", Issue: 7, SessionID: sidUncertain, Mode: "start",
		Trigger: "handoff", TriggerKey: "start:7", Branch: "issue-7-issue", Now: t0.Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.SetWorkplace(ctx, conn, run.ID, "ws-7", "issue-7-issue"); err != nil {
		t.Fatal(err)
	}
	repo, err := repoconfig.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	in, _, _, err := gatherRepo(ctx, env, conn, hostconfig.Dispatch{}, hostFacts{}, repo, botForge(t, env, repo), &errOut)
	if err != nil || in == nil {
		t.Fatalf("gatherRepo = %v, %v, want the repo gathered\n%s", in, err, errOut.String())
	}
	if _, ok := in.Runs[run.ID]; ok {
		t.Errorf("runs = %v, want run %d unmeasured", in.Runs, run.ID)
	}
	want := "lighthouse: measure o/r: sessions of run 1: forge down\n" +
		"lighthouse: measure o/r: run 1: forge down\n" +
		"lighthouse: gathered o/r: 0 items (first)\n"
	if errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
}

// tick and run decide with the limits of host.toml [dispatch] and the brake of the tick.
func TestDecideInputCarriesLimits(t *testing.T) {
	env := tickEnv{Now: func() time.Time { return t0 }}
	cfg := hostconfig.Dispatch{Night: "21:00-08:00", MaxWait: 90 * time.Minute, MaxRuntime: 8 * time.Hour}
	brake := &dispatch.Brake{Paused: true}
	got := decideInput(env, cfg, nil, brake)
	want := dispatch.Input{Now: t0, Night: "21:00-08:00", Brake: brake,
		MaxWait: dispatch.Duration(90 * time.Minute), MaxRuntime: dispatch.Duration(8 * time.Hour)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decideInput = %+v, want %+v", got, want)
	}
}

// An uncertain start whose session shows up in this tick is adopted before the board half of
// the gather reads the open runs: the snapshot has its session key, and its stand counts.
func TestTickAdoptsBeforePrepare(t *testing.T) {
	dir := repoDir(t, completeRepo)
	env, db := tickFixture(t, dir)
	_, w := startable(t, &env)
	conn := openStore(t, db)
	ctx := context.Background()
	const session = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	run, err := runs.Take(ctx, conn, runs.Claim{Repo: "o/r", Issue: 7, SessionID: session, Mode: "start",
		Trigger: "handoff", TriggerKey: "start:7", Branch: "issue-7-issue", Now: t0.Add(-2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.SetWorkplace(ctx, conn, run.ID, "ws-issue-7-issue", "issue-7-issue"); err != nil {
		t.Fatal(err)
	}
	w.sessions["ws-issue-7-issue"] = []workplace.Session{
		{Key: "key-late", WorkspaceID: "ws-issue-7-issue", State: "working", SessionID: session},
	}
	repo, err := repoconfig.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := hostconfig.Dispatch{MaxWait: time.Hour, MaxRuntime: 8 * time.Hour}
	var errOut bytes.Buffer
	in, _, _, err := gatherRepo(ctx, env, conn, cfg, hostFacts{}, repo, botForge(t, env, repo), &errOut)
	if err != nil || in == nil {
		t.Fatalf("gatherRepo = %v, %v\n%s", in, err, errOut.String())
	}
	if open := in.Snapshot.OpenRuns; len(open) != 1 || open[0].WorkplaceSession != "key-late" {
		t.Errorf("snapshot open runs = %+v, want run %d with key-late", open, run.ID)
	}
	if got, ok := in.Stands[run.ID]; !ok || got != workplace.Working {
		t.Errorf("stands = %v, want run %d working", in.Stands, run.ID)
	}
	if _, ok := in.Runs[run.ID]; !ok {
		t.Errorf("runs = %v, want a measurement of run %d", in.Runs, run.ID)
	}
	if want := "lighthouse: gathered o/r: 1 items (first)\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
}

// parkable is startable with issue 7 blocked by the open issue o/r#5, an executing handoff on
// it, and Dutch artefacts.
func parkable(t *testing.T) (tickEnv, string, *forgetest.Server) {
	t.Helper()
	env, db := tickFixture(t, repoDir(t, "[project]\nlanguage = \"nl\"\n\n"+completeRepo))
	srv, _ := startable(t, &env)
	srv.BoardNodes = []map[string]any{boardNode("I7", "c1", issueContent(7, "Issue", map[string]any{
		"blockedBy": map[string]any{"nodes": []any{map[string]any{
			"number": 5, "state": "OPEN", "repository": map[string]any{"nameWithOwner": "o/r"},
			"closedByPullRequestsReferences": map[string]any{"nodes": []any{}},
		}}},
	}))}
	srv.Comments = map[int][]map[string]any{7: {handoffComment("/superpowers:subagent-driven-development /work/issue-7-issue/plans/plan.md", "now")}}
	return env, db, srv
}

// statusSets are the item and option of every SetStatus the server received.
func statusSets(srv *forgetest.Server) []string {
	var out []string
	for _, v := range srv.StatusSets {
		out = append(out, fmt.Sprintf("%v %v", v["item"], v["option"]))
	}
	return out
}

func TestTickParksBlockedHandoff(t *testing.T) {
	env, db, srv := parkable(t)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	posted := bodies(srv, 7)
	if len(posted) != 2 || !strings.HasPrefix(posted[1], "🤖 Geparkeerd") {
		t.Errorf("comments on #7 = %q, want the handoff and then one Geparkeerd", posted)
	}
	// Parking is a give-back with blocked_issue: To do (t1), never On hold (h1).
	if got, want := statusSets(srv), []string{"I7 t1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("status sets = %q, want %q", got, want)
	}
	if got := fmt.Sprint(srv.Issues[7]["labels"]); !strings.Contains(got, "blocked-issue") {
		t.Errorf("labels of #7 = %s, want blocked-issue", got)
	}
	if rows := history(t, openStore(t, db), "o/r"); len(rows) != 0 {
		t.Errorf("runs = %+v, want none: a parked issue starts nothing", rows)
	}
}

// A park whose board change fails keeps its Geparkeerd and no fingerprint; the next tick sees
// its own Geparkeerd, posts nothing new and only sets the board.
func TestTickParkBoardFailureRetries(t *testing.T) {
	env, db, srv := parkable(t)
	const patch = "PATCH /users/o/projectsV2/3/items/5000" // item I7, project_number 3 of completeRepo
	srv.Fail, srv.FailMessage = map[string]int{patch: 500}, map[string]string{patch: "boom"}
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); !errors.Is(err, errSilent) {
		t.Fatalf("tick = %v, want errSilent\n%s", err, errOut.String())
	}
	if !strings.Contains(errOut.String(), "lighthouse: apply o/r: o/r#7 park: ") || !strings.Contains(errOut.String(), "boom") {
		t.Errorf("errOut = %q, want an apply line for the park naming boom", errOut.String())
	}
	if posted := bodies(srv, 7); len(posted) != 2 || !strings.HasPrefix(posted[1], "🤖 Geparkeerd") {
		t.Fatalf("comments on #7 = %q, want the handoff and then one Geparkeerd", posted)
	}
	if s := tickState(t, openStore(t, db), "o/r"); !reflect.DeepEqual(s, dispatch.TickState{}) {
		t.Errorf("tick state = %+v after the failed board change, want none", s)
	}

	srv.Fail = nil
	errOut.Reset()
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("second tick = %v\n%s", err, errOut.String())
	}
	if posted := bodies(srv, 7); len(posted) != 2 {
		t.Errorf("comments on #7 after the second tick = %q, want no new comment", posted)
	}
	// The failed SetStatus of the first tick is recorded too: StatusSets keeps every request.
	if got, want := statusSets(srv), []string{"I7 t1", "I7 t1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("status sets = %q, want %q", got, want)
	}
}

// An idle host with a fresh usage cache starts the new issue (the fixture fails the test when
// the meter refreshes or looks for claude).
func TestTickIdleHostFreshCacheStarts(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, w := startable(t, &env)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	if posted := bodies(srv, 7); len(posted) != 1 || !strings.HasPrefix(posted[0], "🤖 Started\n") {
		t.Errorf("comments on #7 = %q, want one Started", posted)
	}
	if len(w.spawned()) != 1 {
		t.Errorf("spawns = %+v, want one", w.spawned())
	}
}

// deferredRows are the key and value of the deferred rows of repo in tick_state.
func deferredRows(t *testing.T, db *sql.DB, repo string) map[string]string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT key, value FROM tick_state WHERE repo = ? AND kind = 'deferred'`, repo)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		out[k] = v
	}
	return out
}

// A usage at the halt threshold defers the start with its reason on stderr and in the tick
// state; no fingerprint is saved, so an open brake starts it on the next tick and leaves no row.
func TestTickUsageClosedDefers(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, w := startable(t, &env)
	setUsage(t, env, 90, 10)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	want := "lighthouse: gathered o/r: 1 items (first)\nlighthouse: deferred o/r#7: usage: session 90%, week 10%\n"
	if errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
	if p := writes(srv); len(p) != 0 || len(w.spawned()) != 0 {
		t.Errorf("writes %v, spawns %v; want none for a deferred start", p, w.spawned())
	}
	conn := openStore(t, db)
	if s := tickState(t, conn, "o/r"); !reflect.DeepEqual(s, dispatch.TickState{}) {
		t.Errorf("tick state = %+v after a deferral, want none", s)
	}
	rows := deferredRows(t, conn, "o/r")
	if len(rows) != 1 || !strings.Contains(rows["7"], "usage: session 90%, week 10%") {
		t.Errorf("deferred rows = %v, want one for issue 7 with the reason", rows)
	}

	setUsage(t, env, 10, 10)
	errOut.Reset()
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("second tick = %v\n%s", err, errOut.String())
	}
	if len(w.spawned()) != 1 {
		t.Errorf("spawns = %+v after the brake opened, want one", w.spawned())
	}
	if rows := deferredRows(t, conn, "o/r"); len(rows) != 0 {
		t.Errorf("deferred rows = %v after the start, want none", rows)
	}
}

// A pause drops the start without a deferred line and with the tick state saved; the flag is
// in the fingerprint, so the tick after `pause off` gathers again and starts.
func TestTickPausedStartsNothingAndRegathersAfterOff(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, w := startable(t, &env)
	flag, err := park.PauseFile(env.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err := park.WritePause(flag, t0.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	if want := "lighthouse: gathered o/r: 1 items (first)\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q: a pause defers nothing", errOut.String(), want)
	}
	conn := openStore(t, db)
	if p := writes(srv); len(p) != 0 || len(w.spawned()) != 0 {
		t.Errorf("writes %v, spawns %v; want none under a pause", p, w.spawned())
	}
	if s := tickState(t, conn, "o/r"); s.Fingerprint == "" {
		t.Errorf("tick state = %+v, want the fingerprint saved under a pause", s)
	}

	if removed, err := park.ClearPause(flag); err != nil || !removed {
		t.Fatalf("ClearPause = %v, %v", removed, err)
	}
	errOut.Reset()
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick after pause off = %v\n%s", err, errOut.String())
	}
	if want := "lighthouse: gathered o/r: 1 items (changed)\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
	if len(w.spawned()) != 1 {
		t.Errorf("spawns = %+v after pause off, want one", w.spawned())
	}
}

// loadView is the run and deferred views of repo o/r as the UI reads them.
func loadView(t *testing.T, db string) ([]dispatch.RunView, []dispatch.DeferredView) {
	t.Helper()
	rv, dv, err := dispatch.LoadView(context.Background(), openStore(t, db), []string{"o/r"})
	if err != nil {
		t.Fatal(err)
	}
	return rv, dv
}

// A tick writes one run view per open run: the board title, the measured stand and the command.
func TestTickWritesRunView(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	_, w := startable(t, &env)
	for _, after := range []time.Duration{0, 3 * time.Minute} {
		if after > 0 {
			w.setState("idle") // the session waits
		}
		env.Now = func() time.Time { return t0.Add(after) }
		var errOut bytes.Buffer
		if err := tick(context.Background(), env, db, &errOut); err != nil {
			t.Fatalf("tick at +%v = %v\n%s", after, err, errOut.String())
		}
	}
	rv, _ := loadView(t, db)
	if len(rv) != 1 {
		t.Fatalf("views = %+v, want one", rv)
	}
	if v := rv[0]; v.Issue != 7 || v.Title != "Issue" || v.State != "open" || v.Stand != "waiting" ||
		v.Command != "/lighthouse-start-issue 7" || v.WorkspaceID != "ws-issue-7-issue" {
		t.Errorf("view = %+v, want issue 7 \"Issue\", open, waiting, /lighthouse-start-issue 7 in ws-issue-7-issue", v)
	}
}

// The run view is written after apply: a run the tick starts is a view after that same tick,
// and a run it closes is gone after that same tick.
func TestTickRunViewFollowsApply(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	_, w := startable(t, &env)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("first tick = %v\n%s", err, errOut.String())
	}
	if len(w.spawned()) != 1 {
		t.Fatalf("spawns = %+v, want one", w.spawned())
	}
	if rv, _ := loadView(t, db); len(rv) != 1 || rv[0].Issue != 7 || rv[0].State != "open" {
		t.Errorf("views after the tick that starts #7 = %+v, want one open view of issue 7", rv)
	}

	w.setState("done") // without a handoff or question: apply closes the run as returned
	env.Now = func() time.Time { return t0.Add(3 * time.Minute) }
	errOut.Reset()
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("second tick = %v\n%s", err, errOut.String())
	}
	if rows := history(t, openStore(t, db), "o/r"); len(rows) != 1 || rows[0].Open {
		t.Fatalf("runs = %+v, want the one run closed", rows)
	}
	if rv, _ := loadView(t, db); len(rv) != 0 {
		t.Errorf("views after the tick that closes #7 = %+v, want none", rv)
	}
}

// A repo whose precheck passes but whose full gather fails still gets its run view: only a
// failed precheck keeps the rows of the previous tick.
func TestTickRunViewWhenGatherFails(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, w := startable(t, &env)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("first tick = %v\n%s", err, errOut.String())
	}
	// The stand changes, so the repo is due; the permission read inside the full gather fails.
	w.setState("idle")
	srv.Comments = map[int][]map[string]any{7: {{"id": 1, "body": "Hoi",
		"created_at": "2026-10-30T11:00:00Z", "html_url": "https://github.com/o/r/issues/7#issuecomment-1",
		"user": map[string]any{"login": "owner"}}}}
	srv.RoleStatus = 500
	at := t0.Add(3 * time.Minute)
	env.Now = func() time.Time { return at }
	errOut.Reset()
	if err := tick(context.Background(), env, db, &errOut); !errors.Is(err, errSilent) {
		t.Fatalf("second tick = %v, want errSilent\n%s", err, errOut.String())
	}
	if !strings.Contains(errOut.String(), "lighthouse: skip ") {
		t.Fatalf("errOut = %q, want a skip line for the failed gather", errOut.String())
	}
	if rv, _ := loadView(t, db); len(rv) != 1 || !rv[0].At.Equal(at) || rv[0].Stand != "waiting" {
		t.Errorf("views = %+v, want one waiting view of this tick (At %v)", rv, at)
	}
}

// A run that is parked for its resume marker is a view in state parked, without a stand.
func TestTickWritesParkedRunView(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, _ := startable(t, &env)
	srv.BoardNodes = []map[string]any{boardNode("I7", "h1", issueContent(7, "Issue", nil))} // On hold: nothing starts
	claim(t, openStore(t, db), "o/r", 7, "sess-p", "software", "handoff", "start:7", runs.Parked)
	writeMarker(t, env, "sess-p", true)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	rv, _ := loadView(t, db)
	if len(rv) != 1 {
		t.Fatalf("views = %+v, want one", rv)
	}
	if v := rv[0]; v.Issue != 7 || v.State != "parked" || v.Title != "Issue" || v.Stand != "" {
		t.Errorf("view = %+v, want issue 7 \"Issue\", parked, no stand", v)
	}
}

// A tick that is not due still rewrites the run rows: At is the tick, so the UI can tell a live
// tick from a stopped one. The clock moves a minute: At has a resolution of seconds.
func TestTickRunViewWhenNotDue(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, _ := startable(t, &env)
	srv.BoardNodes = []map[string]any{boardNode("I7", "h1", issueContent(7, "Issue", nil))} // On hold: nothing starts
	claim(t, openStore(t, db), "o/r", 7, "sess-o", "software", "handoff", "start:7", "")
	for _, step := range []struct {
		after time.Duration
		want  string
	}{
		{0, "lighthouse: gathered o/r: 1 items (first)\n"},
		{time.Minute, ""},
	} {
		env.Now = func() time.Time { return t0.Add(step.after) }
		var errOut bytes.Buffer
		if err := tick(context.Background(), env, db, &errOut); err != nil {
			t.Fatalf("tick at +%v = %v\n%s", step.after, err, errOut.String())
		}
		if errOut.String() != step.want {
			t.Errorf("tick at +%v: errOut = %q, want %q", step.after, errOut.String(), step.want)
		}
		rv, _ := loadView(t, db)
		if len(rv) != 1 || !rv[0].At.Equal(t0.Add(step.after)) {
			t.Errorf("tick at +%v: views = %+v, want one with At %v", step.after, rv, t0.Add(step.after))
		}
	}
}

// Classification: io with a fake forge. Fails when a tick that is not due reads the prio weights
// (open issues and milestones): only a gathered repo pays that read.
func TestTickNotDueReadsNoPrioInputs(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, _ := startable(t, &env)
	srv.BoardNodes = []map[string]any{boardNode("I7", "h1", issueContent(7, "Issue", nil))} // On hold: nothing starts
	for _, step := range []struct {
		after      time.Duration
		milestones int
	}{
		{0, 1},           // first: gathered, the weights are read once
		{time.Minute, 1}, // not due: no new read
	} {
		env.Now = func() time.Time { return t0.Add(step.after) }
		var errOut bytes.Buffer
		if err := tick(context.Background(), env, db, &errOut); err != nil {
			t.Fatalf("tick at +%v = %v\n%s", step.after, err, errOut.String())
		}
		if n := srv.Count("/repos/o/r/milestones"); n != step.milestones {
			t.Errorf("tick at +%v: milestone reads = %d, want %d\n%s", step.after, n, step.milestones, errOut.String())
		}
	}
}

// A deferred start carries the title of its board item.
func TestTickDeferredTitle(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	startable(t, &env)
	setUsage(t, env, 90, 10)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	_, dv := loadView(t, db)
	if len(dv) != 1 || dv[0].Issue != 7 || dv[0].Title != "Issue" {
		t.Errorf("deferred = %+v, want issue 7 with title \"Issue\"", dv)
	}
}

// A start that apply finds busy (another session works in the workspace) is no run and no
// action of Decide, but the Queue shows it with its reason and title.
func TestTickBusyWorkplaceSavedAsDeferred(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	_, w := startable(t, &env)
	w.sessions["ws-issue-7-issue"] = []workplace.Session{{Key: "other", WorkspaceID: "ws-issue-7-issue", State: "working", SessionID: "x"}}
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	_, dv := loadView(t, db)
	if len(dv) != 1 || dv[0].Issue != 7 || dv[0].Title != "Issue" || !strings.HasPrefix(dv[0].Reason, "workplace busy: session other") {
		t.Errorf("deferred = %+v, want issue 7 titled \"Issue\" with a workplace busy reason", dv)
	}
}

const (
	ws945     = "d7c0b0ea0de54804"
	branch945 = "work-issue-945-immich-smart-search-191b7750"
)

// sweepable is #945: issue 945 of o/r open on the board with status (an option id of repo945),
// a fake workplace for o/r and a git that answers from git. The four done runs on workspace
// ws945 are doneRuns945.
func sweepable(t *testing.T, env *tickEnv, status string, git *gitState) (*forgetest.Server, *fakeWorkplace) {
	t.Helper()
	srv := fakeForge(t, env)
	srv.Roles = map[string]string{"lh-bot": "admin"}
	srv.BoardNodes = []map[string]any{boardNode("I945", status, issueContent(945, "Immich smart search", nil))}
	env.Now = func() time.Time { return t0 }
	srv.Now = env.Now
	w := newFakeWorkplace(t, "o/r")
	useWorkplaces(t, env, map[string]*fakeWorkplace{"o/r": w})
	env.Run = git.runner(t)
	return srv, w
}

// repo945 is completeRepo with an In progress option p1.
func repo945(t *testing.T) string {
	return repoDir(t, strings.Replace(completeRepo, "\"On hold\" = \"h1\"\n", "\"On hold\" = \"h1\"\n\"In progress\" = \"p1\"\n", 1))
}

// doneRuns945 adds the four done runs of #945 on one workspace.
func doneRuns945(t *testing.T, db string) {
	t.Helper()
	ctx := context.Background()
	conn := openStore(t, db)
	for _, key := range []string{"start:945", "answer:1", "answer:2", "handoff:3"} {
		r, err := runs.Take(ctx, conn, runs.Claim{Repo: "o/r", Issue: 945, SessionID: runs.NewSessionID(), Mode: "start",
			Trigger: "handoff", TriggerKey: key, Now: t0.Add(-time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if err := runs.SetWorkplace(ctx, conn, r.ID, ws945, branch945); err != nil {
			t.Fatal(err)
		}
		if err := runs.Release(ctx, conn, r.ID, runs.Done, "", t0.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
}

// gitState is what the fake git answers: the porcelain status and the commits ahead of origin.
type gitState struct {
	mu            sync.Mutex
	status, ahead string
}

func (g *gitState) set(status, ahead string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.status, g.ahead = status, ahead
}

// runner answers git status, rev-parse and rev-list on /work/ws945; any other command fails the test.
func (g *gitState) runner(t *testing.T) workplace.Runner {
	return func(_ context.Context, _ time.Duration, name string, args ...string) (string, int, error) {
		joined := strings.Join(args, " ")
		if name != "git" || !strings.Contains(joined, "-C /work/"+ws945) {
			t.Errorf("unexpected command %s %s", name, joined)
			return "", -1, errors.New("unexpected command")
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		switch {
		case strings.HasSuffix(joined, "status --porcelain"):
			return g.status, 0, nil
		case strings.HasSuffix(joined, "rev-parse HEAD"):
			return "abc\n", 0, nil
		case strings.Contains(joined, "rev-list --count"):
			return g.ahead + "\n", 0, nil
		}
		t.Errorf("unexpected git %s", joined)
		return "", -1, errors.New("unexpected git")
	}
}

func leftovers(t *testing.T, db string) []workplace.Leftover {
	t.Helper()
	ls, err := workplace.Leftovers(context.Background(), openStore(t, db))
	if err != nil {
		t.Fatal(err)
	}
	return ls
}

// AC 1: the workspace of an issue that left Controller, clean and pushed, is removed once.
func TestTickSweepsWorkspaceOffController(t *testing.T) {
	env, db := tickFixture(t, repo945(t))
	_, w := sweepable(t, &env, "p1", &gitState{ahead: "0"})
	doneRuns945(t, db)

	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	if want := "lighthouse: removed workspace " + ws945 + " of o/r#945\n"; !strings.Contains(errOut.String(), want) {
		t.Errorf("errOut = %q, want it to contain %q", errOut.String(), want)
	}
	if _, _, removed := w.calls(); !reflect.DeepEqual(removed, []string{ws945}) {
		t.Errorf("removed = %v, want [%s]", removed, ws945)
	}

	errOut.Reset()
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("second tick = %v\n%s", err, errOut.String())
	}
	looked, asked, removed := w.calls()
	if len(looked) != 1 || len(asked) != 1 || len(removed) != 1 {
		t.Errorf("after two ticks: looked %v, asked %v, removed %v; want one of each", looked, asked, removed)
	}
}

// #121: the workspace of a closed scheduled run goes forcibly, without git, and its line names
// the schedule instead of an issue number.
func TestTickSweepForcesScheduledWorkspace(t *testing.T) {
	env, db := tickFixture(t, repo945(t))
	_, w := sweepable(t, &env, "p1", &gitState{status: " M main.go", ahead: "0"})
	ctx := context.Background()
	conn := openStore(t, db)
	r, err := runs.Take(ctx, conn, runs.Claim{Repo: "o/r", Schedule: "nightly", SessionID: runs.NewSessionID(), Mode: "start",
		Trigger: runs.TriggerScheduled, TriggerKey: "scheduled:nightly:20261116T0400", Now: t0.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.SetWorkplace(ctx, conn, r.ID, "ws-nightly", "scheduled/nightly-20261116T0400-1"); err != nil {
		t.Fatal(err)
	}
	if err := runs.Release(ctx, conn, r.ID, runs.Failed, "postcheck: outside the allowed paths", t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	var errOut bytes.Buffer
	if err := tick(ctx, env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	if want := "lighthouse: removed workspace ws-nightly of o/r scheduled:nightly\n"; !strings.Contains(errOut.String(), want) {
		t.Errorf("errOut = %q, want it to contain %q", errOut.String(), want)
	}
	if _, _, removed := w.calls(); !reflect.DeepEqual(removed, []string{"ws-nightly"}) {
		t.Errorf("removed = %v, want [ws-nightly]", removed)
	}
}

// AC 2: a dirty workspace stays and is listed with its path and reason.
func TestTickSweepKeepsDirtyWorkspace(t *testing.T) {
	env, db := tickFixture(t, repo945(t))
	_, w := sweepable(t, &env, "p1", &gitState{status: " M main.go", ahead: "0"})
	doneRuns945(t, db)

	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	if _, _, removed := w.calls(); len(removed) != 0 {
		t.Errorf("removed = %v, want none", removed)
	}
	if strings.Contains(errOut.String(), "workspace") {
		t.Errorf("errOut = %q, want no line for a leftover", errOut.String())
	}
	want := []workplace.Leftover{{Repo: "o/r", Issue: 945, WorkspaceID: ws945, Path: "/work/" + ws945, Reason: "uncommitted changes"}}
	if got := leftovers(t, db); !reflect.DeepEqual(got, want) {
		t.Errorf("leftovers = %+v, want %+v", got, want)
	}
}

func TestTickSweepLeavesControllerIssue(t *testing.T) {
	env, db := tickFixture(t, repo945(t))
	_, w := sweepable(t, &env, "c1", &gitState{ahead: "0"})
	doneRuns945(t, db)

	// The second tick is not due: the statuses still come from its precheck.
	var errOut bytes.Buffer
	for range 2 {
		_ = tick(context.Background(), env, db, &errOut)
	}
	if strings.Count(errOut.String(), "gathered o/r") != 1 {
		t.Errorf("errOut = %q, want one gathered line over two ticks", errOut.String())
	}
	looked, _, removed := w.calls()
	if slices.Contains(looked, ws945) || slices.Contains(removed, ws945) {
		t.Errorf("looked %v, removed %v; want %s untouched\n%s", looked, removed, ws945, errOut.String())
	}
}

// A parked issue now stands on To do (blocked_issue): its workspace stays for the resume as
// long as its last protocol comment is the tick's own Geparkeerd, and the second tick, on an
// unchanged board, reads none of its comments (the To do marks in tick_state). A To do issue
// without that Geparkeerd is swept like any other. Wiring sweepOne to the statuses alone, or
// dropping the marks, turns this red.
func TestTickSweepKeepsWorkplaceOfParkedToDo(t *testing.T) {
	const comments945 = "/repos/o/r/issues/945/comments"
	for _, tc := range []struct {
		name   string
		parked bool
	}{{"own Geparkeerd", true}, {"no Geparkeerd", false}} {
		t.Run(tc.name, func(t *testing.T) {
			env, db := tickFixture(t, repo945(t))
			srv, w := sweepable(t, &env, "t1", &gitState{ahead: "0"})
			srv.BoardNodes = []map[string]any{boardNode("I945", "t1", issueContent(945, "Immich smart search", map[string]any{
				"blockedBy": map[string]any{"nodes": []any{map[string]any{
					"number": 5, "state": "OPEN", "repository": map[string]any{"nameWithOwner": "o/r"},
					"closedByPullRequestsReferences": map[string]any{"nodes": []any{}},
				}}},
			}))}
			body := "een notitie"
			if tc.parked {
				body = "🤖 Geparkeerd\nblocked_by: o/r#5"
			}
			srv.Comments = map[int][]map[string]any{945: {{"id": 1, "body": body, "created_at": "2026-10-30T10:00:00Z",
				"user": map[string]any{"login": "lh-bot"}}}}
			doneRuns945(t, db)

			var errOut bytes.Buffer
			for range 2 {
				if err := tick(context.Background(), env, db, &errOut); err != nil {
					t.Fatalf("tick = %v\n%s", err, errOut.String())
				}
			}
			_, _, removed := w.calls()
			if got := slices.Contains(removed, ws945); got == tc.parked {
				t.Errorf("removed = %v; parked %v must keep %s\n%s", removed, tc.parked, ws945, errOut.String())
			}
			// Tick 1: the precheck reads the comments once, and the full gather once more when the
			// item is kept. Tick 2 reads none.
			want := 1
			if tc.parked {
				want = 2
			}
			if got := srv.Count(comments945); got != want {
				t.Errorf("comments of #945 read %d times over two ticks, want %d\n%s", got, want, errOut.String())
			}
		})
	}
}

// AC 4: a running run is never stopped or removed, also when its issue left Controller.
func TestTickSweepLeavesOpenRun(t *testing.T) {
	env, db := tickFixture(t, repo945(t))
	_, w := sweepable(t, &env, "p1", &gitState{ahead: "0"})
	doneRuns945(t, db)
	ctx := context.Background()
	conn := openStore(t, db)
	sid := runs.NewSessionID()
	r, err := runs.Take(ctx, conn, runs.Claim{Repo: "o/r", Issue: 945, SessionID: sid, Mode: "resume",
		Trigger: "answer", TriggerKey: "answer:9", Now: t0.Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.SetWorkplace(ctx, conn, r.ID, ws945, branch945); err != nil {
		t.Fatal(err)
	}
	if err := runs.SetSession(ctx, conn, r.ID, "k-945"); err != nil {
		t.Fatal(err)
	}
	w.sessions[ws945] = []workplace.Session{{Key: "k-945", WorkspaceID: ws945, State: "working", SessionID: sid}}

	var errOut bytes.Buffer
	_ = tick(ctx, env, db, &errOut)
	looked, _, removed := w.calls()
	if len(looked) != 0 || len(removed) != 0 {
		t.Errorf("looked %v, removed %v; want none\n%s", looked, removed, errOut.String())
	}
}

// Without a readable board no issue is known to have left Controller: nothing is swept.
func TestTickSweepSkipsRepoWithoutBoard(t *testing.T) {
	path := repo945(t)
	env, db := tickFixture(t, path)
	srv, w := sweepable(t, &env, "p1", &gitState{ahead: "0"})
	srv.Fail = map[string]int{"GET /users/o/projectsV2/3/items": 502}
	doneRuns945(t, db)

	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); !errors.Is(err, errSilent) {
		t.Fatalf("tick = %v, want errSilent\n%s", err, errOut.String())
	}
	if want := "lighthouse: skip " + path + ": "; !strings.HasPrefix(errOut.String(), want) {
		t.Errorf("errOut = %q, want a skip line", errOut.String())
	}
	looked, asked, removed := w.calls()
	if len(looked) != 0 || len(asked) != 0 || len(removed) != 0 {
		t.Errorf("looked %v, asked %v, removed %v; want none", looked, asked, removed)
	}
}

// The sweep runs every tick, also when the repo is not due: a leftover that became clean goes.
func TestTickSweepRunsWhenNotDue(t *testing.T) {
	env, db := tickFixture(t, repo945(t))
	git := &gitState{status: " M main.go", ahead: "0"}
	_, w := sweepable(t, &env, "p1", git)
	doneRuns945(t, db)

	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	if len(leftovers(t, db)) != 1 {
		t.Fatalf("leftovers after the first tick = %+v, want one", leftovers(t, db))
	}
	git.set("", "0")
	errOut.Reset()
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("second tick = %v\n%s", err, errOut.String())
	}
	if want := "lighthouse: removed workspace " + ws945 + " of o/r#945\n"; errOut.String() != want {
		t.Errorf("second tick errOut = %q, want only %q (no gathered line)", errOut.String(), want)
	}
	if _, _, removed := w.calls(); !reflect.DeepEqual(removed, []string{ws945}) {
		t.Errorf("removed = %v, want [%s]", removed, ws945)
	}
	if got := leftovers(t, db); len(got) != 0 {
		t.Errorf("leftovers = %+v, want none", got)
	}
}

// A Remove that fails with something other than a 409 is a cleanup line and exit 1; the
// workspace stays a candidate and is listed, so the next tick tries again.
func TestTickSweepRemoveFails(t *testing.T) {
	env, db := tickFixture(t, repo945(t))
	_, w := sweepable(t, &env, "p1", &gitState{ahead: "0"})
	w.removeErr = errors.New("delete workspace: status 500")
	doneRuns945(t, db)

	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); !errors.Is(err, errSilent) {
		t.Fatalf("tick = %v, want errSilent\n%s", err, errOut.String())
	}
	if want := "lighthouse: cleanup o/r: workspace " + ws945 + " of #945: "; !strings.Contains(errOut.String(), want) ||
		!strings.Contains(errOut.String(), "status 500") || strings.Contains(errOut.String(), "removed workspace") {
		t.Errorf("errOut = %q, want a cleanup line with the remove error and no removed line", errOut.String())
	}
	ws, err := runs.Workspaces(context.Background(), openStore(t, db), "o/r")
	if err != nil || len(ws) != 1 {
		t.Errorf("candidates = %+v, %v; want %s still there", ws, err, ws945)
	}
	if ls := leftovers(t, db); len(ls) != 1 || !strings.HasPrefix(ls[0].Reason, "not measurable: remove: ") {
		t.Errorf("leftovers = %+v, want one with not measurable: remove", ls)
	}
}

func TestTickWorkplaceFollowsHostToml(t *testing.T) {
	repo := repoconfig.Repo{Dir: "/srv/r", Slug: "o/r", Kind: "github", DefaultBranch: "trunk",
		Controller: &repoconfig.Controller{Profile: "software"}}
	env := tickEnv{Getenv: func(k string) string { return map[string]string{"HOME": "/home/u"}[k] }}
	load := func(body string) hostconfig.Dispatch {
		t.Helper()
		path := filepath.Join(t.TempDir(), "host.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := hostconfig.LoadDispatch(path)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}

	t.Run("local", func(t *testing.T) {
		cfg := load("[dispatch]\nworkplace = \"local\"\n")
		a, ok := env.workplace(cfg, repo, "/state", "", "").(*local.Adapter)
		if !ok {
			t.Fatalf("workplace = %T, want *local.Adapter", env.workplace(cfg, repo, "/state", "", ""))
		}
		if a.RepoDir != "/srv/r" || a.DefaultBranch != "trunk" || a.Repo != "o/r" ||
			a.StateDir != "/state" || a.Home != "/home/u" || a.Run == nil {
			t.Errorf("adapter = %+v", a)
		}
	})
	t.Run("unit prefix", func(t *testing.T) {
		cfg := load("[dispatch.forge]\nurl = \"http://localhost:1\"\nunit_prefix = \"other-\"\n")
		a, ok := env.workplace(cfg, repo, "/state", "node-a", "mac").(*kennforge.Adapter)
		if !ok {
			t.Fatalf("workplace = %T, want *kennforge.Adapter", env.workplace(cfg, repo, "/state", "node-a", "mac"))
		}
		if a.UnitPrefix != "other-" || a.Node != "node-a" || a.Profile != "mac" {
			t.Errorf("UnitPrefix, Node, Profile = %q, %q, %q; want %q, %q, %q", a.UnitPrefix, a.Node, a.Profile, "other-", "node-a", "mac")
		}
	})
	for name, cfg := range map[string]hostconfig.Dispatch{
		"forge url only": load("[dispatch.forge]\nurl = \"http://localhost:1\"\n"),
		"zero dispatch":  {},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := env.workplace(cfg, repo, "/state", "", "").(*kennforge.Adapter); !ok {
				t.Errorf("workplace = %T, want *kennforge.Adapter", env.workplace(cfg, repo, "/state", "", ""))
			}
		})
	}
}

const gitlabRepo = `[forge]
kind = "gitlab"
repo = "g/sub/p"

[forge.board.status_options]
"Controller" = "Controller"
"On hold" = "On hold"

[controller]

[usage_guard]
controller = true
`

// tick opens the forge of a GitLab repo whose board has only status_options: it is not refused
// for the project_number, project_id or status_field_id a GitHub board needs.
func TestTickOpensGitLabRepoWithoutProjectIDs(t *testing.T) {
	dir := repoDir(t, gitlabRepo)
	env, _ := tickFixture(t, dir)
	loaded := repoconfig.LoadAll([]string{dir})
	env.Getenv = func(k string) string {
		if k == "GITLAB_TOKEN" {
			return "tok"
		}
		return ""
	}
	out, err := openForges(context.Background(), env, loaded)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].err != nil {
		t.Fatalf("openForges = %+v, want one open forge", out)
	}
}

// Classification: io. Fails when the GitHub client of a tick does not remember enrichments,
// so a tick reads each open PR twice, or when an opener of serve, the pusher or postartefact
// does, so it acts on a stale mergeable.
func TestOnlyTickForgesMemoizeEnrichment(t *testing.T) {
	dir := repoDir(t, completeRepo)
	env, _ := tickFixture(t, dir)
	loaded := repoconfig.LoadAll([]string{dir})
	env.Getenv = func(k string) string { return map[string]string{"GH_TOKEN": "tok"}[k] }
	out, err := openForges(context.Background(), env, loaded)
	if err != nil || len(out) != 1 || out[0].err != nil {
		t.Fatalf("openForges = %+v, %v; want one open forge", out, err)
	}
	if gh, ok := out[0].rf.Forge.(*forge.GitHub); !ok || !gh.EnrichMemo {
		t.Errorf("tick forge = %T, EnrichMemo %v; want a *forge.GitHub with EnrichMemo", out[0].rf.Forge, ok && gh.EnrichMemo)
	}
	rf, err := newForgeOpener(env.Getenv, "").forRepo(context.Background(), loaded[0].Repo)
	if err != nil {
		t.Fatal(err)
	}
	if gh, ok := rf.Forge.(*forge.GitHub); !ok || gh.EnrichMemo {
		t.Errorf("other opener forge = %T, EnrichMemo %v; want a *forge.GitHub without EnrichMemo", rf.Forge, ok && gh.EnrichMemo)
	}
}

// A push-workflows handoff with [dispatch.workflow_push] in host.toml becomes a KindPush; the
// unconfigured repo hands it back instead. Apply knows no KindPush yet (the pusher start comes
// with the apply change), so the push shows as the one apply error of that kind and nothing is
// posted or spawned.
func TestTickPushHandoffYieldsKindPush(t *testing.T) {
	for name, tc := range map[string]struct {
		configured bool
		wantErr    string
		wantPosted string // the kind of the comment tick posts, "" when none
	}{
		"configured": {true, "", ""},
		"off":        {false, "", "Returned"},
	} {
		t.Run(name, func(t *testing.T) {
			env, db := tickFixture(t, repoDir(t, completeRepo))
			srv, w := startable(t, &env)
			if tc.configured {
				appendHostToml(t, env, "\n[dispatch.workflow_push]\ntoken_file = \"/run/t\"\n")
			}
			srv.Comments = map[int][]map[string]any{7: {handoffComment("push-workflows", "now")}}
			var started [][]string
			env.Spawn = func(_ context.Context, args, _ []string) error { started = append(started, args); return nil }
			var errOut bytes.Buffer
			err := tick(context.Background(), env, db, &errOut)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("tick = %v\n%s", err, errOut.String())
				}
			} else if err == nil || !strings.Contains(err.Error()+errOut.String(), tc.wantErr) {
				t.Fatalf("tick = %v\n%s\nwant an error with %q", err, errOut.String(), tc.wantErr)
			}
			posted := bodies(srv, 7)
			switch {
			case tc.wantPosted == "" && len(posted) != 1:
				t.Errorf("comments on #7 = %q, want the handoff only", posted)
			case tc.wantPosted != "" && (len(posted) != 2 || !strings.HasPrefix(posted[1], "🤖 "+tc.wantPosted+"\n") ||
				!strings.Contains(posted[1], "cd /work/issue-7-issue && git push -u origin HEAD")):
				t.Errorf("comments on #7 = %q, want a %s with the push command", posted, tc.wantPosted)
			}
			if len(w.spawned()) != 0 {
				t.Errorf("spawned %v, want no session", w.spawned())
			}
			if (len(started) == 1) != tc.configured || len(started) > 1 {
				t.Errorf("pusher starts = %v, want one when configured (%v)", started, tc.configured)
			}
		})
	}
}

// A Controller issue with a needs_* label starts no run: the tick moves it to To do and leaves
// the label, and a second tick finds nothing more to do. It proves the tick hands the label
// names of the repo to Decide (RepoInput.Labels in tick.go): with them empty the item counts as
// a free issue and the tick starts a run for it instead.
func TestTickNeedsLabelMovesToToDoWithoutRun(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, w := startable(t, &env)
	srv.BoardNodes = []map[string]any{boardNode("I7", "c1", issueContent(7, "Issue", map[string]any{
		"labels": map[string]any{"nodes": []any{map[string]any{"name": "needs-hands"}}}}))}
	srv.Issues = map[int]map[string]any{7: {"number": 7, "title": "Issue", "state": "open", "labels": []any{map[string]any{"name": "needs-hands"}}}}
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	if len(srv.StatusSets) != 1 || srv.StatusSets[0]["option"] != "t1" || srv.StatusSets[0]["item"] != "I7" {
		t.Errorf("StatusSets = %v, want one move of I7 to To do (t1)", srv.StatusSets)
	}
	if posted := bodies(srv, 7); len(posted) != 0 {
		t.Errorf("comments on #7 = %q, want none", posted)
	}
	if rows := history(t, openStore(t, db), "o/r"); len(rows) != 0 {
		t.Errorf("runs = %+v, want none", rows)
	}
	if s := w.spawned(); len(s) != 0 {
		t.Errorf("spawns = %+v, want none", s)
	}
}

// A node whose markers were not readable this tick reaches Decide as MarkersIncomplete: a parked
// run without a marker is then not a lost marker. Without it the gather leaves the field false.
func TestGatherRepoCarriesMarkersIncomplete(t *testing.T) {
	dir := repoDir(t, completeRepo)
	env, db := tickFixture(t, dir)
	srv := fakeForge(t, &env)
	srv.Roles = map[string]string{"lh-bot": "admin"}
	env.Now = func() time.Time { return t0 }
	conn := openStore(t, db)
	repo, err := repoconfig.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, incomplete := range []bool{true, false} {
		in, _, _, err := gatherRepo(context.Background(), env, conn, hostconfig.Dispatch{},
			hostFacts{MarkersIncomplete: incomplete}, repo, botForge(t, env, repo), &bytes.Buffer{})
		if err != nil || in == nil {
			t.Fatalf("gatherRepo = %v, %v", in, err)
		}
		if in.MarkersIncomplete != incomplete {
			t.Errorf("MarkersIncomplete = %v, want %v", in.MarkersIncomplete, incomplete)
		}
	}
}

// idleRun gives issue 7 a closed run under start:7, so newIssue finds its trigger spent: with no
// comment and no PR, issue 7 in Controller has no next step.
func idleRun(t *testing.T, db string) {
	t.Helper()
	ctx := context.Background()
	conn := openStore(t, db)
	r, err := runs.Take(ctx, conn, runs.Claim{Repo: "o/r", Issue: 7, SessionID: runs.NewSessionID(), Mode: "start",
		Trigger: "handoff", TriggerKey: "start:7", Now: t0.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.Release(ctx, conn, r.ID, runs.Handoff, "", t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func loadIdle(t *testing.T, db string) map[int]int {
	t.Helper()
	got, err := dispatch.LoadIdle(context.Background(), openStore(t, db), "o/r")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// Each full gather that finds issue 7 without a next step counts once; the third gives it back
// and clears its count. A full gather comes at most every FullGatherAfter on an unchanged board.
func TestTickIdleCounterAdvancesAndResets(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, _ := startable(t, &env)
	idleRun(t, db)
	wants := []map[int]int{{7: 1}, {7: 2}, {}}
	for i, want := range wants {
		at := t0.Add(time.Duration(i) * (dispatch.FullGatherAfter + time.Minute))
		env.Now = func() time.Time { return at }
		srv.Now = env.Now
		var errOut bytes.Buffer
		if err := tick(context.Background(), env, db, &errOut); err != nil {
			t.Fatalf("tick %d = %v\n%s", i+1, err, errOut.String())
		}
		if got := loadIdle(t, db); !reflect.DeepEqual(got, want) {
			t.Errorf("tick %d: idle = %v, want %v\n%s", i+1, got, want, errOut.String())
		}
		returned := slices.ContainsFunc(bodies(srv, 7), func(b string) bool {
			return strings.Contains(b, "reason: no next step after 3 evaluations; last protocol comment: none")
		})
		if returned != (i == 2) {
			t.Errorf("tick %d: returned = %v, want %v; comments %q", i+1, returned, i == 2, bodies(srv, 7))
		}
	}
}

// The idle mark is no action: apply never sees it, so it fails nothing and the fingerprint is saved.
func TestTickIdleActionNeverReachesApply(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, _ := startable(t, &env)
	idleRun(t, db)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	if strings.Contains(errOut.String(), "lighthouse: apply") {
		t.Errorf("errOut = %q, want no apply line", errOut.String())
	}
	if s := tickState(t, openStore(t, db), "o/r"); s.Fingerprint == "" {
		t.Errorf("fingerprint not saved after an idle tick:\n%s", errOut.String())
	}
	if w := writes(srv); len(w) != 0 {
		t.Errorf("writes = %q, want none", w)
	}
}

// A push by hand that the forge cannot check (BranchContains fails) costs no repo: the tick goes
// on with a line on errOut, but keeps the fingerprint unsaved so the next tick checks again
// (spec §4). The same tick with a forge that answers saves it.
func TestTickPushedCheckFailureHoldsFingerprint(t *testing.T) {
	const sha = "3f2a1bcd4e5f60718293a4b5c6d7e8f901234567"
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("fail=%v", fail), func(t *testing.T) {
			env, db := tickFixture(t, repoDir(t, completeRepo))
			srv, _ := startable(t, &env)
			ctx := context.Background()
			conn := openStore(t, db)
			r, err := runs.Take(ctx, conn, runs.Claim{Repo: "o/r", Issue: 7, Mode: "start", Trigger: "handoff",
				TriggerKey: "push:30", Command: "push-workflows", Branch: "issue-7-issue", Now: t0.Add(-time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			if err := runs.Release(ctx, conn, r.ID, runs.Question, "", t0.Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
			srv.Comments = map[int][]map[string]any{7: {{"id": 31, "created_at": "2026-10-31T11:00:00Z",
				"html_url": "https://github.com/o/r/issues/7#issuecomment-31", "user": map[string]any{"login": "owner"},
				"body": "🤖 Vraag\nsha: " + sha + "\nworktree: /work/issue-7-issue\nbranch: issue-7-issue\n\nRode stap: zizmor"}}}
			srv.SetCompare(sha, "issue-7-issue", "behind")
			if fail {
				srv.Fail = map[string]int{"GET /repos/o/r/compare/" + sha + "...issue-7-issue": 500}
			}
			var errOut bytes.Buffer
			if err := tick(ctx, env, db, &errOut); err != nil {
				t.Fatalf("tick = %v\n%s", err, errOut.String())
			}
			if skipped := strings.Contains(errOut.String(), "lighthouse: skip"); skipped {
				t.Errorf("errOut = %q, want the repo not skipped", errOut.String())
			}
			if line := strings.Contains(errOut.String(), "o/r#7: push by hand not checked"); line != fail {
				t.Errorf("errOut = %q, want the check line: %v", errOut.String(), fail)
			}
			if saved := tickState(t, conn, "o/r").Fingerprint != ""; saved == fail {
				t.Errorf("fingerprint saved = %v, want %v\n%s", saved, !fail, errOut.String())
			}
		})
	}
}

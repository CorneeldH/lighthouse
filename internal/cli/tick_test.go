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
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/usage"
	"github.com/CorneeldH/lighthouse/internal/workplace"
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
	vars := map[string]string{"XDG_RUNTIME_DIR": runtime, "GH_TOKEN": "tok",
		"XDG_DATA_HOME": t.TempDir(), "XDG_STATE_HOME": t.TempDir()}
	env := tickEnv{Getenv: func(k string) string { return vars[k] }, HostToml: host, StateDir: t.TempDir(),
		Workplace: func(r repoconfig.Repo) workplace.Workplace {
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
	if v != 5 {
		t.Errorf("user_version = %d, want 5", v)
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

func (w *fakeWorkplace) Remove(_ context.Context, workspaceID string) error {
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
	env.Workplace = func(r repoconfig.Repo) workplace.Workplace {
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
		l.Profile != "software" || l.MCPConfig != "/etc/lh/mcp.json" || !strings.HasPrefix(l.InstructionsFile, env.StateDir+"/") {
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
	srv.Comments = map[int][]map[string]any{7: {handoffComment("/superpowers:subagent-driven-development /work/plans/plan.md", "night")}}
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
	if s := tickState(t, conn, "o/r"); s != (dispatch.TickState{}) {
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
				skill := filepath.Join(dir, ".claude", "skills", "lighthouse-plan-adversarial-review")
				if err := os.MkdirAll(skill, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\n---\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			env, db := tickFixture(t, dir)
			srv, _ := startable(t, &env)
			srv.Comments = map[int][]map[string]any{7: {handoffComment("/lighthouse-plan-adversarial-review /work/plans/plan.md", "now")}}
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
	if s := tickState(t, conn, "o/r"); s != (dispatch.TickState{}) {
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
// workspace, and TranscriptDir from dirs: a workspace without a dir is not measurable.
type measureWorkplace struct {
	fakeWorkplace
	sessions map[string][]workplace.Session
	errs     map[string]error
	dirs     map[string]string
}

func (w *measureWorkplace) Sessions(_ context.Context, workspaceID string) ([]workplace.Session, error) {
	return w.sessions[workspaceID], w.errs[workspaceID]
}

func (w *measureWorkplace) TranscriptDir(_ context.Context, workspaceID string) (string, error) {
	if dir, ok := w.dirs[workspaceID]; ok {
		return dir, nil
	}
	return "", fmt.Errorf("no project dir for %s: %w", workspaceID, workplace.ErrNotMeasurable)
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
	env := tickEnv{StateDir: t.TempDir(), Workplace: func(repoconfig.Repo) workplace.Workplace { return w }}
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
		3: {},                   // nothing found yet: measured, not an error
		6: {SessionAt: updated}, // no transcript file yet: not measurable, no line
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
	env := tickEnv{StateDir: t.TempDir(), Workplace: func(repoconfig.Repo) workplace.Workplace { return w }}
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
	env.Workplace = func(repoconfig.Repo) workplace.Workplace { return w }
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
	in, _, _, err := gatherRepo(ctx, env, conn, hostconfig.Dispatch{}, hostFacts{}, repo, "tok", func() (string, error) { return "lh-bot", nil }, &errOut)
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
	login := func() (string, error) { return "lh-bot", nil }
	var errOut bytes.Buffer
	in, _, _, err := gatherRepo(ctx, env, conn, cfg, hostFacts{}, repo, "tok", login, &errOut)
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
	srv.Comments = map[int][]map[string]any{7: {handoffComment("/superpowers:subagent-driven-development /work/plans/plan.md", "now")}}
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
	if got, want := statusSets(srv), []string{"I7 h1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("status sets = %q, want %q", got, want)
	}
	if rows := history(t, openStore(t, db), "o/r"); len(rows) != 0 {
		t.Errorf("runs = %+v, want none: a parked issue starts nothing", rows)
	}
}

// A park whose board change fails keeps its Geparkeerd and no fingerprint; the next tick sees
// its own Geparkeerd, posts nothing new and only sets the board.
func TestTickParkBoardFailureRetries(t *testing.T) {
	env, db, srv := parkable(t)
	srv.FailOps = map[string]string{"SetStatus": "boom"}
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
	if s := tickState(t, openStore(t, db), "o/r"); s != (dispatch.TickState{}) {
		t.Errorf("tick state = %+v after the failed board change, want none", s)
	}

	srv.FailOps = nil
	errOut.Reset()
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("second tick = %v\n%s", err, errOut.String())
	}
	if posted := bodies(srv, 7); len(posted) != 2 {
		t.Errorf("comments on #7 after the second tick = %q, want no new comment", posted)
	}
	// The failed SetStatus of the first tick is recorded too: StatusSets keeps every request.
	if got, want := statusSets(srv), []string{"I7 h1", "I7 h1"}; !reflect.DeepEqual(got, want) {
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
	if s := tickState(t, conn, "o/r"); s != (dispatch.TickState{}) {
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
	srv.FailOps = map[string]string{"Board": "board down"}
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

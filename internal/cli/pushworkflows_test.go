package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/testpath"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// runPushWorkflows runs the command with args on env and returns its error.
func runPushWorkflows(env pushEnv, args []string) error {
	cmd := pushWorkflowsCommand(env)
	cmd.SetArgs(args[1:]) // args[0] is the subcommand name
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	return cmd.ExecuteContext(context.Background())
}

func TestPusherResolvesSameDBAndPauseFile(t *testing.T) {
	state, controller, home := t.TempDir(), t.TempDir(), t.TempDir()
	// Tick's environment.
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("LIGHTHOUSE_CONTROLLER_STATE", controller)
	testpath.SetHome(t, home)
	tickDB, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	tickPause, err := park.PauseFile(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	db, pause, clone, err := pusherPaths(os.Getenv, "", "o/r")
	if err != nil {
		t.Fatal(err)
	}
	sent := pushOptions{repo: "o/r", issue: 7, worktree: testpath.Abs("w", "x"), runID: 3, db: db, pauseFile: pause,
		cloneDir: clone, stateDir: testpath.Abs("s"), hostToml: testpath.Abs("h", "host.toml"), permit: strings.Repeat("a", 40)}
	args := pushWorkflowsArgs(sent)

	// The pusher's cleaned environment: neither variable is set.
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("LIGHTHOUSE_CONTROLLER_STATE", "")
	var got pushOptions
	env := pushEnv{Getenv: os.Getenv, Run: func(_ context.Context, _ pushEnv, o pushOptions) error { got = o; return nil }}
	if err := runPushWorkflows(env, args); err != nil {
		t.Fatalf("push-workflows %v: %v", args, err)
	}
	if got.db != tickDB || got.pauseFile != tickPause {
		t.Errorf("pusher opens db %s and pause %s, tick uses %s and %s", got.db, got.pauseFile, tickDB, tickPause)
	}
	if !reflect.DeepEqual(got, sent) {
		t.Errorf("flags round trip to %+v, want %+v", got, sent)
	}
	if want := filepath.Join(state, "lighthouse", "push", "o", "r.git"); clone != want {
		t.Errorf("clone %s, want %s under tick's state home", clone, want)
	}
	// Without --db or --pause-file the pusher resolves nothing of its own.
	for _, drop := range []string{"--db", "--pause-file"} {
		var short []string
		for i := 0; i < len(args); i++ {
			if args[i] == drop {
				i++
				continue
			}
			short = append(short, args[i])
		}
		ran := false
		env.Run = func(context.Context, pushEnv, pushOptions) error { ran = true; return nil }
		if err := runPushWorkflows(env, short); err == nil || ran || !strings.Contains(err.Error(), drop) {
			t.Errorf("push-workflows without %s: err %v, ran %v; want a refusal naming it", drop, err, ran)
		}
	}
}

func TestPushWorkflowsRefusesMissingConfig(t *testing.T) {
	dir := repoDir(t, "[forge]\nrepo = \"o/r\"\n\n[project]\nlanguage = \"nl\"\n")
	host := filepath.Join(t.TempDir(), "host.toml")
	if err := os.WriteFile(host, []byte("[dispatch]\nrepos = ["+strconv.Quote(dir)+"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "lh.db")
	db := openStore(t, dbPath)
	srv := fakeForge(t, &tickEnv{})
	wt := filepath.Join(t.TempDir(), "wt")
	handoff := srv.AddComment(1, "u", "🤖 Handoff\nissue: #1\nworktree: "+wt+"\nbranch: issue-1-wf\nnext: push-workflows\nafter: /lighthouse-write-pr 1\nwhen: now")
	id := commentIDOf(t, handoff["id"])
	run, err := runs.Take(context.Background(), db, runs.Claim{Repo: "o/r", Issue: 1, Mode: "start", Trigger: "handoff",
		TriggerKey: "push:" + strconv.FormatInt(id, 10), Command: "push-workflows", Branch: "issue-1-wf", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{"GH_TOKEN": "tok", "HOME": t.TempDir(), "PATH": os.Getenv("PATH")}
	env := pushEnv{Getenv: func(k string) string { return vars[k] }, ForgeURL: srv.URL(),
		Workplace: func(repoconfig.Repo, string, string) workplace.Workplace { return noLookup{t: t} }}
	state := t.TempDir()
	args := pushWorkflowsArgs(pushOptions{repo: "o/r", issue: 1, worktree: wt, runID: run.ID, db: dbPath,
		pauseFile: filepath.Join(state, "PAUSE"), cloneDir: filepath.Join(state, "push", "o", "r.git"),
		stateDir: state, hostToml: host})
	if err := runPushWorkflows(env, args); err != nil {
		t.Fatalf("push-workflows: %v", err)
	}
	comments := srv.IssueComments(1)
	if len(comments) != 2 {
		t.Fatalf("%d comments, want the handoff and the Teruggegeven", len(comments))
	}
	body, _ := comments[1]["body"].(string)
	h, ok := protocol.ParseHeader(body)
	if !ok || h.Kind != protocol.KindReturned || h.Session != "" || h.Fields["worktree"] != wt || h.Fields["branch"] != "issue-1-wf" {
		t.Fatalf("not the pusher's Teruggegeven on %s:\n%s", wt, body)
	}
	if !strings.Contains(body, protocol.Heading("reason_pusher_off", "nl")) {
		t.Errorf("the Teruggegeven does not say the route is off:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(state, "push")); err == nil {
		t.Error("the pusher made its clone with the route off")
	}
	rows := history(t, db, "o/r")
	if len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.Returned {
		t.Errorf("push row %+v, want closed as returned", rows)
	}
}

func commentIDOf(t *testing.T, v any) int64 {
	t.Helper()
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case int:
		return int64(n)
	}
	t.Fatalf("comment id %v (%T)", v, v)
	return 0
}

// noLookup is a workplace that fails the test on a lookup.
type noLookup struct {
	workplace.Workplace
	t *testing.T
}

func (n noLookup) Lookup(_ context.Context, id string) (workplace.Workspace, error) {
	n.t.Errorf("the pusher looked up workspace %s with the route off", id)
	return workplace.Workspace{}, workplace.ErrGone
}

// The pusher reads the forge token only under its own name: [forge] token_env, else GH_TOKEN for
// GitHub and GITLAB_TOKEN for GitLab, and nothing else besides PATH, HOME and XDG_RUNTIME_DIR.
func TestPushGetenvReadsOnlyTheForgeTokenOfTheRepo(t *testing.T) {
	all := map[string]string{"PATH": "p", "HOME": "h", "XDG_RUNTIME_DIR": "r", "GH_TOKEN": "gh", "GITHUB_TOKEN": "github",
		"GITLAB_TOKEN": "gitlab", "MY_TOKEN": "mine", "XDG_STATE_HOME": "s", "LIGHTHOUSE_CONTROLLER_STATE": "c"}
	getenv := func(k string) string { return all[k] }
	cases := []struct {
		repo repoconfig.Repo
		want map[string]string
	}{
		{repoconfig.Repo{Kind: "github"}, map[string]string{"GH_TOKEN": "gh"}},
		{repoconfig.Repo{Kind: "gitlab"}, map[string]string{"GITLAB_TOKEN": "gitlab"}},
		{repoconfig.Repo{Kind: "github", TokenEnv: "MY_TOKEN"}, map[string]string{"MY_TOKEN": "mine"}},
	}
	for _, c := range cases {
		got := pushGetenv(getenv, c.repo)
		for k := range all {
			want := c.want[k]
			if k == "PATH" || k == "HOME" || k == "XDG_RUNTIME_DIR" {
				want = all[k]
			}
			if v := got(k); v != want {
				t.Errorf("%+v: %s = %q, want %q", c.repo, k, v, want)
			}
		}
	}
}

// The argv tick builds for a real push handoff, through the real tick wiring and a stubbed
// process starter, is what the pusher resolves from its flags in its cleaned environment: the
// database tick opened, tick's pause flag, the clone under tick's state home and its host.toml.
func TestTickStartsPusherThatResolvesTicksPaths(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, _ := startable(t, &env)
	appendHostToml(t, env, "\n[dispatch.workflow_push]\ntoken_file = \"/run/t\"\n")
	srv.Comments = map[int][]map[string]any{7: {handoffComment("push-workflows", "now")}}
	var gotArgs, gotEnv []string
	env.Spawn = func(_ context.Context, args, e []string) error { gotArgs, gotEnv = args, e; return nil }
	if err := tick(context.Background(), env, db, &bytes.Buffer{}); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if gotArgs == nil {
		t.Fatal("tick started no pusher")
	}
	rows := history(t, openStore(t, db), "o/r")
	if len(rows) != 1 || rows[0].Command != "push-workflows" {
		t.Fatalf("rows = %+v; want one push row", rows)
	}
	wantPause, err := park.PauseFile(env.Getenv)
	if err != nil {
		t.Fatal(err)
	}

	// The pusher's cleaned environment: only what Spawn was given, none of tick's paths.
	vars := map[string]string{}
	for _, kv := range gotEnv {
		k, v, _ := strings.Cut(kv, "=")
		vars[k] = v
	}
	var got pushOptions
	pe := pushEnv{Getenv: func(k string) string { return vars[k] },
		Run: func(_ context.Context, _ pushEnv, o pushOptions) error { got = o; return nil }}
	if err := runPushWorkflows(pe, gotArgs); err != nil {
		t.Fatalf("push-workflows %v: %v", gotArgs, err)
	}
	if got.db != db || got.pauseFile != wantPause || got.hostToml != env.HostToml || got.stateDir != env.StateDir ||
		got.runID != rows[0].ID || got.repo != "o/r" || got.issue != 7 {
		t.Errorf("pusher resolves %+v, want db %s pause %s host %s state %s run %d", got, db, wantPause, env.HostToml, env.StateDir, rows[0].ID)
	}
	if want := filepath.Join(env.Getenv("XDG_STATE_HOME"), "lighthouse", "push", "o", "r.git"); got.cloneDir != want {
		t.Errorf("clone %s, want %s", got.cloneDir, want)
	}
	for _, kv := range gotEnv {
		if k, _, _ := strings.Cut(kv, "="); k != "GH_TOKEN" && k != "PATH" && k != "HOME" && k != "XDG_RUNTIME_DIR" {
			t.Errorf("pusher env has %s", k)
		}
	}
	if vars["GH_TOKEN"] != "tok" {
		t.Errorf("pusher env GH_TOKEN = %q, want tick's forge token", vars["GH_TOKEN"])
	}
}

// The real starter runs the binary detached with exactly the given environment, no stdin of
// tick's, and returns without waiting for it.
func TestSpawnDetachedDoesNotWaitAndUsesGivenEnv(t *testing.T) {
	out := filepath.Join(t.TempDir(), "env")
	t.Setenv("LH_TEST_SECRET", "must-not-reach-the-child")
	start := time.Now()
	err := spawnDetached(context.Background(), []string{"-test.run=TestSpawnHelperProcess", "--", out},
		[]string{"LH_SPAWN_HELPER=1", "LH_SPAWN_OUT=" + out})
	if err != nil {
		t.Fatalf("spawnDetached: %v", err)
	}
	if time.Since(start) > time.Second { // the child lives for 3s
		t.Errorf("spawnDetached waited for the child")
	}
	var body []byte
	for i := 0; i < 100 && len(body) == 0; i++ {
		time.Sleep(50 * time.Millisecond)
		body, _ = os.ReadFile(out)
	}
	if strings.Contains(string(body), "LH_TEST_SECRET") || !strings.Contains(string(body), "LH_SPAWN_HELPER=1") {
		t.Errorf("child env = %q, want only the given variables", body)
	}
}

// TestSpawnHelperProcess is the child of TestSpawnDetachedDoesNotWaitAndUsesGivenEnv.
func TestSpawnHelperProcess(t *testing.T) {
	out := os.Getenv("LH_SPAWN_OUT")
	if os.Getenv("LH_SPAWN_HELPER") != "1" || out == "" {
		t.Skip("child of the spawn test")
	}
	if err := os.WriteFile(out, []byte(strings.Join(os.Environ(), "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
}

// The merge pusher's flags round trip: --merge with --pr and --sha and no worktree; check
// refuses a merge without either, with a permit, or with a SHA that is not a full one, and a push
// without a worktree.
func TestPushWorkflowsMergeFlags(t *testing.T) {
	head := strings.Repeat("c", 40)
	wt := testpath.Abs("w", "x")
	sent := pushOptions{repo: "o/r", issue: 7, runID: 3, db: testpath.Abs("d", "l.db"), pauseFile: testpath.Abs("s", "PAUSE"),
		cloneDir: testpath.Abs("s", "push", "o", "r.git"), stateDir: testpath.Abs("s"), hostToml: testpath.Abs("h", "host.toml"), merge: true, pr: 12, sha: head}
	args := pushWorkflowsArgs(sent)
	var got pushOptions
	env := pushEnv{Getenv: os.Getenv, Run: func(_ context.Context, _ pushEnv, o pushOptions) error { got = o; return nil }}
	if err := runPushWorkflows(env, args); err != nil {
		t.Fatalf("push-workflows %v: %v", args, err)
	}
	if !reflect.DeepEqual(got, sent) {
		t.Errorf("flags round trip to %+v, want %+v", got, sent)
	}
	for name, o := range map[string]pushOptions{
		"no pr":      func() pushOptions { o := sent; o.pr = 0; return o }(),
		"short sha":  func() pushOptions { o := sent; o.sha = "abc123"; return o }(),
		"permit":     func() pushOptions { o := sent; o.permit = head; return o }(),
		"worktree":   func() pushOptions { o := sent; o.worktree = wt; return o }(),
		"push no wt": func() pushOptions { o := sent; o.merge, o.pr, o.sha = false, 0, ""; return o }(),
		"pr no merge": func() pushOptions {
			o := sent
			o.merge, o.worktree = false, wt
			return o
		}(),
	} {
		ran := false
		env.Run = func(context.Context, pushEnv, pushOptions) error { ran = true; return nil }
		if err := runPushWorkflows(env, pushWorkflowsArgs(o)); err == nil || ran {
			t.Errorf("%s: err %v, ran %v; want a refusal", name, err, ran)
		}
	}
}

// Tick starts the merge pusher with --merge, the PR and the head, through the same paths and the
// exact environment apply gave, which holds no workflow token.
func TestTickStartsMergePusher(t *testing.T) {
	home := t.TempDir()
	dbPath := testpath.Abs("d", "l.db")
	vars := map[string]string{"HOME": home, "PATH": "/usr/bin"}
	var gotArgs, gotEnv []string
	te := tickEnv{Getenv: func(k string) string { return vars[k] }, HostToml: testpath.Abs("h", "host.toml"), DBPath: dbPath,
		Spawn: func(_ context.Context, args, e []string) error { gotArgs, gotEnv = args, e; return nil }}
	head := strings.Repeat("c", 40)
	spec := dispatch.PusherSpec{Repo: "o/r", Issue: 7, RunID: 3, Merge: true, PR: 12, HeadSHA: head,
		Env: []string{"PATH=/usr/bin", "HOME=" + home, "GH_TOKEN=tok"}}
	if err := te.startPusher(testpath.Abs("s"))(context.Background(), spec); err != nil {
		t.Fatalf("startPusher: %v", err)
	}
	if !reflect.DeepEqual(gotEnv, spec.Env) {
		t.Errorf("env = %q, want exactly %q", gotEnv, spec.Env)
	}
	var got pushOptions
	pe := pushEnv{Getenv: func(string) string { return "" },
		Run: func(_ context.Context, _ pushEnv, o pushOptions) error { got = o; return nil }}
	if err := runPushWorkflows(pe, gotArgs); err != nil {
		t.Fatalf("push-workflows %v: %v", gotArgs, err)
	}
	if !got.merge || got.pr != 12 || got.sha != head || got.worktree != "" || got.runID != 3 || got.db != dbPath {
		t.Errorf("pusher resolves %+v, want the merge of PR 12 on %s for run 3", got, head)
	}
}

// The merge client is its own forge client with the workflow token it is given: the merge request
// carries that token, never the forge token of the process environment.
func TestMergeClientUsesOnlyTheWorkflowToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "forge-tok")
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		if r.Method != http.MethodPut || r.URL.Path != "/repos/o/r/pulls/12/merge" {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"merged": true}`))
	}))
	defer srv.Close()
	m := mergeClient(repoconfig.Repo{Kind: "github", Slug: "o/r"}, srv.URL)("workflow-tok")
	if err := m.MergePR(context.Background(), 12, strings.Repeat("c", 40)); err != nil {
		t.Fatalf("MergePR: %v", err)
	}
	if !reflect.DeepEqual(auth, []string{"Bearer workflow-tok"}) {
		t.Errorf("Authorization = %q, want only the workflow token", auth)
	}
}

package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
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
	t.Setenv("HOME", home)
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
	sent := pushOptions{repo: "o/r", issue: 7, worktree: "/w/x", runID: 3, db: db, pauseFile: pause,
		cloneDir: clone, stateDir: "/s", hostToml: "/h/host.toml", permit: strings.Repeat("a", 40)}
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
		t.Fatalf("%d comments, want the handoff and the Vraag", len(comments))
	}
	body, _ := comments[1]["body"].(string)
	h, ok := protocol.ParseHeader(body)
	if !ok || h.Kind != protocol.KindQuestion || h.Session != "" || h.Fields["worktree"] != wt || h.Fields["branch"] != "issue-1-wf" {
		t.Fatalf("not the pusher's Vraag on %s:\n%s", wt, body)
	}
	if !strings.Contains(body, protocol.Heading("reason_pusher_off", "nl")) {
		t.Errorf("the Vraag does not say the route is off:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(state, "push")); err == nil {
		t.Error("the pusher made its clone with the route off")
	}
	rows := history(t, db, "o/r")
	if len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.Question {
		t.Errorf("push row %+v, want closed as question", rows)
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

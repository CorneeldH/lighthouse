package cli

import (
	"context"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Classification: io through the whole tick (SQLite, fake forge API and workplace, a real git
// workplace, git http-backend behind a local HTTP server that asks for basic auth). These tests
// fail if the push of a finished scheduled run does not carry the forge token as its HTTPS
// credential, if the token reaches the tick's output, or if the finish posts a comment or opens
// another PR than the spec's.

const finishTickToken = "ghp_" + "ticktokenticktokenticktokenticktoken12"

// realWorkplace is the tick's fake workplace whose workspace finWorkspace is the real git
// directory path.
type realWorkplace struct {
	*fakeWorkplace
	path string
}

const finWorkspace = "ws-finish"

func (w *realWorkplace) Lookup(ctx context.Context, id string) (workplace.Workspace, error) {
	if id == finWorkspace {
		return workplace.Workspace{ID: id, Path: w.path}, nil
	}
	return w.fakeWorkplace.Lookup(ctx, id)
}

func tickGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitHTTPServer serves root/o/r.git over smart HTTP to user x-access-token with password token
// only.
func gitHTTPServer(t *testing.T, root, token string) *httptest.Server {
	t.Helper()
	execPath := tickGitCmd(t, ".", "--exec-path")
	backend := filepath.Join(execPath, "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skipf("no git-http-backend: %v", err)
	}
	h := &cgi.Handler{Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "x-access-token" || p != token {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	tickGitCmd(t, root, "init", "-q", "--bare", "o/r.git")
	tickGitCmd(t, filepath.Join(root, "o/r.git"), "config", "http.receivepack", "true")
	return srv
}

// finishTick is a tick with one open scheduled run of codebase-map whose session is done, in a
// real git workplace with an allowed change, its start state recorded; the forge token is
// finishTickToken, and the git server accepts serverToken.
func finishTick(t *testing.T, serverToken string) (*scheduledTick, string, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	s := newScheduledTick(t, "")
	s.srv.BoardNodes = nil // no issue: only the scheduled run
	s.env.Run = nil        // real git
	s.job.AllowedPre = []string{"out/"}
	getenv := s.env.Getenv
	home := t.TempDir()
	s.env.Getenv = func(k string) string {
		switch k {
		case "GH_TOKEN":
			return finishTickToken
		case "PATH":
			return os.Getenv("PATH")
		case "HOME":
			return home
		}
		return getenv(k)
	}
	gitRoot := t.TempDir()
	s.env.GitBase = gitHTTPServer(t, gitRoot, serverToken).URL
	bare := filepath.Join(gitRoot, "o/r.git")

	work := filepath.Join(t.TempDir(), "work")
	branch := "scheduled/codebase-map-" + tickSlot + "-1"
	tickGitCmd(t, filepath.Dir(work), "init", "-q", "-b", "main", work)
	tickGitCmd(t, work, "config", "user.name", "Test")
	tickGitCmd(t, work, "config", "user.email", "test@example.com")
	if err := os.MkdirAll(filepath.Join(work, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "out", "keep.md"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tickGitCmd(t, work, "add", "-A")
	tickGitCmd(t, work, "commit", "-q", "-m", "init")
	tickGitCmd(t, work, "checkout", "-q", "-b", branch)

	ctx := context.Background()
	sha, state, err := scheduled.StartState(ctx, workplace.ExecRunner(), work)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "out", "map.md"), []byte("the map\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := openStore(t, s.db)
	session := runs.NewSessionID()
	run, err := runs.Take(ctx, conn, runs.Claim{Repo: "o/r", Schedule: "codebase-map", SessionID: session, Mode: "start",
		Trigger: runs.TriggerScheduled, TriggerKey: tickKey, Command: "/gsd-map-codebase", Profile: "software",
		Branch: branch, Now: t0.Add(-10 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	rec := scheduled.Record{Verdict: scheduled.Verdict{Mode: "full"}, MaxRuntime: time.Hour, GitState: state}
	for _, err := range []error{
		runs.SetWorkplace(ctx, conn, run.ID, finWorkspace, branch),
		runs.SetStartSHA(ctx, conn, run.ID, sha),
		runs.SetVerdict(ctx, conn, run.ID, rec.Encode()),
		runs.SetSession(ctx, conn, run.ID, "key-"+session),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	fw := s.w.fakeWorkplace
	fw.sessions[finWorkspace] = []workplace.Session{{Key: "key-" + session, WorkspaceID: finWorkspace,
		SessionID: session, State: "done"}}
	rw := &realWorkplace{fakeWorkplace: fw, path: work}
	s.env.Workplace = func(repoconfig.Repo, string, string) workplace.Workplace { return rw }
	return s, bare, branch
}

// (d) Fails if the push does not use the forge token as its HTTPS credential (the server
// refuses it and no branch arrives), if the token is in the tick's output, if the PR body is
// not the spec's, if pr_at is not recorded, or if the tick posts a comment (extra c).
func TestTickScheduledFinishPushesWithTheToken(t *testing.T) {
	s, bare, branch := finishTick(t, finishTickToken)
	out := s.tick(t)
	if strings.Contains(out, finishTickToken) {
		t.Errorf("errOut holds the token: %q", out)
	}
	if !strings.Contains(out, "lighthouse: o/r "+tickKey+": run 1: PR #1\n") {
		t.Errorf("errOut = %q, want the PR line", out)
	}
	if got := tickGitCmd(t, bare, "for-each-ref", "--format=%(objectname)", "refs/heads/"+branch); got == "" {
		t.Error("the branch did not reach the git server")
	}
	var bodies []string
	for _, p := range s.srv.Posts {
		if strings.HasSuffix(p.Path, "/comments") {
			t.Errorf("posted a comment: %s", p.Path)
		}
		if p.Path == "/repos/o/r/pulls" {
			bodies = append(bodies, p.Body["body"].(string))
			if p.Body["head"] != branch {
				t.Errorf("PR head = %v, want %s", p.Body["head"], branch)
			}
		}
	}
	if want := "No issue: geplande run codebase-map (" + tickSlot + ")"; len(bodies) != 1 || bodies[0] != want {
		t.Errorf("PR bodies = %q, want [%q]", bodies, want)
	}
	rows := scheduledRows(t, openStore(t, s.db))
	if len(rows) != 1 || !rows[0].Open || !rows[0].PRAt.Equal(t0) {
		t.Errorf("rows = %+v, want the run open with pr_at %v", rows, t0)
	}
}

// (d) A token the server refuses (here already at the ls-remote before the push) is an apply
// failure that keeps the run open; its line carries no token.
func TestTickScheduledFinishRefusedPushHidesTheToken(t *testing.T) {
	s, _, _ := finishTick(t, "another-token")
	out := s.tick(t)
	if strings.Contains(out, finishTickToken) {
		t.Errorf("errOut holds the token: %q", out)
	}
	if !strings.Contains(out, "lighthouse: apply o/r: o/r "+tickKey+" scheduled_finish: postcheck: ls-remote ") ||
		!strings.Contains(out, "Authentication failed") {
		t.Errorf("errOut = %q, want the failed push as an apply line", out)
	}
	rows := scheduledRows(t, openStore(t, s.db))
	if len(rows) != 1 || !rows[0].Open || !rows[0].PRAt.IsZero() {
		t.Errorf("rows = %+v, want the run open without pr_at", rows)
	}
}

// Classification: pure logic. Fails if the remote of a scheduled push is not the repo on its
// forge host, or names the token itself instead of the variable that holds it.
func TestScheduledRemoteOfARepo(t *testing.T) {
	env := tickEnv{Getenv: func(k string) string {
		return map[string]string{"GITHUB_TOKEN": "t1", "WORK_TOKEN": "t2", "GITLAB_TOKEN": "t3"}[k]
	}}
	repos := map[string]repoconfig.Repo{
		"o/r": {Slug: "o/r"},
		"o/e": {Slug: "o/e", Host: "ghe.example.com", TokenEnv: "WORK_TOKEN"},
		"g/l": {Slug: "g/l", Kind: "gitlab", Host: "gitlab.example.com"},
	}
	remote := scheduledRemote(env, "/state", repos)
	want := map[string]scheduled.HTTPSRemote{
		"o/r": {URL: "https://github.com/o/r.git", User: "x-access-token", TokenVar: "GITHUB_TOKEN", TempDir: "/state"},
		"o/e": {URL: "https://ghe.example.com/o/e.git", User: "x-access-token", TokenVar: "WORK_TOKEN", TempDir: "/state"},
		"g/l": {URL: "https://gitlab.example.com/g/l.git", User: "oauth2", TokenVar: "GITLAB_TOKEN", TempDir: "/state"},
	}
	for slug, w := range want {
		r, err := remote(slug)
		if err != nil {
			t.Fatalf("%s: %v", slug, err)
		}
		got := r.(scheduled.HTTPSRemote)
		if got.Getenv == nil || got.Getenv("WORK_TOKEN") != "t2" {
			t.Errorf("%s: Getenv is not the tick's", slug)
		}
		got.Getenv = nil
		if !reflect.DeepEqual(got, w) {
			t.Errorf("%s: remote = %+v, want %+v", slug, got, w)
		}
	}
	if _, err := remote("x/y"); err == nil {
		t.Error("remote of an unknown repo: want an error")
	}
}

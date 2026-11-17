package scheduled_test

import (
	"context"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/scheduled/scheduledtest"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Classification: io/network (a real git repository, git http-backend behind a local HTTP server
// that asks for basic auth). The regression surface is how the push of a scheduled run
// authenticates: with the forge token through GIT_ASKPASS, never in an argument, a URL or an
// error, and without the machine's or the workplace's credential helpers.

const remoteToken = "ghp_" + "remotetokenremotetokenremotetoken1234"

// gitServer serves the bare repositories under root over smart HTTP and lets a request through
// only with user and token as basic auth. It returns the server URL of repo.
type gitServer struct {
	root  string
	srv   *httptest.Server
	auths int // requests that carried the right credentials
}

func newGitServer(t *testing.T, user, token string) *gitServer {
	t.Helper()
	execPath, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Fatalf("git --exec-path: %v", err)
	}
	backend := filepath.Join(strings.TrimSpace(string(execPath)), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skipf("no git-http-backend: %v", err)
	}
	g := &gitServer{root: t.TempDir()}
	h := &cgi.Handler{Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + g.root, "GIT_HTTP_EXPORT_ALL=1"}}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != user || p != token {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		g.auths++
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(g.srv.Close)
	runGit(t, g.root, "init", "-q", "--bare", "o/r.git")
	runGit(t, filepath.Join(g.root, "o/r.git"), "config", "http.receivepack", "true")
	return g
}

func (g *gitServer) url() string { return g.srv.URL + "/o/r.git" }

// branch is the sha of branch on the server, "" when it is not there.
func (g *gitServer) branch(t *testing.T, branch string) string {
	t.Helper()
	out := runGit(t, g.root, "-C", "o/r.git", "for-each-ref", "--format=%(objectname)", "refs/heads/"+branch)
	return strings.TrimSpace(out)
}

// remoteEnv is the tick's environment: PATH, a HOME whose global git config has a credential
// helper that leaves a marker and sends every http URL elsewhere, and the token under GH_TOKEN.
func remoteEnv(t *testing.T, marker string) func(string) string {
	t.Helper()
	home := t.TempDir()
	cfg := "[credential]\n\thelper = \"!touch " + marker + "-global; echo password=wrong #\"\n" +
		"[url \"" + filepath.Join(home, "nowhere.git") + "/\"]\n\tinsteadOf = http://\n"
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"PATH": os.Getenv("PATH"), "HOME": home, "GH_TOKEN": remoteToken}
	return func(k string) string { return env[k] }
}

// gitSpy puts a git first in PATH that logs its arguments to the returned file and runs the real
// git.
func gitSpy(t *testing.T) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "args.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + log + "\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// The happy path, and every way the token could leak or a helper could run. Fails if the push
// sends no token (the server refuses), takes a credential helper or a URL rewrite of the global
// config, or a helper of the workplace's config (the marker appears, or the helper's wrong
// password is sent), puts the token in an argument, the URL or the askpass script, or goes to
// the workplace's origin.
func TestHTTPSRemotePushesWithTheToken(t *testing.T) {
	f := newFixture(t)
	args := gitSpy(t)
	srv := newGitServer(t, "x-access-token", remoteToken)
	marker := filepath.Join(t.TempDir(), "helper-ran")
	f.git("config", "credential.helper", "!touch "+marker+"-local; echo password=wrong #")
	// The workplace's own origin points nowhere: the push must not use it.
	f.git("remote", "set-url", "origin", filepath.Join(t.TempDir(), "elsewhere.git"))
	scripts := t.TempDir()
	r := scheduled.HTTPSRemote{URL: srv.url(), User: "x-access-token", TokenVar: "GH_TOKEN", ScriptDir: scripts,
		Getenv: remoteEnv(t, marker)}
	ctx := context.Background()

	if sha, err := r.Head(ctx, f.work, testBranch); err != nil || sha != "" {
		t.Fatalf("Head before the push = %q, %v; want \"\", nil", sha, err)
	}
	if err := r.Push(ctx, f.work, f.start, testBranch); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if got := srv.branch(t, testBranch); got != f.start {
		t.Errorf("branch on the server = %q, want %q", got, f.start)
	}
	if sha, err := r.Head(ctx, f.work, testBranch); err != nil || sha != f.start {
		t.Errorf("Head after the push = %q, %v; want %q", sha, err, f.start)
	}
	if srv.auths == 0 {
		t.Error("no request carried the token")
	}
	for _, m := range []string{marker + "-global", marker + "-local"} {
		if _, err := os.Stat(m); err == nil {
			t.Errorf("credential helper ran: %s", filepath.Base(m))
		}
	}
	entries, err := os.ReadDir(scripts)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(scripts, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), remoteToken) {
			t.Errorf("script %s holds the token", e.Name())
		}
	}
	if strings.Contains(f.git("config", "--get-regexp", "."), remoteToken) {
		t.Error("the token is in the workplace's git config")
	}
	logged, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logged), "push") || strings.Contains(string(logged), remoteToken) {
		t.Errorf("git arguments = %q, want a push without the token", logged)
	}
}

// Fails if an error of the remote carries the token, or if a missing token variable runs git
// anyway.
func TestHTTPSRemoteErrorsWithholdTheToken(t *testing.T) {
	f := newFixture(t)
	srv := newGitServer(t, "x-access-token", "another-token")
	r := scheduled.HTTPSRemote{URL: srv.url(), User: "x-access-token", TokenVar: "GH_TOKEN", ScriptDir: t.TempDir(),
		Getenv: remoteEnv(t, filepath.Join(t.TempDir(), "m"))}
	err := r.Push(context.Background(), f.work, f.start, testBranch)
	if err == nil {
		t.Fatal("Push with a refused token succeeded")
	}
	if strings.Contains(err.Error(), remoteToken) {
		t.Errorf("error = %q, holds the token", err)
	}

	r.Getenv = func(k string) string {
		if k == "GH_TOKEN" {
			return ""
		}
		return os.Getenv(k)
	}
	if _, err := r.Head(context.Background(), f.work, testBranch); err == nil || !strings.Contains(err.Error(), "token variable GH_TOKEN is not set") {
		t.Errorf("Head without a token = %v, want the variable named", err)
	}
	for _, bad := range []string{"GH TOKEN", "X;rm", ""} {
		r.TokenVar = bad
		if _, err := r.Head(context.Background(), f.work, testBranch); err == nil || !strings.Contains(err.Error(), "no environment variable name") {
			t.Errorf("Head with token variable %q = %v", bad, err)
		}
	}
	r.TokenVar = "GH_TOKEN"
	r.URL = "https://x-access-token:" + remoteToken + "@example.com/o/r.git"
	if _, err := r.Head(context.Background(), f.work, testBranch); err == nil || strings.Contains(err.Error(), remoteToken) ||
		!strings.Contains(err.Error(), "without credentials") {
		t.Errorf("Head with credentials in the URL = %v, want a refusal without the token", err)
	}
}

// Fails if text the server sends back reaches the error with the token in it: here a hook on the
// server refuses the push and prints the token.
func TestHTTPSRemoteWithholdsTheTokenInServerText(t *testing.T) {
	f := newFixture(t)
	srv := newGitServer(t, "x-access-token", remoteToken)
	hook := filepath.Join(srv.root, "o/r.git", "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho 'refused for "+remoteToken+"' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := scheduled.HTTPSRemote{URL: srv.url(), User: "x-access-token", TokenVar: "GH_TOKEN", ScriptDir: t.TempDir(),
		Getenv: remoteEnv(t, filepath.Join(t.TempDir(), "m"))}
	err := r.Push(context.Background(), f.work, f.start, testBranch)
	if err == nil || strings.Contains(err.Error(), remoteToken) || !strings.Contains(err.Error(), "refused for (withheld)") {
		t.Errorf("Push = %v, want the server's text with the token withheld", err)
	}
}

// Fails if Postcheck reads or pushes the branch through the workplace's origin instead of the
// remote it was given: the origin points nowhere, and the commit must still reach the remote.
func TestPostcheckUsesTheGivenRemoteNotOrigin(t *testing.T) {
	f := newFixture(t)
	f.git("remote", "set-url", "origin", filepath.Join(t.TempDir(), "elsewhere.git"))
	f.write("out/a.md", "new\n")
	env := scheduled.Env{Dir: f.work, Run: workplace.ExecRunner()}
	res, err := scheduled.Postcheck(context.Background(), env.Run, &scheduledtest.Job{AllowedPre: []string{"out/"}}, env,
		scheduled.Verdict{}, f.start, testBranch, f.remote())
	if err != nil || !res.Changed || res.Failure != "" {
		t.Fatalf("Postcheck = %+v, %v; want Changed", res, err)
	}
	if got, _ := f.remote().Head(context.Background(), f.work, testBranch); got == "" || got != f.git("rev-parse", "HEAD") {
		t.Errorf("remote branch = %q, want HEAD %q", got, f.git("rev-parse", "HEAD"))
	}
}

// Fails if GitState depends on HEAD: the agent's own commits must not look like tampering; the
// tick compares only the config the run could change. It must change with config.worktree too.
func TestGitStateIgnoresHeadAndSeesConfigWorktree(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := workplace.ExecRunner()
	_, start, err := scheduled.StartState(ctx, run, f.work)
	if err != nil {
		t.Fatal(err)
	}
	f.write("out/a.md", "agent\n")
	f.git("add", "-A")
	f.git("commit", "-q", "-m", "agent commit")
	if now, err := scheduled.GitState(ctx, run, f.work); err != nil || now != start {
		t.Errorf("GitState after a commit = %q, %v; want the start state %q", now, err, start)
	}
	if err := os.WriteFile(filepath.Join(f.work, ".git", "config.worktree"), []byte("[core]\n\tsshCommand = x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if now, err := scheduled.GitState(ctx, run, f.work); err != nil || now == start {
		t.Errorf("GitState after config.worktree = %q, %v; want a change", now, err)
	}
}

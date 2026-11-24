package pusher

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/flock"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// fixture is a bare "origin", a main checkout on main and a linked worktree on the issue branch
// with one commit that is not on origin yet.
type fixture struct {
	t      *testing.T
	dir    string
	remote string
	main   string
	wt     string
	req    Request
}

const branch = "issue-1-wf"

// tgit runs git for the test's own setup, isolated from the user's config like the pusher.
func tgit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.invalid",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	f := &fixture{t: t, dir: dir, remote: filepath.Join(dir, "remote.git"), main: filepath.Join(dir, "main"), wt: filepath.Join(dir, "wt")}
	tgit(t, dir, "init", "-q", "--bare", f.remote)
	tgit(t, dir, "clone", "-q", f.remote, f.main)
	write(t, filepath.Join(f.main, "README"), "readme\n")
	tgit(t, f.main, "add", "README")
	tgit(t, f.main, "commit", "-q", "-m", "init")
	tgit(t, f.main, "push", "-q", "origin", "main")
	tgit(t, f.main, "worktree", "add", "-q", "-b", branch, f.wt)
	f.commit("feature.txt", "feature\n")
	f.req = Request{
		Repo: "o/r", URL: f.remote, Branch: branch, Default: "main", Worktree: f.wt,
		Issue: 1, RunID: 7, CloneDir: filepath.Join(dir, "push", "o-r.git"),
		PauseFile: filepath.Join(dir, "state", "PAUSE"),
	}
	return f
}

// commit writes a file in the worktree, commits it and returns the new HEAD.
func (f *fixture) commit(name, text string) string {
	f.t.Helper()
	write(f.t, filepath.Join(f.wt, name), text)
	tgit(f.t, f.wt, "add", name)
	tgit(f.t, f.wt, "commit", "-q", "-m", "add "+name)
	return f.head()
}

func (f *fixture) head() string { f.t.Helper(); return tgit(f.t, f.wt, "rev-parse", "HEAD") }

// record writes a roborev record into the worktree's git-dir under name.
func (f *fixture) record(name string, data map[string]any) {
	f.t.Helper()
	gitDir := tgit(f.t, f.wt, "rev-parse", "--absolute-git-dir")
	text, err := json.Marshal(data)
	if err != nil {
		f.t.Fatal(err)
	}
	write(f.t, filepath.Join(gitDir, "lighthouse", "roborev", name), string(text))
}

// done is a done record on sha with the given fixed commits.
func done(sha string, fixed ...string) map[string]any {
	decisions := []any{map[string]any{"path": "a", "line": 1, "severity": "low", "uitkomst": "refuted", "commit": nil, "reden": "no"}}
	for _, c := range fixed {
		decisions = append(decisions, map[string]any{"path": "a", "line": 2, "severity": "high", "uitkomst": "fixed", "commit": c, "reden": nil})
	}
	return map[string]any{"range": "aaaaaaa..bbbbbbb", "uitkomst": "done", "sha": sha, "bevindingen": decisions}
}

func (f *fixture) branchRecord(data map[string]any) { f.record(branch+".json", data) }

// floor runs Floor the way the pusher does: under the clone lock.
func floor(t *testing.T, req Request) (Checked, *Red) {
	t.Helper()
	return floorCtx(t, context.Background(), req)
}

func floorCtx(t *testing.T, ctx context.Context, req Request) (Checked, *Red) {
	t.Helper()
	lock, red := LockClone(ctx, req.CloneDir)
	if red != nil {
		t.Fatalf("LockClone: %s", red.Why)
	}
	defer lock.Unlock()
	return Floor(ctx, req, lock)
}

func wantStep(t *testing.T, red *Red, step string) {
	t.Helper()
	if red == nil {
		t.Fatalf("Floor is green, want red at step %q", step)
	}
	if red.Step != step {
		t.Fatalf("red at step %q (%s), want step %q", red.Step, red.Why, step)
	}
	if red.Why == "" {
		t.Errorf("red at step %q without a reason", step)
	}
}

func wantGreen(t *testing.T, red *Red) {
	t.Helper()
	if red != nil {
		t.Fatalf("Floor red at step %q: %s", red.Step, red.Why)
	}
}

func TestFloorAcceptsReviewedBranch(t *testing.T) {
	f := newFixture(t)
	head := f.head()
	base := tgit(t, f.main, "rev-parse", "main")
	f.branchRecord(done(head))
	got, red := floor(t, f.req)
	wantGreen(t, red)
	want := Checked{SHA: head, MergeBase: base, Files: []string{"feature.txt"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Checked = %+v, want %+v", got, want)
	}
	// The pusher's own bare clone holds the SHA it read.
	if out := tgit(t, f.req.CloneDir, "cat-file", "-t", head); out != "commit" {
		t.Errorf("clone has %s as %q, want commit", head, out)
	}
}

func TestFloorRefusesDefaultBranch(t *testing.T) {
	f := newFixture(t)
	f.req.Branch, f.req.Worktree = "main", f.main
	_, red := floor(t, f.req)
	wantStep(t, red, StepBranch)
}

func TestFloorRefusesOtherBranchThanHandoff(t *testing.T) {
	f := newFixture(t)
	f.branchRecord(done(f.head()))
	f.req.Branch = "issue-1-other"
	_, red := floor(t, f.req)
	wantStep(t, red, StepBranch)
}

func TestFloorRefusesMissingWorktree(t *testing.T) {
	f := newFixture(t)
	f.req.Worktree = filepath.Join(f.dir, "gone")
	_, red := floor(t, f.req)
	wantStep(t, red, StepBranch)
}

func TestFloorRefusesNonFastForward(t *testing.T) {
	f := newFixture(t)
	tgit(t, f.wt, "push", "-q", "origin", branch)
	tgit(t, f.wt, "commit", "-q", "--amend", "-m", "rewritten")
	f.branchRecord(done(f.head()))
	_, red := floor(t, f.req)
	wantStep(t, red, StepFastForward)
}

func TestFloorAcceptsFastForwardOnOrigin(t *testing.T) {
	f := newFixture(t)
	tgit(t, f.wt, "push", "-q", "origin", branch)
	f.branchRecord(done(f.commit("more.txt", "more\n")))
	_, red := floor(t, f.req)
	wantGreen(t, red)
}

// mergeOnOrigin makes origin/<branch> a merge of a moved main on top of what origin has, as a
// hand-made update of the branch does (#551: origin 18e3a81 is such a merge on top of 9187eeda).
func (f *fixture) mergeOnOrigin() string {
	f.t.Helper()
	tgit(f.t, f.main, "checkout", "-q", "main")
	write(f.t, filepath.Join(f.main, "main.txt"), "main\n")
	tgit(f.t, f.main, "add", "main.txt")
	tgit(f.t, f.main, "commit", "-q", "-m", "main moves")
	tgit(f.t, f.main, "push", "-q", "origin", "main")
	tgit(f.t, f.main, "fetch", "-q", "origin")
	tgit(f.t, f.main, "checkout", "-q", "-b", "update", "origin/"+branch)
	tgit(f.t, f.main, "merge", "-q", "--no-ff", "-m", "update", "main")
	tgit(f.t, f.main, "push", "-q", "origin", "update:"+branch)
	return tgit(f.t, f.main, "rev-parse", "update")
}

// A SHA origin already has needs no push: the floor is green with AlreadyPushed, also without a
// roborev record, because there is nothing left to guard.
func TestFloorAcceptsSHAAlreadyOnOrigin(t *testing.T) {
	for name, onOrigin := range map[string]func(f *fixture){
		"origin is the sha":            func(*fixture) {},
		"origin is a merge on the sha": func(f *fixture) { f.mergeOnOrigin() },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			head := f.head()
			base := tgit(t, f.main, "rev-parse", "main")
			tgit(t, f.wt, "push", "-q", "origin", branch)
			onOrigin(f)
			f.branchRecord(map[string]any{"range": "aaaaaaa..bbbbbbb", "uitkomst": "skip", "reden": "build session ran no review"})
			got, red := floor(t, f.req)
			wantGreen(t, red)
			want := Checked{SHA: head, MergeBase: base, Files: []string{"feature.txt"}, AlreadyPushed: true}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Checked = %+v, want %+v", got, want)
			}
		})
	}
}

func TestFloorRefusesDirtyWorktree(t *testing.T) {
	for name, dirty := range map[string]func(f *fixture){
		"modified":  func(f *fixture) { write(f.t, filepath.Join(f.wt, "feature.txt"), "changed\n") },
		"untracked": func(f *fixture) { write(f.t, filepath.Join(f.wt, "new.txt"), "new\n") },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.branchRecord(done(f.head()))
			dirty(f)
			_, red := floor(t, f.req)
			wantStep(t, red, StepClean)
		})
	}
}

func TestFloorRefusesForeignMerge(t *testing.T) {
	f := newFixture(t)
	tgit(t, f.main, "checkout", "-q", "-b", "other")
	write(t, filepath.Join(f.main, "other.txt"), "other\n")
	tgit(t, f.main, "add", "other.txt")
	tgit(t, f.main, "commit", "-q", "-m", "other")
	tgit(t, f.wt, "merge", "-q", "--no-ff", "-m", "merge other", "other")
	f.branchRecord(done(f.head()))
	_, red := floor(t, f.req)
	wantStep(t, red, StepOwnCommits)
}

func TestFloorAcceptsUpdateMerge(t *testing.T) {
	f := newFixture(t)
	tgit(t, f.main, "checkout", "-q", "main")
	write(t, filepath.Join(f.main, "main.txt"), "main\n")
	tgit(t, f.main, "add", "main.txt")
	tgit(t, f.main, "commit", "-q", "-m", "main moves")
	tgit(t, f.main, "push", "-q", "origin", "main")
	newMain := tgit(t, f.main, "rev-parse", "main")
	tgit(t, f.wt, "fetch", "-q", "origin")
	tgit(t, f.wt, "merge", "-q", "--no-ff", "-m", "update", "origin/main")
	f.branchRecord(done(f.head()))
	got, red := floor(t, f.req)
	wantGreen(t, red)
	if got.MergeBase != newMain {
		t.Errorf("MergeBase = %s, want the new main %s", got.MergeBase, newMain)
	}
	if want := []string{"feature.txt"}; !reflect.DeepEqual(got.Files, want) {
		t.Errorf("Files = %v, want %v", got.Files, want)
	}
}

func TestFloorRefusesMissingRoborev(t *testing.T) {
	f := newFixture(t)
	_, red := floor(t, f.req)
	wantStep(t, red, StepRoborev)
}

func TestFloorRefusesSkipRecord(t *testing.T) {
	f := newFixture(t)
	f.branchRecord(map[string]any{"range": "aaaaaaa..bbbbbbb", "uitkomst": "skip", "reden": "3 coderegels (drempel 100)"})
	_, red := floor(t, f.req)
	wantStep(t, red, StepRoborev)
}

func TestFloorRefusesStaleRoborev(t *testing.T) {
	f := newFixture(t)
	f.branchRecord(done(f.head()))
	f.commit("late.txt", "late\n")
	_, red := floor(t, f.req)
	wantStep(t, red, StepRoborev)
}

func TestFloorRefusesRecordOfAnotherHistory(t *testing.T) {
	f := newFixture(t)
	reviewed := f.head()
	tgit(t, f.wt, "commit", "-q", "--amend", "-m", "rewritten")
	f.branchRecord(done(reviewed))
	_, red := floor(t, f.req)
	wantStep(t, red, StepRoborev)
}

func TestFloorAcceptsFixCommitInRecord(t *testing.T) {
	f := newFixture(t)
	reviewed := f.head()
	fix := f.commit("fix.txt", "fix\n")
	f.branchRecord(done(reviewed, fix[:12]))
	_, red := floor(t, f.req)
	wantGreen(t, red)
}

func TestFloorRefusesUnlistedCommitAfterFix(t *testing.T) {
	f := newFixture(t)
	reviewed := f.head()
	fix := f.commit("fix.txt", "fix\n")
	f.commit("extra.txt", "extra\n")
	f.branchRecord(done(reviewed, fix))
	_, red := floor(t, f.req)
	wantStep(t, red, StepRoborev)
}

func TestFloorAcceptsFixCommitInPushRecord(t *testing.T) {
	f := newFixture(t)
	old := f.head()
	tgit(t, f.wt, "push", "-q", "origin", branch)
	fix := f.commit("fix.txt", "fix\n")
	// The branch record is from the build route and stale; the push record covers.
	f.branchRecord(done(tgit(t, f.wt, "rev-parse", "HEAD~2")))
	f.record("push-"+old+".json", done(old, fix))
	_, red := floor(t, f.req)
	wantGreen(t, red)
}

func TestFloorRefusesWhenPaused(t *testing.T) {
	f := newFixture(t)
	f.branchRecord(done(f.head()))
	if err := park.WritePause(f.req.PauseFile, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, red := floor(t, f.req)
	wantStep(t, red, StepPause)
}

func TestFloorRefusesWithoutPauseFile(t *testing.T) {
	f := newFixture(t)
	f.branchRecord(done(f.head()))
	f.req.PauseFile = ""
	_, red := floor(t, f.req)
	wantStep(t, red, StepPause)
}

func TestFloorIgnoresWorktreeGitConfig(t *testing.T) {
	f := newFixture(t)
	f.branchRecord(done(f.head()))
	marker := filepath.Join(f.dir, "marker")
	hook := filepath.Join(f.dir, "evil.sh")
	write(t, hook, "#!/bin/sh\ntouch "+marker+"\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	hooks := filepath.Join(f.dir, "hooks")
	for _, name := range []string{"pre-push", "post-checkout", "reference-transaction", "post-index-change"} {
		write(t, filepath.Join(hooks, name), "#!/bin/sh\ntouch "+marker+"\n")
		if err := os.Chmod(filepath.Join(hooks, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The worktree's own config: hooks, an fsmonitor hook, a clean filter that info/attributes
	// puts on a tracked file, and a URL rewrite of the forge URL. The pusher's git reads its own
	// clone's config, so none applies; the rewrite would only show if worktree config leaked into
	// the clone.
	tgit(t, f.wt, "config", "core.hooksPath", hooks)
	tgit(t, f.wt, "config", "core.fsmonitor", hook)
	tgit(t, f.wt, "config", "filter.evil.clean", hook+"; cat")
	tgit(t, f.wt, "config", "filter.evil.required", "true")
	tgit(t, f.wt, "config", "url."+filepath.Join(f.dir, "nowhere")+".insteadOf", f.remote)
	common := tgit(t, f.wt, "rev-parse", "--path-format=absolute", "--git-common-dir")
	write(t, filepath.Join(common, "info", "attributes"), "* filter=evil\n")
	// ~/.gitconfig: the same, so only GIT_CONFIG_GLOBAL=/dev/null keeps it out.
	write(t, filepath.Join(os.Getenv("HOME"), ".gitconfig"), "[core]\n\thooksPath = "+hooks+
		"\n\tfsmonitor = "+hook+"\n[filter \"evil\"]\n\tclean = "+hook+"; cat\n[url \""+filepath.Join(f.dir, "nowhere")+"\"]\n\tinsteadOf = "+f.remote+"\n")
	// A newer mtime makes every stat check stale, so a status has to refresh and rehash.
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Join(f.wt, "feature.txt"), future, future); err != nil {
		t.Fatal(err)
	}
	_, red := floor(t, f.req)
	wantGreen(t, red)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("marker %s exists (err %v): worktree or global git config reached the pusher's git", marker, err)
	}
}

func TestCloneFetchFailureIsRed(t *testing.T) {
	f := newFixture(t)
	f.branchRecord(done(f.head()))
	f.req.URL = filepath.Join(f.dir, "no-such-remote.git")
	_, red := floor(t, f.req)
	wantStep(t, red, StepClone)
}

func TestSecondPusherWaitsOnFlock(t *testing.T) {
	f := newFixture(t)
	f.branchRecord(done(f.head()))
	first, red := LockClone(context.Background(), f.req.CloneDir)
	if red != nil {
		t.Fatal(red.Why)
	}
	ch := make(chan *CloneLock, 1)
	go func() {
		second, red := LockClone(context.Background(), f.req.CloneDir)
		if red != nil {
			t.Errorf("second LockClone: %s", red.Why)
		}
		ch <- second
	}()
	select {
	case <-ch:
		t.Fatal("a second pusher took the clone lock while the first held it")
	case <-time.After(500 * time.Millisecond):
	}
	first.Unlock()
	select {
	case second := <-ch:
		defer second.Unlock()
		_, red := Floor(context.Background(), f.req, second)
		wantGreen(t, red)
	case <-time.After(30 * time.Second):
		t.Fatal("the second pusher did not get the lock after the first released it")
	}
}

func TestFlockWaitEndsAtTimeout(t *testing.T) {
	f := newFixture(t)
	// Another process's flock on <clone>.lock is the same lock.
	if err := os.MkdirAll(filepath.Dir(f.req.CloneDir), 0o755); err != nil {
		t.Fatal(err)
	}
	held, err := flock.TryLock(f.req.CloneDir + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, red := LockClone(ctx, f.req.CloneDir)
	wantStep(t, red, StepClone)
}

// Floor runs only under the lock on its own clone, and a lock let go is no lock.
func TestFloorRefusesWithoutTheCloneLock(t *testing.T) {
	f := newFixture(t)
	f.branchRecord(done(f.head()))
	_, red := Floor(context.Background(), f.req, nil)
	wantStep(t, red, StepClone)
	other, red := LockClone(context.Background(), filepath.Join(f.dir, "push", "other.git"))
	if red != nil {
		t.Fatal(red.Why)
	}
	defer other.Unlock()
	_, red = Floor(context.Background(), f.req, other)
	wantStep(t, red, StepClone)
	released, red := LockClone(context.Background(), f.req.CloneDir)
	if red != nil {
		t.Fatal(red.Why)
	}
	released.Unlock()
	_, red = Floor(context.Background(), f.req, released)
	wantStep(t, red, StepClone)
}

// TestForgeTokenOnlyInFetchEnv puts a logging git first on PATH: the token reaches the env of
// the one fetch from the forge URL, as an http.extraHeader, and never any argv.
func TestForgeTokenOnlyInFetchEnv(t *testing.T) {
	f := newFixture(t)
	f.branchRecord(done(f.head()))
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(f.dir, "bin")
	log := filepath.Join(f.dir, "git.log")
	write(t, filepath.Join(bin, "git"), "#!/bin/sh\n{ printf 'ARGV'; printf ' %s' \"$@\"; printf '\\n'; env | grep -e '^GIT_CONFIG' -e '^GH_TOKEN' -e '^GIT_DIR' | sed 's/^/ENV /'; } >> "+log+"\nexec "+real+" \"$@\"\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	const token = "ghp_SECRETtoken123"
	f.req.ForgeToken = token
	// The pusher's own process has the forge token and a stray GIT_DIR; no git command sees them.
	t.Setenv("GH_TOKEN", token)
	t.Setenv("GIT_DIR", filepath.Join(f.dir, "elsewhere"))
	_, red := floor(t, f.req)
	wantGreen(t, red)
	text, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	header := "ENV GIT_CONFIG_VALUE_0=Authorization: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	withToken := 0
	for _, block := range strings.Split(string(text), "ARGV")[1:] {
		argv, env, _ := strings.Cut(block, "\n")
		if strings.Contains(argv, token) || strings.Contains(argv, base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))) {
			t.Errorf("token in argv: %s", argv)
		}
		if strings.Contains(env, "ENV GH_TOKEN") || strings.Contains(env, "ENV GIT_DIR") {
			t.Errorf("git%s inherits GH_TOKEN or GIT_DIR:\n%s", argv, env)
		}
		if !strings.Contains(env, "ENV GIT_CONFIG_GLOBAL=/dev/null") || !strings.Contains(env, "ENV GIT_CONFIG_NOSYSTEM=1") {
			t.Errorf("git%s runs without GIT_CONFIG_GLOBAL=/dev/null and GIT_CONFIG_NOSYSTEM=1:\n%s", argv, env)
		}
		if !strings.Contains(env, header) {
			if strings.Contains(env, "GIT_CONFIG_COUNT") {
				t.Errorf("git%s has GIT_CONFIG_COUNT without the header:\n%s", argv, env)
			}
			continue
		}
		withToken++
		if !strings.Contains(argv, " fetch ") || !strings.Contains(argv, " "+f.remote+" ") {
			t.Errorf("token in the env of git%s, want only the fetch from %s", argv, f.remote)
		}
		if !strings.Contains(env, "ENV GIT_CONFIG_KEY_0=http.extraHeader") || !strings.Contains(env, "ENV GIT_CONFIG_COUNT=1") {
			t.Errorf("fetch env lacks the http.extraHeader key or count:\n%s", env)
		}
	}
	// The fetch from the forge URL spawns no git of its own on a file path, so exactly one.
	if withToken != 1 {
		t.Errorf("%d git commands saw the token, want exactly 1\n%s", withToken, text)
	}
}

func TestCovers(t *testing.T) {
	const head = "1111111111111111111111111111111111111111"
	const fix1 = "2222222222222222222222222222222222222222"
	const fix2 = "3333333333333333333333333333333333333333"
	fixed := func(sha string, commits ...string) roborevRecord {
		r := roborevRecord{Outcome: "done", SHA: sha}
		for _, c := range commits {
			r.Decisions = append(r.Decisions, roborevDecision{Outcome: "fixed", Commit: c})
		}
		return r
	}
	cases := []struct {
		name   string
		record roborevRecord
		after  []string
		want   bool
	}{
		{"done on the sha", fixed(head), nil, true},
		{"skip on the sha", roborevRecord{Outcome: "skip", SHA: head}, nil, false},
		{"done on an older sha, nothing listed", fixed(fix1), []string{head}, false},
		{"every later commit fixed", fixed(fix1, head, fix2), []string{fix2, head}, true},
		{"abbreviated fix commit", fixed(fix1, head[:9]), []string{head}, true},
		{"one later commit not fixed", fixed(fix1, fix2), []string{fix2, head}, false},
		{"later commit only refuted", roborevRecord{Outcome: "done", SHA: fix1, Decisions: []roborevDecision{{Outcome: "refuted", Commit: head}}}, []string{head}, false},
		{"too short a prefix", fixed(fix1, head[:4]), []string{head}, false},
		{"older sha with nothing after", fixed(fix1), nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := covers(c.record, head, c.after); got != c.want {
				t.Errorf("covers = %v, want %v", got, c.want)
			}
		})
	}
}

func TestRemoteURL(t *testing.T) {
	cases := []struct {
		repo    repoconfig.Repo
		want    string
		wantErr bool
	}{
		{repoconfig.Repo{Slug: "owner/name", Kind: "github"}, "https://github.com/owner/name.git", false},
		{repoconfig.Repo{Slug: "owner/name", Kind: "github", Host: "github.com"}, "https://github.com/owner/name.git", false},
		{repoconfig.Repo{Slug: "owner/name", Kind: "github", Host: "ghe.example.com"}, "", true},
		{repoconfig.Repo{Slug: "owner/name", Kind: "gitlab"}, "", true},
		{repoconfig.Repo{Slug: "-x/name", Kind: "github"}, "", true},
		{repoconfig.Repo{Slug: "", Kind: "github"}, "", true},
	}
	for _, c := range cases {
		got, err := RemoteURL(c.repo)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("RemoteURL(%+v) = %q, %v; want %q, err %v", c.repo, got, err, c.want, c.wantErr)
		}
	}
}

// lookupOnly is a Workplace that knows only Lookup, and only the workspaces it holds.
type lookupOnly struct {
	workplace.Workplace
	paths map[string]string
}

func (l lookupOnly) Lookup(_ context.Context, id string) (workplace.Workspace, error) {
	p, ok := l.paths[id]
	if !ok {
		return workplace.Workspace{}, workplace.ErrGone
	}
	return workplace.Workspace{ID: id, Path: p, Branch: branch}, nil
}

func TestResolveWorktreeTakesLastHandoffBeforeComment(t *testing.T) {
	ctx := context.Background()
	s, err := store.OpenShared(ctx, filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	t0 := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	add := func(key, ws string, started time.Time, outcome runs.Outcome) {
		r, err := runs.Take(ctx, s.DB, runs.Claim{Repo: "o/r", Issue: 1, SessionID: runs.NewSessionID(), Mode: "start",
			Trigger: "handoff", TriggerKey: key, Branch: branch, Now: started})
		if err != nil {
			t.Fatal(err)
		}
		if err := runs.SetWorkplace(ctx, s.DB, r.ID, ws, branch); err != nil {
			t.Fatal(err)
		}
		if err := runs.Release(ctx, s.DB, r.ID, outcome, "", started.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	add("start:1", "ws-old", t0, runs.Handoff)
	add("handoff:10", "ws-right", t0.Add(time.Hour), runs.Handoff)
	add("handoff:11", "ws-question", t0.Add(2*time.Hour), runs.Question)
	add("handoff:12", "ws-later", t0.Add(4*time.Hour), runs.Handoff)
	w := lookupOnly{paths: map[string]string{"ws-old": "/w/old", "ws-right": "/w/right", "ws-question": "/w/q", "ws-later": "/w/later"}}
	comment := t0.Add(3 * time.Hour)

	got, err := ResolveWorktree(ctx, s.DB, w, Request{Repo: "o/r", Issue: 1, Worktree: "/w/right/"}, comment)
	if err != nil || got != "/w/right" {
		t.Errorf("ResolveWorktree = %q, %v; want /w/right", got, err)
	}
	if _, err := ResolveWorktree(ctx, s.DB, w, Request{Repo: "o/r", Issue: 2, Worktree: "/w/right"}, comment); err == nil {
		t.Error("ResolveWorktree without a handoff row of the issue succeeded")
	}
}

func TestFloorRefusesHandoffPathMismatch(t *testing.T) {
	ctx := context.Background()
	s, err := store.OpenShared(ctx, filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	t0 := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	r, err := runs.Take(ctx, s.DB, runs.Claim{Repo: "o/r", Issue: 1, SessionID: runs.NewSessionID(), Mode: "start",
		Trigger: "handoff", TriggerKey: "start:1", Branch: branch, Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.SetWorkplace(ctx, s.DB, r.ID, "ws-1", branch); err != nil {
		t.Fatal(err)
	}
	if err := runs.Release(ctx, s.DB, r.ID, runs.Handoff, "", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	w := lookupOnly{paths: map[string]string{"ws-1": "/w/run"}}
	got, err := ResolveWorktree(ctx, s.DB, w, Request{Repo: "o/r", Issue: 1, Worktree: "/w/elsewhere"}, t0.Add(time.Hour))
	if err == nil {
		t.Fatalf("ResolveWorktree = %q with a handoff path that is not the run's workspace, want an error", got)
	}
	if !strings.Contains(err.Error(), "/w/elsewhere") || !strings.Contains(err.Error(), "/w/run") {
		t.Errorf("error %q names not both paths", err)
	}
}

// A record on a commit that is in the clone but off the branch never covers, even when it
// lists every commit after it as fixed.
func TestFloorRefusesRecordOffTheBranch(t *testing.T) {
	f := newFixture(t)
	feature := f.head()
	tgit(t, f.main, "checkout", "-q", "main")
	write(t, filepath.Join(f.main, "main.txt"), "main\n")
	tgit(t, f.main, "add", "main.txt")
	tgit(t, f.main, "commit", "-q", "-m", "main moves")
	tgit(t, f.main, "push", "-q", "origin", "main")
	f.branchRecord(done(tgit(t, f.main, "rev-parse", "main"), feature))
	_, red := floor(t, f.req)
	wantStep(t, red, StepRoborev)
}

func TestFloorRefusesOctopusMerge(t *testing.T) {
	f := newFixture(t)
	tgit(t, f.main, "checkout", "-q", "-b", "other")
	write(t, filepath.Join(f.main, "other.txt"), "other\n")
	tgit(t, f.main, "add", "other.txt")
	tgit(t, f.main, "commit", "-q", "-m", "other")
	tgit(t, f.main, "checkout", "-q", "main")
	write(t, filepath.Join(f.main, "main.txt"), "main\n")
	tgit(t, f.main, "add", "main.txt")
	tgit(t, f.main, "commit", "-q", "-m", "main moves")
	tgit(t, f.main, "push", "-q", "origin", "main")
	tgit(t, f.wt, "fetch", "-q", "origin")
	// The second parent is on origin/main; the third is foreign.
	tgit(t, f.wt, "merge", "-q", "-m", "octopus", "origin/main", "other")
	if parents := strings.Fields(tgit(t, f.wt, "rev-list", "--parents", "-n1", "HEAD")); len(parents) != 4 {
		t.Fatalf("setup: HEAD has parents %v, want an octopus of three", parents[1:])
	}
	f.branchRecord(done(f.head()))
	_, red := floor(t, f.req)
	wantStep(t, red, StepOwnCommits)
}

// Run-controlled config and index flags must not hide a dirty worktree.
func TestFloorDirtyDespiteWorktreeSettings(t *testing.T) {
	for name, setup := range map[string]func(f *fixture){
		"showUntrackedFiles=no": func(f *fixture) {
			tgit(f.t, f.wt, "config", "status.showUntrackedFiles", "no")
			write(f.t, filepath.Join(f.wt, "new.txt"), "new\n")
		},
		"assume-unchanged": func(f *fixture) {
			tgit(f.t, f.wt, "update-index", "--assume-unchanged", "feature.txt")
			write(f.t, filepath.Join(f.wt, "feature.txt"), "changed\n")
		},
		"skip-worktree": func(f *fixture) {
			tgit(f.t, f.wt, "update-index", "--skip-worktree", "feature.txt")
			write(f.t, filepath.Join(f.wt, "feature.txt"), "changed\n")
		},
		"core.ignoreStat": func(f *fixture) {
			tgit(f.t, f.wt, "config", "core.ignoreStat", "true")
			tgit(f.t, f.wt, "update-index", "--really-refresh")
			write(f.t, filepath.Join(f.wt, "feature.txt"), "changed\n")
		},
		"untracked in a new directory": func(f *fixture) {
			write(f.t, filepath.Join(f.wt, "deep", "er", "new.txt"), "new\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.branchRecord(done(f.head()))
			setup(f)
			_, red := floor(t, f.req)
			wantStep(t, red, StepClean)
		})
	}
}

// An ignored file is no dirt, and a committed file the worktree does not change is clean even
// when its stat info is stale.
func TestFloorCleanWithIgnoredFileAndStaleStat(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.wt, ".gitignore"), "build/\n")
	f.branchRecord(done(f.commit(".gitignore", "build/\n")))
	write(t, filepath.Join(f.wt, "build", "out.bin"), "x\n")
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Join(f.wt, "feature.txt"), future, future); err != nil {
		t.Fatal(err)
	}
	_, red := floor(t, f.req)
	wantGreen(t, red)
}

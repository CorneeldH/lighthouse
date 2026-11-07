//go:build linux

package local

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com",
		"-c", "commit.gpgsign=false", "-c", "protocol.file.allow=always"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func gitFails(t *testing.T, dir string, args ...string) bool {
	t.Helper()
	return exec.Command("git", append([]string{"-C", dir}, args...)...).Run() != nil
}

func commit(t *testing.T, dir, file string) string {
	t.Helper()
	writeFile(t, filepath.Join(dir, file), file)
	git(t, dir, "add", file)
	git(t, dir, "commit", "-m", file)
	return git(t, dir, "rev-parse", "HEAD")
}

// newRepo makes a bare origin with main at commit m1, repoDir a clone of it (the main
// checkout), and other a second clone to push branches from. Neither has a .gitignore.
func newRepo(t *testing.T) (repoDir, other string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origin := filepath.Join(root, "origin.git")
	repoDir, other = filepath.Join(root, "repo"), filepath.Join(root, "other")
	git(t, root, "init", "--bare", "-b", "main", origin)
	git(t, root, "clone", origin, other)
	git(t, other, "checkout", "-b", "main")
	commit(t, other, "m1")
	git(t, other, "push", "origin", "main")
	git(t, root, "clone", origin, repoDir)
	return repoDir, other
}

func prepare(t *testing.T, a *Adapter, branch string) workplace.Workspace {
	t.Helper()
	ws, err := a.Prepare(t.Context(), branch)
	if err != nil {
		t.Fatalf("Prepare %s: %v", branch, err)
	}
	return ws
}

func record(t *testing.T, a *Adapter, id string) wsRecord {
	t.Helper()
	s, err := a.store()
	if err != nil {
		t.Fatal(err)
	}
	rec, ok, err := s.workspace(id)
	if err != nil || !ok {
		t.Fatalf("record %s: ok %v, err %v", id, ok, err)
	}
	return rec
}

func TestCheckLocal(t *testing.T) {
	for id, ok := range map[string]bool{
		"local-issue-1-x-abcdef": true,
		"local-a.b_c-0":          true,
		"ws1":                    false,
		"":                       false,
		"local-":                 false,
		"local-a/b":              false,
		"local-a b":              false,
		"xlocal-a":               false,
		"LOCAL-a":                false,
	} {
		err := checkLocal(id)
		if (err == nil) != ok {
			t.Errorf("checkLocal(%q) = %v, want ok %v", id, err, ok)
		}
		if err != nil && !strings.Contains(err.Error(), "is not a local workspace") {
			t.Errorf("checkLocal(%q) = %v, want \"is not a local workspace\"", id, err)
		}
	}
}

func TestNewID(t *testing.T) {
	re := regexp.MustCompile(`^local-a-b-c_d.e-[0-9a-f]{6}$`)
	first, second := newID("a/b c_d.e"), newID("a/b c_d.e")
	if !re.MatchString(first) || !re.MatchString(second) {
		t.Errorf("newID = %q, %q; want a match of %s", first, second, re)
	}
	if first == second {
		t.Errorf("newID twice = %q, want a fresh ID each time", first)
	}
}

func TestPrepareNewBranchFromDefault(t *testing.T) {
	repo, other := newRepo(t)
	m1 := git(t, other, "rev-parse", "main")
	a, _, _ := newAdapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	if !regexp.MustCompile(`^local-issue-1-x-[0-9a-f]{6}$`).MatchString(ws.ID) {
		t.Errorf("ID = %q, want local-issue-1-x-<6 hex>", ws.ID)
	}
	if want := filepath.Join(repo, ".claude", "worktrees", ws.ID); ws.Path != want {
		t.Errorf("Path = %q, want %q", ws.Path, want)
	}
	if ws.Branch != "issue-1-x" {
		t.Errorf("Branch = %q, want issue-1-x", ws.Branch)
	}
	if got := git(t, ws.Path, "rev-parse", "HEAD"); got != m1 {
		t.Errorf("HEAD = %s, want m1 %s", got, m1)
	}
	if got := git(t, ws.Path, "rev-parse", "--abbrev-ref", "HEAD"); got != "issue-1-x" {
		t.Errorf("checked out = %s, want issue-1-x", got)
	}
	if !gitFails(t, ws.Path, "rev-parse", "--abbrev-ref", "issue-1-x@{upstream}") {
		t.Error("issue-1-x has an upstream, want none")
	}
	rec := record(t, a, ws.ID)
	if rec.Adopted || rec.Path != ws.Path || rec.Branch != "issue-1-x" || rec.CreatedAt.IsZero() {
		t.Errorf("record = %+v, want not adopted, path %s, branch issue-1-x and a creation time", rec, ws.Path)
	}
}

func TestPrepareTracksRemoteBranch(t *testing.T) {
	repo, other := newRepo(t)
	git(t, other, "checkout", "-b", "issue-1-x")
	b := commit(t, other, "b")
	git(t, other, "push", "origin", "issue-1-x")
	a, _, _ := newAdapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	if got := git(t, ws.Path, "rev-parse", "HEAD"); got != b {
		t.Errorf("HEAD = %s, want b %s", got, b)
	}
	if got := git(t, ws.Path, "rev-parse", "--abbrev-ref", "issue-1-x@{upstream}"); got != "origin/issue-1-x" {
		t.Errorf("upstream = %s, want origin/issue-1-x", got)
	}
}

func TestPrepareKeepsLocalBranchCommits(t *testing.T) {
	repo, _ := newRepo(t)
	git(t, repo, "checkout", "-b", "issue-1-x")
	c := commit(t, repo, "c")
	git(t, repo, "checkout", "main")
	a, _, _ := newAdapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	if got := git(t, ws.Path, "rev-parse", "HEAD"); got != c {
		t.Errorf("HEAD = %s, want c %s", got, c)
	}
	if record(t, a, ws.ID).Adopted {
		t.Error("record adopted, want made by the adapter")
	}
}

// A local branch is found, not created: origin/<branch> must not reset it.
func TestPrepareKeepsLocalBranchCommitsOverRemote(t *testing.T) {
	repo, other := newRepo(t)
	git(t, repo, "checkout", "-b", "issue-1-x")
	c := commit(t, repo, "c")
	git(t, repo, "checkout", "main")
	git(t, other, "checkout", "-b", "issue-1-x")
	commit(t, other, "b")
	git(t, other, "push", "origin", "issue-1-x")
	a, _, _ := newAdapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	if got := git(t, ws.Path, "rev-parse", "HEAD"); got != c {
		t.Errorf("HEAD = %s, want c %s", got, c)
	}
}

func TestPrepareRefusesMainCheckoutBranch(t *testing.T) {
	repo, _ := newRepo(t)
	a, _, _ := newAdapter(t, repo)
	_, err := a.Prepare(t.Context(), "main")
	if err == nil || !strings.Contains(err.Error(), "is checked out in the main checkout") {
		t.Fatalf("Prepare(main) = %v, want \"is checked out in the main checkout\"", err)
	}
	s, err := a.store()
	if err != nil {
		t.Fatal(err)
	}
	if rs, err := s.workspaces(); len(rs) != 0 || err != nil {
		t.Errorf("records = %+v, %v; want none", rs, err)
	}
}

func TestPrepareRefusesBadBranch(t *testing.T) {
	repo, _ := newRepo(t)
	a, _, _ := newAdapter(t, repo)
	for _, b := range []string{"", "-x", "--force"} {
		if _, err := a.Prepare(t.Context(), b); err == nil {
			t.Errorf("Prepare(%q): err = nil, want an error", b)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, ".claude")); !os.IsNotExist(err) {
		t.Errorf("stat .claude: %v, want not exist", err)
	}
}

func TestPrepareAdoptsForeignWorktree(t *testing.T) {
	repo, _ := newRepo(t)
	mine := filepath.Join(filepath.Dir(repo), "mine")
	git(t, repo, "worktree", "add", mine, "-b", "issue-1-x")
	a, _, _ := newAdapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	if ws.Path != mine {
		t.Errorf("Path = %q, want %q", ws.Path, mine)
	}
	if !record(t, a, ws.ID).Adopted {
		t.Error("record not adopted, want adopted")
	}
	entries, err := os.ReadDir(filepath.Join(repo, ".claude", "worktrees"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf(".claude/worktrees holds %d entries, want none", len(entries))
	}
	// Found again, it keeps its record: same ID, still adopted.
	again := prepare(t, a, "issue-1-x")
	if again.ID != ws.ID || !record(t, a, ws.ID).Adopted {
		t.Errorf("second Prepare: ID %q (want %q), adopted %v", again.ID, ws.ID, record(t, a, ws.ID).Adopted)
	}
}

func TestPrepareKeepsOwnRecordNotAdopted(t *testing.T) {
	repo, _ := newRepo(t)
	a, _, _ := newAdapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	if again := prepare(t, a, "issue-1-x"); again.ID != ws.ID || again.Path != ws.Path {
		t.Errorf("second Prepare = %+v, want %+v", again, ws)
	}
	if record(t, a, ws.ID).Adopted {
		t.Error("record adopted after a second Prepare, want made by the adapter")
	}
}

func TestPrepareSlashAndDashBranches(t *testing.T) {
	repo, _ := newRepo(t)
	a, _, _ := newAdapter(t, repo)
	slash, dash := prepare(t, a, "a/b"), prepare(t, a, "a-b")
	if slash.ID == dash.ID || slash.Path == dash.Path {
		t.Errorf("a/b = %+v, a-b = %+v; want different IDs and paths", slash, dash)
	}
	if got := git(t, slash.Path, "rev-parse", "--abbrev-ref", "HEAD"); got != "a/b" {
		t.Errorf("a/b worktree on %s", got)
	}
	if got := git(t, dash.Path, "rev-parse", "--abbrev-ref", "HEAD"); got != "a-b" {
		t.Errorf("a-b worktree on %s", got)
	}
}

func TestPrepareKeepsMainCheckoutClean(t *testing.T) {
	repo, _ := newRepo(t)
	a, _, _ := newAdapter(t, repo)
	prepare(t, a, "issue-1-x")
	if got := git(t, repo, "status", "--porcelain"); got != "" {
		t.Errorf("status of the main checkout = %q, want clean", got)
	}
	prepare(t, a, "issue-2-x")
	raw, err := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "/.claude/worktrees/" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("exclude holds the line %d times, want 1:\n%s", n, raw)
	}
}

func TestPrepareVanishedWorktreeFreshID(t *testing.T) {
	repo, _ := newRepo(t)
	a, _, _ := newAdapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	if err := os.RemoveAll(ws.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Lookup(t.Context(), ws.ID); !errors.Is(err, workplace.ErrGone) {
		t.Fatalf("Lookup = %v, want ErrGone", err)
	}
	if list := git(t, repo, "worktree", "list", "--porcelain"); strings.Contains(list, ws.Path) {
		t.Errorf("worktree list still names %s:\n%s", ws.Path, list)
	}
	again := prepare(t, a, "issue-1-x")
	if again.ID == ws.ID || again.Path == ws.Path {
		t.Errorf("Prepare after a vanished worktree = %+v, want a fresh ID and path (old %+v)", again, ws)
	}
	if _, err := os.Stat(again.Path); err != nil {
		t.Errorf("new worktree: %v", err)
	}
}

func TestLookupBranchFromGit(t *testing.T) {
	repo, _ := newRepo(t)
	a, _, _ := newAdapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	git(t, ws.Path, "checkout", "-b", "other")
	got, err := a.Lookup(t.Context(), ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := (workplace.Workspace{ID: ws.ID, Path: ws.Path, Branch: "other"}); got != want {
		t.Errorf("Lookup = %+v, want %+v", got, want)
	}
}

func TestLookupVanishedButMarked(t *testing.T) {
	repo, _ := newRepo(t)
	a, procs, _ := newAdapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	s, err := a.store()
	if err != nil {
		t.Fatal(err)
	}
	sid := "4b0f8f4e-6a3e-4f3e-9d0e-0123456789ab"
	if err := s.putSession(ws.ID, sessRecord{Key: sid + ".0a1b2c3d", SessionID: sid}); err != nil {
		t.Fatal(err)
	}
	procs.marked[sid] = []int{4242}
	if err := os.RemoveAll(ws.Path); err != nil {
		t.Fatal(err)
	}

	got, err := a.Lookup(t.Context(), ws.ID)
	if err != nil {
		t.Fatalf("Lookup with a marked process: %v", err)
	}
	if want := (workplace.Workspace{ID: ws.ID, Path: ws.Path, Branch: "issue-1-x"}); got != want {
		t.Errorf("Lookup = %+v, want %+v", got, want)
	}

	procs.err = errors.New("cannot read /proc")
	if _, err := a.Lookup(t.Context(), ws.ID); err == nil || errors.Is(err, workplace.ErrGone) {
		t.Errorf("Lookup when processes cannot be measured = %v, want an error that is not ErrGone", err)
	}

	procs.err = nil
	procs.marked[sid] = nil
	if _, err := a.Lookup(t.Context(), ws.ID); !errors.Is(err, workplace.ErrGone) {
		t.Errorf("Lookup without a marked process = %v, want ErrGone", err)
	}
}

func TestLookupUnknownIsGone(t *testing.T) {
	repo, _ := newRepo(t)
	a, _, _ := newAdapter(t, repo)
	if _, err := a.Lookup(t.Context(), "local-never-000000"); !errors.Is(err, workplace.ErrGone) {
		t.Errorf("Lookup of an unknown ID = %v, want ErrGone", err)
	}
}

func TestForeignIDIsNoLocalWorkspace(t *testing.T) {
	a, procs, _ := newAdapter(t, t.TempDir())
	runs := 0
	a.Run = func(context.Context, time.Duration, string, ...string) (string, int, error) {
		runs++
		return "", 0, nil
	}
	ctx, id, sids := t.Context(), "ws1", []string{"4b0f8f4e-6a3e-4f3e-9d0e-0123456789ab"}
	_, spawnErr := a.Spawn(ctx, workplace.Workspace{ID: id, Path: "/p", Branch: "b"}, workplace.LaunchSpec{})
	_, sessErr := a.Sessions(ctx, id)
	_, lookErr := a.Lookup(ctx, id)
	_, transErr := a.Transcript(ctx, id, sids[0])
	errs := map[string]error{
		"Spawn":      spawnErr,
		"Sessions":   sessErr,
		"Stop":       a.Stop(ctx, id, sids),
		"Kill":       a.Kill(ctx, id, sids),
		"Remove":     a.Remove(ctx, id),
		"Lookup":     lookErr,
		"Discard":    a.Discard(ctx, id),
		"Transcript": transErr,
	}
	for name, err := range errs {
		if err == nil || errors.Is(err, workplace.ErrGone) || !strings.Contains(err.Error(), "ws1 is not a local workspace") {
			t.Errorf("%s = %v, want \"ws1 is not a local workspace\", not ErrGone", name, err)
		}
	}
	if !errors.Is(spawnErr, workplace.ErrNotAccepted) {
		t.Errorf("Spawn = %v, want ErrNotAccepted", spawnErr)
	}
	if runs != 0 || procs.calls != 0 {
		t.Errorf("runs = %d, process calls = %d; want 0 and 0", runs, procs.calls)
	}
}

func TestRecordsSurviveNewAdapter(t *testing.T) {
	repo, _ := newRepo(t)
	a, _, _ := newAdapter(t, repo)
	ws := prepare(t, a, "issue-1-x")
	b := &Adapter{
		RepoDir: a.RepoDir, DefaultBranch: a.DefaultBranch, Repo: a.Repo, StateDir: a.StateDir,
		Run: workplace.ExecRunner(), Procs: newFakeProcs(),
	}
	got, err := b.Lookup(t.Context(), ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != ws.Path {
		t.Errorf("Lookup on a new adapter: Path %q, want %q", got.Path, ws.Path)
	}
}

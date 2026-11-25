package nodeserve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/testpath"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// followBranch differs from the git_head_ref the fake forge reports: follow takes the tick's branch.
const followBranch = "issue-2-follow"

// git runs git in dir with a fixed identity and returns its trimmed stdout.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=Test", "-c", "user.email=test@example.test", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, stderr)
	}
	return strings.TrimSpace(string(out))
}

// commitFile writes name in dir, commits it and returns the new HEAD.
func commitFile(t *testing.T, dir, name string) string {
	t.Helper()
	writeFile(t, dir, name, name+"\n")
	git(t, dir, "add", name)
	git(t, dir, "commit", "-q", "-m", name)
	return git(t, dir, "rev-parse", "HEAD")
}

// origin makes a bare origin with main and followBranch, one commit ahead of main, and
// returns it with the seed clone that pushes to it.
func origin(t *testing.T) (bare, seed string) {
	t.Helper()
	bare = t.TempDir()
	git(t, bare, "init", "-q", "--bare", "--initial-branch=main")
	seed = t.TempDir()
	git(t, seed, "init", "-q", "--initial-branch=main")
	git(t, seed, "remote", "add", "origin", bare)
	commitFile(t, seed, "a.txt")
	git(t, seed, "push", "-q", "origin", "main")
	git(t, seed, "checkout", "-q", "-b", followBranch)
	commitFile(t, seed, "b.txt")
	git(t, seed, "push", "-q", "origin", followBranch)
	return bare, seed
}

// clone clones bare into a fresh temp dir on main.
func clone(t *testing.T, bare string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ws")
	if out, err := exec.Command("git", "clone", "-q", "--branch", "main", bare, dir).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v: %s", err, out)
	}
	return dir
}

func follow(f fixture, id string, created bool) *httptest.ResponseRecorder {
	return send(f.h, http.MethodPost, nodeapi.Prefix+"/workspaces/"+id+"/follow",
		nodeapi.FollowRequest{Branch: followBranch, Created: created})
}

func TestFollowSwitchesBranchWithoutOwnWork(t *testing.T) {
	bare, seed := origin(t)
	ws := clone(t, bare)
	want := git(t, seed, "rev-parse", "HEAD")
	f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)

	rec := follow(f, "ws-1", false)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 2xx: %s", rec.Code, rec.Body.String())
	}
	if got := git(t, ws, "rev-parse", "--abbrev-ref", "HEAD"); got != followBranch {
		t.Errorf("branch = %q, want %q", got, followBranch)
	}
	if got := git(t, ws, "rev-parse", "HEAD"); got != want {
		t.Errorf("HEAD = %s, want origin/%s %s", got, followBranch, want)
	}
}

func TestFollowOwnWorkIs409WithFields(t *testing.T) {
	bare, _ := origin(t)
	ws := clone(t, bare)
	commitFile(t, ws, "own1.txt")
	head := commitFile(t, ws, "own2.txt")
	writeFile(t, ws, "untracked.txt", "x\n")
	f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)

	rec := follow(f, "ws-1", false)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	var got nodeapi.OwnWork
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("answer is not an OwnWork: %v: %s", err, rec.Body.String())
	}
	if got.Code != "own_work" || got.Commits != 2 || !got.Dirty {
		t.Errorf("own work = %+v, want code own_work, 2 commits, dirty", got)
	}
	if got.Workspace.ID != "ws-1" || got.Workspace.Branch != followBranch {
		t.Errorf("workspace = %+v, want ws-1 on %s", got.Workspace, followBranch)
	}
	if now := git(t, ws, "rev-parse", "HEAD"); now != head {
		t.Errorf("HEAD moved to %s, want it to stay at %s", now, head)
	}
}

func TestFollowFastForwardsFoundWorkspace(t *testing.T) {
	bare, seed := origin(t)
	ws := clone(t, bare)
	git(t, ws, "checkout", "-q", followBranch)
	want := commitFile(t, seed, "c.txt")
	git(t, seed, "push", "-q", "origin", followBranch)
	f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)

	rec := follow(f, "ws-1", false)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 2xx: %s", rec.Code, rec.Body.String())
	}
	if got := git(t, ws, "rev-parse", "HEAD"); got != want {
		t.Errorf("HEAD = %s, want the new origin/%s %s", got, followBranch, want)
	}
}

func TestFollowUnsafeBranchIs400BeforeAnyCall(t *testing.T) {
	f := newFixture(t, map[string]string{"ws-1": "/nowhere"}, nodeToken)
	for _, branch := range []string{"", "-x", "--upload-pack=evil", "a b", "a..b", "a\x00b"} {
		rec := send(f.h, http.MethodPost, nodeapi.Prefix+"/workspaces/ws-1/follow", nodeapi.FollowRequest{Branch: branch})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400", branch, rec.Code)
		}
	}
	if n := f.forge.hits.Load(); n != 0 {
		t.Errorf("forge requests = %d, want 0", n)
	}
	if n := f.runner.calls.Load(); n != 0 {
		t.Errorf("runner calls = %d, want 0", n)
	}
}

func gitState(t *testing.T, f fixture, id, mergedHead string) nodeapi.GitState {
	t.Helper()
	path := nodeapi.Prefix + "/workspaces/" + id + "/git"
	if mergedHead != "" {
		path += "?merged_head=" + url.QueryEscape(mergedHead)
	}
	rec := send(f.h, http.MethodGet, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var s nodeapi.GitState
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatalf("answer is not a git state: %v: %s", err, rec.Body.String())
	}
	return s
}

func TestGitReportsDirtyAndUnpushed(t *testing.T) {
	bare, _ := origin(t)
	cases := []struct {
		name  string
		setup func(t *testing.T, ws string)
		want  nodeapi.GitState
	}{
		{"clean and pushed", func(*testing.T, string) {}, nodeapi.GitState{}},
		{"untracked file", func(t *testing.T, ws string) { writeFile(t, ws, "new.txt", "x\n") }, nodeapi.GitState{Dirty: true}},
		{"local commit", func(t *testing.T, ws string) { commitFile(t, ws, "own.txt") }, nodeapi.GitState{Unpushed: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws := clone(t, bare)
			c.setup(t, ws)
			f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)
			if got := gitState(t, f, "ws-1", ""); got != c.want {
				t.Errorf("git = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestGitMergedHeadExemptsUnpushed(t *testing.T) {
	bare, _ := origin(t)
	ws := clone(t, bare)
	mainHead := git(t, ws, "rev-parse", "HEAD")
	own := commitFile(t, ws, "own.txt")
	f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)
	cases := []struct {
		name, mergedHead string
		wantUnpushed     bool
	}{
		{"HEAD is the merged head", own, false},
		{"HEAD is ahead of the merged head", mainHead, true},
		{"no merged head", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := gitState(t, f, "ws-1", c.mergedHead)
			if got.Unpushed != c.wantUnpushed {
				t.Errorf("unpushed = %v, want %v", got.Unpushed, c.wantUnpushed)
			}
			direct, err := workplace.Unpushed(context.Background(), workplace.ExecRunner(), ws, c.mergedHead)
			if err != nil {
				t.Fatal(err)
			}
			if got.Unpushed != direct {
				t.Errorf("unpushed = %v, workplace.Unpushed = %v: not the same", got.Unpushed, direct)
			}
		})
	}
}

func TestGitMergedHeadMustBe40Hex(t *testing.T) {
	f := newFixture(t, map[string]string{"ws-1": "/nowhere"}, nodeToken)
	for _, head := range []string{
		"abc",
		strings.Repeat("A", 40),
		strings.Repeat("a", 41),
		strings.Repeat("g", 40),
		"-" + strings.Repeat("a", 39),
	} {
		rec := send(f.h, http.MethodGet, nodeapi.Prefix+"/workspaces/ws-1/git?merged_head="+url.QueryEscape(head), nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400", head, rec.Code)
		}
	}
	if n := f.forge.hits.Load(); n != 0 {
		t.Errorf("forge requests = %d, want 0", n)
	}
	if n := f.runner.calls.Load(); n != 0 {
		t.Errorf("runner calls = %d, want 0", n)
	}
}

func relocate(f fixture, id, command string) *httptest.ResponseRecorder {
	return send(f.h, http.MethodPost, nodeapi.Prefix+"/workspaces/"+id+"/relocate", nodeapi.Relocation{Command: command})
}

func TestRelocateFindsPathInNodeWorktree(t *testing.T) {
	testpath.SkipOnWindowsPOSIXAbs(t)
	repo := gitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "plans"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "plans"), "p.md", "plan\n")
	f := newFixture(t, map[string]string{"ws-1": repo}, nodeToken)

	rec := relocate(f, "ws-1", "/lighthouse-x /home/elsewhere/checkout/plans/p.md")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got nodeapi.Relocation
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if want := "/lighthouse-x " + filepath.Join(root, "plans", "p.md"); got.Command != want {
		t.Errorf("command = %q, want %q", got.Command, want)
	}
}

func TestRelocateErrorIs4xxWithDetail(t *testing.T) {
	repo := gitRepo(t)
	f := newFixture(t, map[string]string{"ws-1": repo}, nodeToken)

	rec := relocate(f, "ws-1", "/lighthouse-x /home/elsewhere/checkout/plans/missing.md")
	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("status = %d, want 4xx: %s", rec.Code, rec.Body.String())
	}
	if p := problem(t, rec); !strings.Contains(p.Detail, "/home/elsewhere/checkout/plans/missing.md") {
		t.Errorf("detail = %q, want it to name the path", p.Detail)
	}
}

func TestSessionsMirrorsForgeAgentSessions(t *testing.T) {
	// The body has the shape kenn-forge's /agent-sessions answers, as kennforge.Adapter.Sessions
	// reads it: runtime_session_key, state (null allowed), session_id, updated_at.
	ff := &fakeForge{sessions: map[string]string{"ws-1": `{"sessions":[
		{"runtime_session_key":"a","state":"working","session_id":"s1","updated_at":"2026-10-31T10:00:00Z"},
		{"runtime_session_key":"b","state":null,"session_id":"s2","updated_at":"garbage"},
		{"state":"idle","session_id":"s3"}]}`}}
	f := newForgeFixture(t, ff, nodeToken)

	rec := send(f.h, http.MethodGet, nodeapi.Prefix+"/workspaces/ws-1/sessions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got []nodeapi.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("answer is not a session list: %v: %s", err, rec.Body.String())
	}
	want := []nodeapi.Session{
		{Key: "a", State: "working", SessionID: "s1", UpdatedAt: "2026-10-31T10:00:00Z"},
		{Key: "b", State: "", SessionID: "s2", UpdatedAt: ""},
	}
	if len(got) != len(want) {
		t.Fatalf("sessions = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("session %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	gone := send(f.h, http.MethodGet, nodeapi.Prefix+"/workspaces/ws-9/sessions", nil)
	if gone.Code != http.StatusNotFound {
		t.Fatalf("unknown workspace: status = %d, want 404: %s", gone.Code, gone.Body.String())
	}
	if p := problem(t, gone); p.Code != nodeapi.CodeWorkspaceGone {
		t.Errorf("code = %q, want workspace_gone", p.Code)
	}
}

func TestSessionsForgeDownIsNotGone(t *testing.T) {
	ff := &fakeForge{sessions: map[string]string{"ws-1": `{"sessions":[]}`}}
	f := newForgeFixture(t, ff, nodeToken)
	ff.fail.Store(true)

	rec := send(f.h, http.MethodGet, nodeapi.Prefix+"/workspaces/ws-1/sessions", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", rec.Code, rec.Body.String())
	}
	if p := problem(t, rec); p.Code != "" {
		t.Errorf("code = %q, want none", p.Code)
	}
}

func TestFollowCreatedSwitchesWhereFoundWouldNot(t *testing.T) {
	// A workspace the hub made just now sits at origin/HEAD: follow switches it to the branch,
	// where a found one with the same commit on main would be own work.
	bare, seed := origin(t)
	ws := clone(t, bare)
	commitFile(t, ws, "own.txt")
	want := git(t, seed, "rev-parse", "HEAD")
	f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)

	rec := follow(f, "ws-1", true)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 2xx: %s", rec.Code, rec.Body.String())
	}
	if got := git(t, ws, "rev-parse", "HEAD"); got != want {
		t.Errorf("HEAD = %s, want origin/%s %s", got, followBranch, want)
	}
}

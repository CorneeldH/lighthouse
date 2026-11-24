// Package pusher is the deterministic step that pushes workflow changes for a controller run:
// the run's worktree is untrusted input, so every check reads the pusher's own bare clone.
package pusher

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/flock"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/roborev"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// The steps of the floor, in the order of spec §3; Red.Step is one of them.
const (
	StepBranch      = "branch"       // 1: the worktree is on the handoff branch, not the default
	StepClean       = "clean"        // 2: no change and no untracked file
	StepClone       = "clone"        // the own clone: lock, fetch of the SHA and of origin
	StepFastForward = "fast-forward" // 3: origin/<branch> is an ancestor of the SHA
	StepOwnCommits  = "own-commits"  // 4: only commits of this branch and update-merges
	StepRoborev     = "roborev"      // 5: a done record covers the SHA
	StepPause       = "pause"        // 6: the brake is not paused
)

// Request is one push: what tick resolved for the handoff, and where the pusher keeps state.
type Request struct {
	Repo, URL, Branch, Default, Worktree string
	Issue                                int
	RunID                                int64
	StateDir, CloneDir                   string
	// PauseFile is the pause flag tick reads (park.PauseFile of tick's environment).
	PauseFile string
	// ForgeToken is read-only and goes only into the env of the fetch from URL.
	ForgeToken string
	Permit     string
	// Config is [dispatch.workflow_push]; nil is the route off.
	Config *hostconfig.WorkflowPush
	// Off is why the route is off besides a missing Config, such as a forge other than github.com.
	Off string
	// Language is [project] language of the repo: the language of the comments.
	Language string
	// Guard are paths no reviewer bind may touch: the forge token file, the state and clone dir.
	Guard []string
}

// Checked is what a green floor read in the own clone.
type Checked struct {
	SHA, MergeBase string
	Files          []string // changed in MergeBase..SHA
	// AlreadyPushed is origin/<branch> holding the SHA already: there is nothing left to push.
	AlreadyPushed bool
}

// Red is the first red step and why.
type Red struct{ Step, Why string }

// repairable is whether the branch itself is wrong, so the run that wrote it can mend it: a
// policy or lint finding. A step that could not run (COULD NOT CHECK) is no mistake of the branch.
func (r Red) repairable() bool {
	return (r.Step == StepPolicy || r.Step == StepLint) && !strings.HasPrefix(r.Why, "COULD NOT CHECK:")
}

// lockPoll is how often a waiting pusher retries the clone lock.
const lockPoll = 50 * time.Millisecond

// Floor runs the hard checks 1–6 of spec §3 and stops at the first red one. The caller holds
// lock on req.CloneDir (LockClone) for the whole push, so no second pusher refreshes the clone
// in between. In the run's worktree Floor runs only rev-parse HEAD and rev-parse --abbrev-ref
// HEAD, and reads the roborev record as a file; the cleanliness check uses the own clone as
// git-dir and the worktree only as work-tree, and every other check reads the SHA in the clone.
func Floor(ctx context.Context, req Request, lock *CloneLock) (Checked, *Red) {
	sha, red := readWorktree(ctx, req)
	if red != nil {
		return Checked{}, red
	}
	if lock == nil || lock.f == nil || lock.dir != req.CloneDir {
		return Checked{}, &Red{StepClone, "COULD NOT CHECK: the caller does not hold the lock on " + req.CloneDir}
	}
	if red := fetchSHA(ctx, req, sha); red != nil {
		return Checked{}, red
	}
	if red := clean(ctx, req); red != nil {
		return Checked{}, red
	}
	if red := fetchOrigin(ctx, req); red != nil {
		return Checked{}, red
	}
	c := Checked{SHA: sha}
	if red := ownHistory(ctx, req, &c); red != nil {
		return Checked{}, red
	}
	// A SHA origin already holds is not pushed, so no review has to cover it.
	if !c.AlreadyPushed {
		if red := reviewed(ctx, req, sha); red != nil {
			return Checked{}, red
		}
	}
	if req.PauseFile == "" {
		return Checked{}, &Red{StepPause, "COULD NOT CHECK: no pause file given"}
	}
	if until, paused := park.ReadPause(req.PauseFile, time.Now()); paused {
		return Checked{}, &Red{StepPause, "the brake is paused until " + until.Format(time.RFC3339)}
	}
	return c, nil
}

var (
	fullSHA   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	recordSHA = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// readWorktree runs check 1 and reads the SHA once.
func readWorktree(ctx context.Context, req Request) (string, *Red) {
	if !filepath.IsAbs(req.Worktree) {
		return "", &Red{StepBranch, fmt.Sprintf("worktree %q is not an absolute path", req.Worktree)}
	}
	if info, err := os.Stat(req.Worktree); err != nil || !info.IsDir() {
		return "", &Red{StepBranch, fmt.Sprintf("worktree %s does not exist", req.Worktree)}
	}
	if !branchName(req.Branch) || !branchName(req.Default) {
		return "", &Red{StepBranch, fmt.Sprintf("branch %q or default %q is not a branch name", req.Branch, req.Default)}
	}
	if req.Branch == req.Default {
		return "", &Red{StepBranch, fmt.Sprintf("branch %s is the default branch", req.Branch)}
	}
	out, err := runGit(ctx, req.Worktree, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", &Red{StepBranch, err.Error()}
	}
	sha := strings.TrimSpace(out)
	if !fullSHA.MatchString(sha) {
		return "", &Red{StepBranch, fmt.Sprintf("HEAD %q is not a sha", sha)}
	}
	out, err = runGit(ctx, req.Worktree, nil, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", &Red{StepBranch, err.Error()}
	}
	if got := strings.TrimSpace(out); got != req.Branch {
		return "", &Red{StepBranch, fmt.Sprintf("the worktree is on %s, the handoff names %s", got, req.Branch)}
	}
	return sha, nil
}

// clean runs check 2 with the clone as git-dir and the worktree only as work-tree, against a
// fresh index of the SHA: config, filter drivers, info/attributes and index flags (assume-unchanged,
// skip-worktree) are the pusher's, never the run's. A change to a tracked file or any file that
// is not ignored is dirt.
func clean(ctx context.Context, req Request) *Red {
	tmp, err := os.MkdirTemp("", "lighthouse-push-index-")
	if err != nil {
		return &Red{StepClean, "COULD NOT CHECK: " + err.Error()}
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(tmp, "index")}
	tree := []string{"--git-dir=" + req.CloneDir, "--work-tree=" + req.Worktree}
	steps := [][]string{
		{"read-tree", runRef(req)},
		{"update-index", "-q", "--refresh"},
		{"diff-files", "--name-status", "--ignore-submodules=dirty"},
		{"ls-files", "--others", "--exclude-standard"},
	}
	var dirt strings.Builder
	for _, step := range steps {
		out, err := runGit(ctx, req.CloneDir, env, append(append([]string{}, tree...), step...)...)
		if err != nil {
			return &Red{StepClean, "COULD NOT CHECK: " + err.Error()}
		}
		dirt.WriteString(out)
	}
	if text := strings.TrimSpace(dirt.String()); text != "" {
		return &Red{StepClean, "the worktree is not clean:\n" + text}
	}
	return nil
}

func runRef(req Request) string { return "refs/pusher/runs/" + strconv.FormatInt(req.RunID, 10) }

var branchRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// branchName is a plain branch name that is safe in a refspec: no option, no glob, no `..`.
func branchName(b string) bool {
	return branchRe.MatchString(b) && !strings.HasPrefix(b, "-") && !strings.Contains(b, "..") &&
		!strings.HasPrefix(b, "/") && !strings.HasSuffix(b, "/") && !strings.HasSuffix(b, ".lock") && !strings.Contains(b, "//")
}

// CloneLock is the flock on <clone>.lock; Floor and the push run under one.
type CloneLock struct {
	dir string
	f   *os.File
}

// Unlock releases the lock; a second call does nothing.
func (l *CloneLock) Unlock() {
	if l != nil && l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
}

// LockClone takes the flock on <clone>.lock, waiting for another pusher until ctx ends.
func LockClone(ctx context.Context, clone string) (*CloneLock, *Red) {
	if !filepath.IsAbs(clone) {
		return nil, &Red{StepClone, fmt.Sprintf("clone dir %q is not an absolute path", clone)}
	}
	if err := os.MkdirAll(filepath.Dir(clone), 0o700); err != nil {
		return nil, &Red{StepClone, err.Error()}
	}
	for {
		f, err := flock.TryLock(clone + ".lock")
		if err == nil {
			return &CloneLock{dir: clone, f: f}, nil
		}
		if !errors.Is(err, flock.ErrHeld) {
			return nil, &Red{StepClone, err.Error()}
		}
		select {
		case <-ctx.Done():
			return nil, &Red{StepClone, "another pusher holds " + clone + ".lock: " + ctx.Err().Error()}
		case <-time.After(lockPoll):
		}
	}
}

// fetchSHA makes the bare clone when missing and fetches the SHA from the worktree into a ref
// of this run, checking every object it receives.
func fetchSHA(ctx context.Context, req Request, sha string) *Red {
	if _, err := os.Stat(filepath.Join(req.CloneDir, "HEAD")); err != nil {
		if _, err := runGit(ctx, filepath.Dir(req.CloneDir), nil, "init", "-q", "--bare", req.CloneDir); err != nil {
			return &Red{StepClone, err.Error()}
		}
	}
	if _, err := runGit(ctx, req.CloneDir, nil, "-c", "transfer.fsckObjects=true",
		"fetch", "-q", "--no-tags", "--", req.Worktree, "+"+sha+":"+runRef(req)); err != nil {
		return &Red{StepClone, err.Error()}
	}
	return nil
}

// fetchOrigin refreshes origin's default and branch from the forge URL, with the token.
func fetchOrigin(ctx context.Context, req Request) *Red {
	if req.URL == "" || strings.HasPrefix(req.URL, "-") {
		return &Red{StepClone, fmt.Sprintf("forge URL %q is not usable", req.URL)}
	}
	// The branch may not be on origin yet: a pattern fetches it when it is, and --prune drops a
	// stale origin/<branch>. Only the exact ref is read afterwards.
	if _, err := runGit(ctx, req.CloneDir, tokenEnv(req.ForgeToken), "fetch", "-q", "--no-tags", "--prune", "--", req.URL,
		"+refs/heads/"+req.Default+":refs/remotes/origin/"+req.Default,
		"+refs/heads/"+req.Branch+"*:refs/remotes/origin/"+req.Branch+"*"); err != nil {
		return &Red{StepClone, "fetch from the forge: " + err.Error()}
	}
	return nil
}

// ownHistory runs checks 3 and 4 in the clone and fills MergeBase and Files.
func ownHistory(ctx context.Context, req Request, c *Checked) *Red {
	originBranch := "refs/remotes/origin/" + req.Branch
	if _, err := runGit(ctx, req.CloneDir, nil, "rev-parse", "--verify", "-q", originBranch); err == nil {
		// A SHA origin already holds, also under a merge pushed by hand, is no fast-forward and
		// needs none: nothing is pushed.
		on, err := isAncestor(ctx, req.CloneDir, c.SHA, originBranch)
		if err != nil {
			return &Red{StepFastForward, err.Error()}
		}
		c.AlreadyPushed = on
		ff, err := isAncestor(ctx, req.CloneDir, originBranch, c.SHA)
		if err != nil {
			return &Red{StepFastForward, err.Error()}
		}
		if !ff && !on {
			return &Red{StepFastForward, fmt.Sprintf("origin/%s is not an ancestor of %s: not a fast-forward", req.Branch, c.SHA)}
		}
	}
	originDefault := "refs/remotes/origin/" + req.Default
	out, err := runGit(ctx, req.CloneDir, nil, "merge-base", originDefault, c.SHA)
	if err != nil {
		return &Red{StepOwnCommits, err.Error()}
	}
	c.MergeBase = strings.TrimSpace(out)
	out, err = runGit(ctx, req.CloneDir, nil, "rev-list", "--parents", c.MergeBase+".."+c.SHA)
	if err != nil {
		return &Red{StepOwnCommits, err.Error()}
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		switch {
		case len(fields) <= 2:
			continue
		case len(fields) > 3:
			return &Red{StepOwnCommits, fmt.Sprintf("commit %s merges more than two parents", fields[0])}
		}
		in, err := isAncestor(ctx, req.CloneDir, fields[2], originDefault)
		if err != nil {
			return &Red{StepOwnCommits, err.Error()}
		}
		if !in {
			return &Red{StepOwnCommits, fmt.Sprintf("merge %s brings in %s, which is not on origin/%s", fields[0], fields[2], req.Default)}
		}
	}
	out, err = runGit(ctx, req.CloneDir, nil, "diff", "--name-only", "--no-renames", "-z", c.MergeBase, c.SHA)
	if err != nil {
		return &Red{StepOwnCommits, err.Error()}
	}
	c.Files = []string{}
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			c.Files = append(c.Files, f)
		}
	}
	return nil
}

// roborevRecord is the part of a roborev state file the floor reads.
type roborevRecord struct {
	Outcome   string            `json:"uitkomst"`
	SHA       string            `json:"sha"`
	Decisions []roborevDecision `json:"bevindingen"`
}

// roborevDecision is one decision on a finding; Commit is set for a fixed one.
type roborevDecision struct {
	Outcome string `json:"uitkomst"`
	Commit  string `json:"commit"`
}

// minPrefix is the shortest abbreviation of a fix commit that counts.
const minPrefix = 7

// covers reports whether a done record covers sha: it reviewed sha itself, or every commit
// after its sha (commitsAfter, full shas) is the commit of a fixed decision.
func covers(record roborevRecord, sha string, commitsAfter []string) bool {
	if record.Outcome != "done" {
		return false
	}
	if record.SHA == sha {
		return true
	}
	if len(commitsAfter) == 0 {
		return false
	}
	for _, c := range commitsAfter {
		if !fixedIn(record, c) {
			return false
		}
	}
	return true
}

func fixedIn(record roborevRecord, commit string) bool {
	for _, d := range record.Decisions {
		if d.Outcome == "fixed" && len(d.Commit) >= minPrefix && strings.HasPrefix(commit, d.Commit) {
			return true
		}
	}
	return false
}

// reviewed runs check 5: the newest push-*.json (the fix-pr route) or the branch record covers
// sha, where a record counts only when its sha is an ancestor of sha in the clone.
func reviewed(ctx context.Context, req Request, sha string) *Red {
	dir, err := recordDir(req.Worktree)
	if err != nil {
		return &Red{StepRoborev, "COULD NOT CHECK: " + err.Error()}
	}
	var paths []string
	if p := newestPushRecord(dir); p != "" {
		paths = append(paths, p)
	}
	paths = append(paths, filepath.Join(dir, roborev.BranchStem(req.Branch)+".json"))
	why := []string{}
	for _, path := range paths {
		ok, reason := recordCovers(ctx, req.CloneDir, path, sha)
		if ok {
			return nil
		}
		why = append(why, filepath.Base(path)+": "+reason)
	}
	return &Red{StepRoborev, "no roborev review covers " + sha + " (" + strings.Join(why, "; ") + ")"}
}

// recordCovers reads one record and checks it against sha in the clone.
func recordCovers(ctx context.Context, clone, path, sha string) (bool, string) {
	text, err := readSmall(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, "no record"
	}
	if err != nil {
		return false, err.Error()
	}
	var record roborevRecord
	if err := json.Unmarshal(text, &record); err != nil {
		return false, "unreadable: " + err.Error()
	}
	if record.Outcome != "done" {
		return false, fmt.Sprintf("uitkomst %q, not done", record.Outcome)
	}
	if !recordSHA.MatchString(record.SHA) {
		return false, fmt.Sprintf("sha %q is not a sha", record.SHA)
	}
	out, err := runGit(ctx, clone, nil, "rev-parse", "--verify", "-q", record.SHA+"^{commit}")
	if err != nil {
		return false, "reviewed sha " + record.SHA + " is not in the history"
	}
	record.SHA = strings.TrimSpace(out)
	if in, err := isAncestor(ctx, clone, record.SHA, sha); err != nil || !in {
		return false, "reviewed sha " + record.SHA + " is not an ancestor"
	}
	out, err = runGit(ctx, clone, nil, "rev-list", record.SHA+".."+sha)
	if err != nil {
		return false, err.Error()
	}
	after := strings.Fields(out)
	if !covers(record, sha, after) {
		return false, fmt.Sprintf("%d commit(s) after the reviewed sha %s are not fixes in the record", len(after), record.SHA)
	}
	return true, ""
}

// recordDir is <git-dir>/lighthouse/roborev of the worktree, read from its .git without git: a
// directory, or a file `gitdir: <path>` in a linked worktree.
func recordDir(worktree string) (string, error) {
	dotGit := filepath.Join(worktree, ".git")
	info, err := os.Lstat(dotGit)
	if err != nil {
		return "", err
	}
	gitDir := dotGit
	if info.Mode().IsRegular() {
		text, err := readSmall(dotGit)
		if err != nil {
			return "", err
		}
		path, ok := strings.CutPrefix(strings.TrimSpace(string(text)), "gitdir: ")
		if !ok {
			return "", fmt.Errorf("%s has no gitdir line", dotGit)
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(worktree, path)
		}
		gitDir = path
	} else if !info.IsDir() {
		return "", fmt.Errorf("%s is neither a directory nor a file", dotGit)
	}
	return filepath.Join(gitDir, "lighthouse", "roborev"), nil
}

// newestPushRecord is the push-*.json with the newest mtime in dir, or "".
func newestPushRecord(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best string
	var bestTime time.Time
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasPrefix(name, "push-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if best == "" || info.ModTime().After(bestTime) {
			best, bestTime = filepath.Join(dir, name), info.ModTime()
		}
	}
	return best
}

// ResolveWorktree is the worktree of the run that wrote the handoff: the workspace of the last
// handoff row of the issue on this host that started before the handoff comment, looked up in
// the workplace. A handoff path that differs from it is an error, so the run cannot point the
// pusher at another checkout.
func ResolveWorktree(ctx context.Context, db *sql.DB, w workplace.Workplace, req Request, handoffTime time.Time) (string, error) {
	id, ok, err := runs.LastHandoffWorkspace(ctx, db, req.Repo, req.Issue, handoffTime)
	if err != nil {
		return "", err
	}
	if !ok || id == "" {
		return "", fmt.Errorf("no run of %s#%d closed with a handoff in a workspace before %s", req.Repo, req.Issue, handoffTime.Format(time.RFC3339))
	}
	ws, err := w.Lookup(ctx, id)
	if err != nil {
		return "", fmt.Errorf("workspace %s of the handoff run: %w", id, err)
	}
	path := filepath.Clean(ws.Path)
	if filepath.Clean(req.Worktree) != path {
		return "", fmt.Errorf("the handoff names worktree %s, the run that wrote it worked in %s", req.Worktree, path)
	}
	return path, nil
}

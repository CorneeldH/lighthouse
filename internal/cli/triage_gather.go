package cli

import (
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	tri "github.com/CorneeldH/lighthouse/internal/triage"
)

// repoGather is what a round reads once per repo, and what it reads the facts of one issue
// with. It lives for one repo in one round.
type repoGather struct {
	repo  repoconfig.Repo
	c     *forge.Client
	db    *sql.DB
	login string          // the token login: its comments are trusted (decision 2)
	trust *dispatch.Trust // answer_permissions
	now   time.Time

	board  map[int]forge.BoardItem // the board items of repo.Slug, by issue
	open   []forge.Issue           // the open issues
	work   workLists
	closed map[int][]tri.Ref // closed issues per milestone number, read once
}

// readRepo fetches the default branch and reads the lists of step 4: the board, the open
// issues and the running work.
func readRepo(ctx context.Context, g *repoGather) error {
	r := g.repo
	if _, err := gitOut(ctx, r.Dir, "fetch", "--quiet", "origin", r.DefaultBranch); err != nil {
		return err
	}
	items, err := g.c.BoardItems(ctx, r.Board.ProjectID)
	if err != nil {
		return err
	}
	g.board = map[int]forge.BoardItem{}
	for _, it := range items {
		if it.Repo == r.Slug && it.Issue > 0 {
			g.board[it.Issue] = it
		}
	}
	if g.open, err = g.c.OpenIssues(ctx); err != nil {
		return err
	}
	g.work, err = readWork(ctx, g.c, g.db, r)
	g.closed = map[int][]tri.Ref{}
	return err
}

// boardFields are status and priority of an item, by option name.
func (g *repoGather) boardFields(it forge.BoardItem) (status, priority string) {
	b := g.repo.Board
	return optionName(b.StatusOptions, it.SingleSelect[b.StatusFieldID]),
		optionName(b.PriorityOptions, it.SingleSelect[b.PriorityFieldID])
}

// optionName is the name of option id in options; "" for an unknown or empty id.
func optionName(options map[string]string, id string) string {
	if id == "" {
		return ""
	}
	for name, oid := range options {
		if oid == id {
			return name
		}
	}
	return ""
}

// itemFunc returns the board item of issue n; ok is false when n is not on the board.
type itemFunc func(n int) (item forge.BoardItem, ok bool, err error)

// boardItem looks n up in the board of the gather.
func (g *repoGather) boardItem(n int) (forge.BoardItem, bool, error) {
	it, ok := g.board[n]
	return it, ok, nil
}

// gatherFacts reads the facts of issue is, in this order: the comments, the author's trust,
// the board item, the running work, the git log and the milestone. item and work are where the
// board item and the running work come from. full false (the recheck) leaves out what Changed
// does not compare: the blockers, the git log and the milestone.
func (g *repoGather) gatherFacts(ctx context.Context, is forge.Issue, item itemFunc, work func() (workLists, error), full bool) (tri.Facts, error) {
	f := tri.Facts{Number: is.Number, Title: is.Title, Body: is.Body, Author: is.Author, State: is.State,
		Labels: is.Labels, UpdatedAt: is.UpdatedAt, Milestone: is.Milestone}
	comments, err := g.c.Comments(ctx, is.Number)
	if err != nil {
		return tri.Facts{}, err
	}
	for _, c := range comments {
		ok := c.Author != "" && c.Author == g.login
		if !ok {
			if ok, err = g.trust.Trusted(ctx, c.Author); err != nil {
				return tri.Facts{}, err
			}
		}
		f.Comments = append(f.Comments, tri.Comment{ID: c.ID, Author: c.Author, Body: c.Body, CreatedAt: c.CreatedAt, Trusted: ok})
	}
	if f.AuthorTrusted, err = g.trust.Trusted(ctx, is.Author); err != nil {
		return tri.Facts{}, err
	}
	it, onBoard, err := item(is.Number)
	if err != nil {
		return tri.Facts{}, err
	}
	if onBoard {
		f.ItemID = it.ItemID
		f.Status, f.Priority = g.boardFields(it)
	}
	if full {
		// Decision 10: the board's blockers for an item, else always the REST read.
		if onBoard {
			f.Blockers = it.BlockedBy
		} else if f.Blockers, err = g.c.BlockedBy(ctx, is.Number); err != nil {
			return tri.Facts{}, err
		}
	}
	w, err := work()
	if err != nil {
		return tri.Facts{}, err
	}
	f.Work = w.of(is.Number)
	if !full {
		return f, nil
	}
	if f.GitLog, err = gitOut(ctx, g.repo.Dir, "log", "--oneline", "origin/"+g.repo.DefaultBranch,
		"-E", "--grep", "#"+strconv.Itoa(is.Number)+"([^0-9]|$)"); err != nil {
		return tri.Facts{}, err
	}
	if m := is.Milestone; m != nil {
		for _, o := range g.open {
			if o.Number != is.Number && o.Milestone != nil && o.Milestone.Number == m.Number {
				f.MilestoneOpen = append(f.MilestoneOpen, tri.Ref{Number: o.Number, Title: o.Title})
			}
		}
		if f.MilestoneClosed, err = g.closedIn(ctx, m.Number); err != nil {
			return tri.Facts{}, err
		}
	}
	return f, nil
}

// closedIn lists the issues closed in milestone m within tri.ClosedWindow, read once per
// milestone.
func (g *repoGather) closedIn(ctx context.Context, m int) ([]tri.Ref, error) {
	if refs, ok := g.closed[m]; ok {
		return refs, nil
	}
	issues, err := g.c.ClosedInMilestone(ctx, m, g.now.Add(-tri.ClosedWindow))
	if err != nil {
		return nil, err
	}
	refs := []tri.Ref{}
	for _, is := range issues {
		refs = append(refs, tri.Ref{Number: is.Number, Title: is.Title})
	}
	g.closed[m] = refs
	return refs, nil
}

// recheck reads issue n afresh, just before the first write (spec "Hercontrole"): the issue, its
// comments, its board item and fresh lists of running work. Nothing of the first gather is
// reused. nodeID is the issue's node ID, for AddToBoard.
func (g *repoGather) recheck(ctx context.Context, n int) (f tri.Facts, nodeID string, err error) {
	is, err := g.c.Issue(ctx, n)
	if err != nil {
		return tri.Facts{}, "", err
	}
	item := func(n int) (forge.BoardItem, bool, error) { return g.c.IssueItem(ctx, g.repo.Board.ProjectID, n) }
	work := func() (workLists, error) { return readWork(ctx, g.c, g.db, g.repo) }
	f, err = g.gatherFacts(ctx, is, item, work, false)
	return f, is.NodeID, err
}

// workLists are the lists running work is read from (candidate rule 5).
type workLists struct {
	prs       []forge.OpenPR
	runs      []runs.OpenRun
	remote    []string    // branch names on origin
	local     []string    // local branch names
	worktrees [][2]string // path and branch name of each worktree
}

// readWork reads the open PRs, the open runs, the branches on origin, the local branches and
// the worktrees of repo.
func readWork(ctx context.Context, c *forge.Client, db *sql.DB, repo repoconfig.Repo) (workLists, error) {
	var w workLists
	var err error
	if w.prs, err = c.OpenPRs(ctx); err != nil {
		return workLists{}, err
	}
	if w.runs, err = runs.Open(ctx, db, repo.Slug); err != nil {
		return workLists{}, err
	}
	out, err := gitOut(ctx, repo.Dir, "ls-remote", "origin", "refs/heads/issue-*")
	if err != nil {
		return workLists{}, err
	}
	for _, line := range lines(out) {
		if _, ref, ok := strings.Cut(line, "\t"); ok {
			w.remote = append(w.remote, strings.TrimPrefix(ref, "refs/heads/"))
		}
	}
	if out, err = gitOut(ctx, repo.Dir, "for-each-ref", "--format=%(refname:short)", "refs/heads/"); err != nil {
		return workLists{}, err
	}
	w.local = lines(out)
	if out, err = gitOut(ctx, repo.Dir, "worktree", "list", "--porcelain"); err != nil {
		return workLists{}, err
	}
	var path string
	for _, line := range lines(out) {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			path = p
		} else if b, ok := strings.CutPrefix(line, "branch refs/heads/"); ok {
			w.worktrees = append(w.worktrees, [2]string{path, b})
		}
	}
	return w, nil
}

// of lists the running work of issue n as Facts.Work names it: an open PR that heads a branch
// of n or names #n (decision 5), a branch of n on origin or local, a worktree on such a branch,
// and an open run.
func (w workLists) of(n int) []string {
	var out []string
	mention := regexp.MustCompile(`#` + strconv.Itoa(n) + `([^0-9]|$)`)
	for _, p := range w.prs {
		if issueBranch(p.HeadRef, n) || mention.MatchString(p.Title) || mention.MatchString(p.Body) {
			out = append(out, "PR #"+strconv.Itoa(p.Number))
		}
	}
	for _, b := range w.remote {
		if issueBranch(b, n) {
			out = append(out, "branch origin/"+b)
		}
	}
	for _, b := range w.local {
		if issueBranch(b, n) {
			out = append(out, "branch "+b)
		}
	}
	for _, wt := range w.worktrees {
		if issueBranch(wt[1], n) {
			out = append(out, "worktree "+wt[0])
		}
	}
	for _, r := range w.runs {
		if r.Issue == n {
			out = append(out, "run "+strconv.FormatInt(r.ID, 10))
		}
	}
	return out
}

// issueBranch reports whether branch is issue-<n> or issue-<n>-….
func issueBranch(branch string, n int) bool {
	name := "issue-" + strconv.Itoa(n)
	return branch == name || strings.HasPrefix(branch, name+"-")
}

// gitOut runs git in dir and returns its stdout; a failure carries the start of stderr.
func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	var stderr strings.Builder
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > judgeStderr {
			msg = msg[:judgeStderr]
		}
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, msg)
	}
	return string(out), nil
}

// lines are the non-empty lines of s.
func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

package pusher

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// The steps of Run around the floor; Red.Step is one of them or a step of the floor.
const (
	StepTrigger = "trigger" // the handoff or the answered Question this row was started for
	StepConfig  = "config"  // [dispatch.workflow_push] and a supported forge
	StepRoute   = "route"   // a PR on the branch (fix-pr route) or an after (build route)
	StepPermit  = "permit"  // the permitted SHA is still the worktree's
	StepToken   = "token"   // reading the workflow token file
	StepPush    = "push"    // the git push itself
)

// Command is the command of a push row in runs.
const Command = "push-workflows"

// Deps are the outside world of Run.
type Deps struct {
	DB        *sql.DB
	Forge     forge.Forge
	Workplace workplace.Workplace
	Now       func() time.Time
	// ReadToken reads the workflow token file; ReadTokenFile in production.
	ReadToken func(path string) (string, error)
	// Merger is a forge client of its own with the workflow token, for Merge only.
	Merger func(token string) Merger
}

// Merger squash-merges a PR on its head, as forge.Forge.MergePR does.
type Merger interface {
	MergePR(ctx context.Context, number int, sha string) error
}

// Run is one push: it reads the push row req.RunID and the comment that started it, runs the
// floor, the policy and the lint, the review for a pre-merge change, then reads the workflow
// token and pushes exactly the SHA the floor read. Every red step posts a 🤖 Question and closes
// the row as question; a push posts the outcome of its route. All of it runs before the work
// deadline of the row (workDeadline), and nothing is pushed once tick handed the row back. Run
// returns an error only when it cannot read its row, its deadline has passed, or it cannot post
// or close: then tick's timeout hands the issue back.
func Run(ctx context.Context, deps Deps, req Request) error {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	row, err := openPushRow(ctx, deps.DB, req)
	if err != nil {
		return err
	}
	ctx, cancel, err := withWorkDeadline(ctx, deps, req, row)
	if err != nil {
		return err
	}
	defer cancel()
	comments, err := deps.Forge.Comments(ctx, req.Issue)
	if err != nil {
		return fmt.Errorf("pusher: read the comments of #%d: %w", req.Issue, err)
	}
	p := &push{deps: deps, req: req, sha: noSHA}
	t, err := targetOf(comments, row.TriggerKey)
	if err != nil {
		return p.ask(ctx, questionInput{red: Red{StepTrigger, err.Error()}})
	}
	p.after, p.lane, p.req.Branch = t.after, t.lane, t.branch
	if t.permit != "" {
		p.sha = t.permit
	}
	if red := p.start(ctx, t); red != nil {
		return p.ask(ctx, questionInput{red: *red})
	}
	route, red := p.route(ctx)
	if red != nil {
		return p.ask(ctx, questionInput{red: *red})
	}
	return p.pushUnderLock(ctx, route)
}

// noSHA is the sha field of a Question from before the floor read one: decide gives no permit for it.
const noSHA = "none"

// push is one Run in progress: the request with the branch of its trigger, and what it read.
type push struct {
	deps  Deps
	req   Request
	after string
	lane  string // the lane of the push handoff: the handoff after the push carries it on
	sha   string // the SHA of the Question: the permitted one, then the one the floor read
	// tokens are the trimmed token values read so far: scrubbed from every posted text.
	tokens []string
}

// start checks what Run can check before the clone: the route is on, the trigger names this
// worktree and the permit is the Question's, and the worktree is the one of the run that wrote the
// handoff.
func (p *push) start(ctx context.Context, t target) *Red {
	switch {
	case p.req.Config == nil:
		return &Red{StepConfig, protocol.Heading("reason_pusher_off", p.req.Language)}
	case p.req.Off != "":
		return &Red{StepConfig, p.req.Off}
	case filepath.Clean(t.worktree) != filepath.Clean(p.req.Worktree):
		return &Red{StepTrigger, fmt.Sprintf("the trigger names worktree %s, the pusher was started on %s", t.worktree, p.req.Worktree)}
	case t.permit != p.req.Permit:
		return &Red{StepPermit, fmt.Sprintf("the permit %q is not the SHA %q of the answered Question", p.req.Permit, t.permit)}
	}
	if _, err := ResolveWorktree(ctx, p.deps.DB, p.deps.Workplace, p.req, t.at); err != nil {
		return &Red{StepBranch, err.Error()}
	}
	return nil
}

// route is true on the fix-pr route: an open PR of this repository on the branch. Without one
// the build route needs an after to hand the chain to.
func (p *push) route(ctx context.Context) (fixPR bool, red *Red) {
	prs, err := p.deps.Forge.BranchPRs(ctx, p.req.Branch)
	if err != nil {
		return false, &Red{StepRoute, "COULD NOT CHECK: the PRs of " + p.req.Branch + ": " + err.Error()}
	}
	for _, pr := range prs {
		if pr.State == "OPEN" && !pr.CrossRepo {
			return true, nil
		}
	}
	if p.after == "" {
		return false, &Red{StepRoute, "no open PR on " + p.req.Branch + " and no after in the handoff: nothing to continue with after the push"}
	}
	return false, nil
}

// deadlineMargin is what the pusher leaves between the end of its last post and the moment tick
// hands its row back.
const deadlineMargin = time.Minute

// workDeadline is when the work of a push row must end: tick hands the row back at started plus
// timeout, and the posts after the work take up to postTimeout. A row without a start time counts
// from now.
func workDeadline(started, now time.Time, timeout time.Duration) time.Time {
	if started.IsZero() {
		started = now
	}
	return started.Add(timeout - postTimeout - deadlineMargin)
}

// timeoutOf is the push timeout tick holds the row to (dispatch.PushTimeout).
func timeoutOf(req Request) time.Duration {
	if req.Config == nil || req.Config.Timeout <= 0 {
		return hostconfig.DefaultPushTimeout
	}
	return req.Config.Timeout
}

// withWorkDeadline is ctx ending at the work deadline of row, measured on deps.Now. A deadline
// that has already passed is an error: the pusher then does nothing and posts nothing, and tick
// hands the row back at its timeout.
func withWorkDeadline(ctx context.Context, deps Deps, req Request, row runs.Row) (context.Context, context.CancelFunc, error) {
	now := deps.Now()
	deadline := workDeadline(row.StartedAt, now, timeoutOf(req))
	left := deadline.Sub(now)
	if left <= 0 {
		return nil, nil, fmt.Errorf("pusher: run %d started at %s: its work deadline %s has passed, tick hands it back",
			req.RunID, row.StartedAt.Format(time.RFC3339), deadline.Format(time.RFC3339))
	}
	ctx, cancel := context.WithTimeout(ctx, left)
	return ctx, cancel, nil
}

// handedBack reports whether the push row is no longer open: tick handed it back, or it closed
// otherwise. A row that cannot be read is not reported as handed back.
func (p *push) handedBack(ctx context.Context) bool {
	_, err := openPushRow(ctx, p.deps.DB, p.req)
	return errors.Is(err, errNotOpen)
}

// pushUnderLock holds the clone lock from the floor to the push, so no second pusher refreshes
// the clone in between, and removes this run's ref from the clone before it lets go. All of it
// runs under the work deadline of Run, so a hung fetch or push cannot hold the lock past it.
//
// The sha of a Question is the SHA an answer push permits, and a permit skips the review. So a
// Question carries the SHA the floor read only once that SHA needs no review from this run: it
// is post-merge, permitted, approved, or the Question itself shows the reviewers' verdict. Before
// that it carries the permit it ran under, or none.
func (p *push) pushUnderLock(ctx context.Context, fixPR bool) error {
	lockWait, _ := Budgets(p.req.Config.Timeout)
	lctx, cancel := context.WithTimeout(ctx, lockWait)
	lock, red := LockClone(lctx, p.req.CloneDir)
	cancel()
	if red != nil {
		return p.ask(ctx, questionInput{red: *red})
	}
	defer lock.Unlock()
	defer p.dropRunRef(ctx)

	c, red := Floor(ctx, p.req, lock)
	if red != nil {
		return p.ask(ctx, questionInput{red: *red})
	}
	if p.req.Permit != "" && c.SHA != p.req.Permit {
		// The Question keeps the permitted SHA: the moved one was never shown to anyone, and an
		// answer push to this Question must give this Question again, never a permit for it.
		return p.ask(ctx, questionInput{red: Red{StepPermit,
			fmt.Sprintf("the worktree moved from the permitted %s to %s", p.req.Permit, c.SHA)}, hint: hintMoved})
	}
	diff, err := workflowDiff(ctx, p.req, c)
	if err != nil {
		return p.ask(ctx, questionInput{red: Red{StepPolicy, "COULD NOT CHECK: the workflow diff: " + err.Error()}})
	}
	class, violations, red := Policy(ctx, p.req, c)
	if red == nil && len(violations) > 0 {
		red = &Red{StepPolicy, violationText(violations)}
	}
	if red == nil {
		red = Lint(ctx, p.req, c, c.Files)
	}
	if red != nil {
		return p.ask(ctx, questionInput{red: *red, diff: diff})
	}
	if class == PreMerge && p.req.Permit == "" {
		v := Review(ctx, *p.req.Config, diff, p.scrub(), p.req.Guard)
		switch {
		case p.req.Config.Review != hostconfig.ReviewEnforce:
			return p.ask(ctx, questionInput{red: Red{StepReview, protocol.Heading("question_push_shadow", p.req.Language)},
				sha: c.SHA, diff: diff, verdict: &v, hint: hintPermit})
		case !v.Approve:
			return p.ask(ctx, questionInput{red: Red{StepReview, "not every reviewer approves"},
				sha: c.SHA, diff: diff, verdict: &v, hint: hintPermit})
		}
	}
	// Post-merge, permitted or approved: from here an answer push may permit exactly this SHA.
	p.sha = c.SHA
	token, err := p.deps.ReadToken(p.req.Config.TokenFile)
	if err != nil {
		return p.ask(ctx, questionInput{red: Red{StepToken, err.Error()}, diff: diff})
	}
	p.tokens = append(p.tokens, token)
	old, err := oldEnd(ctx, p.req, c)
	if err != nil {
		return p.ask(ctx, questionInput{red: Red{StepPush, "COULD NOT CHECK: " + err.Error()}, diff: diff})
	}
	// Tick may have handed the row back while the review ran: a push now would land on origin
	// unrecorded, under an issue tick already gave back. ask then posts nothing either.
	if p.handedBack(ctx) {
		return fmt.Errorf("pusher: run %d was handed back before the push: nothing pushed", p.req.RunID)
	}
	if refused, err := pushSHA(ctx, p.req, c.SHA, token); err != nil {
		why := err.Error()
		if refused {
			why = protocol.Heading("question_push_token_refused", p.req.Language) + "\n" + why
		}
		return p.ask(ctx, questionInput{red: Red{StepPush, why}, diff: diff})
	}
	return p.pushed(ctx, fixPR, old+".."+c.SHA, workflowsOf(c.Files))
}

// scrub is the forge token and every token read so far, trimmed.
func (p *push) scrub() []string {
	out := []string{}
	for _, t := range append([]string{p.req.ForgeToken}, p.tokens...) {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// dropRunRef removes refs/pusher/runs/<RunID> from the clone, also after a cancelled run.
func (p *push) dropRunRef(ctx context.Context) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if _, err := os.Stat(filepath.Join(p.req.CloneDir, "HEAD")); err != nil {
		return
	}
	_, _ = runGit(cctx, p.req.CloneDir, nil, "update-ref", "-d", runRef(p.req))
}

// errNotOpen is the run of a request that is not, or no longer, an open push row.
var errNotOpen = errors.New("is not an open push row")

// openPushRow is the open push row req.RunID of req.Repo and req.Issue.
func openPushRow(ctx context.Context, db *sql.DB, req Request) (runs.Row, error) {
	rows, err := runs.History(ctx, db, req.Repo)
	if err != nil {
		return runs.Row{}, err
	}
	for _, r := range rows {
		if r.ID != req.RunID {
			continue
		}
		if !r.Open || r.Command != Command || r.Issue != req.Issue || r.SessionID != "" {
			return runs.Row{}, fmt.Errorf("pusher: run %d %w of %s#%d", req.RunID, errNotOpen, req.Repo, req.Issue)
		}
		return r, nil
	}
	return runs.Row{}, fmt.Errorf("pusher: no run %d of %s", req.RunID, req.Repo)
}

// target is what the trigger of a push row names: from the handoff for push:<id>, from the
// pusher Question answered by <id> for pushanswer:<id>.
type target struct {
	worktree, branch, after, lane, permit string
	at                                    time.Time // when the handoff or the Question was posted
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// targetOf reads the trigger key of the push row against the comments of the issue.
func targetOf(comments []protocol.Comment, key string) (target, error) {
	kind, idText, _ := strings.Cut(key, ":")
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		return target{}, fmt.Errorf("trigger key %q has no comment id", key)
	}
	switch kind {
	case "push":
		for _, c := range comments {
			if c.ID != id {
				continue
			}
			h, _ := protocol.ParseHeader(c.Body)
			ho, err := protocol.ParseHandoff(h)
			if err != nil {
				return target{}, fmt.Errorf("comment %d: %w", id, err)
			}
			if ho.Next != Command {
				return target{}, fmt.Errorf("comment %d hands off to %q, not %s", id, ho.Next, Command)
			}
			return checkTarget(target{worktree: ho.Worktree, branch: ho.Branch, after: ho.After, lane: ho.Lane,
				at: parseTime(c.CreatedAt)})
		}
	case "pushanswer":
		// The Question the user answered is the last pusher Question before the answer; one after it
		// was never answered, and its SHA never gets the permit.
		var q *protocol.Comment
		for i, c := range comments {
			if c.ID == id {
				if q == nil {
					return target{}, fmt.Errorf("no pusher Question before answer %d", id)
				}
				h, _ := protocol.ParseHeader(q.Body)
				t := target{worktree: h.Fields["worktree"], branch: h.Fields["branch"], after: h.Fields["after"],
					lane: h.Fields["lane"], permit: h.Fields["sha"], at: parseTime(q.CreatedAt)}
				if !shaRe.MatchString(t.permit) {
					return target{}, fmt.Errorf("the answered Question carries no SHA (%q)", t.permit)
				}
				return checkTarget(t)
			}
			if h, ok := protocol.ParseHeader(c.Body); ok && h.Kind == protocol.KindQuestion && h.Session == "" && h.Fields["sha"] != "" {
				q = &comments[i]
			}
		}
	default:
		return target{}, fmt.Errorf("trigger key %q is not a push trigger", key)
	}
	return target{}, fmt.Errorf("comment %d of trigger %s is gone", id, key)
}

func checkTarget(t target) (target, error) {
	if !branchName(t.branch) || !filepath.IsAbs(t.worktree) || t.at.IsZero() {
		return target{}, fmt.Errorf("branch %q, worktree %q or time of the trigger is not usable", t.branch, t.worktree)
	}
	return t, nil
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// violationText is one line per policy violation.
func violationText(vs []Violation) string {
	lines := make([]string, len(vs))
	for i, v := range vs {
		lines[i] = v.File + ": " + v.Rule + ": " + v.Why
	}
	return strings.Join(lines, "\n")
}

// workflowDiff is the diff of the added and changed files under .github/workflows/ between the
// merge-base and the SHA, read in the clone: a deleted workflow is no risk.
func workflowDiff(ctx context.Context, req Request, c Checked) (string, error) {
	return runGit(ctx, req.CloneDir, nil, "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames",
		"--diff-filter=d", c.MergeBase, c.SHA, "--", workflowDir+"/")
}

// workflowsOf are the changed paths under .github/workflows/, sorted.
func workflowsOf(files []string) []string {
	var out []string
	for _, f := range files {
		if strings.HasPrefix(f, workflowDir+"/") {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// oldEnd is the start of the pushed range: origin/<branch> when the branch is on origin, else
// the merge-base.
func oldEnd(ctx context.Context, req Request, c Checked) (string, error) {
	out, err := runGit(ctx, req.CloneDir, nil, "rev-parse", "--verify", "-q", "refs/remotes/origin/"+req.Branch)
	var ge *gitError
	switch {
	case err == nil:
		return strings.TrimSpace(out), nil
	case errors.As(err, &ge) && ge.code == 1:
		return c.MergeBase, nil
	}
	return "", err
}

// pushSHA pushes sha to the branch on the forge URL from the clone. The workflow token goes
// into the Cmd.Env of this one command, as an http.extraHeader through GIT_CONFIG_COUNT, never
// into argv. refused reports GitHub's refusal of the token for a workflow change; that line of
// git's output is left out of the error, because its wording advises a wider token, and the
// caller says the same thing neutrally. Every other line stays.
func pushSHA(ctx context.Context, req Request, sha, token string) (refused bool, err error) {
	if req.URL == "" || strings.HasPrefix(req.URL, "-") || !shaRe.MatchString(sha) || !branchName(req.Branch) {
		return false, fmt.Errorf("COULD NOT CHECK: URL %q, SHA %q or branch %q is not usable", req.URL, sha, req.Branch)
	}
	_, err = runGit(ctx, req.CloneDir, tokenEnv(token), "push", "-q", "--no-verify", req.URL, sha+":refs/heads/"+req.Branch)
	var ge *gitError
	if errors.As(err, &ge) {
		rest, refused := withoutRefusal(ge.stderr)
		return refused, fmt.Errorf("git push to %s refused (exit %d): %s", req.URL, ge.code, rest)
	}
	if err != nil && ctx.Err() != nil {
		return false, fmt.Errorf("git push to %s did not end within the budget: %w", req.URL, ctx.Err())
	}
	return false, err
}

// workflowRefusal is GitHub's refusal of a token that may not change workflows, for a personal
// access token, an OAuth app or a GitHub app alike.
var workflowRefusal = regexp.MustCompile(`(?i)refusing to allow .* to create or update workflow`)

// withoutRefusal drops the lines of GitHub's workflow refusal from git's output.
func withoutRefusal(text string) (rest string, refused bool) {
	var keep []string
	for _, line := range strings.Split(text, "\n") {
		if workflowRefusal.MatchString(line) {
			refused = true
			continue
		}
		keep = append(keep, line)
	}
	return strings.TrimSpace(strings.Join(keep, "\n")), refused
}

// ReadTokenFile reads the workflow token: a regular file of this user, not a symlink, of mode
// 0600 exactly, holding one token. It checks the open file itself, so a swap after the check
// changes nothing. The error names the file and its mode, never the content.
func ReadTokenFile(path string) (string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|oNoFollow|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("token file %s does not exist", path)
		}
		return "", fmt.Errorf("token file %s cannot be opened (a symlink is refused): %w", path, err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("token file %s is not a regular file", path)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		return "", fmt.Errorf("token file %s has mode %#o, it must have mode 0600", path, perm)
	}
	if !ownedByMe(info) {
		return "", fmt.Errorf("token file %s is not owned by this user", path)
	}
	text, err := io.ReadAll(io.LimitReader(f, maxRead+1))
	if err != nil {
		return "", err
	}
	if len(text) > maxRead {
		return "", fmt.Errorf("token file %s is larger than %d bytes", path, maxRead)
	}
	token := strings.TrimSpace(string(text))
	if token == "" || strings.ContainsFunc(token, func(r rune) bool { return r < 0x21 || r == 0x7f }) {
		return "", fmt.Errorf("token file %s does not hold one token", path)
	}
	return token, nil
}

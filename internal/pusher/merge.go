package pusher

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

// The steps of Merge besides those of Run.
const (
	StepAncestry = "ancestry" // the pushed SHA is an ancestor of the head, with no workflow change after it
	StepMerge    = "merge"    // the merge itself
)

// MergeKey is the trigger key of the merge pusher's row: one per PR and head. decide's
// mergeKey is the same.
func MergeKey(pr int, head string) string { return "merge:" + strconv.Itoa(pr) + ":" + head }

// Merge merges PR pr on head with the workflow token, for a PR the pusher pushed workflow
// changes to: GitHub refuses that merge to the run's token. It merges only when the SHA the
// pusher pushed last on the PR's branch is an ancestor of head and nothing under
// .github/workflows/ changed between them, so everything that reaches the default branch under
// .github/workflows/ went through the pusher's floor and review. The pushed SHA comes from the
// pusher's own push rows (runs.SetPushed), never from a comment, which a run can post. Every
// refusal posts a 🤖 Vraag without a SHA to permit and closes the row as question; a merge posts a
// 🤖 Done for the PR and closes it as done. Merge returns an error only when it cannot read its
// row or cannot post or close.
func Merge(ctx context.Context, deps Deps, req Request, pr int, head string) error {
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
	req.Branch, req.Worktree, req.Permit = row.Branch, "", ""
	m := &merging{push: push{deps: deps, req: req, sha: noSHA}, pr: pr, head: head}
	if red := m.start(row); red != nil {
		return m.ask(ctx, *red)
	}
	url, red := m.openPR(ctx)
	if red != nil {
		return m.ask(ctx, *red)
	}
	pushed, err := lastPushed(ctx, deps.DB, req.Repo, req.Issue, req.Branch)
	if err != nil {
		return m.ask(ctx, Red{StepAncestry, "COULD NOT CHECK: the push rows: " + err.Error()})
	}
	if pushed == "" {
		return m.ask(ctx, Red{StepAncestry, "no push of the pusher on " + req.Branch + " is recorded"})
	}
	if red := m.checkUnderLock(ctx, pushed); red != nil {
		return m.ask(ctx, *red)
	}
	token, err := deps.ReadToken(req.Config.TokenFile)
	if err != nil {
		return m.ask(ctx, Red{StepToken, err.Error()})
	}
	m.tokens = append(m.tokens, token)
	if red := m.merge(ctx, token); red != nil {
		return m.ask(ctx, *red)
	}
	return m.merged(ctx, url)
}

// merging is one Merge in progress: a push for its request and tokens, and the PR and head.
type merging struct {
	push
	pr   int
	head string
}

// start checks what Merge can check without the forge: the row is the merge row of this PR and
// head, the route is on, and the branch is one git may see.
func (m *merging) start(row runs.Row) *Red {
	switch {
	case m.pr <= 0 || !shaRe.MatchString(m.head):
		return &Red{StepTrigger, fmt.Sprintf("PR %d or head %q is not usable", m.pr, m.head)}
	case row.TriggerKey != MergeKey(m.pr, m.head):
		return &Red{StepTrigger, fmt.Sprintf("the row's trigger %q is not %s", row.TriggerKey, MergeKey(m.pr, m.head))}
	case m.req.Config == nil:
		return &Red{StepConfig, protocol.Heading("reason_pusher_off", m.req.Language)}
	case m.req.Off != "":
		return &Red{StepConfig, m.req.Off}
	case !branchName(m.req.Branch) || !branchName(m.req.Default) || m.req.Branch == m.req.Default:
		return &Red{StepBranch, fmt.Sprintf("branch %q of the row is not a PR branch", m.req.Branch)}
	}
	return nil
}

// openPR reads the PR from the forge: open, of this repository, on the row's branch, and still on
// head. It returns the PR's URL.
func (m *merging) openPR(ctx context.Context) (string, *Red) {
	prs, err := m.deps.Forge.BranchPRs(ctx, m.req.Branch)
	if err != nil {
		return "", &Red{StepRoute, "COULD NOT CHECK: the PRs of " + m.req.Branch + ": " + err.Error()}
	}
	for _, p := range prs {
		if p.Number != m.pr {
			continue
		}
		switch {
		case p.State != "OPEN" || p.CrossRepo || p.HeadRef != m.req.Branch:
			return "", &Red{StepRoute, fmt.Sprintf("PR #%d is not an open PR of this repository on %s", m.pr, m.req.Branch)}
		case p.HeadSHA != m.head:
			return "", &Red{StepRoute, fmt.Sprintf("the head of PR #%d moved from %s to %s", m.pr, m.head, p.HeadSHA)}
		}
		return p.URL, nil
	}
	return "", &Red{StepRoute, fmt.Sprintf("PR #%d is not an open PR of this repository on %s", m.pr, m.req.Branch)}
}

// lastPushed is the SHA of the newest push row of issue on branch that recorded a push: the last
// SHA the pusher pushed there. "" when it pushed nothing there.
func lastPushed(ctx context.Context, db *sql.DB, repo string, issue int, branch string) (string, error) {
	rows, err := runs.History(ctx, db, repo)
	if err != nil {
		return "", err
	}
	var best runs.Row
	for _, r := range rows {
		if r.Issue == issue && r.Command == Command && r.SessionID == "" && r.Branch == branch &&
			shaRe.MatchString(r.PushedSHA) && r.ID > best.ID {
			best = r
		}
	}
	return best.PushedSHA, nil
}

// checkUnderLock fetches the branch from the forge into the clone, under the clone lock and the
// work deadline of Merge, and checks there that origin is still on head, that pushed is an ancestor of
// head, and that pushed..head touches no path under .github/workflows/: added, changed, deleted
// or renamed, any of it counts.
func (m *merging) checkUnderLock(ctx context.Context, pushed string) *Red {
	lockWait, _ := Budgets(m.req.Config.Timeout)
	lctx, cancel := context.WithTimeout(ctx, lockWait)
	lock, red := LockClone(lctx, m.req.CloneDir)
	cancel()
	if red != nil {
		return red
	}
	defer lock.Unlock()
	defer m.dropRunRef(ctx)
	if red := m.fetchHead(ctx); red != nil {
		return red
	}
	clone := m.req.CloneDir
	if _, err := runGit(ctx, clone, nil, "rev-parse", "--verify", "-q", pushed+"^{commit}"); err != nil {
		return &Red{StepAncestry, fmt.Sprintf("the pushed SHA %s is not in the history of %s", pushed, m.req.Branch)}
	}
	in, err := isAncestor(ctx, clone, pushed, m.head)
	if err != nil {
		return &Red{StepAncestry, "COULD NOT CHECK: " + err.Error()}
	}
	if !in {
		return &Red{StepAncestry, fmt.Sprintf("the pushed SHA %s is not an ancestor of the head %s", pushed, m.head)}
	}
	out, err := runGit(ctx, clone, nil, "diff", "--name-only", "--no-renames", "--no-ext-diff", "--no-textconv", "-z",
		pushed, m.head, "--", workflowDir+"/")
	if err != nil {
		return &Red{StepAncestry, "COULD NOT CHECK: " + err.Error()}
	}
	var changed []string
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			changed = append(changed, headerValue(f))
		}
	}
	if len(changed) > 0 {
		return &Red{StepAncestry, fmt.Sprintf("after the pushed SHA %s the head %s changes, unreviewed by the pusher:\n%s",
			pushed, m.head, strings.Join(changed, "\n"))}
	}
	return nil
}

// fetchHead makes the bare clone when missing and fetches the PR's branch from the forge URL, with
// the forge token, into this run's ref; that ref must be head.
func (m *merging) fetchHead(ctx context.Context) *Red {
	clone := m.req.CloneDir
	if m.req.URL == "" || strings.HasPrefix(m.req.URL, "-") {
		return &Red{StepClone, fmt.Sprintf("forge URL %q is not usable", m.req.URL)}
	}
	if _, err := os.Stat(filepath.Join(clone, "HEAD")); err != nil {
		if _, err := runGit(ctx, filepath.Dir(clone), nil, "init", "-q", "--bare", clone); err != nil {
			return &Red{StepClone, err.Error()}
		}
	}
	if _, err := runGit(ctx, clone, tokenEnv(m.req.ForgeToken), "-c", "transfer.fsckObjects=true",
		"fetch", "-q", "--no-tags", "--", m.req.URL, "+refs/heads/"+m.req.Branch+":"+runRef(m.req)); err != nil {
		return &Red{StepClone, "fetch from the forge: " + err.Error()}
	}
	out, err := runGit(ctx, clone, nil, "rev-parse", "--verify", "-q", runRef(m.req))
	if err != nil {
		return &Red{StepClone, "COULD NOT CHECK: " + err.Error()}
	}
	if got := strings.TrimSpace(out); got != m.head {
		return &Red{StepRoute, fmt.Sprintf("%s on the forge moved from %s to %s", m.req.Branch, m.head, got)}
	}
	return nil
}

// merge asks the forge, through a client of its own with the workflow token, to squash-merge the
// PR on head. A refusal of the token for a workflow change is said in the pusher's own neutral
// words; GitHub's line, which advises on the token, is left out.
func (m *merging) merge(ctx context.Context, token string) *Red {
	if m.deps.Merger == nil {
		return &Red{StepMerge, "COULD NOT CHECK: no merge client"}
	}
	if m.handedBack(ctx) {
		return &Red{StepMerge, fmt.Sprintf("run %d was handed back before the merge: nothing merged", m.req.RunID)}
	}
	err := m.deps.Merger(token).MergePR(ctx, m.pr, m.head)
	var refused *forge.MergeRefused
	switch {
	case err == nil:
		return nil
	case errors.Is(err, forge.ErrHeadMoved):
		return &Red{StepMerge, fmt.Sprintf("the forge says the head or base of PR #%d moved since %s", m.pr, m.head)}
	case errors.As(err, &refused):
		why, byToken := withoutRefusal(refused.Message)
		if byToken {
			why = strings.TrimSpace(protocol.Heading("question_push_token_refused", m.req.Language) + "\n" + why)
		}
		return &Red{StepMerge, fmt.Sprintf("the forge refused the merge of PR #%d: %s", m.pr, why)}
	}
	return &Red{StepMerge, fmt.Sprintf("the merge of PR #%d failed: %s", m.pr, err.Error())}
}

// merged posts the 🤖 Done of the merge and closes the row as done.
func (m *merging) merged(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), postTimeout)
	defer cancel()
	body := protocol.RenderPusherDone(headerValue(url), protocol.Heading("note_merged", m.req.Language), m.req.Language)
	if _, err := m.deps.Forge.CreateComment(ctx, m.req.Issue, scrubText(body, scrubList(m.scrub()))); err != nil {
		return fmt.Errorf("pusher: post the Done of PR #%d on #%d: %w", m.pr, m.req.Issue, err)
	}
	return runs.Release(ctx, m.deps.DB, m.req.RunID, runs.Done, "", m.deps.Now())
}

// ask posts the merge pusher's 🤖 Vraag for red and closes the row as question. Its sha is none, so
// an answer push starts nothing; it names the PR and head, never a push.
func (m *merging) ask(ctx context.Context, red Red) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), postTimeout)
	defer cancel()
	if m.handedBack(ctx) {
		return fmt.Errorf("pusher: run %d was handed back: no Question posted for step %s", m.req.RunID, red.Step)
	}
	scrub := scrubList(m.scrub())
	red.Why = scrubText(red.Why, scrub)
	body := scrubText(renderMergeQuestion(red, m.req.Branch, m.pr, m.head, m.req.Language), scrub)
	if _, err := m.deps.Forge.CreateComment(ctx, m.req.Issue, body); err != nil {
		return fmt.Errorf("pusher: post the Question on #%d: %w", m.req.Issue, err)
	}
	return runs.Release(ctx, m.deps.DB, m.req.RunID, runs.Question, "merge: "+red.Step, m.deps.Now())
}

// renderMergeQuestion is the merge pusher's 🤖 Vraag: sha none (decide gives no permit), branch,
// pr and head in the header, the red step and why, and how to merge by hand. No session footer.
func renderMergeQuestion(red Red, branch string, pr int, head, language string) string {
	h := func(key string) string { return protocol.Heading(key, language) }
	lines := []string{
		protocol.Robot + " " + h("kind_question") + " · " + Command,
		"sha: " + noSHA,
		"branch: " + headerValue(branch),
		"pr: " + strconv.Itoa(pr),
		"head: " + headerValue(head),
		"step: " + headerValue(red.Step), "",
		"**" + h("question_push_step") + ":** " + prose(red.Step, maxProse),
	}
	lines = append(lines, fenced("text", capRunes(strings.TrimSpace(red.Why), maxWhy))...)
	lines = append(lines, "", h("question_merge_manual"), "", "**"+h("question_merge_command")+":**")
	lines = append(lines, fenced("bash", "gh pr merge "+strconv.Itoa(pr)+" --squash --match-head-commit "+shellWord(head))...)
	return strings.Join(lines, "\n")
}

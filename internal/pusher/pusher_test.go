package pusher

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
)

// fakeForge keeps the comments of issue 1 and the PRs of the branch; it knows nothing else.
type fakeForge struct {
	forge.Forge
	mu       sync.Mutex
	comments []protocol.Comment
	prs      []forge.PR
	posted   []string
	// refuse makes CreateComment fail for a body it matches.
	refuse func(body string) bool
	// reads counts the BranchPRs reads of a wrapper that changes its answer.
	reads int
	// issue is issue 1; issueErr makes Issue fail.
	issue    forge.Issue
	issueErr error
}

func (f *fakeForge) Issue(_ context.Context, n int) (forge.Issue, error) {
	if n != 1 {
		return forge.Issue{}, errors.New("fake forge: only issue 1")
	}
	if f.issueErr != nil {
		return forge.Issue{}, f.issueErr
	}
	return f.issue, nil
}

func (f *fakeForge) Comments(_ context.Context, n int) ([]protocol.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n != 1 {
		return nil, errors.New("fake forge: only issue 1")
	}
	return append([]protocol.Comment{}, f.comments...), nil
}

func (f *fakeForge) CreateComment(_ context.Context, n int, body string) (protocol.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n != 1 {
		return protocol.Comment{}, errors.New("fake forge: only issue 1")
	}
	if f.refuse != nil && f.refuse(body) {
		return protocol.Comment{}, errors.New("fake forge: post refused")
	}
	c := f.add(body, time.Now())
	f.posted = append(f.posted, body)
	return c, nil
}

func (f *fakeForge) BranchPRs(_ context.Context, b string) ([]forge.PR, error) {
	if b != branch {
		return nil, errors.New("fake forge: unknown branch " + b)
	}
	return f.prs, nil
}

// add appends a comment created at at; the caller holds mu or runs alone.
func (f *fakeForge) add(body string, at time.Time) protocol.Comment {
	c := protocol.Comment{ID: int64(10 + len(f.comments)), Author: "u", Body: body, CreatedAt: at.UTC().Format(time.RFC3339)}
	f.comments = append(f.comments, c)
	return c
}

func (f *fakeForge) last(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posted) == 0 {
		t.Fatal("the pusher posted nothing")
	}
	return f.posted[len(f.posted)-1]
}

// runFix is a fixture for Run: the git fixture, a database with the run that wrote the handoff
// and the push row, the handoff comment, a workflow token file and fake tools on PATH.
type runFix struct {
	*fixture
	db     *sql.DB
	forge  *fakeForge
	deps   Deps
	cfg    hostconfig.WorkflowPush
	rowID  int64
	bin    string
	t0     time.Time
	marker string // a reviewer that should not run creates this file
}

const after = "/lighthouse-write-pr 1"

// handoffBody is the handoff of the run; an empty after leaves the field out (the fix-pr route).
func handoffBody(wt, after string) string {
	text := "🤖 Handoff\nissue: #1\nworktree: " + wt + "\nbranch: " + branch + "\nnext: push-workflows\nwhen: now"
	if after != "" {
		text += "\nafter: " + after
	}
	return text
}

func newRunFix(t *testing.T, review string, scripts map[string]string) *runFix {
	t.Helper()
	f := newFixture(t)
	tgit(t, f.remote, "config", "receive.autogc", "false")
	ctx := context.Background()
	s, err := store.OpenShared(ctx, filepath.Join(f.dir, "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	r := &runFix{fixture: f, db: s.DB, forge: &fakeForge{issue: forge.Issue{Number: 1, Title: issueTitle, Body: issueBody}}, t0: time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
		marker: filepath.Join(f.dir, "reviewer-ran")}
	run, err := runs.Take(ctx, s.DB, runs.Claim{Repo: "o/r", Issue: 1, SessionID: runs.NewSessionID(), Mode: "start",
		Trigger: "manual", TriggerKey: "start:1", Command: "/lighthouse-start-issue 1", Branch: branch, Now: r.t0})
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.SetWorkplace(ctx, s.DB, run.ID, "ws-1", branch); err != nil {
		t.Fatal(err)
	}
	if err := runs.Release(ctx, s.DB, run.ID, runs.Handoff, "", r.t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	r.handoff(after)

	token := filepath.Join(f.dir, "workflow.token")
	if err := os.WriteFile(token, []byte(workflowTok+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.bin = filepath.Join(f.dir, "bin")
	all := map[string]string{
		"actionlint": fakeActionlint, "zizmor": fakeZizmor,
		"bwrap":   strings.ReplaceAll(passBwrap, "LOG", filepath.Join(f.dir, "bwrap.log")),
		"approve": answer(approveJSON), "reject": answer(rejectJSON),
		"never": "#!/bin/sh\ntouch " + r.marker + "\n" + strings.TrimPrefix(answer(approveJSON), "#!/bin/sh\n"),
	}
	for name, text := range scripts {
		all[name] = text
	}
	for name, text := range all {
		write(t, filepath.Join(r.bin, name), text)
		if err := os.Chmod(filepath.Join(r.bin, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", r.bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	r.cfg = hostconfig.WorkflowPush{TokenFile: token, Review: review, Timeout: 30 * time.Minute,
		Reviewers: []hostconfig.Reviewer{rv("supply-chain", "approve"), rv("injection", "approve")}}
	f.req.Config = &r.cfg
	f.req.Language = "nl"
	f.req.ForgeToken = forgeTok
	f.req.Default = "main"
	f.req.Branch = ""
	f.req.StateDir = filepath.Join(f.dir, "state")
	r.deps = Deps{DB: s.DB, Forge: r.forge, Workplace: lookupOnly{paths: map[string]string{"ws-1": f.wt}},
		Now: time.Now, ReadToken: ReadTokenFile}
	return r
}

// handoff posts the handoff of the run and claims the push row for it now, as tick does when it
// starts the pusher: the work deadline counts from the claim.
func (r *runFix) handoff(after string) {
	r.t.Helper()
	c := r.forge.add(handoffBody(r.wt, after), r.t0.Add(2*time.Minute))
	r.claim("handoff", "push:"+strconv.FormatInt(c.ID, 10), time.Now())
}

func (r *runFix) claim(trigger, key string, at time.Time) {
	r.t.Helper()
	run, err := runs.Take(context.Background(), r.db, runs.Claim{Repo: "o/r", Issue: 1, Mode: "start",
		Trigger: trigger, TriggerKey: key, Command: "push-workflows", Branch: branch, Now: at})
	if err != nil {
		r.t.Fatal(err)
	}
	r.rowID = run.ID
	r.req.RunID = run.ID
}

// commitWorkflow commits a workflow and a done record on the new head.
func (r *runFix) commitWorkflow(name, text string) string {
	r.t.Helper()
	head := r.commit(".github/workflows/"+name, text)
	r.branchRecord(done(head))
	return head
}

func (r *runFix) run() {
	r.t.Helper()
	if err := Run(context.Background(), r.deps, r.req); err != nil {
		r.t.Fatalf("Run: %v", err)
	}
}

// remoteHead is the branch on origin, "" when it is not there.
func (r *runFix) remoteHead() string {
	r.t.Helper()
	out, err := exec.Command("git", "-C", r.remote, "rev-parse", "--verify", "-q", "refs/heads/"+branch).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (r *runFix) row() runs.Row {
	r.t.Helper()
	rows, err := runs.History(context.Background(), r.db, "o/r")
	if err != nil {
		r.t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == r.rowID {
			return row
		}
	}
	r.t.Fatalf("no row %d", r.rowID)
	return runs.Row{}
}

func (r *runFix) reviewerRan() bool {
	_, err := os.Stat(r.marker)
	return err == nil
}

// question checks that body is a pusher Vraag as decide reads it and returns its header.
func question(t *testing.T, body, sha, wt string) protocol.Header {
	t.Helper()
	h, ok := protocol.ParseHeader(body)
	if !ok || h.Kind != protocol.KindQuestion {
		t.Fatalf("not a Vraag:\n%s", body)
	}
	if h.Session != "" {
		t.Errorf("the pusher Vraag carries a session footer %q", h.Session)
	}
	if h.Fields["sha"] != sha || h.Fields["worktree"] != wt || h.Fields["branch"] != branch {
		t.Errorf("header fields %v, want sha %s, worktree %s, branch %s", h.Fields, sha, wt, branch)
	}
	if !strings.Contains(body, "cd "+wt+" && git push -u origin HEAD") {
		t.Errorf("Vraag lacks the push-by-hand command:\n%s", body)
	}
	return h
}

// returned checks that body is the pusher's Returned for red step step, as finishReturn reads it:
// no session, reason_pusher_red, the worktree and branch, no SHA to permit, and the push by hand.
func returned(t *testing.T, body, step, wt string) protocol.Header {
	t.Helper()
	h, ok := protocol.ParseHeader(body)
	if !ok || h.Kind != protocol.KindReturned {
		t.Fatalf("not a Teruggegeven:\n%s", body)
	}
	if h.Session != "" {
		t.Errorf("the pusher Teruggegeven carries a session footer %q", h.Session)
	}
	if key, ok := protocol.ReasonKey(h.Fields["reason"]); !ok || key != "reason_pusher_red" {
		t.Errorf("reason %q reads as %q, want reason_pusher_red", h.Fields["reason"], key)
	}
	if h.Fields["step"] != step || h.Fields["worktree"] != wt || h.Fields["branch"] != branch || h.Fields["sha"] != "" {
		t.Errorf("header fields %v, want step %s, worktree %s, branch %s and no sha", h.Fields, step, wt, branch)
	}
	for _, want := range []string{protocol.Heading("question_push_manual", "nl"), "cd " + wt + " && git push -u origin HEAD"} {
		if !strings.Contains(body, want) {
			t.Errorf("Teruggegeven lacks %q:\n%s", want, body)
		}
	}
	return h
}

func wantClosed(t *testing.T, row runs.Row, o runs.Outcome) {
	t.Helper()
	if row.Open || row.Outcome != o {
		t.Fatalf("row open %v outcome %q, want closed %q", row.Open, row.Outcome, o)
	}
}

func permitLine() string { return protocol.Heading("question_push_permit", "nl") }
func retryLine() string  { return protocol.Heading("question_push_retry", "nl") }

const preMergeWF = "on: pull_request\npermissions:\n  contents: read\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@" + pin + "\n"

func TestPostMergeWorkflowPushesWithoutReview(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	r.cfg.Reviewers = []hostconfig.Reviewer{rv("supply-chain", "never"), rv("injection", "never")}
	head := r.commitWorkflow("nightly.yml", scheduleWF)
	r.run()
	if got := r.remoteHead(); got != head {
		t.Fatalf("origin/%s = %q, want the pushed %s", branch, got, head)
	}
	if r.reviewerRan() {
		t.Error("a reviewer ran for a post-merge workflow")
	}
	wantClosed(t, r.row(), runs.Handoff)
}

func TestPreMergeApprovePushesInEnforce(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewEnforce, nil)
	head := r.commitWorkflow("ci.yml", preMergeWF)
	r.run()
	if got := r.remoteHead(); got != head {
		t.Fatalf("origin/%s = %q, want the pushed %s", branch, got, head)
	}
	wantClosed(t, r.row(), runs.Handoff)
}

func TestPreMergeRejectDoesNotPushInEnforce(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewEnforce, nil)
	r.cfg.Reviewers = []hostconfig.Reviewer{rv("supply-chain", "approve"), rv("injection", "reject")}
	head := r.commitWorkflow("ci.yml", preMergeWF)
	r.run()
	if got := r.remoteHead(); got != "" {
		t.Fatalf("pushed %s after a reject", got)
	}
	body := r.forge.last(t)
	question(t, body, head, r.wt)
	wantClosed(t, r.row(), runs.Question)
}

// The reviewer moves the worktree on while the review runs: the pusher pushes the SHA it read.
func TestPushesTheSHAItReadOnce(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewEnforce, nil)
	head := r.commitWorkflow("ci.yml", preMergeWF)
	mover := "#!/bin/sh\ncat >/dev/null\n" +
		"git -C " + r.wt + " -c user.name=T -c user.email=t@x -c commit.gpgsign=false commit -q --allow-empty -m moved\n" +
		"printf '%s' '" + approveJSON + "' > \"$1\"\n"
	write(t, filepath.Join(r.bin, "mover"), mover)
	if err := os.Chmod(filepath.Join(r.bin, "mover"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.cfg.Reviewers = []hostconfig.Reviewer{rv("supply-chain", "approve"), rv("injection", "mover")}
	r.run()
	if moved := r.head(); moved == head {
		t.Fatal("the reviewer did not move the worktree: the test proves nothing")
	}
	if got := r.remoteHead(); got != head {
		t.Fatalf("origin/%s = %q, want the SHA read before the review %s", branch, got, head)
	}
}

func TestShadowAsksEvenOnUnanimousApprove(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	head := r.commitWorkflow("ci.yml", preMergeWF)
	r.run()
	if got := r.remoteHead(); got != "" {
		t.Fatalf("shadow mode pushed %s", got)
	}
	body := r.forge.last(t)
	question(t, body, head, r.wt)
	for _, want := range []string{protocol.Heading("question_push_shadow", "nl"), permitLine(), "supply-chain", "injection", "approve"} {
		if !strings.Contains(body, want) {
			t.Errorf("shadow Vraag lacks %q:\n%s", want, body)
		}
	}
	wantClosed(t, r.row(), runs.Question)
}

func TestRejectAsks(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	r.cfg.Reviewers = []hostconfig.Reviewer{rv("supply-chain", "reject"), rv("injection", "approve")}
	head := r.commitWorkflow("ci.yml", preMergeWF)
	r.run()
	if got := r.remoteHead(); got != "" {
		t.Fatalf("pushed %s after a reject", got)
	}
	body := r.forge.last(t)
	question(t, body, head, r.wt)
	for _, want := range []string{"unpinned action", "uses: foo/bar@v1", permitLine(), protocol.Heading("question_push_diff", "nl")} {
		if !strings.Contains(body, want) {
			t.Errorf("reject Vraag lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, retryLine()) {
		t.Errorf("a review reject gives the retry text, not the permit:\n%s", body)
	}
	wantClosed(t, r.row(), runs.Question)
}

// answerPush posts the answer push to the last Vraag and claims the push row of that answer.
func (r *runFix) answerPush() {
	r.t.Helper()
	r.forge.mu.Lock()
	c := r.forge.add("push", time.Now().Add(time.Second))
	r.forge.mu.Unlock()
	r.claim("answer", "pushanswer:"+strconv.FormatInt(c.ID, 10), time.Now().Add(2*time.Second))
}

func TestPermitPushesSameSHAOnly(t *testing.T) {
	t.Run("same sha pushes without review", func(t *testing.T) {
		r := newRunFix(t, hostconfig.ReviewShadow, nil)
		head := r.commitWorkflow("ci.yml", preMergeWF)
		r.run()
		h := question(t, r.forge.last(t), head, r.wt)
		r.cfg.Reviewers = []hostconfig.Reviewer{rv("supply-chain", "never"), rv("injection", "never")}
		r.answerPush()
		r.req.Permit = h.Fields["sha"]
		r.run()
		if got := r.remoteHead(); got != head {
			t.Fatalf("origin/%s = %q, want the permitted %s", branch, got, head)
		}
		if r.reviewerRan() {
			t.Error("a reviewer ran under a permit")
		}
		wantClosed(t, r.row(), runs.Handoff)
	})
	t.Run("moved worktree asks again", func(t *testing.T) {
		r := newRunFix(t, hostconfig.ReviewShadow, nil)
		head := r.commitWorkflow("ci.yml", preMergeWF)
		r.run()
		question(t, r.forge.last(t), head, r.wt)
		moved := r.commitWorkflow("ci.yml", preMergeWF+"# moved\n")
		r.answerPush()
		r.req.Permit = head
		r.run()
		if got := r.remoteHead(); got != "" {
			t.Fatalf("pushed %s, but the permit was for %s", got, head)
		}
		body := r.forge.last(t)
		// The Vraag keeps the permitted SHA: the moved one was never shown with a review.
		question(t, body, head, r.wt)
		if !strings.Contains(body, StepPermit) || !strings.Contains(body, moved) {
			t.Errorf("Vraag does not name the permit step and the moved SHA:\n%s", body)
		}
		if !strings.Contains(body, protocol.Heading("question_push_moved", "nl")) || strings.Contains(body, permitLine()) {
			t.Errorf("Vraag does not say an answer push gives it again:\n%s", body)
		}
		wantClosed(t, r.row(), runs.Question)
	})
	t.Run("flag that is not the Vraag's sha", func(t *testing.T) {
		r := newRunFix(t, hostconfig.ReviewShadow, nil)
		head := r.commitWorkflow("ci.yml", preMergeWF)
		r.run()
		// The flag names the worktree's new head, which no Vraag showed the user.
		moved := r.commitWorkflow("ci.yml", preMergeWF+"# moved\n")
		r.answerPush()
		r.req.Permit = moved
		r.run()
		if got := r.remoteHead(); got != "" {
			t.Fatalf("pushed %s with a permit flag the Vraag does not carry", got)
		}
		body := r.forge.last(t)
		question(t, body, head, r.wt)
		if !strings.Contains(body, StepPermit) {
			t.Errorf("Vraag does not name the permit step:\n%s", body)
		}
	})
}

// The full chain of answers on a moved worktree: each answer push permits only the SHA of the
// Vraag it answers, as decide reads it, and the moved SHA is never pushed nor reviewed.
func TestAnswerToMovedQuestionDoesNotPush(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	head := r.commitWorkflow("ci.yml", preMergeWF)
	r.run()
	r.commitWorkflow("ci.yml", preMergeWF+"# moved, never reviewed\n")
	r.cfg.Reviewers = []hostconfig.Reviewer{rv("supply-chain", "never"), rv("injection", "never")}
	for i := 0; i < 3; i++ {
		h, _ := protocol.ParseHeader(r.forge.last(t))
		r.answerPush()
		r.req.Permit = h.Fields["sha"]
		r.run()
		if got := r.remoteHead(); got != "" {
			t.Fatalf("answer %d pushed %s", i+1, got)
		}
		question(t, r.forge.last(t), head, r.wt)
	}
	if r.reviewerRan() {
		t.Error("a reviewer ran")
	}
}

// repair checks that body is the pusher's Repair for red step step: no session, no SHA to permit,
// the worktree and branch, and neither a push command nor a hint to push by hand.
func repair(t *testing.T, body, step, wt string) protocol.Header {
	t.Helper()
	h, ok := protocol.ParseHeader(body)
	if !ok || h.Kind != protocol.KindRepair {
		t.Fatalf("not a Herstel:\n%s", body)
	}
	if !strings.HasPrefix(body, protocol.Robot+" "+protocol.Heading("kind_repair", "nl")) {
		t.Errorf("the Herstel does not open with its heading:\n%s", body)
	}
	if h.Session != "" || strings.Contains(body, "<sub>session") {
		t.Errorf("the pusher Herstel carries a session footer %q", h.Session)
	}
	if h.Fields["step"] != step || h.Fields["worktree"] != wt || h.Fields["branch"] != branch {
		t.Errorf("header fields %v, want step %s, worktree %s, branch %s", h.Fields, step, wt, branch)
	}
	if strings.Contains(body, "git push") || strings.Contains(body, protocol.Heading("question_push_manual", "nl")) {
		t.Errorf("the Herstel offers a push by hand:\n%s", body)
	}
	return h
}

// A red policy before any SHA was read is no question and no Teruggegeven: the run that wrote the
// workflow can mend it. The pusher posts a Herstel with the folded diff and closes as handoff.
func TestPolicyRedPostsRepair(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	r.commitWorkflow("ci.yml", "on: pull_request\npermissions: write-all\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: a/b@v1\n")
	r.run()
	body := r.forge.last(t)
	repair(t, body, StepPolicy, r.wt)
	if !strings.Contains(body, "<details><summary>"+protocol.Heading("question_push_diff", "nl")+"</summary>") ||
		!strings.Contains(body, "+permissions: write-all") {
		t.Errorf("Herstel lacks the folded workflow diff:\n%s", body)
	}
	wantClosed(t, r.row(), runs.Handoff)
}

func TestLintRedPostsRepair(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	r.commitWorkflow("nightly.yml", scheduleWF+"# ACTIONLINT\n")
	r.run()
	body := r.forge.last(t)
	repair(t, body, StepLint, r.wt)
	if !strings.Contains(body, "+# ACTIONLINT") {
		t.Errorf("Herstel lacks the folded workflow diff:\n%s", body)
	}
	if got := r.remoteHead(); got != "" {
		t.Errorf("pushed %s on a red lint", got)
	}
	wantClosed(t, r.row(), runs.Handoff)
}

// A roborev record that is a skip, or missing, is no review of the SHA: the run that wrote the
// handoff can run roborev and write a new handoff, so it is a Herstel, not a Teruggegeven (#551).
func TestRoborevRedPostsRepair(t *testing.T) {
	for name, record := range map[string]func(r *runFix){
		"skip": func(r *runFix) {
			r.branchRecord(map[string]any{"uitkomst": "skip", "reden": "build session ran no review"})
		},
		"no record": func(r *runFix) {
			gitDir := tgit(r.t, r.wt, "rev-parse", "--absolute-git-dir")
			if err := os.Remove(filepath.Join(gitDir, "lighthouse", "roborev", branch+".json")); err != nil {
				r.t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRunFix(t, hostconfig.ReviewShadow, nil)
			r.commitWorkflow("nightly.yml", scheduleWF)
			record(r)
			r.run()
			repair(t, r.forge.last(t), StepRoborev, r.wt)
			if got := r.remoteHead(); got != "" {
				t.Errorf("pushed %s without a review", got)
			}
			wantClosed(t, r.row(), runs.Handoff)
		})
	}
}

// A SHA origin already has, here under a merge pushed by hand, closes the row as after a push of
// its own: the handoff to after, the SHA recorded, origin untouched. Neither the skip record nor
// the shadow review stops it: there is nothing left to push (#551, 9187eeda under 18e3a81).
func TestAlreadyOnOriginClosesWithoutPush(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	r.cfg.Reviewers = []hostconfig.Reviewer{rv("supply-chain", "never"), rv("injection", "never")}
	head := r.commitWorkflow("ci.yml", preMergeWF)
	r.branchRecord(map[string]any{"uitkomst": "skip", "reden": "build session ran no review"})
	tgit(t, r.wt, "push", "-q", "origin", branch)
	merge := r.mergeOnOrigin()
	base := tgit(t, r.main, "rev-parse", "main~1")
	r.run()
	if got := r.remoteHead(); got != merge {
		t.Fatalf("origin/%s = %q, want the hand-made merge %s left alone", branch, got, merge)
	}
	if r.reviewerRan() {
		t.Error("a reviewer ran for a SHA origin already has")
	}
	h, ok := protocol.ParseHeader(r.forge.last(t))
	if !ok {
		t.Fatalf("not a protocol comment:\n%s", r.forge.last(t))
	}
	ho, err := protocol.ParseHandoff(h)
	if err != nil {
		t.Fatalf("ParseHandoff: %v", err)
	}
	if ho.Next != after || ho.Branch != branch {
		t.Errorf("handoff %+v, want next %q on %s", ho, after, branch)
	}
	if h.Fields["range"] != base+".."+head {
		t.Errorf("range %q, want %s..%s", h.Fields["range"], base, head)
	}
	wantClosed(t, r.row(), runs.Handoff)
	if got := r.row().PushedSHA; got != head {
		t.Errorf("row pushed %q, want %s", got, head)
	}
}

// A step that could not run is no mistake of the branch: it stays a Teruggegeven.
func TestCouldNotCheckStaysReturned(t *testing.T) {
	cases := map[string]struct {
		setup func(r *runFix)
	}{
		"linter not on PATH": {setup: func(r *runFix) {
			if err := os.Remove(filepath.Join(r.bin, "actionlint")); err != nil {
				r.t.Fatal(err)
			}
		}},
		"linter output no JSON": {setup: func(r *runFix) {
			write(r.t, filepath.Join(r.bin, "zizmor"), "#!/bin/sh\necho nope\nexit 13\n")
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRunFix(t, hostconfig.ReviewShadow, nil)
			r.commitWorkflow("nightly.yml", scheduleWF)
			tc.setup(r)
			r.run()
			body := r.forge.last(t)
			returned(t, body, StepLint, r.wt)
			if !strings.Contains(body, "COULD NOT CHECK") {
				t.Errorf("Teruggegeven does not say it could not check:\n%s", body)
			}
			wantClosed(t, r.row(), runs.Returned)
		})
	}
	t.Run("workflow diff error", func(t *testing.T) {
		r := newRunFix(t, hostconfig.ReviewShadow, nil)
		req := r.req
		req.Worktree, req.Branch = r.wt, branch
		p := &push{deps: r.deps, req: req}
		if err := p.ask(context.Background(), questionInput{red: Red{StepPolicy, "COULD NOT CHECK: the workflow diff: boom"}}); err != nil {
			t.Fatal(err)
		}
		returned(t, r.forge.last(t), StepPolicy, r.wt)
		wantClosed(t, r.row(), runs.Returned)
	})
}

func TestRepairableClassifiesOnPrefix(t *testing.T) {
	for _, tc := range []struct {
		red  Red
		want bool
	}{
		{Red{StepPolicy, "x"}, true},
		{Red{StepLint, "x"}, true},
		{Red{StepPolicy, violationText([]Violation{{"a.yml", RuleParse, "a.yml cannot be parsed"}})}, true},
		{Red{StepPolicy, "COULD NOT CHECK: x"}, false},
		{Red{StepLint, "COULD NOT CHECK: x"}, false},
		{Red{StepRoborev, `no roborev review covers abc (x.json: uitkomst "skip", not done)`}, true},
		{Red{StepRoborev, "COULD NOT CHECK: x"}, false},
		{Red{StepReview, "x"}, false},
		{Red{StepToken, "x"}, false},
		{Red{StepPush, "x"}, false},
	} {
		if got := tc.red.repairable(); got != tc.want {
			t.Errorf("%+v repairable = %v, want %v", tc.red, got, tc.want)
		}
	}
}

func TestRepairTruncatesLikeQuestion(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	var long strings.Builder
	long.WriteString("on: pull_request\npermissions: write-all\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: a/b@v1\n")
	for i := 0; long.Len() < 200_000; i++ {
		long.WriteString("# filler line " + strconv.Itoa(i) + " ünïcödé ````\n")
	}
	r.commitWorkflow("ci.yml", long.String())
	r.run()
	body := r.forge.last(t)
	repair(t, body, StepPolicy, r.wt)
	if n := len([]rune(body)); n >= maxComment {
		t.Fatalf("the Herstel has %d characters, want under %d", n, maxComment)
	}
	if !strings.Contains(body, protocol.Heading("question_push_attachment", "nl")) {
		t.Errorf("the cut Herstel does not name the attachment")
	}
}

// A Vraag opens with its question and the close line, so the controller's list shows only what
// an answer push does; the red step, the verdict and the diff stand below the close line.
func TestQuestionTextIsTheQuestionLine(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	head := r.commitWorkflow("ci.yml", preMergeWF)
	r.run()
	body := r.forge.last(t)
	question(t, body, head, r.wt)
	if got := protocol.QuestionText(body); got != permitLine() {
		t.Errorf("QuestionText = %q, want the permit line %q", got, permitLine())
	}
	closeLine := strings.Index(body, protocol.Heading("question_close", "nl"))
	diff := strings.Index(body, "+on: pull_request")
	if closeLine < 0 || diff < closeLine {
		t.Errorf("the diff does not stand below the close line:\n%s", body)
	}
}

func TestPermitDoesNotLiftRedFloor(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	head := r.commitWorkflow("ci.yml", preMergeWF)
	r.run()
	question(t, r.forge.last(t), head, r.wt)
	// The worktree gets dirty after the Vraag: the floor is red at check 2.
	write(t, filepath.Join(r.wt, "stray.txt"), "x\n")
	r.answerPush()
	r.req.Permit = head
	r.run()
	if got := r.remoteHead(); got != "" {
		t.Fatalf("pushed %s on a red floor", got)
	}
	body := r.forge.last(t)
	question(t, body, head, r.wt)
	if !strings.Contains(body, StepClean) || !strings.Contains(body, retryLine()) {
		t.Errorf("Vraag does not name the red step %s and the floor text:\n%s", StepClean, body)
	}
	if strings.Contains(body, permitLine()) {
		t.Errorf("a red floor offers the answer push as permission:\n%s", body)
	}
	wantClosed(t, r.row(), runs.Question)
}

func TestRedQuestionHasNoTokenScopeAdvice(t *testing.T) {
	cases := map[string]func(r *runFix){
		"shadow": func(r *runFix) { r.commitWorkflow("ci.yml", preMergeWF) },
		"policy": func(r *runFix) {
			r.commitWorkflow("ci.yml", "on: pull_request\npermissions: write-all\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: a/b@v1\n")
		},
		"token": func(r *runFix) {
			r.commitWorkflow("nightly.yml", scheduleWF)
			if err := os.Chmod(r.cfg.TokenFile, 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"push refused": func(r *runFix) {
			r.commitWorkflow("nightly.yml", scheduleWF)
			write(t, filepath.Join(r.remote, "hooks", "pre-receive"), "#!/bin/sh\necho 'refusing to allow a token to create or update workflow without workflow scope' >&2\nexit 1\n")
			if err := os.Chmod(filepath.Join(r.remote, "hooks", "pre-receive"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"config off": func(r *runFix) { r.commitWorkflow("nightly.yml", scheduleWF); r.req.Config = nil },
	}
	scope := regexp.MustCompile(`(?i)scope`)
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRunFix(t, hostconfig.ReviewShadow, nil)
			setup(r)
			r.run()
			if got := r.remoteHead(); got != "" {
				t.Fatalf("pushed %s", got)
			}
			body := r.forge.last(t)
			h, ok := protocol.ParseHeader(body)
			if !ok || h.Kind != protocol.KindQuestion && h.Kind != protocol.KindReturned && h.Kind != protocol.KindRepair {
				t.Fatalf("neither a Vraag, a Herstel nor a Teruggegeven:\n%s", body)
			}
			if scope.MatchString(strings.ReplaceAll(body, r.dir, "")) {
				t.Errorf("the comment mentions a token scope:\n%s", body)
			}
			want := runs.Question
			switch h.Kind {
			case protocol.KindReturned:
				want = runs.Returned
			case protocol.KindRepair:
				want = runs.Handoff
			}
			wantClosed(t, r.row(), want)
		})
	}
}

func TestQuestionFenceLongerThanBackticks(t *testing.T) {
	diff := "+      - run: echo ```` and `` and ```````\n+```\n"
	body := renderQuestion(questionInput{red: Red{StepReview, "x"}, sha: strings.Repeat("a", 40), worktree: "/w", branch: branch, diff: diff}, "nl")
	longest := 0
	for _, run := range regexp.MustCompile("`+").FindAllString(diff, -1) {
		longest = max(longest, len(run))
	}
	fence := strings.Repeat("`", longest+1)
	open := strings.Index(body, fence+"diff\n")
	if open < 0 {
		t.Fatalf("no fence of %d backticks opens the diff:\n%s", longest+1, body)
	}
	rest := body[open+len(fence+"diff\n"):]
	if !strings.HasPrefix(rest, diff) {
		t.Fatalf("the diff is not intact after the fence:\n%s", rest)
	}
	if !strings.HasPrefix(rest[len(diff):], fence+"\n") {
		t.Errorf("the diff block does not close with the same fence:\n%s", rest[len(diff):])
	}
}

func TestQuestionTruncatedUnderLimit(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	var long strings.Builder
	long.WriteString(preMergeWF)
	for i := 0; long.Len() < 200_000; i++ {
		long.WriteString("# filler line " + strconv.Itoa(i) + " ünïcödé ````\n")
	}
	head := r.commitWorkflow("ci.yml", long.String())
	r.run()
	body := r.forge.last(t)
	question(t, body, head, r.wt)
	if n := len([]rune(body)); n >= maxComment {
		t.Fatalf("the Vraag has %d characters, want under %d", n, maxComment)
	}
	note := protocol.Heading("question_push_attachment", "nl")
	i := strings.Index(body, note)
	if i < 0 {
		t.Fatalf("the cut Vraag does not name the attachment:\n%s", body[:500])
	}
	path := strings.Trim(strings.Fields(body[i+len(note):])[0], "`.")
	full, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("attachment %s: %v", path, err)
	}
	want := tgit(t, r.req.CloneDir, "diff", "--no-renames", "--diff-filter=d", tgit(t, r.main, "rev-parse", "main"), head, "--", ".github/workflows/")
	if strings.TrimSpace(string(full)) != want {
		t.Errorf("the attachment is not the full workflow diff (%d bytes, want %d)", len(full), len(want))
	}
	if want := filepath.Join(r.req.StateDir, "push", "o", "r", head+"-"+strconv.FormatInt(r.rowID, 10)+".diff"); path != want {
		t.Errorf("attachment %s, want %s in the pusher's state dir", path, want)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("attachment mode %v, %v; want 0600", info, err)
	}
	// The attachment leaves the worktree clean: a permit can still pass the floor.
	if out := tgit(t, r.wt, "status", "--porcelain"); out != "" {
		t.Errorf("the attachment made the worktree dirty:\n%s", out)
	}
}

func TestTokenFileModeMustBe0600(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	r.commitWorkflow("nightly.yml", scheduleWF)
	if err := os.Chmod(r.cfg.TokenFile, 0o640); err != nil {
		t.Fatal(err)
	}
	r.run()
	if got := r.remoteHead(); got != "" {
		t.Fatalf("pushed %s with a token file of mode 0640", got)
	}
	body := r.forge.last(t)
	if !strings.Contains(body, StepToken) || !strings.Contains(body, "0600") {
		t.Errorf("Vraag does not name the token step and mode 0600:\n%s", body)
	}
	wantClosed(t, r.row(), runs.Question)

	for mode, ok := range map[os.FileMode]bool{0o600: true, 0o400: false, 0o644: false, 0o660: false} {
		path := filepath.Join(t.TempDir(), "tok")
		if err := os.WriteFile(path, []byte(" tok \n"), mode); err != nil {
			t.Fatal(err)
		}
		got, err := ReadTokenFile(path)
		if ok && (err != nil || got != "tok") {
			t.Errorf("ReadTokenFile(mode %#o) = %q, %v; want tok", mode, got, err)
		}
		if !ok && err == nil {
			t.Errorf("ReadTokenFile(mode %#o) accepted the file", mode)
		}
	}
	// A symlink to a good token file is refused too.
	target := filepath.Join(t.TempDir(), "tok")
	if err := os.WriteFile(target, []byte("tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadTokenFile(target); err != nil || got != "tok" {
		t.Fatalf("ReadTokenFile(target) = %q, %v", got, err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTokenFile(link); err == nil {
		t.Error("ReadTokenFile followed a symlink")
	}
}

func TestPushTokenOnlyInPushCommandEnv(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewEnforce, nil)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(r.dir, "git.log")
	write(t, filepath.Join(r.bin, "git"), "#!/bin/sh\n{ printf 'ARGV'; printf ' %s' \"$@\"; printf '\\n'; env | sed 's/^/ENV /'; } >> "+log+"\nexec "+real+" \"$@\"\n")
	envLog := filepath.Join(r.dir, "reviewer.env")
	write(t, filepath.Join(r.bin, "envdump"), "#!/bin/sh\nenv >> "+envLog+"\ncat >/dev/null\nprintf '%s' '"+approveJSON+"' > \"$1\"\n")
	for _, name := range []string{"git", "envdump"} {
		if err := os.Chmod(filepath.Join(r.bin, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r.cfg.Reviewers = []hostconfig.Reviewer{rv("supply-chain", "envdump"), rv("injection", "envdump")}
	// The pusher's own process carries the forge token for its comments, as tick starts it.
	t.Setenv("GH_TOKEN", forgeTok)
	head := r.commitWorkflow("ci.yml", preMergeWF)
	r.run()
	if got := r.remoteHead(); got != head {
		t.Fatalf("origin/%s = %q, want %s", branch, got, head)
	}
	b64 := func(tok string) string { return base64.StdEncoding.EncodeToString([]byte("x-access-token:" + tok)) }
	text, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	pusherGit := regexp.MustCompile(`^ -C \S+ -c core\.hooksPath=/dev/null -c core\.fsmonitor=false `)
	pushes, commands := 0, 0
	for _, block := range strings.Split(string(text), "ARGV")[1:] {
		argv, env, _ := strings.Cut(block, "\n")
		// Only the pusher's own commands, which all carry the hook guard; the test's own setup
		// and the fake origin run theirs.
		if !pusherGit.MatchString(argv) {
			continue
		}
		for _, secret := range []string{workflowTok, forgeTok, b64(workflowTok), b64(forgeTok)} {
			if strings.Contains(argv, secret) {
				t.Errorf("a token in argv: %s", argv)
			}
		}
		if strings.Contains(env, workflowTok) || strings.Contains(env, "ENV GH_TOKEN") {
			t.Errorf("git%s sees a raw token in its env", argv)
		}
		commands++
		hasWorkflow := strings.Contains(env, b64(workflowTok))
		isPush := regexp.MustCompile(` push( |$)`).MatchString(argv)
		if hasWorkflow != isPush {
			t.Errorf("git%s: workflow token in env %v, push %v", argv, hasWorkflow, isPush)
		}
		if isPush {
			pushes++
			want := " " + r.remote + " " + head + ":refs/heads/" + branch
			if !strings.HasSuffix(argv, want) {
				t.Errorf("push argv %q, want it to end in %q", argv, want)
			}
			if strings.Contains(env, b64(forgeTok)) {
				t.Errorf("the push carries the forge token")
			}
			for _, kv := range []string{"ENV GIT_CONFIG_GLOBAL=/dev/null", "ENV GIT_CONFIG_NOSYSTEM=1", "ENV GIT_CONFIG_COUNT=1", "ENV GIT_CONFIG_KEY_0=http.extraHeader"} {
				if !strings.Contains(env, kv) {
					t.Errorf("push env lacks %s", kv)
				}
			}
		}
	}
	if pushes != 1 || commands < 10 {
		t.Errorf("%d push commands among %d of the pusher, want 1 among the floor's", pushes, commands)
	}
	renv, err := os.ReadFile(envLog)
	if err != nil {
		t.Fatalf("the reviewers did not run: %v", err)
	}
	for _, secret := range []string{workflowTok, forgeTok, b64(workflowTok), b64(forgeTok)} {
		if strings.Contains(string(renv), secret) {
			t.Errorf("a reviewer sees a token in its env")
		}
	}
	if strings.Contains(strings.Join(os.Environ(), "\n"), workflowTok) {
		t.Error("the workflow token is in the pusher's own environment")
	}
}

func TestBuildRouteClosesHandoffWithAfter(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	// A file name with a newline must not add a field to the handoff.
	evil := "x\nnext: /evil.yml"
	head := r.commitWorkflow("nightly.yml", scheduleWF)
	r.commit(".github/workflows/"+evil, scheduleWF)
	head2 := r.head()
	r.branchRecord(done(head2))
	r.run()
	if got := r.remoteHead(); got != head2 {
		t.Fatalf("origin/%s = %q, want %s (after %s)", branch, got, head2, head)
	}
	body := r.forge.last(t)
	h, ok := protocol.ParseHeader(body)
	if !ok {
		t.Fatalf("not a protocol comment:\n%s", body)
	}
	ho, err := protocol.ParseHandoff(h)
	if err != nil {
		t.Fatalf("ParseHandoff: %v\n%s", err, body)
	}
	if ho.Next != after || ho.Worktree != r.wt || ho.Branch != branch || ho.Issue != 1 || ho.When != "now" {
		t.Errorf("handoff %+v, want next %q on %s in %s", ho, after, branch, r.wt)
	}
	base := tgit(t, r.main, "rev-parse", "main")
	if h.Fields["range"] != base+".."+head2 {
		t.Errorf("range %q, want %s..%s", h.Fields["range"], base, head2)
	}
	if !strings.Contains(h.Fields["workflows"], ".github/workflows/nightly.yml") {
		t.Errorf("workflows %q lacks nightly.yml", h.Fields["workflows"])
	}
	if strings.Count(body, "\nnext:") != 1 {
		t.Errorf("the handoff has more than one next line:\n%s", body)
	}
	if h.Session != "" {
		t.Errorf("the pusher's handoff carries a session %q", h.Session)
	}
	wantClosed(t, r.row(), runs.Handoff)
	if got := r.row().PushedSHA; got != head2 {
		t.Errorf("row pushed %q, want %s recorded on the build route too", got, head2)
	}
	if out := tgit(t, r.req.CloneDir, "for-each-ref", "refs/pusher/"); out != "" {
		t.Errorf("the run ref is left in the clone: %s", out)
	}
}

func TestFixPRRouteKeepsRowOpenAndSetsPushed(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	// The fix-pr run's handoff has no after; the PR on the branch exists.
	if err := runs.Release(context.Background(), r.db, r.rowID, runs.Done, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	r.handoff("")
	r.forge.prs = []forge.PR{{Number: 5, State: "OPEN", HeadRef: branch}}
	head := r.commitWorkflow("nightly.yml", scheduleWF)
	r.run()
	if got := r.remoteHead(); got != head {
		t.Fatalf("origin/%s = %q, want %s", branch, got, head)
	}
	row := r.row()
	if !row.Open || row.PushedSHA != head {
		t.Fatalf("row open %v pushed %q, want open with pushed %s", row.Open, row.PushedSHA, head)
	}
	body := r.forge.last(t)
	if !strings.HasPrefix(body, protocol.Robot+" "+protocol.Heading("note_pushed", "nl")) {
		t.Errorf("not the push note:\n%s", body)
	}
	if !strings.Contains(body, "range: "+tgit(t, r.main, "rev-parse", "main")+".."+head) {
		t.Errorf("push note lacks the range:\n%s", body)
	}
}

func TestNoPRAndNoAfterAsks(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	if err := runs.Release(context.Background(), r.db, r.rowID, runs.Done, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	r.handoff("")
	r.commitWorkflow("nightly.yml", scheduleWF)
	r.run()
	if got := r.remoteHead(); got != "" {
		t.Fatalf("pushed %s without a route", got)
	}
	returned(t, r.forge.last(t), StepRoute, r.wt)
	wantClosed(t, r.row(), runs.Returned)
}

func TestRunRefusesARowThatIsNotAnOpenPushRow(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	r.commitWorkflow("nightly.yml", scheduleWF)
	if err := runs.Release(context.Background(), r.db, r.rowID, runs.Done, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), r.deps, r.req); err == nil {
		t.Fatal("Run on a closed row succeeded")
	}
	if got := r.remoteHead(); got != "" {
		t.Fatalf("pushed %s for a closed row", got)
	}
}

// A build-route handoff that cannot be posted leaves the row open with the pushed SHA, so decide
// reports a push without a handoff, not a silent pusher.
func TestBuildRouteRecordsPushBeforeTheHandoff(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	head := r.commitWorkflow("nightly.yml", scheduleWF)
	r.forge.refuse = func(body string) bool { return strings.Contains(body, "next: "+after) }
	if err := Run(context.Background(), r.deps, r.req); err == nil {
		t.Fatal("Run succeeded although the handoff was not posted")
	}
	if got := r.remoteHead(); got != head {
		t.Fatalf("origin/%s = %q, want %s", branch, got, head)
	}
	row := r.row()
	if !row.Open || row.PushedSHA != head {
		t.Fatalf("row open %v pushed %q, want open with pushed %s", row.Open, row.PushedSHA, head)
	}
}

// GitHub's refusal of the token for a workflow change becomes the neutral sentence; other lines
// of the forge's answer stay, also when they hold the word scope.
func TestPushRefusalSaysTokenRefusedAndKeepsOtherLines(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	r.commitWorkflow("nightly.yml", scheduleWF)
	write(t, filepath.Join(r.remote, "hooks", "pre-receive"), "#!/bin/sh\n"+
		"echo 'the scope of this check is the whole repo' >&2\n"+
		"echo 'refusing to allow a Personal Access Token to create or update workflow `.github/workflows/nightly.yml` without `workflow` scope' >&2\n"+
		"exit 1\n")
	if err := os.Chmod(filepath.Join(r.remote, "hooks", "pre-receive"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.run()
	if got := r.remoteHead(); got != "" {
		t.Fatalf("pushed %s", got)
	}
	body := r.forge.last(t)
	for _, want := range []string{protocol.Heading("question_push_token_refused", "nl"), "the scope of this check is the whole repo", retryLine()} {
		if !strings.Contains(body, want) {
			t.Errorf("Vraag lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "refusing to allow") {
		t.Errorf("Vraag carries GitHub's refusal wording:\n%s", body)
	}
	if regexp.MustCompile(`(?i)scope`).MatchString(protocol.Heading("question_push_token_refused", "nl") + protocol.Heading("question_push_token_refused", "en")) {
		t.Error("the neutral sentence talks of a scope")
	}
}

// lateBy moves the pusher's clock on by d from the claim of its row.
func (r *runFix) lateBy(d time.Duration) {
	r.deps.Now = func() time.Time { return time.Now().Add(d) }
}

// A push that hangs on the network ends at the work deadline of the row, with a Vraag, and lets
// the clone lock go. The clock stands 4s before that deadline: timeout minus the post window and
// the margin after the claim.
func TestHungPushEndsWithinTimeout(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	r.lateBy(r.cfg.Timeout - postTimeout - deadlineMargin - 4*time.Second)
	r.commitWorkflow("nightly.yml", scheduleWF)
	write(t, filepath.Join(r.remote, "hooks", "pre-receive"), "#!/bin/sh\nexec sleep 30\n")
	if err := os.Chmod(filepath.Join(r.remote, "hooks", "pre-receive"), 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	r.run()
	if took := time.Since(start); took > 15*time.Second {
		t.Errorf("Run took %v with 4s left before its work deadline", took)
	}
	if body := r.forge.last(t); !strings.Contains(body, "step: "+StepPush) {
		t.Errorf("Vraag does not name the push step:\n%s", body)
	}
	wantClosed(t, r.row(), runs.Question)
	lock, red := LockClone(context.Background(), r.req.CloneDir)
	if red != nil {
		t.Fatalf("the clone lock is still held: %s", red.Why)
	}
	lock.Unlock()
}

// A symlink below the state dir never takes the attachment elsewhere: the diff is cut without one.
func TestAttachmentRefusesASymlinkedDir(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	elsewhere := filepath.Join(r.dir, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(r.req.StateDir, "push"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(r.req.StateDir, "push", "o")); err != nil {
		t.Fatal(err)
	}
	var long strings.Builder
	long.WriteString(preMergeWF)
	for i := 0; long.Len() < 100_000; i++ {
		long.WriteString("# filler line " + strconv.Itoa(i) + "\n")
	}
	r.commitWorkflow("ci.yml", long.String())
	r.run()
	body := r.forge.last(t)
	if n := len([]rune(body)); n >= maxComment {
		t.Fatalf("the Vraag has %d characters", n)
	}
	if strings.Contains(body, protocol.Heading("question_push_attachment", "nl")) || !strings.Contains(body, "could not be saved") {
		t.Errorf("the Vraag names an attachment through a symlink:\n%s", body[:400])
	}
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Errorf("the attachment went through the symlink: %v", entries)
	}
}

// A pusher that starts after the work deadline of its row (timeout minus the post window and the
// margin, from the claim) pushes nothing and posts nothing: whatever it posted could come after
// tick handed the row back. The row stays open for tick.
func TestLateStartDoesNotPush(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	r.lateBy(r.cfg.Timeout - postTimeout - deadlineMargin + time.Second)
	r.commitWorkflow("nightly.yml", scheduleWF)
	err := Run(context.Background(), r.deps, r.req)
	if err == nil || !strings.Contains(err.Error(), "work deadline") {
		t.Errorf("Run = %v, want the passed work deadline", err)
	}
	if got := r.remoteHead(); got != "" {
		t.Errorf("origin has %s, want no push", got)
	}
	if len(r.forge.posted) != 0 {
		t.Errorf("posted %q, want nothing", r.forge.posted)
	}
	if !r.row().Open {
		t.Error("the row closed, want it left open for tick")
	}
}

// A row tick hands back while the pusher works (here: right when it reads the workflow token, after
// the review) is never pushed for: the push would land on origin unrecorded. Nothing is posted
// under tick's Returned either.
func TestHandedBackRowIsNotPushed(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	r.deps.ReadToken = func(path string) (string, error) {
		if err := runs.Release(context.Background(), r.db, r.rowID, runs.Returned, "reason_pusher_silent", time.Now()); err != nil {
			t.Fatal(err)
		}
		return ReadTokenFile(path)
	}
	r.commitWorkflow("nightly.yml", scheduleWF)
	err := Run(context.Background(), r.deps, r.req)
	if err == nil || !strings.Contains(err.Error(), "handed back") {
		t.Errorf("Run = %v, want the handed-back row", err)
	}
	if got := r.remoteHead(); got != "" {
		t.Errorf("origin has %s, want no push", got)
	}
	if len(r.forge.posted) != 0 {
		t.Errorf("posted %q, want nothing under tick's Returned", r.forge.posted)
	}
	if row := r.row(); row.PushedSHA != "" || row.Outcome != runs.Returned {
		t.Errorf("row = %+v, want tick's Returned without a pushed SHA", row)
	}
}

func TestWorkDeadline(t *testing.T) {
	started := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	now := started.Add(time.Hour)
	if got, want := workDeadline(started, now, 30*time.Minute), started.Add(27*time.Minute); !got.Equal(want) {
		t.Errorf("deadline = %s, want %s: the timeout from the claim, less the post window and the margin", got, want)
	}
	if got, want := workDeadline(time.Time{}, now, 30*time.Minute), now.Add(27*time.Minute); !got.Equal(want) {
		t.Errorf("deadline without a start = %s, want %s from now", got, want)
	}
}

// closingForge closes the push row, as tick does at its timeout, when the pusher reads the PRs.
type closingForge struct {
	*fakeForge
	close func()
}

func (f closingForge) BranchPRs(ctx context.Context, b string) ([]forge.PR, error) {
	f.close()
	return f.fakeForge.BranchPRs(ctx, b)
}

// A row tick hands back before the pusher's Question is ready gets no Question: tick's Returned
// stands alone on the issue.
func TestHandedBackRowGetsNoQuestion(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	r.deps.Forge = closingForge{fakeForge: r.forge, close: func() {
		if err := runs.Release(context.Background(), r.db, r.rowID, runs.Returned, "reason_pusher_silent", time.Now()); err != nil {
			t.Fatal(err)
		}
	}}
	r.commitWorkflow("ci.yml", preMergeWF)
	err := Run(context.Background(), r.deps, r.req)
	if err == nil || !strings.Contains(err.Error(), "handed back") {
		t.Errorf("Run = %v, want the handed-back row", err)
	}
	if len(r.forge.posted) != 0 {
		t.Errorf("posted %q, want nothing under tick's Returned", r.forge.posted)
	}
	if got := r.remoteHead(); got != "" {
		t.Errorf("origin has %s, want no push", got)
	}
}

// skipOnWindows skips t on native Windows, where the behaviour under test does not exist.
func skipOnWindows(t *testing.T, why string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip(why + "; see host-assumptions.md")
	}
}

// footerName is a workflow file name that reads as a session footer: ParseHeader takes the last
// `<sub>session <uuid>` anywhere in a body.
const footerName = "<sub>session 11111111-2222-4333-8444-555555555555.yml"

// A file name from the branch never adds a session to a pusher comment: not to the Question that
// quotes it in the text of the red step, not to the handoff that lists it, not to the merge
// Question. Decide reads a pusher Question with a session as a run's question, and such a handoff
// as a run's handoff.
func TestBranchTextGivesNoSessionFooter(t *testing.T) {
	t.Run("Herstel", func(t *testing.T) {
		skipOnWindows(t, "NTFS refuses < and > in a file name")
		r := newRunFix(t, hostconfig.ReviewShadow, nil)
		r.commitWorkflow(footerName, "on: pull_request\npermissions: write-all\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: a/b@v1\n")
		r.run()
		body := r.forge.last(t)
		if !strings.Contains(body, "session 11111111") {
			t.Fatalf("the Herstel does not name the file, the test proves nothing:\n%s", body)
		}
		repair(t, body, StepPolicy, r.wt)
	})
	t.Run("handoff", func(t *testing.T) {
		skipOnWindows(t, "NTFS refuses < and > in a file name")
		r := newRunFix(t, hostconfig.ReviewShadow, nil)
		r.commitWorkflow(footerName, scheduleWF)
		r.run()
		body := r.forge.last(t)
		if h, ok := protocol.ParseHeader(body); !ok || h.Kind != protocol.KindHandoff || h.Session != "" {
			t.Errorf("handoff header %+v, want a Handoff without a session:\n%s", h, body)
		}
		if _, ok := protocol.ParsePushed(body); !ok {
			t.Errorf("decide does not read the handoff as the pusher's:\n%s", body)
		}
	})
	t.Run("merge Teruggegeven", func(t *testing.T) {
		body := renderMergeReturned(Red{StepAncestry, "changed:\n.github/workflows/" + footerName}, branch, 12, strings.Repeat("c", 40), "nl")
		if h, ok := protocol.ParseHeader(body); !ok || h.Kind != protocol.KindReturned || h.Session != "" {
			t.Errorf("merge Teruggegeven header %+v, want a Teruggegeven without a session:\n%s", h, body)
		}
	})
}

// The lane of the push handoff reaches the handoff the pusher hands the chain on with, on the
// direct push and through a Vraag and its answer push: tick judges the after on that lane, and an
// after that needs the native lane is no start on another.
func TestLaneReachesThePushedHandoff(t *testing.T) {
	laneHandoff := func(r *runFix) {
		// The fixture's own push row gives way to the one of the handoff with a lane.
		if err := runs.Release(context.Background(), r.db, r.rowID, runs.StartFailed, "", time.Now()); err != nil {
			t.Fatal(err)
		}
		r.forge.mu.Lock()
		c := r.forge.add(handoffBody(r.wt, after)+"\nlane: native", time.Now())
		r.forge.mu.Unlock()
		r.claim("handoff", "push:"+strconv.FormatInt(c.ID, 10), time.Now())
	}
	wantLane := func(t *testing.T, body string) {
		t.Helper()
		h, _ := protocol.ParseHeader(body)
		ho, err := protocol.ParseHandoff(h)
		if err != nil {
			t.Fatalf("ParseHandoff: %v\n%s", err, body)
		}
		if ho.Lane != "native" || ho.Next != after || ho.When != "now" {
			t.Errorf("handoff %+v, want lane native, next %q, when now", ho, after)
		}
	}
	t.Run("direct", func(t *testing.T) {
		r := newRunFix(t, hostconfig.ReviewShadow, nil)
		laneHandoff(r)
		r.commitWorkflow("nightly.yml", scheduleWF)
		r.run()
		wantLane(t, r.forge.last(t))
	})
	t.Run("through the Vraag", func(t *testing.T) {
		r := newRunFix(t, hostconfig.ReviewShadow, nil)
		laneHandoff(r)
		head := r.commitWorkflow("ci.yml", preMergeWF)
		r.run()
		h := question(t, r.forge.last(t), head, r.wt)
		if h.Fields["lane"] != "native" {
			t.Errorf("Vraag fields %v, want lane native", h.Fields)
		}
		r.answerPush()
		r.req.Permit = head
		r.run()
		wantLane(t, r.forge.last(t))
	})
}

// The issue the intent reviewer reads in a runFix.
const (
	issueTitle = "Werk de CI bij"
	issueBody  = "Zet FOO in de build op baz."
)

// intentBase puts baseWF on origin/main and merges it into the branch, so the merge-base has
// it: a change to it on the branch can be in the class intent.
func (r *runFix) intentBase() {
	r.t.Helper()
	write(r.t, filepath.Join(r.main, ".github/workflows/ci.yml"), baseWF)
	tgit(r.t, r.main, "add", ".")
	tgit(r.t, r.main, "commit", "-q", "-m", "workflows")
	tgit(r.t, r.main, "push", "-q", "origin", "main")
	tgit(r.t, r.wt, "merge", "-q", "--no-edit", "main")
}

// inClassChange is a change of ci.yml inside the class intent: only an env value.
func (r *runFix) inClassChange() string {
	r.t.Helper()
	r.intentBase()
	return r.commitWorkflow("ci.yml", swap(r.t, baseWF, "FOO: bar", "FOO: baz"))
}

// intentReviewer makes the intent reviewer of r a script that touches a marker of its own, copies
// its stdin to a file and answers text; it returns whether it ran and the stdin it read.
func (r *runFix) intentReviewer(text string) (ran func() bool, stdin func() string) {
	r.t.Helper()
	marker, in := filepath.Join(r.dir, "intent-ran"), filepath.Join(r.dir, "intent-stdin")
	write(r.t, filepath.Join(r.bin, "intent"), "#!/bin/sh\ntouch "+marker+"\ncat > "+in+"\nprintf '%s' '"+text+"' > \"$1\"\n")
	if err := os.Chmod(filepath.Join(r.bin, "intent"), 0o755); err != nil {
		r.t.Fatal(err)
	}
	r.cfg.Intent = &hostconfig.Reviewer{Command: []string{"intent", "{out}", "-"}}
	ran = func() bool { _, err := os.Stat(marker); return err == nil }
	stdin = func() string {
		text, err := os.ReadFile(in)
		if err != nil {
			r.t.Fatalf("the intent reviewer read no stdin: %v", err)
		}
		return string(text)
	}
	return ran, stdin
}

func shadowLine() string { return protocol.Heading("question_push_shadow", "nl") }

// An approving intent reviewer replaces the security review for a change in the class: the
// pusher pushes in shadow and in enforce, and no security reviewer runs. Fails when
// pushUnderLock does not act on the intent verdict.
func TestIntentApprovePushesInShadow(t *testing.T) {
	for _, review := range []string{hostconfig.ReviewShadow, hostconfig.ReviewEnforce} {
		t.Run(review, func(t *testing.T) {
			r := newRunFix(t, review, nil)
			r.cfg.Reviewers = []hostconfig.Reviewer{rv("supply-chain", "never"), rv("injection", "never")}
			ran, _ := r.intentReviewer(approveJSON)
			head := r.inClassChange()
			r.run()
			if got := r.remoteHead(); got != head {
				t.Fatalf("origin/%s = %q, want the pushed %s:\n%s", branch, got, head, r.forge.last(t))
			}
			if !ran() {
				t.Error("the intent reviewer did not run")
			}
			if r.reviewerRan() {
				t.Error("a security reviewer ran after an intent approve")
			}
			if strings.Contains(r.forge.last(t), "🤖 Vraag") {
				t.Errorf("a Vraag after an intent approve:\n%s", r.forge.last(t))
			}
			wantClosed(t, r.row(), runs.Handoff)
		})
	}
}

// An intent reject is a Vraag that permits the SHA, with the intent verdict, and no push, also in
// enforce with security reviewers that would approve.
func TestIntentRejectAsksWithVerdict(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewEnforce, nil)
	r.cfg.Reviewers = []hostconfig.Reviewer{rv("supply-chain", "never"), rv("injection", "never")}
	ran, _ := r.intentReviewer(rejectJSON)
	head := r.inClassChange()
	r.run()
	if got := r.remoteHead(); got != "" {
		t.Fatalf("pushed %s after an intent reject", got)
	}
	if !ran() {
		t.Fatal("the intent reviewer did not run")
	}
	if r.reviewerRan() {
		t.Error("a security reviewer ran after an intent reject")
	}
	body := r.forge.last(t)
	h := question(t, body, head, r.wt)
	if h.Fields["step"] != StepReview {
		t.Errorf("step %q, want %s", h.Fields["step"], StepReview)
	}
	verdict := "**" + protocol.Heading("question_push_verdict", "nl") + ":**\n- intent: reject"
	for _, want := range []string{permitLine(), verdict, "unpinned action", "uses: foo/bar@v1"} {
		if !strings.Contains(body, want) {
			t.Errorf("intent Vraag lacks %q:\n%s", want, body)
		}
	}
	wantClosed(t, r.row(), runs.Question)
}

// ordinaryRoad checks the shadow Vraag of the security review: the reviewers ran, nothing pushed.
func (r *runFix) ordinaryRoad(head string) {
	r.t.Helper()
	if got := r.remoteHead(); got != "" {
		r.t.Fatalf("pushed %s on the ordinary road in shadow", got)
	}
	if !r.reviewerRan() {
		r.t.Error("the security reviewers did not run")
	}
	body := r.forge.last(r.t)
	question(r.t, body, head, r.wt)
	if !strings.Contains(body, shadowLine()) || strings.Contains(body, "- intent:") {
		r.t.Errorf("not the shadow Vraag of the security review:\n%s", body)
	}
}

// Without [dispatch.workflow_push.intent] a change in the class takes the ordinary road, not a
// COULD NOT CHECK reject of a missing intent reviewer.
func TestNoIntentTableTakesOrdinaryRoad(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	r.cfg.Reviewers = []hostconfig.Reviewer{rv("supply-chain", "never"), rv("injection", "never")}
	head := r.inClassChange()
	r.run()
	r.ordinaryRoad(head)
}

// An issue the forge does not give takes the ordinary road: the intent reviewer never judges
// without the one anchor from outside the run.
func TestIssueUnreadableTakesOrdinaryRoad(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	r.cfg.Reviewers = []hostconfig.Reviewer{rv("supply-chain", "never"), rv("injection", "never")}
	ran, _ := r.intentReviewer(approveJSON)
	r.forge.issueErr = errors.New("fake forge: 502")
	head := r.inClassChange()
	r.run()
	r.ordinaryRoad(head)
	if ran() {
		t.Error("the intent reviewer ran without the issue")
	}
}

// A new action puts the change outside the class: the intent reviewer is never asked.
func TestOutsideClassNeverRunsIntentReviewer(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	r.cfg.Reviewers = []hostconfig.Reviewer{rv("supply-chain", "never"), rv("injection", "never")}
	ran, _ := r.intentReviewer(approveJSON)
	r.intentBase()
	head := r.commitWorkflow("ci.yml", swap(t, baseWF, "      - run: make\n", "      - uses: actions/setup-go@"+pin+"\n      - run: make\n"))
	r.run()
	r.ordinaryRoad(head)
	if ran() {
		t.Error("the intent reviewer ran for a change with a new action")
	}
}

// The Vraag of the review says what is missing: no reviewers, no intent table. Each line stands
// only when it holds, and a cut diff keeps them.
func TestQuestionNamesMissingReviewers(t *testing.T) {
	noReviewers := protocol.Heading("question_push_no_reviewers", "nl")
	noIntent := protocol.Heading("question_push_no_intent", "nl")
	for _, line := range []string{noReviewers, noIntent} {
		for _, want := range []string{"docs/tick.md", "De review"} {
			if !strings.Contains(line, want) {
				t.Errorf("%q does not name %q", line, want)
			}
		}
	}
	for _, want := range []string{"[[dispatch.workflow_push.reviewers]]", `review = "enforce"`} {
		if !strings.Contains(noReviewers, want) {
			t.Errorf("%q does not name %q", noReviewers, want)
		}
	}
	if !strings.Contains(noIntent, "[dispatch.workflow_push.intent]") {
		t.Errorf("%q does not name the intent table", noIntent)
	}
	cases := []struct {
		name                  string
		reviewers             []hostconfig.Reviewer
		intent                bool
		wantNoRevs, wantNoInt bool
	}{
		{"no reviewers and no intent", nil, false, true, true},
		{"reviewers and no intent", []hostconfig.Reviewer{rv("supply-chain", "approve"), rv("injection", "approve")}, false, false, true},
		{"reviewers and intent", []hostconfig.Reviewer{rv("supply-chain", "approve"), rv("injection", "approve")}, true, false, false},
		{"intent and no reviewers", nil, true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRunFix(t, hostconfig.ReviewShadow, nil)
			r.cfg.Reviewers = tc.reviewers
			if tc.intent {
				r.intentReviewer(approveJSON)
			}
			head := r.commitWorkflow("ci.yml", preMergeWF) // a new workflow: outside the class
			r.run()
			body := r.forge.last(t)
			question(t, body, head, r.wt)
			if got := strings.Contains(body, noReviewers); got != tc.wantNoRevs {
				t.Errorf("no-reviewers line %v, want %v:\n%s", got, tc.wantNoRevs, body)
			}
			if got := strings.Contains(body, noIntent); got != tc.wantNoInt {
				t.Errorf("no-intent line %v, want %v:\n%s", got, tc.wantNoInt, body)
			}
		})
	}
	t.Run("a cut diff keeps the lines", func(t *testing.T) {
		q := questionInput{red: Red{StepReview, "x"}, sha: strings.Repeat("a", 40), worktree: "/w", branch: branch,
			diff: strings.Repeat("+line\n", maxComment), hint: hintPermit, noReviewers: true, noIntent: true}
		body := fitQuestion(q, "nl", nil, func(string) (string, error) { return "/s/x.diff", nil })
		if !strings.Contains(body, noReviewers) || !strings.Contains(body, noIntent) {
			t.Errorf("the cut Vraag lost a line:\n%s", body[:2000])
		}
	})
	t.Run("only on a review Vraag", func(t *testing.T) {
		q := questionInput{red: Red{StepPermit, "moved"}, sha: strings.Repeat("a", 40), worktree: "/w", branch: branch,
			hint: hintMoved, noReviewers: true, noIntent: true}
		body := renderQuestion(q, "nl")
		if strings.Contains(body, noReviewers) || strings.Contains(body, noIntent) {
			t.Errorf("a moved Vraag names the review config:\n%s", body)
		}
	})
}

// The spec and the plan the branch adds or changes reach the intent reviewer, with the issue; a
// spec the branch does not touch, a file outside the two dirs and a symlink do not.
func TestSpecAndPlanOfBranchReachReviewer(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	r.req.SpecsDir, r.req.PlansDir = "design/specs", "design/plans"
	_, stdin := r.intentReviewer(approveJSON)
	write(t, filepath.Join(r.main, "design/specs/old.md"), "OLD-SPEC-TEXT\n")
	r.intentBase()
	r.commit("design/specs/new-design.md", "NEW-SPEC-TEXT\n")
	r.commit("design/plans/new.md", "NEW-PLAN-TEXT\n")
	r.commit("design/other.md", "OTHER-TEXT\n")
	secret := filepath.Join(r.dir, "outside.md")
	write(t, secret, "OUTSIDE-TEXT\n")
	if err := os.Symlink(secret, filepath.Join(r.wt, "design/specs/link.md")); err != nil {
		t.Fatal(err)
	}
	tgit(t, r.wt, "add", "design/specs/link.md")
	tgit(t, r.wt, "commit", "-q", "-m", "link")
	head := r.commitWorkflow("ci.yml", swap(t, baseWF, "FOO: bar", "FOO: baz"))
	r.run()
	if got := r.remoteHead(); got != head {
		t.Fatalf("origin/%s = %q, want the pushed %s:\n%s", branch, got, head, r.forge.last(t))
	}
	in := stdin()
	for _, want := range []string{issueTitle, issueBody, "design/specs/new-design.md", "NEW-SPEC-TEXT", "design/plans/new.md", "NEW-PLAN-TEXT", "FOO: baz"} {
		if !strings.Contains(in, want) {
			t.Errorf("the intent reviewer did not get %q:\n%s", want, in)
		}
	}
	if !strings.Contains(in, "Path: design/specs/link.md\n\n[not read: ") {
		t.Errorf("the symlink is not named as not read:\n%s", in)
	}
	// A symlink's blob is its target: read as a file, the target path would reach the reviewer.
	for _, not := range []string{"OLD-SPEC-TEXT", "OTHER-TEXT", "OUTSIDE-TEXT", secret} {
		if strings.Contains(in, not) {
			t.Errorf("the intent reviewer got %q:\n%s", not, in)
		}
	}
}

// pushedQuestion is the pusher Vraag of sha in wt as renderQuestion writes it, with after and lane.
func pushedQuestion(sha, wt string) string {
	return renderQuestion(questionInput{sha: sha, worktree: wt, branch: branch, after: after, lane: "native",
		red: Red{StepReview, "shadow"}, hint: hintPermit}, "nl")
}

const pushedSHA = "0123456789abcdef0123456789abcdef01234567"

// A pushed:<id> key names the pusher Vraag with that id: its worktree, branch, after and lane at
// the time of the Vraag, and no permit. Fails when targetOf does not know pushed, or gives the
// Vraag's SHA as a permit: nobody answered, so nothing lifts the review.
func TestTargetOfPushed(t *testing.T) {
	at := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	comments := []protocol.Comment{
		{ID: 7, Body: handoffBody("/w", after), CreatedAt: at.Add(-time.Hour).Format(time.RFC3339)},
		{ID: 8, Body: pushedQuestion(pushedSHA, "/w"), CreatedAt: at.Format(time.RFC3339)},
	}
	got, err := targetOf(comments, "pushed:8")
	if err != nil {
		t.Fatalf("targetOf: %v", err)
	}
	want := target{worktree: "/w", branch: branch, after: after, lane: "native", at: at}
	if got != want {
		t.Errorf("targetOf = %+v, want %+v", got, want)
	}
}

// Only a pusher Vraag starts a pushed row: a handoff, a Vraag of a session, a Herstel with a sha,
// a Vraag without a usable sha and a gone comment are refused.
func TestTargetOfPushedRejectsNonQuestion(t *testing.T) {
	at := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	q := pushedQuestion(pushedSHA, "/w")
	cases := map[string]string{
		"handoff":           handoffBody("/w", after),
		"question of a run": q + "\n<sub>session " + runs.NewSessionID() + "</sub>",
		"herstel":           strings.Replace(q, protocol.Heading("kind_question", "nl"), protocol.Heading("kind_repair", "nl"), 1),
		"no sha":            pushedQuestion("", "/w"),
		"short sha":         pushedQuestion("0123abc", "/w"),
		"relative wt":       pushedQuestion(pushedSHA, "w"),
		"not a protocol":    "push",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := targetOf([]protocol.Comment{{ID: 8, Body: body, CreatedAt: at}}, "pushed:8"); err == nil {
				t.Errorf("targetOf accepted %s:\n%s", name, body)
			}
		})
	}
	t.Run("gone", func(t *testing.T) {
		if _, err := targetOf([]protocol.Comment{{ID: 8, Body: q, CreatedAt: at}}, "pushed:9"); err == nil {
			t.Error("targetOf accepted the id of a comment that is not there")
		}
	})
}

// lastQuestion is the id of the last comment the pusher posted, which must be a pusher Vraag of sha.
func (r *runFix) lastQuestion(sha string) int64 {
	r.t.Helper()
	body := r.forge.last(r.t)
	question(r.t, body, sha, r.wt)
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	for i := len(r.forge.comments) - 1; i >= 0; i-- {
		if r.forge.comments[i].Body == body {
			return r.forge.comments[i].ID
		}
	}
	r.t.Fatal("the last post is not among the comments")
	return 0
}

// pushedRow claims the push row tick starts for a Vraag pushed by hand: trigger answer, key
// pushed:<id>, and no permit.
func (r *runFix) pushedRow(id int64) {
	r.t.Helper()
	r.claim("answer", "pushed:"+strconv.FormatInt(id, 10), time.Now().Add(2*time.Second))
	r.req.Permit = ""
}

// Someone pushed the SHA of the Vraag by hand: the pushed row reads the Vraag, the floor is green
// with AlreadyPushed and the chain goes on to after, with no review and no push of its own.
// Fails when targetOf does not know pushed (the row then asks at the trigger step).
func TestRunPushedAlreadyOnOriginHandsOn(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	head := r.commitWorkflow("ci.yml", preMergeWF)
	r.run()
	id := r.lastQuestion(head)
	r.cfg.Reviewers = []hostconfig.Reviewer{rv("supply-chain", "never"), rv("injection", "never")}
	tgit(t, r.wt, "push", "-q", "origin", branch)
	base := tgit(t, r.main, "rev-parse", "main")
	r.pushedRow(id)
	r.run()
	if r.reviewerRan() {
		t.Error("a reviewer ran for a SHA origin already has")
	}
	if got := r.remoteHead(); got != head {
		t.Fatalf("origin/%s = %q, want the hand-pushed %s", branch, got, head)
	}
	h, _ := protocol.ParseHeader(r.forge.last(t))
	ho, err := protocol.ParseHandoff(h)
	if err != nil {
		t.Fatalf("ParseHandoff: %v\n%s", err, r.forge.last(t))
	}
	if ho.Next != after || ho.Branch != branch || h.Fields["range"] != base+".."+head {
		t.Errorf("handoff %+v range %q, want next %q on %s, range %s..%s", ho, h.Fields["range"], after, branch, base, head)
	}
	wantClosed(t, r.row(), runs.Handoff)
	if got := r.row().PushedSHA; got != head {
		t.Errorf("row pushed %q, want %s", got, head)
	}
}

// The forge said origin had the SHA, but it has not (a race, or the forge lied): the pushed row
// runs the whole road, review included, and ends in a new Vraag with a new id, never in a push.
// Fails when a pushed start counts as a permit.
func TestRunPushedNotOnOriginRunsFullRoad(t *testing.T) {
	r := newRunFix(t, hostconfig.ReviewShadow, nil)
	head := r.commitWorkflow("ci.yml", preMergeWF)
	r.run()
	id := r.lastQuestion(head)
	r.cfg.Reviewers = []hostconfig.Reviewer{rv("supply-chain", "never"), rv("injection", "never")}
	r.pushedRow(id)
	r.run()
	if !r.reviewerRan() {
		t.Error("no reviewer ran: the pushed start skipped the review")
	}
	if got := r.remoteHead(); got != "" {
		t.Fatalf("pushed %s without an answer", got)
	}
	if again := r.lastQuestion(head); again == id {
		t.Errorf("the new Vraag has the id %d of the old one", again)
	}
	wantClosed(t, r.row(), runs.Question)
}

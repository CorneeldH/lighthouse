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
	r := &runFix{fixture: f, db: s.DB, forge: &fakeForge{}, t0: time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
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

// handoff posts the handoff of the run and claims the push row for it.
func (r *runFix) handoff(after string) {
	r.t.Helper()
	c := r.forge.add(handoffBody(r.wt, after), r.t0.Add(2*time.Minute))
	r.claim("handoff", "push:"+strconv.FormatInt(c.ID, 10), r.t0.Add(3*time.Minute))
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

func wantClosed(t *testing.T, row runs.Row, o runs.Outcome) {
	t.Helper()
	if row.Open || row.Outcome != o {
		t.Fatalf("row open %v outcome %q, want closed %q", row.Open, row.Outcome, o)
	}
}

func permitLine() string { return protocol.Heading("question_push_permit", "nl") }
func floorLine() string  { return protocol.Heading("question_push_floor", "nl") }

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
	if strings.Contains(body, floorLine()) {
		t.Errorf("a review reject says the floor is red:\n%s", body)
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
		question(t, body, moved, r.wt)
		if !strings.Contains(body, StepPermit) {
			t.Errorf("Vraag does not name the permit step:\n%s", body)
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
	if !strings.Contains(body, StepClean) || !strings.Contains(body, floorLine()) {
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
			if h, ok := protocol.ParseHeader(body); !ok || h.Kind != protocol.KindQuestion {
				t.Fatalf("not a Vraag:\n%s", body)
			}
			if scope.MatchString(strings.ReplaceAll(body, r.dir, "")) {
				t.Errorf("the Vraag mentions a token scope:\n%s", body)
			}
			wantClosed(t, r.row(), runs.Question)
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
	gitDir := tgit(t, r.wt, "rev-parse", "--absolute-git-dir")
	if !strings.HasPrefix(path, gitDir+string(filepath.Separator)) {
		t.Errorf("attachment %s is not in the worktree's git-dir %s", path, gitDir)
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
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(r.cfg.TokenFile, link); err != nil {
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
	if body := r.forge.last(t); !strings.Contains(body, StepRoute) {
		t.Errorf("Vraag does not name the route step:\n%s", body)
	}
	wantClosed(t, r.row(), runs.Question)
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

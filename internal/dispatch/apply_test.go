package dispatch

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

const (
	applyRepo    = "o/r"
	freshSession = "11111111-1111-4111-8111-111111111111"
	oldSession   = "22222222-2222-4222-8222-222222222222"
)

// testLabels names every blocking label after its key.
var testLabels = repoconfig.Labels{BlockedIssue: "blocked_issue", BlockedExternal: "blocked_external",
	NeedsShaping: "needs_shaping", NeedsDecision: "needs_decision", NeedsHands: "needs_hands", NeedsTesting: "needs_testing"}

var applyNow = time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)

// callLog records the boundary calls of one apply, poster and workplace together, in order.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(call string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, call)
}

func (l *callLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

// fakeForge is the forge of one repo: it keeps what was posted, the rockets and the status
// sets; postErr, rocketErr, mergeErr and setErr make the call fail. A method apply must not
// call panics on the nil Forge.
type fakeForge struct {
	forge.Forge
	log       *callLog
	setErr    error
	sets      []string // "<itemID> <status>" per SetStatus
	postErr   error
	rocketErr error
	mergeErr  error
	merges    []string // "<pr> <sha>" per MergePR
	created   []string // "<branch> <title>" per CreatePR
	prBodies  []string // the body per CreatePR
	createErr error
	closed    []int // the number per ClosePR
	closeErr  error
	deleted   []string // the branch per DeleteBranch
	deleteErr error
	onPost    func() // runs at the start of CreateComment
	nextID    int64
	issues    []int
	bodies    []string
	rockets   []int64
	rocketOn  []int // the issue number per Acknowledge
	// The issue a 🤖 Returned gives back (issuestate.Return): closedIssue closes it, labels are
	// its label names, status its board status ("" is Controller), editErr fails EditIssue.
	closedIssue bool
	labels      []string
	status      string
	editErr     error
}

func (p *fakeForge) Issue(_ context.Context, number int) (forge.Issue, error) {
	p.log.add("Issue")
	state := "open"
	if p.closedIssue {
		state = "closed"
	}
	return forge.Issue{Number: number, State: state, Labels: slices.Clone(p.labels)}, nil
}

func (p *fakeForge) EditIssue(_ context.Context, number int, e forge.IssueEdit) (forge.Issue, error) {
	p.log.add("EditIssue")
	if p.editErr != nil {
		return forge.Issue{}, p.editErr
	}
	p.labels = slices.DeleteFunc(p.labels, func(l string) bool { return slices.Contains(e.RemoveLabels, l) })
	p.labels = append(p.labels, e.AddLabels...)
	return forge.Issue{Number: number, Labels: slices.Clone(p.labels)}, nil
}

func (p *fakeForge) IssueItem(_ context.Context, number int) (forge.BoardItem, bool, error) {
	p.log.add("IssueItem")
	status := cmp.Or(p.status, statusController)
	return forge.BoardItem{ItemID: "PVTI_" + strconv.Itoa(number), Issue: number, Status: status, StatusName: status,
		Priority: "P1"}, true, nil
}

func (p *fakeForge) CreateComment(_ context.Context, number int, body string) (protocol.Comment, error) {
	p.log.add("CreateComment")
	if p.onPost != nil {
		p.onPost()
	}
	if p.postErr != nil {
		return protocol.Comment{}, p.postErr
	}
	p.nextID++
	p.issues = append(p.issues, number)
	p.bodies = append(p.bodies, body)
	return protocol.Comment{ID: 1000 + p.nextID, Body: body}, nil
}

func (p *fakeForge) Acknowledge(_ context.Context, number int, commentID int64) error {
	p.log.add("Acknowledge")
	p.rocketOn = append(p.rocketOn, number)
	p.rockets = append(p.rockets, commentID)
	return p.rocketErr
}

func (p *fakeForge) MergePR(_ context.Context, number int, sha string) error {
	p.log.add("MergePR")
	p.merges = append(p.merges, fmt.Sprintf("%d %s", number, sha))
	return p.mergeErr
}

func (p *fakeForge) CreatePR(_ context.Context, in forge.NewPR) (forge.PR, error) {
	branch, title, body := in.Head, in.Title, in.Body
	p.log.add("CreatePR")
	if p.createErr != nil {
		return forge.PR{}, p.createErr
	}
	p.created = append(p.created, branch+" "+title)
	p.prBodies = append(p.prBodies, body)
	return forge.PR{Number: 900 + len(p.created), HeadRef: branch, State: "OPEN"}, nil
}

func (p *fakeForge) ClosePR(_ context.Context, number int, _ string) error {
	p.log.add("ClosePR")
	if p.closeErr != nil {
		return p.closeErr
	}
	p.closed = append(p.closed, number)
	return nil
}

func (p *fakeForge) DeleteBranch(_ context.Context, branch string) error {
	p.log.add("DeleteBranch")
	if p.deleteErr != nil {
		return p.deleteErr
	}
	p.deleted = append(p.deleted, branch)
	return nil
}

func (p *fakeForge) SetStatus(_ context.Context, item forge.BoardItem, status string) error {
	p.log.add("SetStatus")
	if p.setErr != nil {
		return p.setErr
	}
	p.sets = append(p.sets, item.ItemID+" "+status)
	p.status = status
	return nil
}

// fakeWorkplace prepares a workspace per branch and records every spawn.
type fakeWorkplace struct {
	log        *callLog
	prepareErr error
	sessions   []workplace.Session
	spawnErr   error
	onSpawn    func() // runs at the start of Spawn
	spawned    []workplace.LaunchSpec
	stopErr    error
	stopped    []string // "<workspace> <session ids>" per Stop
	killErr    error
	onKill     func()   // runs at the start of Kill
	killed     []string // "<workspace> <session ids>" per Kill
	forgot     []string // workspace per Discard
	discardErr error
	onDiscard  func()            // runs at the start of Discard
	root       string            // prefixed to the workspace path, so a test can give it real files
	paths      map[string]string // the path Lookup gives a workspace id; missing is /work/<id>
}

func (w *fakeWorkplace) Prepare(_ context.Context, branch string) (workplace.Workspace, error) {
	w.log.add("Prepare")
	if w.prepareErr != nil {
		return workplace.Workspace{}, w.prepareErr
	}
	return workplace.Workspace{ID: "ws-" + branch, Path: w.root + "/forge/" + branch, Branch: branch}, nil
}

func (w *fakeWorkplace) Spawn(_ context.Context, ws workplace.Workspace, l workplace.LaunchSpec) (workplace.Session, error) {
	w.log.add("Spawn")
	w.spawned = append(w.spawned, l)
	if w.onSpawn != nil {
		w.onSpawn()
	}
	if w.spawnErr != nil {
		return workplace.Session{}, w.spawnErr
	}
	return workplace.Session{Key: "key-" + l.SessionID, WorkspaceID: ws.ID, SessionID: l.SessionID, State: "working"}, nil
}

func (w *fakeWorkplace) Sessions(_ context.Context, workspaceID string) ([]workplace.Session, error) {
	w.log.add("Sessions " + workspaceID)
	return w.sessions, nil
}

func (w *fakeWorkplace) Stop(_ context.Context, workspaceID string, sessionIDs []string) error {
	w.log.add("Stop")
	w.stopped = append(w.stopped, workspaceID+" "+strings.Join(sessionIDs, ","))
	return w.stopErr
}

func (w *fakeWorkplace) Kill(_ context.Context, workspaceID string, sessionIDs []string) error {
	w.log.add("Kill")
	if w.onKill != nil {
		w.onKill()
	}
	w.killed = append(w.killed, workspaceID+" "+strings.Join(sessionIDs, ","))
	return w.killErr
}

func (w *fakeWorkplace) Remove(context.Context, string, workplace.RemoveOptions) error {
	w.log.add("Remove")
	return nil
}

func (w *fakeWorkplace) Lookup(_ context.Context, workspaceID string) (workplace.Workspace, error) {
	w.log.add("Lookup " + workspaceID)
	if p, ok := w.paths[workspaceID]; ok {
		return workplace.Workspace{ID: workspaceID, Path: p}, nil
	}
	return workplace.Workspace{ID: workspaceID, Path: "/work/" + workspaceID}, nil
}

func (w *fakeWorkplace) Discard(_ context.Context, workspaceID string) error {
	w.log.add("Discard")
	if w.onDiscard != nil {
		w.onDiscard()
	}
	if w.discardErr != nil {
		return w.discardErr
	}
	w.forgot = append(w.forgot, workspaceID)
	return nil
}

// harness is one apply process: a database, a poster and a workplace per repo.
type harness struct {
	db      *sql.DB
	log     *callLog
	posters map[string]*fakeForge
	wp      *fakeWorkplace
	places  []string // "<repo> <node> <profile>" per Env.Workplace call
	out     *bytes.Buffer
	env     Env
}

func openApplyDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	s, err := store.OpenShared(context.Background(), path)
	if err != nil {
		t.Fatalf("OpenShared: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s.DB
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessOn(t, openApplyDB(t, filepath.Join(t.TempDir(), "l.db")))
}

func newHarnessOn(t *testing.T, db *sql.DB) *harness {
	t.Helper()
	log := &callLog{}
	h := &harness{db: db, log: log, posters: map[string]*fakeForge{},
		wp: &fakeWorkplace{log: log}, out: &bytes.Buffer{}}
	h.env = Env{
		DB: db,
		Forge: func(repo string) forge.Forge {
			p, ok := h.posters[repo]
			if !ok {
				p = &fakeForge{log: log}
				h.posters[repo] = p
			}
			return p
		},
		Workplace: func(repo, node, profile string) workplace.Workplace {
			h.places = append(h.places, repo+" "+node+" "+profile)
			return h.wp
		},
		MCPConfig:  func(profile string) string { return "/etc/lighthouse/" + profile + ".json" },
		RoundLimit: func(string) string { return "stop" },
		Labels:     func(string) repoconfig.Labels { return testLabels },
		StateDir:   t.TempDir(),
		MaxWait:    10 * time.Minute,
		Now:        func() time.Time { return applyNow },
		NewSession: func() string { return freshSession },
		Log:        h.out,
	}
	return h
}

func (h *harness) poster(repo string) *fakeForge {
	return h.env.Forge(repo).(*fakeForge)
}

func (h *harness) history(t *testing.T) []runs.Row {
	t.Helper()
	rows, err := runs.History(context.Background(), h.db, applyRepo)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	return rows
}

// seed claims a run and, unless outcome is "", closes it.
func (h *harness) seed(t *testing.T, issue int, session, key string, outcome runs.Outcome) int64 {
	t.Helper()
	ctx := context.Background()
	run, err := runs.Take(ctx, h.db, runs.Claim{
		Repo: applyRepo, Issue: issue, SessionID: session, Mode: "start", Trigger: "handoff",
		TriggerKey: key, Command: "/lighthouse-start-issue 7", Branch: "issue-7-x", Now: applyNow.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("seed Take: %v", err)
	}
	if outcome != "" {
		if err := runs.Release(ctx, h.db, run.ID, outcome, "", applyNow.Add(-time.Minute)); err != nil {
			t.Fatalf("seed Release: %v", err)
		}
	}
	return run.ID
}

func startAction() Action {
	return Action{
		Kind: KindStart, Repo: applyRepo, Issue: 7, Mode: "start", Trigger: "handoff",
		TriggerKey: "handoff:55", Attempt: 1, Command: "/lighthouse-review-spec /forge/issue-7-x/spec.md",
		Prompt: "/lighthouse-review-spec /forge/issue-7-x/spec.md (issue #7, controller-run)",
		Branch: "issue-7-x", Native: true, Skip: "skip_plan_review", CommentID: 55, Language: "nl",
		Profile: "writing",
	}
}

func resumeAction() Action {
	return Action{
		Kind: KindResume, Repo: applyRepo, Issue: 7, Mode: "resume", Trigger: "answer",
		TriggerKey: "answer:88", Attempt: 1, SessionID: oldSession, Command: "/lighthouse-start-issue 7",
		Prompt: "the answer", Branch: "issue-7-x", CommentID: 88, Language: "nl", Profile: "writing",
	}
}

func TestApplyStartOrder(t *testing.T) {
	h := newHarness(t)
	res, err := Apply(context.Background(), h.env, []Action{startAction()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	wantCalls := []string{"Prepare", "Sessions ws-issue-7-x", "CreateComment", "Acknowledge", "Prepare", "Spawn"}
	if got := h.log.all(); !reflect.DeepEqual(got, wantCalls) {
		t.Fatalf("calls = %q, want %q", got, wantCalls)
	}
	if res.Started != 1 {
		t.Errorf("Started = %d, want 1", res.Started)
	}
	rows := h.history(t)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one", rows)
	}
	row := rows[0]
	if !row.Open || row.SessionID != freshSession || row.Mode != "start" || row.TriggerKey != "handoff:55" ||
		row.Branch != "issue-7-x" || row.Profile != "writing" {
		t.Errorf("row = %+v", row)
	}
	p := h.poster(applyRepo)
	wantBody := "🤖 Gestart\n" +
		"session: " + freshSession + "\n" +
		"worktree: /forge/issue-7-x\n" +
		"command: /lighthouse-review-spec /forge/issue-7-x/spec.md\n" +
		"start: 2026-10-31T09:00:00Z\n" +
		"branch: issue-7-x\n" +
		"skip: plan-review overgeslagen (config)\n" +
		"runner: forge\n" +
		"workspace: ws-issue-7-x\n\n" +
		"<sub>session " + freshSession + "</sub>"
	if !reflect.DeepEqual(p.bodies, []string{wantBody}) || !reflect.DeepEqual(p.issues, []int{7}) {
		t.Errorf("posted %v on %v, want\n%s\non [7]", p.bodies, p.issues, wantBody)
	}
	if !reflect.DeepEqual(p.rockets, []int64{55}) {
		t.Errorf("rockets = %v, want [55]", p.rockets)
	}
	if !reflect.DeepEqual(p.rocketOn, []int{7}) {
		t.Errorf("rocket on issues %v, want [7]", p.rocketOn)
	}
	instructions := filepath.Join(h.env.StateDir, "runs", freshSession, "instructions.md")
	wantSpec := workplace.LaunchSpec{
		Repo: applyRepo, Issue: 7, SessionID: freshSession, Mode: "start", Profile: "writing",
		Prompt: "/lighthouse-review-spec /forge/issue-7-x/spec.md (issue #7, controller-run)", Native: true,
		InstructionsFile: instructions, MCPConfig: "/etc/lighthouse/writing.json",
	}
	if !reflect.DeepEqual(h.wp.spawned, []workplace.LaunchSpec{wantSpec}) {
		t.Errorf("spawned %+v, want %+v", h.wp.spawned, wantSpec)
	}
	if _, err := os.Stat(instructions); err != nil {
		t.Errorf("instructions file: %v", err)
	}
}

func TestApplyStartWithoutTriggerCommentPostsNoRocket(t *testing.T) {
	h := newHarness(t)
	a := startAction()
	a.CommentID, a.TriggerKey, a.Skip = 0, "start:7", ""
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []string{"Prepare", "Sessions ws-issue-7-x", "CreateComment", "Prepare", "Spawn"}
	if got := h.log.all(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	if body := h.poster(applyRepo).bodies[0]; strings.Contains(body, "skip:") {
		t.Errorf("body has a skip line without Skip:\n%s", body)
	}
}

// A handoff written in another checkout names its plan there (#237): the run launches with the
// same plan in its own worktree, and the Started and the run row say so.
func TestApplyStartRelocatesAPlanFromAnotherCheckout(t *testing.T) {
	h := newHarness(t)
	h.wp.root = t.TempDir()
	plan := h.wp.root + "/forge/issue-7-x/design/plans/p.md"
	writeEmpty(t, plan)
	a := startAction()
	a.Command = "/lighthouse-review-plan /home/u/repo/.claude/worktrees/issue-7-x/design/plans/p.md"
	a.Prompt = a.Command + " (issue #7, controller-run)"
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	wantPrompt := "/lighthouse-review-plan " + plan + " (issue #7, controller-run)"
	if len(h.wp.spawned) != 1 || h.wp.spawned[0].Prompt != wantPrompt {
		t.Fatalf("spawned %+v, want one with prompt %q", h.wp.spawned, wantPrompt)
	}
	if body := h.poster(applyRepo).bodies[0]; !strings.Contains(body, "command: /lighthouse-review-plan "+plan+"\n") {
		t.Errorf("Started does not name the relocated plan:\n%s", body)
	}
	if rows := h.history(t); len(rows) != 1 || rows[0].Command != "/lighthouse-review-plan "+plan {
		t.Errorf("rows = %+v, want one with the relocated command", rows)
	}
}

// A plan the run's worktree does not hold closes the run as start_failed within the tick, with
// the path in the reason, and launches nothing.
func TestApplyStartPlanNotInTheWorktreeFails(t *testing.T) {
	h := newHarness(t)
	h.wp.root = t.TempDir()
	a := startAction()
	a.Command = "/lighthouse-review-plan /home/u/repo/design/plans/p.md"
	a.Prompt = a.Command + " (issue #7, controller-run)"
	if _, err := Apply(context.Background(), h.env, []Action{a}); err == nil {
		t.Fatal("Apply: want an error")
	}
	if len(h.wp.spawned) != 0 {
		t.Errorf("spawned %+v, want none", h.wp.spawned)
	}
	rows := h.history(t)
	if len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.StartFailed ||
		!strings.HasPrefix(rows[0].Reason, "handoff path: /home/u/repo/design/plans/p.md ") {
		t.Errorf("rows = %+v, want one closed start_failed with reason handoff path: /home/u/repo/design/plans/p.md …", rows)
	}
}

func TestApplyPostFailsReleases(t *testing.T) {
	h := newHarness(t)
	h.poster(applyRepo).postErr = errors.New("502 bad gateway")
	res, err := Apply(context.Background(), h.env, []Action{startAction()})
	if err == nil {
		t.Fatal("Apply: want an error")
	}
	want := []string{"Prepare", "Sessions ws-issue-7-x", "CreateComment"}
	if got := h.log.all(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	rows := h.history(t)
	if len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.StartFailed || rows[0].Reason != "post: 502 bad gateway" {
		t.Errorf("rows = %+v, want one closed start_failed with reason %q", rows, "post: 502 bad gateway")
	}
	if res.Started != 0 {
		t.Errorf("Started = %d, want 0", res.Started)
	}
}

// A workspace with own work origin lacks never starts on its old HEAD: the run is claimed,
// closed as returned and handed back with the workspace and the number of own commits, so
// its trigger is spent and no later tick starts it again.
func TestApplyOwnWorkReturns(t *testing.T) {
	h := newHarness(t)
	h.wp.prepareErr = fmt.Errorf("follow remote: %w", &workplace.OwnWorkError{
		Workspace: workplace.Workspace{ID: "9c5c2b84ce761720", Path: "/forge/issue-7-x", Branch: "issue-7-x"},
		Commits:   7,
	})
	res, err := Apply(context.Background(), h.env, []Action{startAction()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := append([]string{"Prepare", "Sessions 9c5c2b84ce761720", "CreateComment"}, giveBackCalls...)
	if got := h.log.all(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	reason := "werkplek heeft eigen werk dat niet op origin staat: workspace 9c5c2b84ce761720: 7 commits not on origin/issue-7-x"
	body := "🤖 Teruggegeven\nreason: " + reason + "\nsession: " + freshSession + "\n\n<sub>session " + freshSession + "</sub>"
	if p := h.poster(applyRepo); !reflect.DeepEqual(p.bodies, []string{body}) {
		t.Errorf("posted %q, want %q", p.bodies, body)
	}
	rows := h.history(t)
	if len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.Returned || rows[0].Reason != reason ||
		rows[0].TriggerKey != "handoff:55" {
		t.Errorf("rows = %+v, want one closed returned run of handoff:55 with reason %q", rows, reason)
	}
	if res.Started != 0 || len(h.wp.spawned) != 0 {
		t.Errorf("Started = %d, spawned = %d; want no start", res.Started, len(h.wp.spawned))
	}
}

// A failed post of the hand-back closes the run as start_failed, so retry tries once more.
func TestApplyOwnWorkPostFailsStartFailed(t *testing.T) {
	h := newHarness(t)
	h.wp.prepareErr = &workplace.OwnWorkError{
		Workspace: workplace.Workspace{ID: "ws-issue-7-x", Path: "/forge/issue-7-x", Branch: "issue-7-x"}, Dirty: true,
	}
	h.poster(applyRepo).postErr = errors.New("502 bad gateway")
	if _, err := Apply(context.Background(), h.env, []Action{startAction()}); err == nil {
		t.Fatal("Apply: want an error")
	}
	rows := h.history(t)
	if len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.StartFailed || rows[0].Reason != "post: 502 bad gateway" {
		t.Errorf("rows = %+v, want one closed start_failed with reason %q", rows, "post: 502 bad gateway")
	}
}

func TestApplyInstructionsFailReleases(t *testing.T) {
	h := newHarness(t)
	file := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	h.env.StateDir = file
	_, err := Apply(context.Background(), h.env, []Action{startAction()})
	if err == nil {
		t.Fatal("Apply: want an error")
	}
	want := []string{"Prepare", "Sessions ws-issue-7-x"}
	if got := h.log.all(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	rows := h.history(t)
	if len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.StartFailed ||
		!strings.HasPrefix(rows[0].Reason, "instructions: ") || !strings.Contains(rows[0].Reason, "not a directory") {
		t.Errorf("rows = %+v, want one closed start_failed with reason instructions: …not a directory", rows)
	}
}

func TestApplyFailedPerRepo(t *testing.T) {
	h := newHarness(t)
	h.poster("a/r").postErr = errors.New("403")
	ret := func(repo string) Action {
		return Action{Kind: KindReturn, Repo: repo, Issue: 3, Reason: "reason_state_lost", Language: "en"}
	}
	res, err := Apply(context.Background(), h.env, []Action{ret("a/r"), ret("b/r")})
	if err == nil {
		t.Fatal("Apply: want an error")
	}
	if len(res.Failed) != 1 || res.Failed["a/r"] == nil || !strings.Contains(res.Failed["a/r"].Error(), "403") {
		t.Errorf("Failed = %v, want only a/r with 403", res.Failed)
	}
	if got := h.poster("b/r").bodies; !reflect.DeepEqual(got, []string{"🤖 Returned\nreason: run state lost"}) {
		t.Errorf("b/r posted %q", got)
	}
}

func TestApplyRocketFailsStillLaunches(t *testing.T) {
	h := newHarness(t)
	h.poster(applyRepo).rocketErr = errors.New("rocket refused")
	res, err := Apply(context.Background(), h.env, []Action{startAction()})
	if err != nil {
		t.Fatalf("Apply: %v, want no error for a failed rocket", err)
	}
	if len(res.Failed) != 0 {
		t.Errorf("Failed = %v, want none", res.Failed)
	}
	want := []string{"Prepare", "Sessions ws-issue-7-x", "CreateComment", "Acknowledge", "Prepare", "Spawn"}
	if got := h.log.all(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	if res.Started != 1 {
		t.Errorf("Started = %d, want 1", res.Started)
	}
	if !strings.Contains(h.out.String(), "rocket refused") {
		t.Errorf("log %q does not name the rocket error", h.out.String())
	}
}

func TestApplyBusyDefers(t *testing.T) {
	h := newHarness(t)
	h.wp.sessions = []workplace.Session{{Key: "other", WorkspaceID: "ws-issue-7-x", State: "working", SessionID: "x"}}
	res, err := Apply(context.Background(), h.env, []Action{startAction()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []string{"o/r#7: workplace busy: session other, working since unknown"}
	if !reflect.DeepEqual(res.Deferred, want) {
		t.Errorf("Deferred = %q, want %q", res.Deferred, want)
	}
	if rows := h.history(t); len(rows) != 0 {
		t.Errorf("rows = %+v, want none", rows)
	}
	if got := h.log.all(); !reflect.DeepEqual(got, []string{"Prepare", "Sessions ws-issue-7-x"}) {
		t.Errorf("calls = %q, want only Prepare and Sessions", got)
	}
}

func TestApplyOwnClosedSessionIsNotBusy(t *testing.T) {
	h := newHarness(t)
	id := h.seed(t, 7, oldSession, "start:7", "")
	if err := runs.SetSession(context.Background(), h.db, id, "key-old"); err != nil {
		t.Fatal(err)
	}
	if err := runs.Release(context.Background(), h.db, id, runs.Question, "", applyNow); err != nil {
		t.Fatal(err)
	}
	h.wp.sessions = []workplace.Session{{Key: "key-old", State: "working", SessionID: oldSession}}
	res, err := Apply(context.Background(), h.env, []Action{resumeAction()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Deferred) != 0 || res.Started != 1 {
		t.Errorf("Deferred = %q, Started = %d; want no deferral and one start", res.Deferred, res.Started)
	}
}

func TestApplyClaimLostPostsNothing(t *testing.T) {
	cases := []struct {
		name string
		seed func(t *testing.T, h *harness)
	}{
		{"issue has an open run", func(t *testing.T, h *harness) { h.seed(t, 7, oldSession, "start:7", "") }},
		{"trigger already spent", func(t *testing.T, h *harness) { h.seed(t, 7, oldSession, "handoff:55", runs.Handoff) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			tc.seed(t, h)
			res, err := Apply(context.Background(), h.env, []Action{startAction()})
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if got := h.log.all(); !reflect.DeepEqual(got, []string{"Prepare", "Sessions ws-issue-7-x"}) {
				t.Errorf("calls = %q, want only Prepare and Sessions", got)
			}
			if res.Started != 0 || len(res.Failed) != 0 {
				t.Errorf("Started = %d, Failed = %v; want 0 and none", res.Started, res.Failed)
			}
		})
	}
}

// Two processes (two database handles on one file) apply the same resume at once: exactly one
// 🤖 Resumed and one spawn between them.
func TestApplyTwoProcessesOneTrigger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l.db")
	first := newHarnessOn(t, openApplyDB(t, path))
	second := newHarnessOn(t, openApplyDB(t, path))
	first.seed(t, 7, oldSession, "start:7", runs.Question)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, h := range []*harness{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = Apply(context.Background(), h.env, []Action{resumeAction()})
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Apply %d: %v", i, err)
		}
	}
	posts := len(first.poster(applyRepo).bodies) + len(second.poster(applyRepo).bodies)
	spawns := len(first.wp.spawned) + len(second.wp.spawned)
	if posts != 1 || spawns != 1 {
		t.Errorf("posts = %d, spawns = %d; want 1 and 1", posts, spawns)
	}
}

func TestApplyResumeKeepsSession(t *testing.T) {
	h := newHarness(t)
	h.seed(t, 7, oldSession, "start:7", runs.Question)
	res, err := Apply(context.Background(), h.env, []Action{resumeAction()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	rows := h.history(t)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want two", rows)
	}
	if r := rows[1]; !r.Open || r.Mode != "resume" || r.SessionID != oldSession || r.TriggerKey != "answer:88" {
		t.Errorf("resume row = %+v", r)
	}
	wantBody := "🤖 Hervat\nsession: " + oldSession + "\nanswer: 88\n\n<sub>session " + oldSession + "</sub>"
	if got := h.poster(applyRepo).bodies; !reflect.DeepEqual(got, []string{wantBody}) {
		t.Errorf("posted %q, want %q", got, wantBody)
	}
	if len(h.wp.spawned) != 1 || h.wp.spawned[0].SessionID != oldSession || h.wp.spawned[0].Mode != "resume" ||
		h.wp.spawned[0].Prompt != "the answer" {
		t.Errorf("spawned %+v, want one resume of %s with the answer", h.wp.spawned, oldSession)
	}
	if res.Started != 1 {
		t.Errorf("Started = %d, want 1", res.Started)
	}
}

func TestApplyNotAcceptedPostsReturned(t *testing.T) {
	h := newHarness(t)
	h.wp.spawnErr = fmt.Errorf("kenn-forge answered 409: %w", workplace.ErrNotAccepted)
	res, err := Apply(context.Background(), h.env, []Action{startAction()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Started != 0 {
		t.Errorf("Started = %d, want 0", res.Started)
	}
	if want := []string{applyRepo + "#7"}; !reflect.DeepEqual(res.NotAccepted, want) {
		t.Errorf("NotAccepted = %q, want %q", res.NotAccepted, want)
	}
	bodies := h.poster(applyRepo).bodies
	if len(bodies) != 2 {
		t.Fatalf("posted %q, want the Started and a Returned", bodies)
	}
	session := h.wp.spawned[0].SessionID
	head := "🤖 " + protocol.Heading("kind_returned", "nl") + "\nreason: " +
		protocol.Heading("reason_start_failed", "nl") + ": kenn-forge answered 409: workplace: spawn not accepted\nsession: " + session
	if !strings.HasPrefix(bodies[1], head) || !strings.HasSuffix(bodies[1], protocol.Footer(session)) {
		t.Errorf("Returned = %q, want %q... ending in the footer of %s", bodies[1], head, session)
	}
	if !strings.Contains(h.out.String(), "not accepted") {
		t.Errorf("log %q does not report the refused launch", h.out.String())
	}
}

func TestApplyNotAcceptedReturnPostFails(t *testing.T) {
	h := newHarness(t)
	h.wp.spawnErr = workplace.ErrNotAccepted
	p := h.poster(applyRepo)
	posts := 0
	p.onPost = func() {
		if posts++; posts == 2 {
			p.postErr = errors.New("forge down")
		}
	}
	res, err := Apply(context.Background(), h.env, []Action{startAction()})
	if err != nil || res.Failed[applyRepo] != nil {
		t.Errorf("err = %v, Failed = %v; want no action error: the run is closed", err, res.Failed)
	}
	if want := []string{applyRepo + "#7"}; !reflect.DeepEqual(res.NotAccepted, want) {
		t.Errorf("NotAccepted = %q, want %q", res.NotAccepted, want)
	}
	if !strings.Contains(h.out.String(), "post Returned of run 1: forge down") {
		t.Errorf("log %q does not report the failed post", h.out.String())
	}
}

func TestApplyUncertainPostsNoReturn(t *testing.T) {
	h := newHarness(t)
	h.wp.spawnErr = workplace.ErrUncertain
	if _, err := Apply(context.Background(), h.env, []Action{startAction()}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if bodies := h.poster(applyRepo).bodies; len(bodies) != 1 {
		t.Errorf("posted %q, want only the Started: an agent may be running", bodies)
	}
}

func TestApplyNotAcceptedLogsReason(t *testing.T) {
	h := newHarness(t)
	h.wp.spawnErr = fmt.Errorf("kenn-forge answered 409: session\nnot live: %w", workplace.ErrNotAccepted)
	if _, err := Apply(context.Background(), h.env, []Action{startAction()}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := applyRepo + "#7: launch of run 1 not accepted: kenn-forge answered 409: session; not live: workplace: spawn not accepted"
	if !strings.Contains(h.out.String(), want) {
		t.Errorf("log %q, want %q", h.out.String(), want)
	}
	if n := strings.Count(h.out.String(), "\n"); n != 1 {
		t.Errorf("log %q has %d lines, want 1", h.out.String(), n)
	}
}

func TestApplyUncertainLogsReason(t *testing.T) {
	h := newHarness(t)
	h.wp.spawnErr = fmt.Errorf("sessions: timeout\nretry: %w", workplace.ErrUncertain)
	if _, err := Apply(context.Background(), h.env, []Action{startAction()}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := "launch of run 1 uncertain: spawn run 1: sessions: timeout; retry: "
	if !strings.Contains(h.out.String(), want) {
		t.Errorf("log %q, want %q", h.out.String(), want)
	}
	if n := strings.Count(h.out.String(), "\n"); n != 1 {
		t.Errorf("log %q has %d lines, want 1", h.out.String(), n)
	}
}

func TestApplyUncertainCountsAsStarted(t *testing.T) {
	h := newHarness(t)
	h.wp.spawnErr = workplace.ErrUncertain
	res, err := Apply(context.Background(), h.env, []Action{startAction()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Started != 1 {
		t.Errorf("Started = %d, want 1", res.Started)
	}
}

func TestApplyReturnTranslates(t *testing.T) {
	cases := []struct {
		name string
		a    Action
		want string
	}{
		{
			"nl with argument",
			Action{Kind: KindReturn, Repo: applyRepo, Issue: 7, Reason: "reason_not_allowed", ReasonArg: "/x", Language: "nl"},
			"🤖 Teruggegeven\nreason: commando niet toegestaan: /x",
		},
		{
			"en without argument, with session",
			Action{Kind: KindReturn, Repo: applyRepo, Issue: 7, Reason: "reason_needs_brainstorm", SessionID: oldSession, Language: "en"},
			"🤖 Returned\nreason: needs a brainstorm\nsession: " + oldSession + "\n\n<sub>session " + oldSession + "</sub>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			if _, err := Apply(context.Background(), h.env, []Action{tc.a}); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			p := h.poster(applyRepo)
			if !reflect.DeepEqual(p.bodies, []string{tc.want}) || !reflect.DeepEqual(p.issues, []int{7}) {
				t.Errorf("posted %q on %v, want %q on [7]", p.bodies, p.issues, tc.want)
			}
		})
	}
}

func TestApplyDonePostsThenCloses(t *testing.T) {
	done := func(id int64) Action {
		return Action{Kind: KindDone, Repo: applyRepo, Issue: 7, RunID: id, SessionID: oldSession,
			PRURL: "https://github.com/o/r/pull/9", Language: "nl"}
	}
	t.Run("posted", func(t *testing.T) {
		h := newHarness(t)
		id := h.seed(t, 7, oldSession, "start:7", "")
		if _, err := Apply(context.Background(), h.env, []Action{done(id)}); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		want := "🤖 Klaar\nsession: " + oldSession + "\npr: https://github.com/o/r/pull/9\n\n" +
			"Checks groen, klaar voor review. Mergen blijft bij jou; na de merge toetst de controller het issue opnieuw als het nog open staat." +
			"\n\n<sub>session " + oldSession + "</sub>"
		if got := h.poster(applyRepo).bodies; !reflect.DeepEqual(got, []string{want}) {
			t.Errorf("posted %q, want %q", got, want)
		}
		if rows := h.history(t); rows[0].Open || rows[0].Outcome != runs.Done {
			t.Errorf("row = %+v, want closed as done", rows[0])
		}
	})
	t.Run("post fails", func(t *testing.T) {
		h := newHarness(t)
		id := h.seed(t, 7, oldSession, "start:7", "")
		h.poster(applyRepo).postErr = errors.New("502")
		if _, err := Apply(context.Background(), h.env, []Action{done(id)}); err == nil {
			t.Fatal("Apply: want an error")
		}
		if rows := h.history(t); !rows[0].Open {
			t.Errorf("row = %+v, want still open", rows[0])
		}
	})
}

func TestApplyClose(t *testing.T) {
	h := newHarness(t)
	open := h.seed(t, 7, oldSession, "start:7", "")
	closed := h.seed(t, 8, freshSession, "start:8", runs.Handoff)
	actions := []Action{
		{Kind: KindClose, Repo: applyRepo, Issue: 7, RunID: open, Outcome: runs.Question},
		{Kind: KindClose, Repo: applyRepo, Issue: 8, RunID: closed, Outcome: runs.Done},
	}
	if _, err := Apply(context.Background(), h.env, actions); err != nil {
		t.Fatalf("Apply: %v, want a closed run to be no error", err)
	}
	rows := h.history(t)
	if rows[0].Open || rows[0].Outcome != runs.Question {
		t.Errorf("row 7 = %+v, want closed as question", rows[0])
	}
	if rows[1].Outcome != runs.Handoff {
		t.Errorf("row 8 = %+v, want its handoff outcome kept", rows[1])
	}
	if got := h.log.all(); len(got) != 0 {
		t.Errorf("calls = %q, want none", got)
	}
}

func TestApplyRocketAndDefer(t *testing.T) {
	h := newHarness(t)
	actions := []Action{
		{Kind: KindRocket, Repo: applyRepo, Issue: 7, CommentID: 41},
		{Kind: KindDefer, Repo: applyRepo, Issue: 9, Reason: "night"},
	}
	res, err := Apply(context.Background(), h.env, actions)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := h.poster(applyRepo).rockets; !reflect.DeepEqual(got, []int64{41}) {
		t.Errorf("rockets = %v, want [41]", got)
	}
	if got := h.poster(applyRepo).rocketOn; !reflect.DeepEqual(got, []int{7}) {
		t.Errorf("rocket on issues %v, want [7]", got)
	}
	if !reflect.DeepEqual(res.Deferred, []string{"o/r#9: night"}) {
		t.Errorf("Deferred = %q, want [o/r#9: night]", res.Deferred)
	}
}

func TestApplyOneFailureDoesNotStopOthers(t *testing.T) {
	h := newHarness(t)
	id := h.seed(t, 7, oldSession, "start:7", "")
	h.poster(applyRepo).rocketErr = errors.New("rocket refused")
	actions := []Action{
		{Kind: KindRocket, Repo: applyRepo, Issue: 6, CommentID: 41},
		{Kind: KindClose, Repo: applyRepo, Issue: 7, RunID: id, Outcome: runs.Handoff},
		{Kind: KindDefer, Repo: applyRepo, Issue: 9, Reason: "night"},
	}
	res, err := Apply(context.Background(), h.env, actions)
	if err == nil || !strings.Contains(err.Error(), "rocket refused") {
		t.Fatalf("Apply: %v, want the rocket error", err)
	}
	if rows := h.history(t); rows[0].Open || rows[0].Outcome != runs.Handoff {
		t.Errorf("row = %+v, want closed as handoff after the failed rocket", rows[0])
	}
	if !reflect.DeepEqual(res.Deferred, []string{"o/r#9: night"}) {
		t.Errorf("Deferred = %q", res.Deferred)
	}
	if res.Failed[applyRepo] == nil {
		t.Errorf("Failed = %v, want %s", res.Failed, applyRepo)
	}
}

func TestApplyOpenRunSessionIsBusy(t *testing.T) {
	h := newHarness(t)
	h.seed(t, 7, oldSession, "start:7", "")
	quiet := applyNow.Add(-time.Hour)
	h.wp.sessions = []workplace.Session{{Key: "k", State: "idle", SessionID: oldSession, UpdatedAt: quiet}}
	res, err := Apply(context.Background(), h.env, []Action{startAction()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []string{"o/r#7: workplace busy: session k, idle since " + quiet.Local().Format("15:04")}
	if !reflect.DeepEqual(res.Deferred, want) {
		t.Errorf("Deferred = %q, want %q", res.Deferred, want)
	}
}

// closeOpenRun closes the open run of issue 7 behind apply's back, as another process would.
func (h *harness) closeOpenRun(t *testing.T) {
	t.Helper()
	for _, r := range h.history(t) {
		if r.Open && r.Issue == 7 {
			if err := runs.Release(context.Background(), h.db, r.ID, runs.Returned, "elsewhere", applyNow); err != nil {
				t.Errorf("closeOpenRun: %v", err)
			}
		}
	}
}

func TestApplyNotAcceptedReleaseFailsIsFailed(t *testing.T) {
	h := newHarness(t)
	h.wp.spawnErr = workplace.ErrNotAccepted
	h.wp.onSpawn = func() { h.closeOpenRun(t) }
	res, err := Apply(context.Background(), h.env, []Action{startAction()})
	if err == nil || res.Failed[applyRepo] == nil || !errors.Is(res.Failed[applyRepo], runs.ErrNotOpen) {
		t.Errorf("err = %v, Failed = %v; want the failed release of run in Failed[%s]", err, res.Failed, applyRepo)
	}
	if res.Started != 0 {
		t.Errorf("Started = %d, want 0", res.Started)
	}
	if bodies := h.poster(applyRepo).bodies; len(bodies) != 1 {
		t.Errorf("posted %q, want only the Started: the run is still open", bodies)
	}
}

func TestApplyNotAcceptedLogsNoNilError(t *testing.T) {
	h := newHarness(t)
	h.wp.spawnErr = workplace.ErrNotAccepted
	if _, err := Apply(context.Background(), h.env, []Action{startAction()}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if strings.Contains(h.out.String(), "<nil>") {
		t.Errorf("log %q prints a nil error", h.out.String())
	}
}

func TestApplyAcceptedSessionNotRecordedIsFailed(t *testing.T) {
	h := newHarness(t)
	h.wp.onSpawn = func() { h.closeOpenRun(t) }
	res, err := Apply(context.Background(), h.env, []Action{startAction()})
	if err == nil || res.Failed[applyRepo] == nil || !errors.Is(res.Failed[applyRepo], runs.ErrNotOpen) {
		t.Errorf("err = %v, Failed = %v; want the failed SetSession in Failed[%s]", err, res.Failed, applyRepo)
	}
	if res.Started != 1 {
		t.Errorf("Started = %d, want 1: the agent was accepted", res.Started)
	}
}

func TestApplyPostCancelledStillReleases(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.poster(applyRepo).onPost = cancel
	h.poster(applyRepo).postErr = context.Canceled
	if _, err := Apply(ctx, h.env, []Action{startAction()}); err == nil {
		t.Fatal("Apply: want an error")
	}
	rows := h.history(t)
	if len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.StartFailed || rows[0].Reason != "post: context canceled" {
		t.Errorf("rows = %+v, want one closed start_failed with reason %q", rows, "post: context canceled")
	}
}

// seedWorkplace claims an open run on workspace ws; session "" leaves it without a workplace
// session, as an uncertain start is.
func (h *harness) seedWorkplace(t *testing.T, ws, session string) int64 {
	t.Helper()
	id := h.seed(t, 7, oldSession, "start:7", "")
	if err := runs.SetWorkplace(context.Background(), h.db, id, ws, "issue-7-x"); err != nil {
		t.Fatalf("SetWorkplace: %v", err)
	}
	if session != "" {
		if err := runs.SetSession(context.Background(), h.db, id, session); err != nil {
			t.Fatalf("SetSession: %v", err)
		}
	}
	return id
}

func returnRun(id int64) Action {
	return Action{Kind: KindReturn, Repo: applyRepo, Issue: 7, RunID: id, Reason: "reason_silent",
		SessionID: oldSession, Language: "nl"}
}

func TestApplyReturnRunPostsThenStopsThenCloses(t *testing.T) {
	h := newHarness(t)
	id := h.seedWorkplace(t, "ws-1", "key-1")
	a := returnRun(id)
	a.Stop = true
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, want := h.log.all(), append(append([]string{"CreateComment"}, giveBackCalls...), "Stop"); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
	if want := []string{"ws-1 " + oldSession}; !reflect.DeepEqual(h.wp.stopped, want) {
		t.Errorf("stopped = %q, want %q", h.wp.stopped, want)
	}
	rows := h.history(t)
	if rows[0].Open || rows[0].Outcome != runs.Returned || rows[0].Reason != "geen activiteit in max_wait" {
		t.Errorf("row = %+v, want closed as returned with the reason text", rows[0])
	}
}

func TestApplyReturnRunStopFailsKeepsRowOpen(t *testing.T) {
	h := newHarness(t)
	id := h.seedWorkplace(t, "ws-1", "key-1")
	h.wp.stopErr = errors.New("forge down")
	a := returnRun(id)
	a.Stop = true
	res, err := Apply(context.Background(), h.env, []Action{a})
	if err == nil || res.Failed[applyRepo] == nil {
		t.Fatalf("Apply = %v, Failed = %v, want an error", err, res.Failed)
	}
	if rows := h.history(t); !rows[0].Open {
		t.Errorf("row = %+v, want still open", rows[0])
	}
	want := "🤖 Teruggegeven\nreason: geen activiteit in max_wait\nsession: " + oldSession + "\n\n<sub>session " + oldSession + "</sub>"
	if got := h.poster(applyRepo).bodies; !reflect.DeepEqual(got, []string{want}) {
		t.Errorf("posted %q, want %q", got, want)
	}
}

func TestApplyReturnRunDiscard(t *testing.T) {
	h := newHarness(t)
	id := h.seedWorkplace(t, "ws-1", "")
	a := returnRun(id)
	a.Discard = true
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, want := h.log.all(), append(append([]string{"CreateComment"}, giveBackCalls...), "Discard"); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
	if want := []string{"ws-1"}; !reflect.DeepEqual(h.wp.forgot, want) {
		t.Errorf("discarded = %q, want %q", h.wp.forgot, want)
	}
	if rows := h.history(t); rows[0].Open || rows[0].Outcome != runs.Returned {
		t.Errorf("row = %+v, want closed as returned", rows[0])
	}
}

func TestApplyCloseDiscard(t *testing.T) {
	h := newHarness(t)
	id := h.seedWorkplace(t, "ws-1", "")
	a := Action{Kind: KindClose, Repo: applyRepo, Issue: 7, RunID: id, Outcome: runs.Returned,
		Reason: "reason_silent", Discard: true, Language: "nl"}
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, want := h.log.all(), []string{"Discard"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want %q (no post)", got, want)
	}
	if rows := h.history(t); rows[0].Open || rows[0].Outcome != runs.Returned {
		t.Errorf("row = %+v, want closed as returned", rows[0])
	}
}

func TestApplyCloseStop(t *testing.T) {
	h := newHarness(t)
	id := h.seedWorkplace(t, "ws-1", "key-1")
	a := Action{Kind: KindClose, Repo: applyRepo, Issue: 7, RunID: id, Outcome: runs.Returned,
		Reason: "reason_silent", Stop: true, Language: "nl"}
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, want := h.log.all(), []string{"Stop"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want %q (no post)", got, want)
	}
	if want := []string{"ws-1 " + oldSession}; !reflect.DeepEqual(h.wp.stopped, want) {
		t.Errorf("stopped = %q, want %q (the claude session id, not the forge key)", h.wp.stopped, want)
	}
	if rows := h.history(t); rows[0].Open || rows[0].Outcome != runs.Returned {
		t.Errorf("row = %+v, want closed as returned", rows[0])
	}
}

func TestApplyCloseStartFailedKeepsCause(t *testing.T) {
	h := newHarness(t)
	id := h.seedWorkplace(t, "ws-1", "key-1")
	a := Action{Kind: KindClose, Repo: applyRepo, Issue: 7, RunID: id, Outcome: runs.StartFailed,
		ReasonArg: "post: 502", Stop: true, Language: "nl"}
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := []string{"ws-1 " + oldSession}; !reflect.DeepEqual(h.wp.stopped, want) {
		t.Errorf("stopped = %q, want %q", h.wp.stopped, want)
	}
	rows := h.history(t)
	if rows[0].Outcome != runs.StartFailed || rows[0].Reason != "post: 502" {
		t.Errorf("row = %+v, want start_failed with reason %q", rows[0], "post: 502")
	}
}

func TestApplyStopWithoutSessionIsSkipped(t *testing.T) {
	h := newHarness(t)
	id := h.seedWorkplace(t, "ws-1", "") // an unmeasured run: workspace, no session
	a := Action{Kind: KindClose, Repo: applyRepo, Issue: 7, RunID: id, Outcome: runs.Returned,
		Reason: "reason_silent", Stop: true, Language: "nl"}
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v, want a stop without session to be no error", err)
	}
	if got := h.log.all(); len(got) != 0 {
		t.Errorf("calls = %q, want none", got)
	}
	if rows := h.history(t); rows[0].Open {
		t.Errorf("row = %+v, want closed", rows[0])
	}
}

func TestApplyStopWithoutSessionIDIsSkipped(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	run, err := runs.Take(ctx, h.db, runs.Claim{
		Repo: applyRepo, Issue: 7, Mode: "start", Trigger: "handoff", TriggerKey: "start:7",
		Command: "/lighthouse-start-issue 7", Branch: "issue-7-x", Now: applyNow.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if err := runs.SetWorkplace(ctx, h.db, run.ID, "ws-1", "issue-7-x"); err != nil {
		t.Fatalf("SetWorkplace: %v", err)
	}
	if err := runs.SetSession(ctx, h.db, run.ID, "key-1"); err != nil {
		t.Fatalf("SetSession: %v", err)
	}
	a := Action{Kind: KindClose, Repo: applyRepo, Issue: 7, RunID: run.ID, Outcome: runs.Returned,
		Reason: "reason_silent", Stop: true, Language: "nl"}
	if _, err := Apply(ctx, h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if h.wp.stopped != nil {
		t.Errorf("stopped = %q, want none: a run without a session id has nothing Stop can find", h.wp.stopped)
	}
}

func TestApplyCloseQuestionEndsOnlyTheClaude(t *testing.T) {
	h := newHarness(t)
	id := h.seedWorkplace(t, "ws-1", "key-1")
	a := Action{Kind: KindClose, Repo: applyRepo, Issue: 7, RunID: id, Outcome: runs.Question, End: true, Language: "nl"}
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, want := h.log.all(), []string{"Kill"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want %q (no Stop of the unit, no post)", got, want)
	}
	if want := []string{"ws-1 " + oldSession}; !reflect.DeepEqual(h.wp.killed, want) {
		t.Errorf("killed = %q, want %q (the claude session id, not the forge key)", h.wp.killed, want)
	}
	if rows := h.history(t); rows[0].Open || rows[0].Outcome != runs.Question {
		t.Errorf("row = %+v, want closed as question", rows[0])
	}
}

// A run whose launch was never adopted can still have a claude: Kill finds it by session id.
func TestApplyEndWithoutForgeKeyStillKills(t *testing.T) {
	h := newHarness(t)
	id := h.seedWorkplace(t, "ws-1", "")
	a := Action{Kind: KindClose, Repo: applyRepo, Issue: 7, RunID: id, Outcome: runs.Handoff, End: true, Language: "nl"}
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := []string{"ws-1 " + oldSession}; !reflect.DeepEqual(h.wp.killed, want) {
		t.Errorf("killed = %q, want %q", h.wp.killed, want)
	}
}

func TestApplyDonePostsThenEndsThenCloses(t *testing.T) {
	h := newHarness(t)
	id := h.seedWorkplace(t, "ws-1", "key-1")
	a := Action{Kind: KindDone, Repo: applyRepo, Issue: 7, RunID: id, SessionID: oldSession,
		PRURL: "https://github.com/o/r/pull/9", End: true, Language: "nl"}
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, want := h.log.all(), []string{"CreateComment", "Kill"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
	if want := []string{"ws-1 " + oldSession}; !reflect.DeepEqual(h.wp.killed, want) {
		t.Errorf("killed = %q, want %q", h.wp.killed, want)
	}
	if rows := h.history(t); rows[0].Open || rows[0].Outcome != runs.Done {
		t.Errorf("row = %+v, want closed as done", rows[0])
	}
}

// A kill that fails keeps the row open, so the next tick ends the claude before a resume can
// collide with it.
func TestApplyEndFailsKeepsRowOpen(t *testing.T) {
	h := newHarness(t)
	id := h.seedWorkplace(t, "ws-1", "key-1")
	h.wp.killErr = errors.New("podman ps: exit code 125")
	a := Action{Kind: KindClose, Repo: applyRepo, Issue: 7, RunID: id, Outcome: runs.Question, End: true, Language: "nl"}
	res, err := Apply(context.Background(), h.env, []Action{a})
	if err == nil || res.Failed[applyRepo] == nil {
		t.Fatalf("Apply = %v, Failed = %v, want an error", err, res.Failed)
	}
	if rows := h.history(t); !rows[0].Open {
		t.Errorf("row = %+v, want still open", rows[0])
	}
}

// The resume reuses the session of the earlier run: its claude is killed after the claim, so a
// second process that lost the claim never kills the claude the winner just started.
func TestApplyResumeKillsEarlierClaudeAfterTheClaim(t *testing.T) {
	h := newHarness(t)
	h.seed(t, 7, oldSession, "start:7", runs.Question)
	openAtKill := -1
	h.wp.onKill = func() {
		open, err := runs.Open(context.Background(), h.db, applyRepo)
		if err != nil {
			t.Errorf("Open: %v", err)
		}
		openAtKill = len(open)
	}
	if _, err := Apply(context.Background(), h.env, []Action{resumeAction()}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := []string{"ws-issue-7-x " + oldSession}; !reflect.DeepEqual(h.wp.killed, want) {
		t.Errorf("killed = %q, want %q", h.wp.killed, want)
	}
	if openAtKill != 1 {
		t.Errorf("open runs at Kill = %d, want 1: the resume's own claim", openAtKill)
	}
	want := []string{"Prepare", "Sessions ws-issue-7-x", "Kill", "CreateComment", "Acknowledge", "Prepare", "Spawn"}
	if got := h.log.all(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
}

func TestApplyResumeKillFailsClosesStartFailedWithoutSpawn(t *testing.T) {
	h := newHarness(t)
	h.seed(t, 7, oldSession, "start:7", runs.Question)
	h.wp.killErr = errors.New("session still runs in container c1 after pkill")
	if _, err := Apply(context.Background(), h.env, []Action{resumeAction()}); err == nil {
		t.Fatal("Apply: err = nil, want the kill error")
	}
	if len(h.wp.spawned) != 0 || len(h.poster(applyRepo).bodies) != 0 {
		t.Errorf("spawned %d, posted %q; want neither", len(h.wp.spawned), h.poster(applyRepo).bodies)
	}
	rows := h.history(t)
	if r := rows[1]; r.Open || r.Outcome != runs.StartFailed ||
		r.Reason != "end earlier claude: session still runs in container c1 after pkill" {
		t.Errorf("resume row = %+v, want start_failed with the kill as reason", r)
	}
}

// seedReturned seeds a closed run of issue 7 that was handed back with its session left alive
// (rule 16, waiting on a permission), and lists that session in the workspace in forge state
// approval.
func (h *harness) seedReturned(t *testing.T) {
	t.Helper()
	id := h.seed(t, 7, oldSession, "start:7", "")
	if err := runs.SetSession(context.Background(), h.db, id, "key-old"); err != nil {
		t.Fatal(err)
	}
	if err := runs.Release(context.Background(), h.db, id, runs.Returned, "", applyNow.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	h.wp.sessions = []workplace.Session{{Key: "key-old", WorkspaceID: "ws-issue-7-x", State: "approval", SessionID: oldSession}}
}

// A run handed back while waiting on a permission leaves its claude alive, and forge refuses a new
// start in that worktree while it lives (#263): the start kills it after the claim, before the
// 🤖 Started and the launch.
func TestApplyStartKillsClaudeOfReturnedRunAfterTheClaim(t *testing.T) {
	h := newHarness(t)
	h.seedReturned(t)
	openAtKill := -1
	h.wp.onKill = func() {
		open, err := runs.Open(context.Background(), h.db, applyRepo)
		if err != nil {
			t.Errorf("Open: %v", err)
		}
		openAtKill = len(open)
	}
	res, err := Apply(context.Background(), h.env, []Action{startAction()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := []string{"ws-issue-7-x " + oldSession}; !reflect.DeepEqual(h.wp.killed, want) {
		t.Errorf("killed = %q, want %q", h.wp.killed, want)
	}
	if openAtKill != 1 {
		t.Errorf("open runs at Kill = %d, want 1: the start's own claim", openAtKill)
	}
	want := []string{"Prepare", "Sessions ws-issue-7-x", "Kill", "CreateComment", "Acknowledge", "Prepare", "Spawn"}
	if got := h.log.all(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
	if len(res.Deferred) != 0 || res.Started != 1 {
		t.Errorf("Deferred = %q, Started = %d; want no deferral and one start", res.Deferred, res.Started)
	}
}

func TestApplyStartKillOfReturnedRunFailsClosesStartFailedWithoutSpawn(t *testing.T) {
	h := newHarness(t)
	h.seedReturned(t)
	h.wp.killErr = errors.New("session still runs in container c1 after pkill")
	if _, err := Apply(context.Background(), h.env, []Action{startAction()}); err == nil {
		t.Fatal("Apply: err = nil, want the kill error")
	}
	if len(h.wp.spawned) != 0 || len(h.poster(applyRepo).bodies) != 0 {
		t.Errorf("spawned %d, posted %q; want neither", len(h.wp.spawned), h.poster(applyRepo).bodies)
	}
	rows := h.history(t)
	if r := rows[1]; r.Open || r.Outcome != runs.StartFailed ||
		r.Reason != "end earlier claude: session still runs in container c1 after pkill" {
		t.Errorf("start row = %+v, want start_failed with the kill as reason", r)
	}
}

// A start makes a new session: there is no earlier claude of it to kill.
func TestApplyStartKillsNothing(t *testing.T) {
	h := newHarness(t)
	if _, err := Apply(context.Background(), h.env, []Action{startAction()}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if h.wp.killed != nil {
		t.Errorf("killed = %q, want none", h.wp.killed)
	}
}

func parkAction() Action {
	return Action{Kind: KindPark, Repo: applyRepo, Issue: 7, ItemID: "I7", Language: "nl",
		Blockers: []string{"o/r#5 (In progress)"}}
}

// A park posts the 🤖 Geparkeerd, then labels blocked_issue and moves the item to To do; the run
// and its workplace stay. Writing On hold, or skipping the label, turns this red.
func TestApplyParkSetsToDoWithBlockedIssue(t *testing.T) {
	h := newHarness(t)
	id := h.seedWorkplace(t, "ws-1", "key-1")
	if _, err := Apply(context.Background(), h.env, []Action{parkAction()}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, want := h.log.all(), []string{"CreateComment", "Issue", "EditIssue", "SetStatus"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
	p := h.poster(applyRepo)
	if len(p.bodies) != 1 || p.issues[0] != 7 || !strings.HasPrefix(p.bodies[0], "🤖 Geparkeerd\nblocked_by: o/r#5 (In progress)") {
		t.Errorf("posted %q on %v, want one Geparkeerd naming o/r#5 (In progress) on #7", p.bodies, p.issues)
	}
	if want := []string{"I7 To do"}; !reflect.DeepEqual(p.sets, want) {
		t.Errorf("status sets = %q, want %q", p.sets, want)
	}
	if want := []string{"blocked_issue"}; !reflect.DeepEqual(p.labels, want) {
		t.Errorf("labels = %q, want %q", p.labels, want)
	}
	if rows := h.history(t); len(rows) != 1 || rows[0].ID != id || !rows[0].Open {
		t.Errorf("history = %+v, want run %d still open", rows, id)
	}
	if len(h.wp.stopped) != 0 || len(h.wp.forgot) != 0 {
		t.Errorf("stopped %v, discarded %v; want the workplace untouched", h.wp.stopped, h.wp.forgot)
	}
}

// A park whose label edit fails keeps its Geparkeerd and leaves the board as it is: the next
// tick holds without a second comment.
func TestApplyParkLabelFailsNoBoard(t *testing.T) {
	h := newHarness(t)
	h.poster(applyRepo).editErr = errors.New("label refused")
	if _, err := Apply(context.Background(), h.env, []Action{parkAction()}); err == nil || !strings.Contains(err.Error(), "label refused") {
		t.Fatalf("Apply: %v, want the label error", err)
	}
	if p := h.poster(applyRepo); len(p.bodies) != 1 || p.sets != nil {
		t.Errorf("posted %q, board %q; want the Geparkeerd and no board change", p.bodies, p.sets)
	}
}

func TestApplyParkPostFailsNoBoard(t *testing.T) {
	h := newHarness(t)
	h.poster(applyRepo).postErr = errors.New("post refused")
	res, err := Apply(context.Background(), h.env, []Action{parkAction()})
	if err == nil || !strings.Contains(err.Error(), "post refused") {
		t.Fatalf("Apply: %v, want the post error", err)
	}
	if res.Failed[applyRepo] == nil {
		t.Errorf("Failed = %v, want %s", res.Failed, applyRepo)
	}
	if got, want := h.log.all(), []string{"CreateComment"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want %q: no board change without the comment", got, want)
	}
}

func TestApplyParkBoardFails(t *testing.T) {
	h := newHarness(t)
	h.poster(applyRepo).setErr = errors.New("board refused")
	res, err := Apply(context.Background(), h.env, []Action{parkAction()})
	if err == nil || !strings.Contains(err.Error(), "board refused") {
		t.Fatalf("Apply: %v, want the board error", err)
	}
	if res.Failed[applyRepo] == nil {
		t.Errorf("Failed = %v, want %s", res.Failed, applyRepo)
	}
	if p := h.poster(applyRepo); len(p.bodies) != 1 {
		t.Errorf("posted %q, want the Geparkeerd to stand", p.bodies)
	}
}

// A hold is a park without its comment (the own Geparkeerd stands): blocked_issue and To do.
// A label already there is not added again.
func TestApplyHoldSetsToDoWithBlockedIssue(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels []string
		calls  []string
	}{
		{"without label", nil, []string{"Issue", "EditIssue", "SetStatus"}},
		{"label stands", []string{"blocked_issue"}, []string{"Issue", "SetStatus"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			p := h.poster(applyRepo)
			p.labels = tc.labels
			if _, err := Apply(context.Background(), h.env, []Action{{Kind: KindHold, Repo: applyRepo, Issue: 7, ItemID: "I7"}}); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if got := h.log.all(); !reflect.DeepEqual(got, tc.calls) {
				t.Errorf("calls = %q, want %q", got, tc.calls)
			}
			if want := []string{"I7 To do"}; !reflect.DeepEqual(p.sets, want) {
				t.Errorf("status sets = %q, want %q", p.sets, want)
			}
			if want := []string{"blocked_issue"}; !reflect.DeepEqual(p.labels, want) || len(p.bodies) != 0 {
				t.Errorf("labels %q, posts %q; want [blocked_issue] and no comment", p.labels, p.bodies)
			}
		})
	}
}

// An unpark takes blocked_issue off (only when it is there; other labels stay) and moves the
// item to Controller. Dropping the removal, or removing without the label, turns this red.
func TestApplyUnparkRemovesLabelSetsController(t *testing.T) {
	for _, tc := range []struct {
		name         string
		labels, want []string
		calls        []string
	}{
		{"label on", []string{"blocked_issue", "2-loc:mac"}, []string{"2-loc:mac"}, []string{"Issue", "EditIssue", "SetStatus"}},
		{"label already off", []string{"2-loc:mac"}, []string{"2-loc:mac"}, []string{"Issue", "SetStatus"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			p := h.poster(applyRepo)
			p.labels, p.status = tc.labels, statusToDo
			if _, err := Apply(context.Background(), h.env, []Action{{Kind: KindUnpark, Repo: applyRepo, Issue: 7, ItemID: "I7"}}); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if got := h.log.all(); !reflect.DeepEqual(got, tc.calls) {
				t.Errorf("calls = %q, want %q", got, tc.calls)
			}
			if !reflect.DeepEqual(p.labels, tc.want) {
				t.Errorf("labels = %q, want %q", p.labels, tc.want)
			}
			if want := []string{"I7 Controller"}; !reflect.DeepEqual(p.sets, want) {
				t.Errorf("status sets = %q, want %q", p.sets, want)
			}
		})
	}
}

// A label removal that fails leaves the item on To do: the next tick unparks it again.
func TestApplyUnparkLabelFailsKeepsToDo(t *testing.T) {
	h := newHarness(t)
	p := h.poster(applyRepo)
	p.labels, p.editErr = []string{"blocked_issue"}, errors.New("label refused")
	if _, err := Apply(context.Background(), h.env, []Action{{Kind: KindUnpark, Repo: applyRepo, Issue: 7, ItemID: "I7"}}); err == nil {
		t.Fatal("Apply: err = nil, want the label error")
	}
	if p.sets != nil {
		t.Errorf("status sets = %q, want none", p.sets)
	}
}

// The round trip on the fake forge: a park leaves To do with blocked_issue; while the blocker
// is open the next ticks do nothing; once it is closed the tick unparks (label off, Controller);
// the tick after starts the Handoff. No step writes On hold.
func TestParkThenUnparkRoundTrip(t *testing.T) {
	h := newHarness(t)
	p := h.poster(applyRepo)
	p.bodies = []string{"🤖 Handoff\nissue: #7\nworktree: /w\nbranch: issue-7-x\nanchor: abc\nnext: /superpowers:subagent-driven-development /w/plan.md\nwhen: now"}
	blocker := forge.Blocker{Repo: "x/y", Issue: 5, State: "OPEN"}
	decide := func() []Action {
		in := h.tickInput(t)
		in.Repos[0].Snapshot.Items[0].BlockedBy = []forge.Blocker{blocker}
		return Decide(in)
	}
	tick := func() []Kind {
		t.Helper()
		actions := decide()
		if _, err := Apply(context.Background(), h.env, actions); err != nil {
			t.Fatalf("Apply %v: %v", kinds(actions), err)
		}
		return kinds(actions)
	}
	if got := tick(); !reflect.DeepEqual(got, []Kind{KindPark}) {
		t.Fatalf("tick 1 = %v, want park", got)
	}
	if p.status != statusToDo || !reflect.DeepEqual(p.labels, []string{"blocked_issue"}) {
		t.Fatalf("after park: status %q, labels %q; want To do with blocked_issue", p.status, p.labels)
	}
	if got := tick(); len(got) != 0 {
		t.Fatalf("tick 2, blocker open = %v, want nothing", got)
	}
	blocker.MergedPR = true // the blocker in another repo has a merged PR: no longer active
	if got := tick(); !reflect.DeepEqual(got, []Kind{KindUnpark}) {
		t.Fatalf("tick 3 = %v, want unpark", got)
	}
	if p.status != statusController || len(p.labels) != 0 {
		t.Fatalf("after unpark: status %q, labels %q; want Controller without label", p.status, p.labels)
	}
	// Decide only: the start itself is not what this test is about.
	if got := kinds(decide()); !reflect.DeepEqual(got, []Kind{KindStart}) {
		t.Fatalf("tick 4 = %v, want the start of the Handoff", got)
	}
	for _, set := range p.sets {
		if strings.HasSuffix(set, statusOnHold) {
			t.Errorf("status set %q: nothing may write On hold", set)
		}
	}
}

const usageKey = "usage:" + oldSession + "@2026-10-31T08:00:00Z:2026-10-31T08:30:00Z"

func usageResumeAction() Action {
	return Action{
		Kind: KindResume, Repo: applyRepo, Issue: 7, Mode: "resume", Trigger: "usage", TriggerKey: usageKey,
		Attempt: 1, SessionID: oldSession, Command: "/lighthouse-start-issue 7", Prompt: "ga verder",
		Branch: "issue-7-x", Language: "nl", Profile: "writing",
	}
}

// writeMarker writes the resume marker of oldSession into dir with these times.
func writeMarker(t *testing.T, dir, created, reset string) string {
	t.Helper()
	path := filepath.Join(dir, oldSession+".json")
	body := `{"session_id":"` + oldSession + `","cwd":"/forge/issue-7-x","window":"venster","reset_at":"` + reset +
		`","created_at":"` + created + `","source":"tool","controller":true}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

func TestApplyUsageResumePostsNothing(t *testing.T) {
	h := newHarness(t)
	h.seed(t, 7, oldSession, "start:7", runs.Parked)
	res, err := Apply(context.Background(), h.env, []Action{usageResumeAction()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// A parked run's claude may still wait in the workplace: the usage resume kills it too.
	want := []string{"Prepare", "Sessions ws-issue-7-x", "Kill", "Prepare", "Spawn"}
	if got := h.log.all(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	if p := h.poster(applyRepo); len(p.bodies) != 0 || len(p.rockets) != 0 {
		t.Errorf("posted %q, rockets %v; want nothing", p.bodies, p.rockets)
	}
	if len(h.wp.spawned) != 1 || h.wp.spawned[0].Mode != "resume" || h.wp.spawned[0].SessionID != oldSession ||
		h.wp.spawned[0].Prompt != "ga verder" {
		t.Errorf("spawned %+v, want one resume of %s with ga verder", h.wp.spawned, oldSession)
	}
	if res.Started != 1 {
		t.Errorf("Started = %d, want 1", res.Started)
	}
	if rows := h.history(t); len(rows) != 2 || !rows[1].Open || rows[1].TriggerKey != usageKey {
		t.Errorf("rows = %+v, want an open usage resume", rows)
	}
}

// A resume continues the session that made the work in its workspace: own work never hands
// it back (#356), also when origin moved on and HEAD diverged, and the launch runs in the
// workspace the OwnWorkError names.
func TestApplyResumeOwnWorkStarts(t *testing.T) {
	for _, a := range []Action{usageResumeAction(), resumeAction()} {
		h := newHarness(t)
		h.seed(t, 7, oldSession, "start:7", runs.Parked)
		h.wp.prepareErr = &workplace.OwnWorkError{
			Workspace: workplace.Workspace{ID: "9c5c2b84ce761720", Path: "/forge/issue-7-x", Branch: "issue-7-x"},
			Commits:   14, Dirty: true,
		}
		res, err := Apply(context.Background(), h.env, []Action{a})
		if err != nil {
			t.Fatalf("%s: Apply: %v", a.Trigger, err)
		}
		if res.Started != 1 || len(h.wp.spawned) != 1 || h.wp.spawned[0].SessionID != oldSession {
			t.Errorf("%s: Started = %d, spawned %+v; want one resume of %s", a.Trigger, res.Started, h.wp.spawned, oldSession)
		}
		for _, body := range h.poster(applyRepo).bodies {
			if strings.Contains(body, "Teruggegeven") {
				t.Errorf("%s: posted %q, want no Returned", a.Trigger, body)
			}
		}
		rows := h.history(t)
		if len(rows) != 2 || !rows[1].Open {
			t.Fatalf("%s: rows = %+v, want an open resume", a.Trigger, rows)
		}
		var wsID string
		if err := h.db.QueryRow(`SELECT workspace_id FROM runs WHERE id = ?`, rows[1].ID).Scan(&wsID); err != nil {
			t.Fatal(err)
		}
		if wsID != "9c5c2b84ce761720" {
			t.Errorf("%s: workspace = %q, want 9c5c2b84ce761720", a.Trigger, wsID)
		}
	}
}

// The second attempt of a usage resume posts no header, so a Returned after the first refusal
// would stay the last header while its agent runs.
func TestApplyUsageResumeNotAcceptedReturnsOnSecondAttempt(t *testing.T) {
	for _, attempt := range []int{1, 2} {
		h := newHarness(t)
		h.seed(t, 7, oldSession, "start:7", runs.Parked)
		h.wp.spawnErr = workplace.ErrNotAccepted
		a := usageResumeAction()
		a.Attempt = attempt
		if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		bodies := h.poster(applyRepo).bodies
		if want := attempt - 1; len(bodies) != want {
			t.Errorf("attempt %d: posted %q, want %d comments", attempt, bodies, want)
		}
		if attempt == 2 && len(bodies) == 1 && !strings.HasSuffix(bodies[0], protocol.Footer(oldSession)) {
			t.Errorf("attempt 2: Returned = %q, want the footer of %s", bodies[0], oldSession)
		}
	}
}

func TestApplyUsageResumeRemovesMarker(t *testing.T) {
	cases := []struct {
		name     string
		created  string
		reset    string
		spawnErr error
		gone     bool
	}{
		{"accepted, same key", "2026-10-31T08:00:00Z", "2026-10-31T08:30:00Z", nil, true},
		{"uncertain, same key", "2026-10-31T08:00:00Z", "2026-10-31T08:30:00Z", workplace.ErrUncertain, true},
		{"accepted, other created_at", "2026-10-31T08:05:00Z", "2026-10-31T08:30:00Z", nil, false},
		{"accepted, other reset_at", "2026-10-31T08:00:00Z", "2026-10-31T09:30:00Z", nil, false},
		{"not accepted", "2026-10-31T08:00:00Z", "2026-10-31T08:30:00Z", workplace.ErrNotAccepted, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.seed(t, 7, oldSession, "start:7", runs.Parked)
			h.env.MarkerDir = t.TempDir()
			h.wp.spawnErr = tc.spawnErr
			path := writeMarker(t, h.env.MarkerDir, tc.created, tc.reset)
			if _, err := Apply(context.Background(), h.env, []Action{usageResumeAction()}); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if got := !exists(t, path); got != tc.gone {
				t.Errorf("marker gone = %v, want %v", got, tc.gone)
			}
		})
	}
}

// A resume of another trigger leaves the marker of its session alone.
func TestApplyAnswerResumeKeepsMarker(t *testing.T) {
	h := newHarness(t)
	h.seed(t, 7, oldSession, "start:7", runs.Question)
	h.env.MarkerDir = t.TempDir()
	path := writeMarker(t, h.env.MarkerDir, "2026-10-31T08:00:00Z", "2026-10-31T08:30:00Z")
	a := resumeAction()
	a.TriggerKey = usageKey // only the trigger tells a usage resume apart
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !exists(t, path) {
		t.Error("marker removed by an answer resume")
	}
	if len(h.poster(applyRepo).bodies) != 1 {
		t.Errorf("posted %q, want the Resumed", h.poster(applyRepo).bodies)
	}
}

func TestApplyDropMarker(t *testing.T) {
	drop := Action{Kind: KindDropMarker, Repo: applyRepo, Issue: 7, SessionID: oldSession, TriggerKey: usageKey, Language: "nl"}
	t.Run("same key", func(t *testing.T) {
		h := newHarness(t)
		h.env.MarkerDir = t.TempDir()
		path := writeMarker(t, h.env.MarkerDir, "2026-10-31T08:00:00Z", "2026-10-31T08:30:00Z")
		res, err := Apply(context.Background(), h.env, []Action{drop})
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if exists(t, path) {
			t.Error("marker still there")
		}
		if got := h.log.all(); len(got) != 0 {
			t.Errorf("calls = %q, want none", got)
		}
		if res.Started != 0 || len(h.wp.spawned) != 0 {
			t.Errorf("Started = %d, spawned %+v; want no launch", res.Started, h.wp.spawned)
		}
	})
	t.Run("other key", func(t *testing.T) {
		h := newHarness(t)
		h.env.MarkerDir = t.TempDir()
		path := writeMarker(t, h.env.MarkerDir, "2026-10-31T08:05:00Z", "2026-10-31T08:30:00Z")
		if _, err := Apply(context.Background(), h.env, []Action{drop}); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if !exists(t, path) {
			t.Error("marker with another key removed")
		}
	})
	t.Run("no marker dir", func(t *testing.T) {
		h := newHarness(t)
		dir := t.TempDir()
		t.Chdir(dir) // "" must not resolve against the working directory
		path := writeMarker(t, dir, "2026-10-31T08:00:00Z", "2026-10-31T08:30:00Z")
		if _, err := Apply(context.Background(), h.env, []Action{drop}); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if !exists(t, path) {
			t.Error("marker removed without a MarkerDir")
		}
	})
	t.Run("removal fails", func(t *testing.T) {
		h := newHarness(t)
		h.env.MarkerDir = t.TempDir()
		bad := drop
		bad.SessionID = "../x"
		res, err := Apply(context.Background(), h.env, []Action{bad})
		if err != nil || len(res.Failed) != 0 {
			t.Fatalf("Apply: %v, Failed = %v; want a removal error to be no action error", err, res.Failed)
		}
		if lines := strings.Count(h.out.String(), "\n"); lines != 1 || !strings.Contains(h.out.String(), "invalid session id") {
			t.Errorf("log %q, want one line with the removal error", h.out.String())
		}
	})
}

// fixPRAction is the start of /lighthouse-fix-pr that Decide plans after a red PR.
func fixPRAction(attempt int) Action {
	return Action{
		Kind: KindStart, Repo: applyRepo, Issue: 7, Mode: "start", Trigger: "fixpr",
		TriggerKey: "fixpr:12:abc123", Attempt: attempt, Command: "/lighthouse-fix-pr 12",
		Prompt: "/lighthouse-fix-pr 12 (issue #7, controller-run)", Branch: "issue-7-x", Language: "nl",
		Profile: "writing",
	}
}

// fixPRReturned is the 🤖 Returned a first fix-pr start posts when it stops before its claim.
const fixPRReturned = "🤖 Teruggegeven\nreason: PR rood"

// The run that found the red PR closed as handoff: a fix-pr start the busy workplace defers
// hands the issue back, so it does not sit silent on a red PR.
func TestApplyFixPRBusyReturns(t *testing.T) {
	h := newHarness(t)
	h.wp.sessions = []workplace.Session{{Key: "other", WorkspaceID: "ws-issue-7-x", State: "working", SessionID: "x"}}
	res, err := Apply(context.Background(), h.env, []Action{fixPRAction(1)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	p := h.poster(applyRepo)
	if !reflect.DeepEqual(p.bodies, []string{fixPRReturned}) || !reflect.DeepEqual(p.issues, []int{7}) {
		t.Errorf("posted %q on %v, want %q on [7]", p.bodies, p.issues, fixPRReturned)
	}
	want := []string{"o/r#7: workplace busy: session other, working since unknown"}
	if !reflect.DeepEqual(res.Deferred, want) {
		t.Errorf("Deferred = %q, want %q", res.Deferred, want)
	}
	if rows := h.history(t); len(rows) != 0 {
		t.Errorf("rows = %+v, want none", rows)
	}
}

func TestApplyFixPRPrepareFailsReturns(t *testing.T) {
	h := newHarness(t)
	h.wp.prepareErr = errors.New("no forge")
	res, err := Apply(context.Background(), h.env, []Action{fixPRAction(1)})
	if err == nil || !strings.Contains(err.Error(), "prepare: no forge") {
		t.Fatalf("Apply error = %v, want it to name prepare: no forge", err)
	}
	if res.Failed[applyRepo] == nil {
		t.Errorf("Failed = %v, want the prepare error under %s", res.Failed, applyRepo)
	}
	if p := h.poster(applyRepo); !reflect.DeepEqual(p.bodies, []string{fixPRReturned}) {
		t.Errorf("posted %q, want %q", p.bodies, fixPRReturned)
	}
	if rows := h.history(t); len(rows) != 0 {
		t.Errorf("rows = %+v, want none", rows)
	}
}

// Another run holds the issue: the claim fails, and the first fix-pr start hands the issue back.
func TestApplyFixPRClaimLostReturns(t *testing.T) {
	h := newHarness(t)
	h.seed(t, 7, oldSession, "start:7", "")
	res, err := Apply(context.Background(), h.env, []Action{fixPRAction(1)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if p := h.poster(applyRepo); !reflect.DeepEqual(p.bodies, []string{fixPRReturned}) {
		t.Errorf("posted %q, want %q", p.bodies, fixPRReturned)
	}
	if res.Started != 0 || len(h.wp.spawned) != 0 {
		t.Errorf("Started = %d, spawned = %d; want no start", res.Started, len(h.wp.spawned))
	}
}

// A post that fails joins the error; the defer still stands.
func TestApplyFixPRReturnPostFails(t *testing.T) {
	h := newHarness(t)
	h.wp.sessions = []workplace.Session{{Key: "other", WorkspaceID: "ws-issue-7-x", State: "working", SessionID: "x"}}
	h.poster(applyRepo).postErr = errors.New("forge down")
	res, err := Apply(context.Background(), h.env, []Action{fixPRAction(1)})
	if err == nil || !strings.Contains(err.Error(), "forge down") {
		t.Fatalf("Apply error = %v, want it to name forge down", err)
	}
	if len(res.Deferred) != 1 {
		t.Errorf("Deferred = %q, want the busy defer", res.Deferred)
	}
}

// A second attempt comes from retry, which plans it every tick and hands the issue back after
// two failed starts: a busy workplace only defers it.
func TestApplyFixPRSecondAttemptBusyPostsNothing(t *testing.T) {
	h := newHarness(t)
	h.wp.sessions = []workplace.Session{{Key: "other", WorkspaceID: "ws-issue-7-x", State: "working", SessionID: "x"}}
	res, err := Apply(context.Background(), h.env, []Action{fixPRAction(2)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if p := h.poster(applyRepo); len(p.bodies) != 0 {
		t.Errorf("posted %q, want nothing", p.bodies)
	}
	if want := []string{"o/r#7: workplace busy: session other, working since unknown"}; !reflect.DeepEqual(res.Deferred, want) {
		t.Errorf("Deferred = %q, want %q", res.Deferred, want)
	}
}

// mergeAction is the merge Decide plans for a green own PR of the open run runID.
func mergeAction(runID int64) Action {
	return Action{Kind: KindMerge, Repo: applyRepo, Issue: 7, RunID: runID, SessionID: oldSession, PR: 12,
		HeadSHA: "abc1234", PRURL: "https://github.com/o/r/pull/12", Language: "nl", End: true}
}

func TestApplyMergeSucceeds(t *testing.T) {
	h := newHarness(t)
	id := h.seed(t, 7, oldSession, "start:7", "")
	if _, err := Apply(context.Background(), h.env, []Action{mergeAction(id)}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, want := h.log.all(), []string{"MergePR", "CreateComment", "Kill"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
	p := h.poster(applyRepo)
	if !reflect.DeepEqual(p.merges, []string{"12 abc1234"}) {
		t.Errorf("merges = %q, want [12 abc1234]", p.merges)
	}
	if len(p.bodies) != 1 ||
		!strings.Contains(p.bodies[0], "Checks groen, door de controller gesquasht en gemerged.") ||
		!strings.Contains(p.bodies[0], "pr: https://github.com/o/r/pull/12") {
		t.Errorf("posted %q, want one Klaar with note_merged and the PR url", p.bodies)
	}
	if rows := h.history(t); rows[0].Open || rows[0].Outcome != runs.Done {
		t.Errorf("row = %+v, want closed as done", rows[0])
	}
}

func TestApplyMergeHeadMoved(t *testing.T) {
	h := newHarness(t)
	id := h.seed(t, 7, oldSession, "start:7", "")
	h.poster(applyRepo).mergeErr = forge.ErrHeadMoved
	res, err := Apply(context.Background(), h.env, []Action{mergeAction(id)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := h.log.all(); !reflect.DeepEqual(got, []string{"MergePR"}) {
		t.Errorf("calls = %q, want only MergePR", got)
	}
	if rows := h.history(t); !rows[0].Open || rows[0].MergeSHA != "" {
		t.Errorf("row = %+v, want open without a merge note", rows[0])
	}
	if want := []string{"o/r#7: merge of PR #12 skipped: head moved"}; !reflect.DeepEqual(res.Deferred, want) {
		t.Errorf("Deferred = %q, want %q", res.Deferred, want)
	}
	if _, ok := res.Failed[applyRepo]; ok {
		t.Errorf("Failed = %v, want no %s", res.Failed, applyRepo)
	}
}

func TestApplyMergeRefusedNotes(t *testing.T) {
	h := newHarness(t)
	id := h.seed(t, 7, oldSession, "start:7", "")
	h.poster(applyRepo).mergeErr = &forge.MergeRefused{Message: "Pull Request is not mergeable"}
	if _, err := Apply(context.Background(), h.env, []Action{mergeAction(id)}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := h.log.all(); !reflect.DeepEqual(got, []string{"MergePR"}) {
		t.Errorf("calls = %q, want only MergePR", got)
	}
	row := h.history(t)[0]
	if !row.Open || row.MergeSHA != "abc1234" || row.MergeError != "Pull Request is not mergeable" {
		t.Errorf("row = %+v, want open with merge note abc1234 / Pull Request is not mergeable", row)
	}
	if want := "o/r#7: merge of PR #12 refused: Pull Request is not mergeable"; !strings.Contains(h.out.String(), want) {
		t.Errorf("log %q, want %q", h.out.String(), want)
	}
}

func TestApplyMergeOtherErrorFails(t *testing.T) {
	h := newHarness(t)
	id := h.seed(t, 7, oldSession, "start:7", "")
	h.poster(applyRepo).mergeErr = errors.New("status 500")
	res, err := Apply(context.Background(), h.env, []Action{mergeAction(id)})
	if err == nil || !strings.Contains(err.Error(), "merge: status 500") {
		t.Fatalf("Apply: %v, want the merge error", err)
	}
	if res.Failed[applyRepo] == nil {
		t.Errorf("Failed = %v, want %s", res.Failed, applyRepo)
	}
	if bodies := h.poster(applyRepo).bodies; len(bodies) != 0 {
		t.Errorf("posted %q, want nothing", bodies)
	}
	if row := h.history(t)[0]; !row.Open || row.MergeSHA != "" || row.MergeError != "" {
		t.Errorf("row = %+v, want open without a merge note", row)
	}
}

func TestApplyMergePostFailsKeepsRowOpen(t *testing.T) {
	h := newHarness(t)
	id := h.seed(t, 7, oldSession, "start:7", "")
	h.poster(applyRepo).postErr = errors.New("502")
	if _, err := Apply(context.Background(), h.env, []Action{mergeAction(id)}); err == nil {
		t.Fatal("Apply: want an error")
	}
	if got := h.poster(applyRepo).merges; len(got) != 1 {
		t.Errorf("merges = %q, want one", got)
	}
	if len(h.wp.killed) != 0 {
		t.Errorf("killed = %q, want none", h.wp.killed)
	}
	if row := h.history(t)[0]; !row.Open || row.MergeSHA != "" {
		t.Errorf("row = %+v, want open without a merge note", row)
	}
}

func TestApplyMergeRefusedOnClosedRunFails(t *testing.T) {
	h := newHarness(t)
	id := h.seed(t, 7, oldSession, "start:7", runs.Handoff)
	h.poster(applyRepo).mergeErr = &forge.MergeRefused{Message: "Pull Request is not mergeable"}
	_, err := Apply(context.Background(), h.env, []Action{mergeAction(id)})
	if !errors.Is(err, runs.ErrNotOpen) {
		t.Errorf("Apply: %v, want ErrNotOpen", err)
	}
}

// A first fix-pr start that stops before its claim names the cause decide gave it.
func TestApplyFixPRReturnCarriesCause(t *testing.T) {
	cases := []struct {
		name, reason, want string
	}{
		{"conflict", "reason_pr_conflict", "🤖 Teruggegeven\nreason: PR heeft een merge-conflict"},
		{"no reason", "", fixPRReturned},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.wp.sessions = []workplace.Session{{Key: "other", WorkspaceID: "ws-issue-7-x", State: "working", SessionID: "x"}}
			a := fixPRAction(1)
			a.Reason = tc.reason
			if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if got := h.poster(applyRepo).bodies; !reflect.DeepEqual(got, []string{tc.want}) {
				t.Errorf("posted %q, want %q", got, tc.want)
			}
		})
	}
}

// A start on another node asks the workplace of that node and profile, and claims the run there:
// a later tick finds its node in the run row, whatever host.toml says by then.
func TestApplyStartOnNode(t *testing.T) {
	h := newHarness(t)
	a := startAction()
	a.Node, a.Profile = "node-a", "mac"
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := []string{"o/r node-a mac"}; !reflect.DeepEqual(h.places, want) {
		t.Errorf("workplaces = %q, want %q", h.places, want)
	}
	rows := h.history(t)
	if len(rows) != 1 || rows[0].Node != "node-a" || rows[0].Profile != "mac" {
		t.Errorf("rows = %+v, want one on node node-a with profile mac", rows)
	}
	if len(h.wp.spawned) != 1 || h.wp.spawned[0].Profile != "mac" || h.wp.spawned[0].MCPConfig != "/etc/lighthouse/mac.json" {
		t.Errorf("spawned %+v, want one with profile mac and its mcp config", h.wp.spawned)
	}
}

// seedOn claims an open run of issue 7 on node with profile, on workspace ws with forge key key.
func (h *harness) seedOn(t *testing.T, node, profile, ws, key string) int64 {
	t.Helper()
	ctx := context.Background()
	run, err := runs.Take(ctx, h.db, runs.Claim{
		Repo: applyRepo, Issue: 7, SessionID: oldSession, Mode: "start", Trigger: "handoff",
		TriggerKey: "start:7", Command: "/lighthouse-start-issue 7", Branch: "issue-7-x",
		Profile: profile, Node: node, Now: applyNow.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if err := runs.SetWorkplace(ctx, h.db, run.ID, ws, "issue-7-x"); err != nil {
		t.Fatalf("SetWorkplace: %v", err)
	}
	if err := runs.SetSession(ctx, h.db, run.ID, key); err != nil {
		t.Fatalf("SetSession: %v", err)
	}
	return run.ID
}

// Stop, Kill and Discard of a run go to the node and profile its row holds, not to self or
// to the repo's profile.
func TestApplyReleaseUsesNodeOfRun(t *testing.T) {
	for name, a := range map[string]Action{
		"stop":    {Kind: KindClose, Outcome: runs.Returned, Reason: "reason_silent", Stop: true},
		"end":     {Kind: KindClose, Outcome: runs.Question, End: true},
		"discard": {Kind: KindClose, Outcome: runs.Returned, Reason: "reason_silent", Discard: true},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			a.Repo, a.Issue, a.Language = applyRepo, 7, "nl"
			a.RunID = h.seedOn(t, "node-a", "mac", "ws-1", "key-1")
			if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if want := []string{"o/r node-a mac"}; !reflect.DeepEqual(h.places, want) {
				t.Errorf("workplaces = %q, want %q", h.places, want)
			}
			if got := len(h.wp.stopped) + len(h.wp.killed) + len(h.wp.forgot); got != 1 {
				t.Errorf("stopped %q, killed %q, discarded %q; want one release", h.wp.stopped, h.wp.killed, h.wp.forgot)
			}
		})
	}
}

// randomNode is a node id no fixture shares.
func randomNode() string { return "node-" + strings.ToLower(rand.Text()[:8]) }

// With Kill through serve a node is like self (#327): a live session of the issue's closed run is
// killed after the claim and the start goes on, and the session of an open run is busy.
func TestBusyOnNodeLikeSelf(t *testing.T) {
	for _, node := range []string{"", randomNode()} {
		t.Run("closed run, live session, node "+node, func(t *testing.T) {
			h := newHarness(t)
			id := h.seedOn(t, node, "mac", "ws-issue-7-x", "key-old")
			if err := runs.Release(context.Background(), h.db, id, runs.Returned, "", applyNow.Add(-time.Minute)); err != nil {
				t.Fatal(err)
			}
			h.wp.sessions = []workplace.Session{{Key: "key-old", WorkspaceID: "ws-issue-7-x", State: "running", SessionID: oldSession}}
			a := startAction()
			a.Node, a.Profile = node, "mac"
			res, err := Apply(context.Background(), h.env, []Action{a})
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if len(res.Deferred) != 0 || len(h.wp.spawned) != 1 {
				t.Errorf("Deferred = %q, spawned %d; want no deferral and one spawn", res.Deferred, len(h.wp.spawned))
			}
			if want := []string{"ws-issue-7-x " + oldSession}; !reflect.DeepEqual(h.wp.killed, want) {
				t.Errorf("killed = %q, want %q", h.wp.killed, want)
			}
		})
		t.Run("open run, node "+node, func(t *testing.T) {
			h := newHarness(t)
			h.seedOn(t, node, "mac", "ws-issue-7-x", "key-old")
			h.wp.sessions = []workplace.Session{{Key: "key-old", WorkspaceID: "ws-issue-7-x", State: "running", SessionID: oldSession}}
			a := startAction()
			a.Node, a.Profile = node, "mac"
			res, err := Apply(context.Background(), h.env, []Action{a})
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			want := []string{"o/r#7: workplace busy: session key-old, running since unknown"}
			if !reflect.DeepEqual(res.Deferred, want) || len(h.wp.spawned) != 0 {
				t.Errorf("Deferred = %q, spawned %d; want %q and no spawn", res.Deferred, len(h.wp.spawned), want)
			}
		})
	}
}

// relocWP is a workplace on a node that relocates through Relocator: it answers command with
// to for workspace ws-issue-7-x, or with err.
type relocWP struct {
	*fakeWorkplace
	t       *testing.T
	command string
	to      string
	err     error
}

func (r *relocWP) Relocate(_ context.Context, workspaceID, command string) (string, error) {
	if workspaceID != "ws-issue-7-x" || command != r.command {
		r.t.Errorf("Relocate(%q, %q), want (ws-issue-7-x, %q)", workspaceID, command, r.command)
	}
	return r.to, r.err
}

// On a node the start command is relocated by the node (Relocator), not looked up on this host;
// a refusal is a relocErr: the run closes start_failed with the cause and nothing launches.
func TestApplyRelocatesOnNode(t *testing.T) {
	const from = "/lighthouse-review-plan /home/u/repo/design/plans/p.md"
	const to = "/lighthouse-review-plan /srv/forge/issue-7-x/design/plans/p.md"
	t.Run("relocated", func(t *testing.T) {
		h := newHarness(t)
		r := &relocWP{fakeWorkplace: h.wp, t: t, command: from, to: to}
		h.env.Workplace = func(string, string, string) workplace.Workplace { return r }
		a := startAction()
		a.Node, a.Profile = randomNode(), "mac"
		a.Command, a.Prompt = from, from+" (issue #7, controller-run)"
		if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if want := to + " (issue #7, controller-run)"; len(h.wp.spawned) != 1 || h.wp.spawned[0].Prompt != want {
			t.Fatalf("spawned %+v, want one with prompt %q", h.wp.spawned, want)
		}
		if rows := h.history(t); len(rows) != 1 || rows[0].Command != to {
			t.Errorf("rows = %+v, want one with command %q", rows, to)
		}
	})
	t.Run("refused", func(t *testing.T) {
		h := newHarness(t)
		r := &relocWP{fakeWorkplace: h.wp, t: t, command: from, err: errors.New("p.md not in the worktree")}
		h.env.Workplace = func(string, string, string) workplace.Workplace { return r }
		a := startAction()
		a.Node, a.Profile = randomNode(), "mac"
		a.Command, a.Prompt = from, from+" (issue #7, controller-run)"
		if _, err := Apply(context.Background(), h.env, []Action{a}); err == nil {
			t.Fatal("Apply: want an error")
		}
		if len(h.wp.spawned) != 0 {
			t.Errorf("spawned %+v, want none", h.wp.spawned)
		}
		rows := h.history(t)
		if len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.StartFailed || rows[0].Reason != "handoff path: p.md not in the worktree" {
			t.Errorf("rows = %+v, want one closed start_failed with reason handoff path: p.md not in the worktree", rows)
		}
	})
}

// On self the start command is relocated on this host, also when the workplace is a Relocator:
// every kenn-forge adapter is one, and on self it cannot answer. A start on self whose workplace
// would refuse to relocate still launches once.
func TestApplyOnSelfIgnoresRelocator(t *testing.T) {
	h := newHarness(t)
	a := startAction()
	r := &relocWP{fakeWorkplace: h.wp, t: t, command: a.Command, err: errors.New("relocate: not measurable on self")}
	h.env.Workplace = func(string, string, string) workplace.Workplace { return r }
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(h.wp.spawned) != 1 || h.wp.spawned[0].Prompt != a.Prompt {
		t.Errorf("spawned %+v, want one with prompt %q", h.wp.spawned, a.Prompt)
	}
	if rows := h.history(t); len(rows) != 1 || !rows[0].Open || rows[0].Command != a.Command {
		t.Errorf("rows = %+v, want one open run with command %q", rows, a.Command)
	}
}

// A drop_marker carries no node: apply hands session and key to Env.RemoveMarker, which knows
// where the marker came from, and leaves the local marker dir alone.
func TestDropMarkerCallsRemoveMarkerWithSessionAndKey(t *testing.T) {
	h := newHarness(t)
	h.seed(t, 7, oldSession, "start:7", runs.Parked)
	h.env.MarkerDir = t.TempDir()
	path := writeMarker(t, h.env.MarkerDir, "2026-10-31T08:00:00Z", "2026-10-31T08:30:00Z")
	var got []string
	h.env.RemoveMarker = func(session, key string) error {
		got = append(got, session+" "+key)
		return nil
	}
	drop := Action{Kind: KindDropMarker, Repo: applyRepo, Issue: 7, SessionID: oldSession, TriggerKey: usageKey, Language: "nl"}
	if _, err := Apply(context.Background(), h.env, []Action{drop, usageResumeAction()}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := []string{oldSession + " " + usageKey, oldSession + " " + usageKey}; !reflect.DeepEqual(got, want) {
		t.Errorf("RemoveMarker calls = %q, want %q (drop, then the usage resume)", got, want)
	}
	if !exists(t, path) {
		t.Error("local marker removed although Env.RemoveMarker is set")
	}
}

// A failing Env.RemoveMarker is one log line, never an action error: the next tick drops it again.
func TestMarkerRemovalFailureIsLogNotError(t *testing.T) {
	h := newHarness(t)
	h.env.RemoveMarker = func(string, string) error { return errors.New("serve: connection refused") }
	drop := Action{Kind: KindDropMarker, Repo: applyRepo, Issue: 7, SessionID: oldSession, TriggerKey: usageKey, Language: "nl"}
	res, err := Apply(context.Background(), h.env, []Action{drop})
	if err != nil || len(res.Failed) != 0 {
		t.Fatalf("Apply: %v, Failed = %v; want no action error", err, res.Failed)
	}
	if want := "o/r#7: remove marker of session " + oldSession + ": serve: connection refused\n"; h.out.String() != want {
		t.Errorf("log %q, want %q", h.out.String(), want)
	}
}

func pushAction() Action {
	return Action{
		Kind: KindPush, Repo: applyRepo, Issue: 7, Trigger: triggerPush, TriggerKey: "push:55", Attempt: 1,
		Command: cmdPush, Worktree: "/work/issue-7-x", Branch: "issue-7-x", CommentID: 55, Language: "nl",
	}
}

// pushHarness gives h a polluted tick environment and a pusher starter that records its specs.
func pushHarness(t *testing.T) (*harness, *[]PusherSpec) {
	t.Helper()
	h := newHarness(t)
	tick := map[string]string{
		"PATH": "/usr/bin", "HOME": "/home/u", "XDG_RUNTIME_DIR": "/run/user/1", "GH_TOKEN": "forge-tok",
		"GITHUB_TOKEN": "other-tok", "WORKFLOW_PUSH_TOKEN": "workflow-tok", "XDG_STATE_HOME": "/s",
		"LIGHTHOUSE_CONTROLLER_STATE": "/c", "AWS_SECRET_ACCESS_KEY": "aws",
	}
	h.env.Getenv = func(k string) string { return tick[k] }
	h.env.TokenEnv = func(string) string { return "GH_TOKEN" }
	var specs []PusherSpec
	h.env.StartPusher = func(_ context.Context, s PusherSpec) error { specs = append(specs, s); return nil }
	return h, &specs
}

func TestApplyPushClaimsRowAndStartsPusher(t *testing.T) {
	h, specs := pushHarness(t)
	res, err := Apply(context.Background(), h.env, []Action{pushAction()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	rows := h.history(t)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one push row", rows)
	}
	r := rows[0]
	if r.Command != cmdPush || r.SessionID != "" || r.Mode != "start" || r.Trigger != "handoff" ||
		r.TriggerKey != "push:55" || r.Branch != "issue-7-x" || !r.Open {
		t.Errorf("row = %+v, want an open sessionless push-workflows row on issue-7-x, trigger handoff, key push:55", r)
	}
	if len(*specs) != 1 {
		t.Fatalf("specs = %+v, want one pusher start", *specs)
	}
	s := (*specs)[0]
	if s.Repo != applyRepo || s.Issue != 7 || s.Worktree != "/work/issue-7-x" || s.RunID != r.ID || s.Permit != "" {
		t.Errorf("spec = %+v", s)
	}
	want := []string{"PATH=/usr/bin", "HOME=/home/u", "XDG_RUNTIME_DIR=/run/user/1", "GH_TOKEN=forge-tok"}
	if !reflect.DeepEqual(s.Env, want) {
		t.Errorf("pusher env = %q, want exactly %q", s.Env, want)
	}
	if res.Started != 1 {
		t.Errorf("Started = %d, want 1", res.Started)
	}
	if got := h.poster(applyRepo).rockets; !reflect.DeepEqual(got, []int64{55}) {
		t.Errorf("rockets = %v, want the handoff", got)
	}
	if got := h.log.all(); slices.ContainsFunc(got, func(c string) bool { return strings.HasPrefix(c, "Spawn") || strings.HasPrefix(c, "Prepare") }) {
		t.Errorf("workplace calls = %q, want none: a pusher is no session", got)
	}
}

func TestApplyPushAnswerCarriesPermitAndTrigger(t *testing.T) {
	h, specs := pushHarness(t)
	a := pushAction()
	a.Trigger, a.TriggerKey, a.PermitSHA = triggerPushAnswer, "pushanswer:88", strings.Repeat("a", 40)
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if r := h.history(t)[0]; r.Trigger != "answer" || r.TriggerKey != "pushanswer:88" {
		t.Errorf("row = %+v, want trigger answer and key pushanswer:88", r)
	}
	if s := (*specs)[0]; s.Permit != a.PermitSHA {
		t.Errorf("permit = %q, want %q", s.Permit, a.PermitSHA)
	}
}

func TestApplyPushStartFailureClosesStartFailed(t *testing.T) {
	h, _ := pushHarness(t)
	h.env.StartPusher = func(context.Context, PusherSpec) error { return errors.New("no such file") }
	res, err := Apply(context.Background(), h.env, []Action{pushAction()})
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("Apply error = %v, want the start error", err)
	}
	rows := h.history(t)
	if len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.StartFailed || !strings.Contains(rows[0].Reason, "no such file") {
		t.Errorf("rows = %+v, want one start_failed row with the cause", rows)
	}
	if res.Started != 0 {
		t.Errorf("Started = %d, want 0", res.Started)
	}
}

func TestApplyPushSpentTriggerStartsNothing(t *testing.T) {
	h, specs := pushHarness(t)
	h.seed(t, 7, "", "push:55", runs.Handoff)
	res, err := Apply(context.Background(), h.env, []Action{pushAction()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(*specs) != 0 || res.Started != 0 || len(h.history(t)) != 1 || len(h.poster(applyRepo).rockets) != 0 {
		t.Errorf("specs = %v, Started = %d, rows = %d, rockets = %v; want nothing started or posted",
			*specs, res.Started, len(h.history(t)), h.poster(applyRepo).rockets)
	}
}

func mergePushAction() Action {
	return Action{
		Kind: KindPush, Repo: applyRepo, Issue: 7, Trigger: triggerMerge, TriggerKey: "merge:12:" + strings.Repeat("c", 40),
		Attempt: 1, Command: cmdPush, Branch: "issue-7-x", PR: 12, HeadSHA: strings.Repeat("c", 40), Language: "nl",
	}
}

// The merge pusher is started like the pusher: a sessionless push-workflows row on the PR's
// branch, trigger handoff (the only trigger runs allows besides answer), keyed per PR and head,
// and a process that gets PATH, HOME, XDG_RUNTIME_DIR and the forge token, never the workflow
// token or any other variable of tick. It carries the PR and head instead of a worktree.
func TestApplyMergePushStartsPusherWithScrubbedEnv(t *testing.T) {
	h, specs := pushHarness(t)
	a := mergePushAction()
	res, err := Apply(context.Background(), h.env, []Action{a})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	rows := h.history(t)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one merge row", rows)
	}
	r := rows[0]
	if r.Command != cmdPush || r.SessionID != "" || r.Trigger != "handoff" || r.TriggerKey != a.TriggerKey ||
		r.Branch != "issue-7-x" || !r.Open {
		t.Errorf("row = %+v, want an open sessionless push-workflows row keyed %s", r, a.TriggerKey)
	}
	if len(*specs) != 1 {
		t.Fatalf("specs = %+v, want one pusher start", *specs)
	}
	s := (*specs)[0]
	if !s.Merge || s.PR != 12 || s.HeadSHA != a.HeadSHA || s.Worktree != "" || s.Permit != "" || s.RunID != r.ID {
		t.Errorf("spec = %+v, want the merge of PR 12 on its head for row %d", s, r.ID)
	}
	want := []string{"PATH=/usr/bin", "HOME=/home/u", "XDG_RUNTIME_DIR=/run/user/1", "GH_TOKEN=forge-tok"}
	if !reflect.DeepEqual(s.Env, want) {
		t.Errorf("pusher env = %q, want exactly %q", s.Env, want)
	}
	if res.Started != 1 || len(h.poster(applyRepo).rockets) != 0 {
		t.Errorf("Started = %d, rockets = %v; want 1 start and no rocket", res.Started, h.poster(applyRepo).rockets)
	}
}

// A token_env that names a variable the pusher gets anyway (PATH, HOME, XDG_RUNTIME_DIR) does not
// put it in the pusher's environment twice.
func TestApplyPushEnvNamesEachVariableOnce(t *testing.T) {
	for _, name := range []string{"PATH", "HOME", "XDG_RUNTIME_DIR"} {
		h, specs := pushHarness(t)
		h.env.TokenEnv = func(string) string { return name }
		if _, err := Apply(context.Background(), h.env, []Action{pushAction()}); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if len(*specs) != 1 {
			t.Fatalf("specs = %+v, want one pusher start", *specs)
		}
		want := []string{"PATH=/usr/bin", "HOME=/home/u", "XDG_RUNTIME_DIR=/run/user/1"}
		if got := (*specs)[0].Env; !reflect.DeepEqual(got, want) {
			t.Errorf("token_env %s: pusher env = %q, want %q", name, got, want)
		}
	}
}

// A merge key a row already spent starts no second merge pusher for the same head.
func TestApplyMergePushSpentKeyStartsNothing(t *testing.T) {
	h, specs := pushHarness(t)
	a := mergePushAction()
	h.seed(t, 7, "", a.TriggerKey, runs.Question)
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(*specs) != 0 || len(h.history(t)) != 1 {
		t.Errorf("specs = %v, rows = %d; want nothing started", *specs, len(h.history(t)))
	}
}

func TestApplyRepostPostsBody(t *testing.T) {
	h := newHarness(t)
	id := h.seed(t, 7, oldSession, "start:7", "")
	body := "🤖 Klaar\nsession: x\n\nliteral  body"
	a := Action{Kind: KindRepost, Repo: applyRepo, Issue: 7, RunID: id, Body: body}
	res, err := Apply(context.Background(), h.env, []Action{a})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	p := h.poster(applyRepo)
	if !reflect.DeepEqual(p.bodies, []string{body}) || !reflect.DeepEqual(p.issues, []int{7}) {
		t.Errorf("posted %q on %v, want the body on issue 7", p.bodies, p.issues)
	}
	if len(res.Deferred) != 0 || len(res.Failed) != 0 {
		t.Errorf("Deferred = %q, Failed = %v, want none", res.Deferred, res.Failed)
	}
	if rows := h.history(t); !rows[0].Open || rows[0].Outcome != "" {
		t.Errorf("row = %+v, want the run still open without an outcome", rows[0])
	}
}

func TestApplyRepostFailureDefers(t *testing.T) {
	h := newHarness(t)
	id := h.seed(t, 7, oldSession, "start:7", "")
	h.poster(applyRepo).postErr = errors.New("502\nbad gateway")
	actions := []Action{
		{Kind: KindRepost, Repo: applyRepo, Issue: 7, RunID: id, Body: "b"},
		{Kind: KindDefer, Repo: applyRepo, Issue: 9, Reason: "night"},
	}
	res, err := Apply(context.Background(), h.env, actions)
	if err != nil {
		t.Fatalf("Apply: %v, want nil", err)
	}
	if _, ok := res.Failed[applyRepo]; ok {
		t.Errorf("Failed = %v, want no key for the repo", res.Failed)
	}
	want := []string{"o/r#7: repost run " + strconv.FormatInt(id, 10) + ": 502; bad gateway", "o/r#9: night"}
	if !reflect.DeepEqual(res.Deferred, want) {
		t.Errorf("Deferred = %q, want %q", res.Deferred, want)
	}
	if rows := h.history(t); !rows[0].Open {
		t.Errorf("row = %+v, want the run still open", rows[0])
	}
}

// giveBackCalls are the forge calls of a 🤖 Returned after its post: the closed check, then
// issuestate.Return (the label edit, the board item, the move to To do).
var giveBackCalls = []string{"Issue", "Issue", "EditIssue", "IssueItem", "SetStatus"}

// wantGivenBack checks that issue 7 carries exactly label and moved to To do once. The board
// Priority is never written: fakeForge has no SetPriority, so a write would panic.
func wantGivenBack(t *testing.T, p *fakeForge, label string) {
	t.Helper()
	if !reflect.DeepEqual(p.labels, []string{label}) {
		t.Errorf("labels = %q, want [%s]", p.labels, label)
	}
	if want := []string{"PVTI_7 To do"}; !reflect.DeepEqual(p.sets, want) {
		t.Errorf("board = %q, want %q", p.sets, want)
	}
}

// A KindReturn posts, labels by the spec table and moves the issue to To do, then stops the
// session and closes the run as returned.
func TestApplyReturnSetsToDoWithLabel(t *testing.T) {
	tests := []struct {
		reason  string
		blocked bool
		label   string
	}{
		{"reason_needs_brainstorm", false, "needs_shaping"},
		{"reason_interactive", false, "needs_decision"},
		{"reason_pr_red", true, "blocked_issue"},
		{"reason_silent", false, "needs_hands"},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			h := newHarness(t)
			a := returnRun(h.seedWorkplace(t, "ws-1", "key-1"))
			a.Reason, a.Blocked, a.Stop = tt.reason, tt.blocked, true
			if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			want := append(append([]string{"CreateComment"}, giveBackCalls...), "Stop")
			if got := h.log.all(); !reflect.DeepEqual(got, want) {
				t.Errorf("calls = %q, want %q", got, want)
			}
			wantGivenBack(t, h.poster(applyRepo), tt.label)
			if rows := h.history(t); rows[0].Open || rows[0].Outcome != runs.Returned {
				t.Errorf("row = %+v, want closed as returned", rows[0])
			}
		})
	}
}

func TestApplyReturnWithoutRunStillLabels(t *testing.T) {
	h := newHarness(t)
	a := Action{Kind: KindReturn, Repo: applyRepo, Issue: 7, Reason: "reason_bad_handoff", Language: "nl"}
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	wantGivenBack(t, h.poster(applyRepo), "needs_hands")
}

// The forge moved a closed issue to Done: the Returned still lands, the label and board do not.
func TestApplyReturnClosedIssueSkipsLabelAndStatus(t *testing.T) {
	h := newHarness(t)
	p := h.poster(applyRepo)
	p.closedIssue = true
	id := h.seed(t, 7, oldSession, "start:7", "")
	if _, err := Apply(context.Background(), h.env, []Action{returnRun(id)}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, want := h.log.all(), []string{"CreateComment", "Issue"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
	if len(p.bodies) != 1 || p.labels != nil || p.sets != nil {
		t.Errorf("posted %q, labels %q, board %q; want the Returned only", p.bodies, p.labels, p.sets)
	}
	if rows := h.history(t); rows[0].Open {
		t.Errorf("row = %+v, want closed", rows[0])
	}
}

func TestApplyReturnOwnWorkSetsToDo(t *testing.T) {
	h := newHarness(t)
	h.wp.prepareErr = &workplace.OwnWorkError{
		Workspace: workplace.Workspace{ID: "ws-issue-7-x", Path: "/forge/issue-7-x", Branch: "issue-7-x"}, Commits: 2,
	}
	if _, err := Apply(context.Background(), h.env, []Action{startAction()}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	wantGivenBack(t, h.poster(applyRepo), "needs_hands")
	if rows := h.history(t); len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.Returned {
		t.Errorf("rows = %+v, want one closed returned run", rows)
	}
}

func TestApplyReturnFixPRSetsToDo(t *testing.T) {
	h := newHarness(t)
	h.wp.sessions = []workplace.Session{{Key: "other", WorkspaceID: "ws-issue-7-x", State: "working", SessionID: "x"}}
	if _, err := Apply(context.Background(), h.env, []Action{fixPRAction(1)}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	wantGivenBack(t, h.poster(applyRepo), "needs_hands")
}

// The first refused launch keeps the issue on Controller without a label: retry plans the second
// attempt, and a To do would hide it from that.
func TestApplyReturnStartFailedAttempt1KeepsController(t *testing.T) {
	h := newHarness(t)
	h.wp.spawnErr = workplace.ErrNotAccepted
	if _, err := Apply(context.Background(), h.env, []Action{startAction()}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	p := h.poster(applyRepo)
	if len(p.bodies) != 2 || p.labels != nil || p.sets != nil {
		t.Errorf("posted %d, labels %q, board %q; want the Started and Returned only", len(p.bodies), p.labels, p.sets)
	}
}

// The second refused launch is the last: no retry follows, so the issue goes to To do.
func TestApplyReturnStartFailedAttempt2SetsToDo(t *testing.T) {
	h := newHarness(t)
	h.wp.spawnErr = workplace.ErrNotAccepted
	a := startAction()
	a.Attempt = 2
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	wantGivenBack(t, h.poster(applyRepo), "needs_hands")
}

// tickInput is what the next tick gathers of issue 7: the item as the fake forge holds it, with
// every post as a comment of the token login, and the runs of the database.
func (h *harness) tickInput(t *testing.T) Input {
	t.Helper()
	p := h.poster(applyRepo)
	it := Item{Issue: 7, Title: "x", State: "OPEN", ItemID: "PVTI_7", Status: cmp.Or(p.status, statusController),
		Priority: "P1", Labels: p.labels}
	for i, body := range p.bodies {
		it.Comments = append(it.Comments, Comment{ID: int64(1001 + i), Author: "lh-bot", Body: body,
			CreatedAt: applyNow.Format(time.RFC3339)})
	}
	return Input{Now: applyNow, Repos: []RepoInput{{
		Snapshot: Snapshot{Version: SnapshotVersion, Repo: applyRepo, TokenLogin: "lh-bot", Items: []Item{it}},
		Language: "nl", Labels: testLabels, History: h.history(t),
	}}}
}

// tick is one Decide and Apply round on what tickInput gathers.
func (h *harness) tick(t *testing.T) ([]Action, error) {
	t.Helper()
	actions := Decide(h.tickInput(t))
	_, err := Apply(context.Background(), h.env, actions)
	return actions, err
}

// A Returned whose label edit failed keeps its run open; the next tick closes the run (settle),
// the one after finishes the hand-back with the label of its reason, and then nothing is left.
func TestApplyReturnHalfWrittenFinishes(t *testing.T) {
	h := newHarness(t)
	p := h.poster(applyRepo)
	p.editErr = errors.New("forge down")
	a := returnRun(h.seed(t, 7, oldSession, "start:7", ""))
	if _, err := Apply(context.Background(), h.env, []Action{a}); err == nil || !strings.Contains(err.Error(), "forge down") {
		t.Fatalf("Apply = %v, want the label error", err)
	}
	if rows := h.history(t); !rows[0].Open {
		t.Fatalf("row = %+v, want still open after the failed label", rows[0])
	}
	p.editErr = nil
	for i, want := range [][]Kind{{KindClose}, {KindFinishReturn}, {}} {
		got, err := h.tick(t)
		if err != nil || !reflect.DeepEqual(kinds(got), want) {
			t.Fatalf("tick %d = %+v, %v; want %v", i+1, got, err, want)
		}
	}
	wantGivenBack(t, p, "needs_hands")
	if rows := h.history(t); rows[0].Open || rows[0].Outcome != runs.Returned || len(p.bodies) != 1 {
		t.Errorf("row = %+v, posts %d; want closed as returned under one Returned", rows[0], len(p.bodies))
	}
}

// A Returned without a run (a handoff that wants a brainstorm) whose label edit failed is finished
// by the next tick with the label its reason text names, needs_shaping, not the default.
func TestDecideFinishesReturnWithoutRun(t *testing.T) {
	h := newHarness(t)
	p := h.poster(applyRepo)
	p.editErr = errors.New("forge down")
	a := Action{Kind: KindReturn, Repo: applyRepo, Issue: 7, Reason: "reason_needs_brainstorm", Language: "nl"}
	if _, err := Apply(context.Background(), h.env, []Action{a}); err == nil {
		t.Fatal("Apply: want the label error")
	}
	p.editErr = nil
	for i, want := range [][]Kind{{KindFinishReturn}, {}} {
		got, err := h.tick(t)
		if err != nil || !reflect.DeepEqual(kinds(got), want) {
			t.Fatalf("tick %d = %+v, %v; want %v", i+1, got, err, want)
		}
	}
	wantGivenBack(t, p, "needs_shaping")
}

// Only the tick's own Returned is finished: a 🤖 Teruggegeven another author wrote by hand moves
// nothing.
func TestDecideFinishesOnlyOwnReturn(t *testing.T) {
	h := newHarness(t)
	p := h.poster(applyRepo)
	p.bodies = []string{protocol.RenderReturned("PR rood", "", "", "nl")}
	in := h.tickInput(t)
	if got := Decide(in); !reflect.DeepEqual(kinds(got), []Kind{KindFinishReturn}) {
		t.Fatalf("own Returned: Decide = %+v, want one finish_return", got)
	}
	in.Repos[0].Snapshot.Items[0].Comments[0].Author = "owner"
	if got := Decide(in); len(got) != 0 {
		t.Errorf("Returned of owner: Decide = %+v, want nothing", got)
	}
}

// A Controller issue that carries blocked_issue while its own Returned names reason_interactive
// (the label write failed) is finished with needs_decision: blocked_issue is no input of the
// label, the rank rule drops it. Assigning a.Blocked in finishReturn from the blocked_issue
// label turns this red: ReturnLabelKey then gives blocked_issue.
func TestDecideFinishReturnKeepsReasonOverBlockedIssue(t *testing.T) {
	h := newHarness(t)
	p := h.poster(applyRepo)
	p.bodies = []string{protocol.RenderReturned(protocol.Heading("reason_interactive", "nl"), "", "", "nl")}
	p.labels = []string{"blocked_issue"}
	got, err := h.tick(t)
	if err != nil || !reflect.DeepEqual(kinds(got), []Kind{KindFinishReturn}) {
		t.Fatalf("tick = %+v, %v; want one finish_return", got, err)
	}
	wantGivenBack(t, p, "needs_decision")
}

// A KindToDo only sets To do on the board item: no comment, no label change, and an open run
// and its workplace stay as they are. Any extra write in the KindToDo case turns this red.
func TestApplyToDoOnlySetsStatus(t *testing.T) {
	h := newHarness(t)
	id := h.seedWorkplace(t, "ws-1", "key-1")
	a := Action{Kind: KindToDo, Repo: applyRepo, Issue: 7, ItemID: "PVTI_7"}
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, want := h.log.all(), []string{"SetStatus"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
	if want := []string{"PVTI_7 To do"}; !reflect.DeepEqual(h.poster(applyRepo).sets, want) {
		t.Errorf("board = %q, want %q", h.poster(applyRepo).sets, want)
	}
	if rows := h.history(t); len(rows) != 1 || rows[0].ID != id || !rows[0].Open {
		t.Errorf("history = %+v, want run %d still open", rows, id)
	}
}

// A Controller issue whose last comment is the tick's own Returned and that carries needs_hands
// gets exactly one todo and then nothing: it never reaches finishReturn (the needs_* rule of
// decideIssue comes first), and once the board says To do neither rule fires. Dropping the
// needs_* rule gives a start or a finish_return on tick 1; the blocking-label check inside
// finishReturn is a second guard that matters for blocked_* labels and does not turn this red.
func TestDecideNeedsLabelOwnReturnedNoLoop(t *testing.T) {
	h := newHarness(t)
	p := h.poster(applyRepo)
	p.bodies = []string{protocol.RenderReturned("PR rood", "", "", "nl")}
	p.labels = []string{"needs_hands"}
	for i, want := range [][]Kind{{KindToDo}, {}, {}} {
		got, err := h.tick(t)
		if err != nil || !reflect.DeepEqual(kinds(got), want) {
			t.Fatalf("tick %d = %+v, %v; want %v", i+1, got, err, want)
		}
	}
	if want := []string{"PVTI_7 To do"}; !reflect.DeepEqual(p.sets, want) {
		t.Errorf("board = %q, want one To do", p.sets)
	}
}

// A rule-13 Returned with an active blocker whose label landed but whose To do did not stays on
// Controller with blocked_issue; the next tick finishes it as blocked (label kept, To do), and
// then nothing is left. Skipping every blocking label in finishReturn turns tick 1 red.
func TestDecideFinishesBlockedReturnStuckOnController(t *testing.T) {
	h := newHarness(t)
	p := h.poster(applyRepo)
	p.setErr = errors.New("board down")
	a := Action{Kind: KindReturn, Repo: applyRepo, Issue: 7, Reason: "reason_pr_red", Blocked: true, Language: "nl"}
	if _, err := Apply(context.Background(), h.env, []Action{a}); err == nil {
		t.Fatal("Apply: want the board error")
	}
	if p.status != "" || !reflect.DeepEqual(p.labels, []string{"blocked_issue"}) {
		t.Fatalf("after the failed board write: status %q, labels %q; want Controller with blocked_issue", p.status, p.labels)
	}
	p.setErr = nil
	for i, want := range [][]Kind{{KindFinishReturn}, {}} {
		got, err := h.tick(t)
		if err != nil || !reflect.DeepEqual(kinds(got), want) {
			t.Fatalf("tick %d = %+v, %v; want %v", i+1, got, err, want)
		}
	}
	wantGivenBack(t, p, "blocked_issue")
	// A Controller issue with own Returned and blocked_external is a human's: no finish.
	p.status, p.labels, p.sets = "", []string{"blocked_external"}, nil
	if got := Decide(h.tickInput(t)); len(got) != 0 {
		t.Errorf("blocked_external: Decide = %+v, want nothing", got)
	}
}

// A repair resume (#505) is claimed as an answer: runs knows no trigger repair, and its key
// repair:<id> still spends the comment once.
func TestApplyClaimsRepairResumeAsAnswer(t *testing.T) {
	h := newHarness(t)
	h.seed(t, 7, oldSession, "start:7", runs.Question)
	a := resumeAction()
	a.Trigger, a.TriggerKey, a.CommentID = "repair", "repair:77", 77
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	rows := h.history(t)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want two", rows)
	}
	if r := rows[1]; r.Trigger != "answer" || r.TriggerKey != "repair:77" {
		t.Errorf("repair row = %+v, want trigger answer, key repair:77", r)
	}
	if _, err := Apply(context.Background(), h.env, []Action{a}); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if rows := h.history(t); len(rows) != 2 {
		t.Errorf("rows = %+v, want still two", rows)
	}
	if !strings.Contains(h.out.String(), "repair:77 not claimed") {
		t.Errorf("log lacks a not-claimed line:\n%s", h.out.String())
	}
}

func pushRecoverAction() Action {
	return Action{
		Kind: KindPushRecover, Repo: applyRepo, Issue: 7, Trigger: triggerPushAnswer, TriggerKey: "pushanswer:71", Attempt: 1,
		Command: cmdPush, Worktree: "/work/issue-7-x", Branch: "issue-7-x", CommentID: 71, Language: "nl",
		PermitSHA: strings.Repeat("a", 40), AnswerText: "gepushed", After: "/lighthouse-write-pr 7",
	}
}

// lsRemoteFake records what the applier asked and answers sha or err.
func lsRemoteFake(sha string, err error, asked *[]string) func(context.Context, string, string, []string) (string, error) {
	return func(_ context.Context, worktree, branch string, env []string) (string, error) {
		*asked = append(*asked, worktree+" "+branch+" "+strings.Join(env, ","))
		return sha, err
	}
}

// The branch stands on the Vraag's sha already: the pusher is not started, a handoff with the
// Vraag's after command is posted and the row closes as handoff.
func TestApplyPushRecoverBranchOnSHA(t *testing.T) {
	h, specs := pushHarness(t)
	var asked []string
	h.env.LsRemote = lsRemoteFake(strings.Repeat("a", 40), nil, &asked)
	res, err := Apply(context.Background(), h.env, []Action{pushRecoverAction()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	wantAsked := []string{"/work/issue-7-x issue-7-x PATH=/usr/bin,HOME=/home/u,XDG_RUNTIME_DIR=/run/user/1,GH_TOKEN=forge-tok"}
	if !reflect.DeepEqual(asked, wantAsked) {
		t.Errorf("ls-remote asked %q, want %q", asked, wantAsked)
	}
	p := h.poster(applyRepo)
	wantBody := "🤖 Handoff\nissue: #7\nworktree: /work/issue-7-x\nbranch: issue-7-x\nnext: /lighthouse-write-pr 7\nwhen: now\nrange: \nworkflows: "
	if !reflect.DeepEqual(p.bodies, []string{wantBody}) {
		t.Errorf("posted %q, want %q", p.bodies, wantBody)
	}
	if len(*specs) != 0 || res.Started != 0 {
		t.Errorf("specs = %v, Started = %d, want no pusher", *specs, res.Started)
	}
	rows := h.history(t)
	if len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.Handoff || rows[0].TriggerKey != "pushanswer:71" || rows[0].Trigger != "answer" {
		t.Errorf("rows = %+v, want one closed handoff row keyed pushanswer:71", rows)
	}
	if !reflect.DeepEqual(p.rockets, []int64{71}) {
		t.Errorf("rockets = %v, want the answer", p.rockets)
	}
}

func TestApplyPushRecoverBranchElsewhere(t *testing.T) {
	h, specs := pushHarness(t)
	var asked []string
	h.env.LsRemote = lsRemoteFake(strings.Repeat("b", 40), nil, &asked)
	if _, err := Apply(context.Background(), h.env, []Action{pushRecoverAction()}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	p := h.poster(applyRepo)
	if len(p.bodies) != 1 || !strings.HasPrefix(p.bodies[0], "🤖 Teruggegeven\nreason: ") || !strings.Contains(p.bodies[0], "gepushed") {
		t.Errorf("posted %q, want one Teruggegeven naming the answer gepushed", p.bodies)
	}
	if !slices.Contains(p.labels, testLabels.NeedsHands) {
		t.Errorf("labels = %q, want the needs_hands label %q", p.labels, testLabels.NeedsHands)
	}
	if len(*specs) != 0 {
		t.Errorf("specs = %v, want no pusher", *specs)
	}
	rows := h.history(t)
	if len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.Returned {
		t.Errorf("rows = %+v, want one row closed as returned", rows)
	}
}

func TestApplyPushRecoverGitFails(t *testing.T) {
	h, specs := pushHarness(t)
	var asked []string
	h.env.LsRemote = lsRemoteFake("", errors.New("fatal: no network"), &asked)
	if _, err := Apply(context.Background(), h.env, []Action{pushRecoverAction()}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	p := h.poster(applyRepo)
	if len(p.bodies) != 1 || !strings.Contains(p.bodies[0], "COULD NOT CHECK") || !strings.Contains(p.bodies[0], "fatal: no network") {
		t.Errorf("posted %q, want a Teruggegeven with COULD NOT CHECK and the git error", p.bodies)
	}
	if len(*specs) != 0 {
		t.Errorf("specs = %v, want no pusher", *specs)
	}
	if rows := h.history(t); len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.Returned {
		t.Errorf("rows = %+v, want one row closed as returned", rows)
	}
}

// A spent trigger key does nothing: no git call, no comment.
func TestApplyPushRecoverSpentKeyDoesNothing(t *testing.T) {
	h, _ := pushHarness(t)
	h.seed(t, 7, "", "pushanswer:71", runs.Handoff)
	var asked []string
	h.env.LsRemote = lsRemoteFake(strings.Repeat("a", 40), nil, &asked)
	if _, err := Apply(context.Background(), h.env, []Action{pushRecoverAction()}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(asked) != 0 || len(h.poster(applyRepo).bodies) != 0 {
		t.Errorf("asked = %q, posted = %q, want nothing", asked, h.poster(applyRepo).bodies)
	}
}

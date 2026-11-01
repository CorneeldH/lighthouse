package dispatch

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

const (
	applyRepo    = "o/r"
	freshSession = "11111111-1111-4111-8111-111111111111"
	oldSession   = "22222222-2222-4222-8222-222222222222"
)

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

// fakePoster keeps what was posted; postErr and rocketErr make the call fail.
type fakePoster struct {
	log       *callLog
	postErr   error
	rocketErr error
	onPost    func() // runs at the start of CreateComment
	nextID    int64
	issues    []int
	bodies    []string
	rockets   []int64
}

func (p *fakePoster) CreateComment(_ context.Context, number int, body string) (protocol.Comment, error) {
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

func (p *fakePoster) AddRocket(_ context.Context, commentID int64) error {
	p.log.add("AddRocket")
	p.rockets = append(p.rockets, commentID)
	return p.rocketErr
}

// fakeWorkplace prepares a workspace per branch and records every spawn.
type fakeWorkplace struct {
	log      *callLog
	sessions []workplace.Session
	spawnErr error
	onSpawn  func() // runs at the start of Spawn
	spawned  []workplace.LaunchSpec
	stopErr  error
	stopped  []string // "<workspace> <session ids>" per Stop
	forgot   []string // workspace per Discard
}

func (w *fakeWorkplace) Prepare(_ context.Context, branch string) (workplace.Workspace, error) {
	w.log.add("Prepare")
	return workplace.Workspace{ID: "ws-" + branch, Path: "/forge/" + branch, Branch: branch}, nil
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

func (w *fakeWorkplace) Remove(context.Context, string) error {
	w.log.add("Remove")
	return nil
}

func (w *fakeWorkplace) Discard(_ context.Context, workspaceID string) error {
	w.log.add("Discard")
	w.forgot = append(w.forgot, workspaceID)
	return nil
}

// harness is one apply process: a database, a poster and a workplace per repo.
type harness struct {
	db      *sql.DB
	log     *callLog
	posters map[string]*fakePoster
	wp      *fakeWorkplace
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
	h := &harness{db: db, log: log, posters: map[string]*fakePoster{}, wp: &fakeWorkplace{log: log}, out: &bytes.Buffer{}}
	h.env = Env{
		DB: db,
		Poster: func(repo string) Poster {
			p, ok := h.posters[repo]
			if !ok {
				p = &fakePoster{log: log}
				h.posters[repo] = p
			}
			return p
		},
		Workplace:  func(string) workplace.Workplace { return h.wp },
		Profile:    func(string) (string, string) { return "writing", "/etc/lighthouse/mcp.json" },
		RoundLimit: func(string) string { return "stop" },
		StateDir:   t.TempDir(),
		MaxWait:    10 * time.Minute,
		Now:        func() time.Time { return applyNow },
		NewSession: func() string { return freshSession },
		Log:        h.out,
	}
	return h
}

func (h *harness) poster(repo string) *fakePoster {
	return h.env.Poster(repo).(*fakePoster)
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
		TriggerKey: "handoff:55", Attempt: 1, Command: "/lighthouse-spec-adversarial-review /w/spec.md",
		Prompt: "/lighthouse-spec-adversarial-review /w/spec.md (issue #7, controller-run)",
		Branch: "issue-7-x", Native: true, Skip: "skip_plan_review", CommentID: 55, Language: "nl",
	}
}

func resumeAction() Action {
	return Action{
		Kind: KindResume, Repo: applyRepo, Issue: 7, Mode: "resume", Trigger: "answer",
		TriggerKey: "answer:88", Attempt: 1, SessionID: oldSession, Command: "/lighthouse-start-issue 7",
		Prompt: "the answer", Branch: "issue-7-x", CommentID: 88, Language: "nl",
	}
}

func TestApplyStartOrder(t *testing.T) {
	h := newHarness(t)
	res, err := Apply(context.Background(), h.env, []Action{startAction()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	wantCalls := []string{"Prepare", "Sessions ws-issue-7-x", "CreateComment", "AddRocket", "Prepare", "Spawn"}
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
		"command: /lighthouse-spec-adversarial-review /w/spec.md\n" +
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
	instructions := filepath.Join(h.env.StateDir, "runs", freshSession, "instructions.md")
	wantSpec := workplace.LaunchSpec{
		Repo: applyRepo, Issue: 7, SessionID: freshSession, Mode: "start", Profile: "writing",
		Prompt: "/lighthouse-spec-adversarial-review /w/spec.md (issue #7, controller-run)", Native: true,
		InstructionsFile: instructions, MCPConfig: "/etc/lighthouse/mcp.json",
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
	want := []string{"Prepare", "Sessions ws-issue-7-x", "CreateComment", "AddRocket", "Prepare", "Spawn"}
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

func TestApplyNotAcceptedCountsNoStart(t *testing.T) {
	h := newHarness(t)
	h.wp.spawnErr = workplace.ErrNotAccepted
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
	if len(h.poster(applyRepo).bodies) != 1 {
		t.Errorf("posted %d, want the Started to stay", len(h.poster(applyRepo).bodies))
	}
	if !strings.Contains(h.out.String(), "not accepted") {
		t.Errorf("log %q does not report the refused launch", h.out.String())
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
		{Kind: KindDefer, Repo: applyRepo, Issue: 9, Reason: "blocker"},
	}
	res, err := Apply(context.Background(), h.env, actions)
	if err == nil || !strings.Contains(err.Error(), "rocket refused") {
		t.Fatalf("Apply: %v, want the rocket error", err)
	}
	if rows := h.history(t); rows[0].Open || rows[0].Outcome != runs.Handoff {
		t.Errorf("row = %+v, want closed as handoff after the failed rocket", rows[0])
	}
	if !reflect.DeepEqual(res.Deferred, []string{"o/r#9: blocker"}) {
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
	if got, want := h.log.all(), []string{"CreateComment", "Stop"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
	if want := []string{"ws-1 key-1"}; !reflect.DeepEqual(h.wp.stopped, want) {
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
	if got, want := h.log.all(), []string{"CreateComment", "Discard"}; !reflect.DeepEqual(got, want) {
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
	if want := []string{"ws-1 key-1"}; !reflect.DeepEqual(h.wp.stopped, want) {
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

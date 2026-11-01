package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/flock"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

func TestRunStartsOnlyThatIssue(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, w := startable(t, &env)
	srv.BoardNodes = append(srv.BoardNodes, boardNode("I8", "c1", issueContent(8, "Ander", nil)))
	var errOut bytes.Buffer
	if err := runIssue(context.Background(), env, "o/r#8", db, &errOut); err != nil {
		t.Fatalf("run = %v\n%s", err, errOut.String())
	}
	if want := "lighthouse: started o/r#8\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
	if posted := bodies(srv, 7); len(posted) != 0 {
		t.Errorf("comments on #7 = %q, want none", posted)
	}
	if posted := bodies(srv, 8); len(posted) != 1 || !strings.HasPrefix(posted[0], "🤖 Started\n") {
		t.Errorf("comments on #8 = %q, want one Started", posted)
	}
	rows := history(t, openStore(t, db), "o/r")
	if len(rows) != 1 || rows[0].Issue != 8 || !rows[0].Open || rows[0].TriggerKey != "start:8" {
		t.Errorf("runs = %+v, want one open start:8", rows)
	}
	if spawns := w.spawned(); len(spawns) != 1 || spawns[0].Issue != 8 {
		t.Errorf("spawns = %+v, want one for issue 8", spawns)
	}
}

// A handoff decide hands back is the tick's to post: run leaves the issue alone.
func TestRunLeavesReturnToTick(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, w := startable(t, &env)
	srv.Comments = map[int][]map[string]any{7: {handoffComment("/superpowers:brainstorming /work/plans/idea.md", "now")}}
	var errOut bytes.Buffer
	if err := runIssue(context.Background(), env, "o/r#7", db, &errOut); err != nil {
		t.Fatalf("run = %v\n%s", err, errOut.String())
	}
	if want := "lighthouse: nothing to start for o/r#7\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
	if p := writes(srv); len(p) != 0 || len(w.spawned()) != 0 {
		t.Errorf("writes %v, spawns %v; want none", p, w.spawned())
	}
	if rows := history(t, openStore(t, db), "o/r"); len(rows) != 0 {
		t.Errorf("runs = %+v, want none", rows)
	}
}

func TestRunNightDefers(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, w := startable(t, &env)
	srv.Comments = map[int][]map[string]any{7: {handoffComment("/superpowers:subagent-driven-development /work/plans/plan.md", "night")}}
	var errOut bytes.Buffer
	if err := runIssue(context.Background(), env, "o/r#7", db, &errOut); err != nil {
		t.Fatalf("run = %v\n%s", err, errOut.String())
	}
	if want := "lighthouse: deferred o/r#7: night\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q (12:00 is outside the night window 21:00-08:00)", errOut.String(), want)
	}
	if p := writes(srv); len(p) != 0 || len(w.spawned()) != 0 {
		t.Errorf("writes %v, spawns %v; want none for a deferred start", p, w.spawned())
	}
	if rows := history(t, openStore(t, db), "o/r"); len(rows) != 0 {
		t.Errorf("runs = %+v, want none", rows)
	}
}

// The open run asked a question and the answer is there: the close of the run comes along,
// otherwise the resume's claim fails on the open run.
func TestRunClosesThenResumes(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, w := startable(t, &env)
	conn := openStore(t, db)
	const session = "11111111-2222-4333-8444-555555555555"
	started := t0.Add(-2 * time.Hour)
	if _, err := runs.Take(context.Background(), conn, runs.Claim{Repo: "o/r", Issue: 7, SessionID: session,
		Mode: "start", Trigger: "handoff", TriggerKey: "start:7", Command: "/lighthouse-start-issue 7",
		Prompt: "/lighthouse-start-issue 7 (issue #7, controller-run)", Branch: "issue-7-issue", Now: started}); err != nil {
		t.Fatal(err)
	}
	comment := func(id int64, login, at, body string) map[string]any {
		return map[string]any{"id": id, "body": body, "created_at": at, "user": map[string]any{"login": login},
			"html_url": fmt.Sprintf("https://github.com/o/r/issues/7#issuecomment-%d", id)}
	}
	srv.Comments = map[int][]map[string]any{7: {
		comment(61, "lh-bot", "2026-10-31T10:00:00Z", protocol.RenderStarted(protocol.Started{Session: session,
			Worktree: "/work/issue-7-issue", Command: "/lighthouse-start-issue 7", Start: "2026-10-31T10:00:00Z",
			Branch: "issue-7-issue", Runner: "forge", Workspace: "ws-issue-7-issue"}, "en")),
		comment(62, "lh-bot", "2026-10-31T11:00:00Z",
			"🤖 Vraag\n\n**Vraag 1.** Links of rechts?\n\n<sub>session "+session+" · issue #7</sub>"),
		comment(63, "owner", "2026-10-31T11:30:00Z", "Links."),
	}}
	var errOut bytes.Buffer
	if err := runIssue(context.Background(), env, "o/r#7", db, &errOut); err != nil {
		t.Fatalf("run = %v\n%s", err, errOut.String())
	}
	if want := "lighthouse: started o/r#7\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
	rows := history(t, conn, "o/r")
	if len(rows) != 2 {
		t.Fatalf("runs = %+v, want the closed start and the resume", rows)
	}
	if r := rows[0]; r.Open || r.Outcome != runs.Question {
		t.Errorf("first run = %+v, want closed as question", r)
	}
	if r := rows[1]; !r.Open || r.TriggerKey != "answer:63" || r.SessionID != session || r.Mode != "resume" {
		t.Errorf("second run = %+v, want an open resume of answer:63 in session %s", r, session)
	}
	posted := bodies(srv, 7)
	if len(posted) != 4 {
		t.Fatalf("comments on #7 = %q, want one Resumed after the three", posted)
	}
	if h, ok := protocol.ParseHeader(posted[3]); !ok || h.Kind != protocol.KindResumed || h.Session != session {
		t.Errorf("posted %q, want a Resumed of session %s", posted[3], session)
	}
	if spawns := w.spawned(); len(spawns) != 1 || spawns[0].Mode != "resume" || spawns[0].SessionID != session {
		t.Errorf("spawns = %+v, want one resume of session %s", spawns, session)
	}
}

func TestRunUnknownRepo(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	fakeForge(t, &env)
	err := runIssue(context.Background(), env, "o/x#7", db, &bytes.Buffer{})
	if want := "run: o/x is not in [dispatch] repos of " + env.HostToml; err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

func TestRunBadTarget(t *testing.T) {
	for _, target := range []string{"o/r", "o/r#", "o/r#0", "o/r#-1", "o/r#x", "#7", "r#7", "o/r#7#8"} {
		env, db := tickFixture(t, repoDir(t, completeRepo))
		err := runIssue(context.Background(), env, target, db, &bytes.Buffer{})
		if want := "run: " + target + " is not <owner/name>#<issue>"; err == nil || err.Error() != want {
			t.Errorf("target %q: err = %v, want %q", target, err, want)
		}
	}
}

func TestRunCommandInRoot(t *testing.T) {
	var out bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &bytes.Buffer{})
	root.SetArgs([]string{"run", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"lighthouse run <owner/name>#<issue>", "--db string"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help lacks %q:\n%s", want, out.String())
		}
	}
}

func TestRunIgnoresTickLock(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	startable(t, &env)
	dir := filepath.Join(env.Getenv("XDG_RUNTIME_DIR"), "lighthouse")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	held, err := flock.TryLock(filepath.Join(dir, "tick.lock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })
	var errOut bytes.Buffer
	if err := runIssue(context.Background(), env, "o/r#7", db, &errOut); err != nil {
		t.Fatalf("run = %v\n%s", err, errOut.String())
	}
	if want := "lighthouse: started o/r#7\n"; errOut.String() != want {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
}

// claimingWorkplace lets a concurrent tick claim the trigger after run decided and before
// run's own claim.
type claimingWorkplace struct {
	*fakeWorkplace
	claim func()
}

func (w claimingWorkplace) Prepare(ctx context.Context, branch string) (workplace.Workspace, error) {
	w.claim()
	return w.fakeWorkplace.Prepare(ctx, branch)
}

// Review focus 1: the tick wins the claim of start:7; run posts nothing and starts nothing.
func TestRunClaimLostPostsNothing(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, w := startable(t, &env)
	conn := openStore(t, db)
	cw := claimingWorkplace{fakeWorkplace: w, claim: func() {
		_, err := runs.Take(context.Background(), conn, runs.Claim{Repo: "o/r", Issue: 7,
			SessionID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", Mode: "start", Trigger: "handoff",
			TriggerKey: "start:7", Command: "/lighthouse-start-issue 7", Branch: "issue-7-issue", Now: t0})
		if err != nil {
			t.Errorf("claim of the concurrent tick: %v", err)
		}
	}}
	env.Workplace = func(repoconfig.Repo) workplace.Workplace { return cw }
	var errOut bytes.Buffer
	if err := runIssue(context.Background(), env, "o/r#7", db, &errOut); err != nil {
		t.Fatalf("run = %v\n%s", err, errOut.String())
	}
	if !strings.HasSuffix(errOut.String(), "\nlighthouse: nothing to start for o/r#7\n") ||
		!strings.Contains(errOut.String(), "start:7 not claimed") {
		t.Errorf("errOut = %q, want the lost claim and then nothing to start", errOut.String())
	}
	if p := writes(srv); len(p) != 0 || len(w.spawned()) != 0 {
		t.Errorf("writes %v, spawns %v; want none after a lost claim", p, w.spawned())
	}
	if rows := history(t, conn, "o/r"); len(rows) != 1 || rows[0].SessionID != "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee" {
		t.Errorf("runs = %+v, want only the tick's claim", rows)
	}
}

// A failed post closes the run as start_failed: an apply line, no result line, and exit 1.
func TestRunFailedApply(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, w := startable(t, &env)
	srv.Fail = map[string]int{"POST /repos/o/r/issues/7/comments": 500}
	var errOut bytes.Buffer
	if err := runIssue(context.Background(), env, "o/r#7", db, &errOut); !errors.Is(err, errSilent) {
		t.Fatalf("run = %v, want errSilent\n%s", err, errOut.String())
	}
	got := errOut.String()
	if !strings.HasPrefix(got, "lighthouse: apply o/r: o/r#7 start: post: ") || strings.Count(got, "\n") != 1 {
		t.Errorf("errOut = %q, want only an apply line naming the failed post", got)
	}
	if rows := history(t, openStore(t, db), "o/r"); len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.StartFailed {
		t.Errorf("runs = %+v, want one closed as start_failed", rows)
	}
	if n := len(w.spawned()); n != 0 {
		t.Errorf("spawns = %d, want 0", n)
	}
}

// refusingWorkplace refuses every spawn, as a kenn-forge that does not accept the launch.
type refusingWorkplace struct{ *fakeWorkplace }

func (refusingWorkplace) Spawn(context.Context, workplace.Workspace, workplace.LaunchSpec) (workplace.Session, error) {
	return workplace.Session{}, workplace.ErrNotAccepted
}

// A refused launch is not "nothing to start": the Started stands and the run closed as
// start_failed, so the next tick tries again.
func TestRunNotAccepted(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	srv, w := startable(t, &env)
	env.Workplace = func(repoconfig.Repo) workplace.Workplace { return refusingWorkplace{w} }
	var errOut bytes.Buffer
	if err := runIssue(context.Background(), env, "o/r#7", db, &errOut); err != nil {
		t.Fatalf("run = %v\n%s", err, errOut.String())
	}
	if !strings.HasSuffix(errOut.String(), "\nlighthouse: not accepted o/r#7\n") ||
		strings.Contains(errOut.String(), "nothing to start") {
		t.Errorf("errOut = %q, want the result line 'lighthouse: not accepted o/r#7'", errOut.String())
	}
	if posted := bodies(srv, 7); len(posted) != 1 || !strings.HasPrefix(posted[0], "🤖 Started\n") {
		t.Errorf("comments on #7 = %q, want the Started to stay", posted)
	}
	if rows := history(t, openStore(t, db), "o/r"); len(rows) != 1 || rows[0].Open || rows[0].Outcome != runs.StartFailed {
		t.Errorf("runs = %+v, want one closed as start_failed", rows)
	}
}

// `lighthouse run` checks the pause and the usage, not the slots: a person who runs an issue
// decides for themselves.
func TestRunIgnoresSlots(t *testing.T) {
	full := func(t *testing.T, env tickEnv, db string) {
		t.Helper()
		conn := openStore(t, db)
		for i := 1; i <= 4; i++ {
			claim(t, conn, "o/x", i, fmt.Sprintf("sess-x%d", i), "infra", "handoff", fmt.Sprintf("start:%d", i), "")
		}
	}
	t.Run("slots full", func(t *testing.T) {
		env, db := tickFixture(t, repoDir(t, completeRepo))
		_, w := startable(t, &env)
		full(t, env, db)
		var errOut bytes.Buffer
		if err := runIssue(context.Background(), env, "o/r#7", db, &errOut); err != nil {
			t.Fatalf("run = %v\n%s", err, errOut.String())
		}
		if want := "lighthouse: started o/r#7\n"; errOut.String() != want {
			t.Errorf("errOut = %q, want %q", errOut.String(), want)
		}
		if len(w.spawned()) != 1 {
			t.Errorf("spawns = %+v, want one", w.spawned())
		}
	})
	t.Run("paused", func(t *testing.T) {
		env, db := tickFixture(t, repoDir(t, completeRepo))
		_, w := startable(t, &env)
		flag, err := park.PauseFile(env.Getenv)
		if err != nil {
			t.Fatal(err)
		}
		if err := park.WritePause(flag, t0.Add(3*time.Hour)); err != nil {
			t.Fatal(err)
		}
		var errOut bytes.Buffer
		if err := runIssue(context.Background(), env, "o/r#7", db, &errOut); err != nil {
			t.Fatalf("run = %v\n%s", err, errOut.String())
		}
		if want := "lighthouse: nothing to start for o/r#7\n"; errOut.String() != want {
			t.Errorf("errOut = %q, want %q", errOut.String(), want)
		}
		if len(w.spawned()) != 0 {
			t.Errorf("spawns = %+v under a pause, want none", w.spawned())
		}
	})
}

// run passes the markers of its issue to Decide, so the run the guard parked closes as parked
// and the usage resume can claim it.
func TestRunSettlesParked(t *testing.T) {
	env, db := tickFixture(t, repoDir(t, completeRepo))
	startable(t, &env)
	const session = "sess-p"
	conn := openStore(t, db)
	claim(t, conn, "o/r", 7, session, "", "handoff", "start:7", "")
	m := park.Marker{SessionID: session, Cwd: "/work", Window: "session", Source: "guard", Controller: true,
		CreatedAt: t0.Add(-time.Hour).Format(time.RFC3339), ResetAt: t0.Add(time.Hour).Format(time.RFC3339)}
	dir, err := park.MarkerDir(env.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, session+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	if err := runIssue(context.Background(), env, "o/r#7", db, &errOut); err != nil {
		t.Fatalf("run = %v\n%s", err, errOut.String())
	}
	rows := history(t, conn, "o/r")
	if len(rows) == 0 || rows[0].Open || rows[0].Outcome != runs.Parked {
		t.Errorf("runs = %+v, want the first closed as parked\n%s", rows, errOut.String())
	}
}

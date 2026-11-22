package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/scheduled/scheduledtest"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Classification: io (SQLite, fake forge, fake workplace, fake git) through the whole tick. These
// tests fail if the tick stops planning, gating and starting a due schedule under the brake, marks
// a slot skipped on a gate error, starts a schedule twice, or retries a failed start other than
// once on its own branch.

// scheduledToml is completeRepo with schedule codebase-map, daily at 04:00: at t0 (12:00 UTC)
// its slot is 20261031T0400.
const scheduledToml = completeRepo + `
[[schedule]]
name = "codebase-map"
job  = "map-refresh"
cron = "0 4 * * *"
`

const (
	tickSlot = "20261031T0400"
	tickKey  = "scheduled:codebase-map:" + tickSlot
	tickHead = "89abcdef0123456789abcdef0123456789abcdef"
)

// tickGit answers the git a scheduled start and the sweep run: HEAD is tickHead, the git dir
// does not exist, nothing is dirty or unpushed.
func tickGit(_ context.Context, _ time.Duration, _ string, args ...string) (string, int, error) {
	switch {
	case slices.Contains(args, "--verify"):
		return tickHead + "\n", 0, nil
	case slices.Contains(args, "--absolute-git-dir"), slices.Contains(args, "--git-common-dir"):
		return "/nonexistent-lighthouse-test/.git\n", 0, nil
	case slices.Contains(args, "rev-list"):
		return "0\n", 0, nil
	}
	return "", 0, nil
}

// spawnFailWorkplace is a fakeWorkplace whose Spawn fails with spawnErr while it is set.
type spawnFailWorkplace struct {
	*fakeWorkplace
	mu       sync.Mutex
	spawnErr error
	tried    []workplace.LaunchSpec
	prepared []string // the branch per Prepare
}

func (w *spawnFailWorkplace) Prepare(ctx context.Context, branch string) (workplace.Workspace, error) {
	w.mu.Lock()
	w.prepared = append(w.prepared, branch)
	w.mu.Unlock()
	return w.fakeWorkplace.Prepare(ctx, branch)
}

func (w *spawnFailWorkplace) Spawn(ctx context.Context, ws workplace.Workspace, l workplace.LaunchSpec) (workplace.Session, error) {
	w.mu.Lock()
	w.tried = append(w.tried, l)
	err := w.spawnErr
	w.mu.Unlock()
	if err != nil {
		return workplace.Session{}, err
	}
	return w.fakeWorkplace.Spawn(ctx, ws, l)
}

func (w *spawnFailWorkplace) attempts() []workplace.LaunchSpec {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.tried)
}

// scheduledTick is a tick environment with o/r holding schedule codebase-map and issue 7 in
// Controller, the clock at t0, an open brake, job as map-refresh and tickGit as git.
type scheduledTick struct {
	env tickEnv
	srv *forgetest.Server
	db  string
	w   *spawnFailWorkplace
	job *scheduledtest.Job
}

func newScheduledTick(t *testing.T, hostExtra string, others ...string) *scheduledTick {
	t.Helper()
	repos := []string{repoDir(t, scheduledToml)}
	for _, body := range others {
		repos = append(repos, repoDir(t, body))
	}
	env, db := tickFixture(t, repos...)
	if hostExtra != "" {
		appendHostToml(t, env, hostExtra)
	}
	srv, fw := startable(t, &env)
	srv.Issues = nil // a scheduled run has no issue; its PR numbers start at 1
	w := &spawnFailWorkplace{fakeWorkplace: fw}
	env.Workplace = func(r repoconfig.Repo, node, _ string) workplace.Workplace {
		if r.Slug != "o/r" {
			t.Errorf("workplace for unknown repo %s", r.Slug)
		}
		return w
	}
	job := &scheduledtest.Job{JobName: "map-refresh", Plan: scheduled.LaunchPlan{
		Command: "/gsd-map-codebase", Instructions: "Ask nothing.\n", Tools: []string{"Read", "Glob", "Write"},
		MaxRuntime: time.Hour,
	}}
	env.Jobs = func(name string) (scheduled.Job, bool) {
		if name == "map-refresh" {
			return job, true
		}
		return nil, false
	}
	env.Run = tickGit
	return &scheduledTick{env: env, srv: srv, db: db, w: w, job: job}
}

// tick runs one tick and returns its error output; a tick error other than errSilent fails.
func (s *scheduledTick) tick(t *testing.T) string {
	t.Helper()
	var errOut bytes.Buffer
	if err := tick(context.Background(), s.env, s.db, &errOut); err != nil && !errors.Is(err, errSilent) {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	return errOut.String()
}

func (s *scheduledTick) at(when time.Time) { s.env.Now = func() time.Time { return when } }

// scheduledRows are the runs of o/r with a schedule.
func scheduledRows(t *testing.T, db *sql.DB) []runs.Row {
	t.Helper()
	var out []runs.Row
	for _, r := range history(t, db, "o/r") {
		if r.Schedule != "" {
			out = append(out, r)
		}
	}
	return out
}

func gateCalls(job *scheduledtest.Job) int {
	n := 0
	for _, c := range job.Calls() {
		if c == "Gate" {
			n++
		}
	}
	return n
}

func slotSkipped(t *testing.T, db *sql.DB) bool {
	t.Helper()
	ok, err := runs.SlotSkipped(context.Background(), db, "o/r", "codebase-map", tickSlot)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// (a) A due schedule under an open brake starts before the issue: with one slot it takes the slot
// and issue 7 waits.
func TestTickStartsScheduledRunBeforeIssue(t *testing.T) {
	s := newScheduledTick(t, "slots = 1\n")
	out := s.tick(t)
	if !strings.Contains(out, "lighthouse: deferred o/r#7: slots: 1 of 1 in use\n") {
		t.Errorf("errOut = %q, want issue 7 deferred on the slot the scheduled run took", out)
	}
	conn := openStore(t, s.db)
	rows := history(t, conn, "o/r")
	if len(rows) != 1 {
		t.Fatalf("runs = %+v, want only the scheduled run", rows)
	}
	if r := rows[0]; !r.Open || r.Schedule != "codebase-map" || r.Issue != 0 || r.TriggerKey != tickKey ||
		r.Branch != "scheduled/codebase-map-"+tickSlot+"-1" || r.StartSHA != tickHead || r.Profile != "software" {
		t.Errorf("run = %+v, want the open scheduled run of %s", r, tickKey)
	}
	if got := s.job.Calls(); !slices.Equal(got, []string{"Gate", "Launch", "Stage"}) {
		t.Errorf("job calls = %q, want Gate, Launch, Stage", got)
	}
}

// (f) The agent starts with the job's tools and no MCP config, although the profile has one.
func TestTickScheduledSpawnHasToolsAndNoMCP(t *testing.T) {
	s := newScheduledTick(t, "\n[dispatch.profiles.software]\nmcp_config = \"/etc/lh/mcp.json\"\n")
	s.tick(t)
	var spawn *workplace.LaunchSpec
	for _, l := range s.w.spawned() {
		if l.Prompt == "/gsd-map-codebase" {
			spawn = &l
		}
	}
	if spawn == nil {
		t.Fatalf("spawns = %+v, want the scheduled one", s.w.spawned())
	}
	if spawn.MCPConfig != "" || !slices.Equal(spawn.Tools, []string{"Read", "Glob", "Write"}) || spawn.Issue != 0 {
		t.Errorf("spawn = %+v, want tools Read, Glob, Write and no MCP config", *spawn)
	}
	// The issue start next to it keeps the profile's MCP config: only the scheduled run drops it.
	issueSpawns := 0
	for _, l := range s.w.spawned() {
		if l.Issue == 7 {
			issueSpawns++
			if l.MCPConfig != "/etc/lh/mcp.json" {
				t.Errorf("issue spawn = %+v, want the profile's MCP config", l)
			}
		}
	}
	if issueSpawns != 1 {
		t.Errorf("issue 7 spawns = %d, want 1", issueSpawns)
	}
}

// (b) A closed brake claims nothing and leaves the slot open: the next tick, with the brake open,
// starts it.
func TestTickScheduledRunWaitsForTheBrake(t *testing.T) {
	tests := []struct {
		name  string
		close func(t *testing.T, s *scheduledTick, db *sql.DB)
		open  func(t *testing.T, s *scheduledTick)
	}{
		{"pause", func(t *testing.T, s *scheduledTick, _ *sql.DB) {
			flag, err := park.PauseFile(s.env.Getenv)
			if err != nil {
				t.Fatal(err)
			}
			if err := park.WritePause(flag, t0.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, s *scheduledTick) {
			flag, _ := park.PauseFile(s.env.Getenv)
			if _, err := park.ClearPause(flag); err != nil {
				t.Fatal(err)
			}
		}},
		{"usage", func(t *testing.T, s *scheduledTick, _ *sql.DB) { setUsage(t, s.env, 95, 10) },
			func(t *testing.T, s *scheduledTick) { setUsage(t, s.env, 10, 10) }},
		{"memory", func(t *testing.T, s *scheduledTick, _ *sql.DB) { writeMeminfo(t, s.env, 1048576) },
			func(t *testing.T, s *scheduledTick) { writeMeminfo(t, s.env, 33554432) }},
		{"slots", func(t *testing.T, s *scheduledTick, db *sql.DB) {
			if _, err := runs.Take(context.Background(), db, runs.Claim{Repo: "o/other", Issue: 1, SessionID: runs.NewSessionID(),
				Mode: "start", Trigger: "handoff", TriggerKey: "start:1", Now: t0}); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, s *scheduledTick) {
			conn := openStore(t, s.db)
			if _, err := conn.Exec(`UPDATE runs SET outcome = 'done', ended_at = ? WHERE repo = 'o/other'`, t0.Format(time.RFC3339)); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newScheduledTick(t, "slots = 1\n")
			s.srv.BoardNodes = nil // only the scheduled run: no issue defer holds the tick state back
			conn := openStore(t, s.db)
			tc.close(t, s, conn)
			out := s.tick(t)
			if rows := scheduledRows(t, conn); len(rows) != 0 {
				t.Fatalf("scheduled runs = %+v, want none under a closed brake", rows)
			}
			if slotSkipped(t, conn) {
				t.Error("slot marked skipped under a closed brake")
			}
			if tc.name != "pause" && !strings.Contains(out, "lighthouse: deferred o/r "+tickKey+": "+tc.name) {
				t.Errorf("errOut = %q, want the scheduled run deferred on %s", out, tc.name)
			}
			if st := tickState(t, conn, "o/r"); tc.name != "pause" && st.Fingerprint != "" {
				t.Errorf("tick state = %+v after a deferral, want none: the next tick must try again", st)
			}
			if strings.Contains(out, "o/r#0") {
				t.Errorf("errOut = %q names issue 0", out)
			}
			var deferred int
			if err := conn.QueryRow(`SELECT count(*) FROM tick_state WHERE repo = 'o/r' AND kind = 'deferred' AND key = '0'`).Scan(&deferred); err != nil || deferred != 0 {
				t.Errorf("deferred rows keyed 0 = %d, %v; want none", deferred, err)
			}
			// The queue in /api/runs shows the deferral under the schedule; a pause drops it.
			_, views, err := dispatch.LoadView(context.Background(), conn, []string{"o/r"})
			if err != nil {
				t.Fatal(err)
			}
			wantViews := 1
			if tc.name == "pause" {
				wantViews = 0
			}
			if len(views) != wantViews || (wantViews == 1 && (views[0].Schedule != "codebase-map" || views[0].Issue != 0 ||
				!strings.HasPrefix(views[0].Reason, tc.name))) {
				t.Errorf("deferred views = %+v, want %d of schedule codebase-map deferred on %s", views, wantViews, tc.name)
			}

			tc.open(t, s)
			s.at(t0.Add(5 * time.Minute))
			s.tick(t)
			rows := scheduledRows(t, conn)
			if len(rows) != 1 || !rows[0].Open || rows[0].TriggerKey != tickKey {
				t.Errorf("scheduled runs after the brake opened = %+v, want the open run of %s", rows, tickKey)
			}
		})
	}
}

// writeMeminfo writes a meminfo with availableKB of MemAvailable.
func writeMeminfo(t *testing.T, env tickEnv, availableKB int) {
	t.Helper()
	body := fmt.Sprintf("MemTotal: 67108864 kB\nMemAvailable: %d kB\n", availableKB)
	if err := os.WriteFile(env.MemInfo, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// (c) A gate that says skip marks the slot skipped with one log line and starts nothing; a later
// tick does not ask the gate about that slot again.
func TestTickScheduledGateSkip(t *testing.T) {
	s := newScheduledTick(t, "")
	s.job.Verdict = scheduled.Verdict{Skip: "no drift since the last map"}
	out := s.tick(t)
	line := "lighthouse: o/r " + tickKey + ": skipped: no drift since the last map\n"
	if strings.Count(out, "skipped") != 1 || !strings.Contains(out, line) {
		t.Errorf("errOut = %q, want the one line %q", out, line)
	}
	conn := openStore(t, s.db)
	if !slotSkipped(t, conn) {
		t.Error("slot not marked skipped")
	}
	if rows := scheduledRows(t, conn); len(rows) != 0 {
		t.Errorf("scheduled runs = %+v, want none", rows)
	}
	s.job.Verdict = scheduled.Verdict{}
	s.at(t0.Add(time.Hour))
	s.tick(t)
	if n := gateCalls(s.job); n != 1 {
		t.Errorf("gate calls = %d, want 1: a skipped slot is settled", n)
	}
	if rows := scheduledRows(t, conn); len(rows) != 0 {
		t.Errorf("scheduled runs after the skip = %+v, want none", rows)
	}
}

// (d) Review focus 3: a gate that fails does not skip the slot. The error is logged, the tick
// reports a failure, and the next tick asks the gate again and starts.
func TestTickScheduledGateErrorLeavesSlotOpen(t *testing.T) {
	s := newScheduledTick(t, "")
	s.job.GateErr = errors.New("git fetch origin: exit code 128")
	var errOut bytes.Buffer
	err := tick(context.Background(), s.env, s.db, &errOut)
	if !errors.Is(err, errSilent) {
		t.Errorf("tick = %v, want errSilent for a failed gate", err)
	}
	line := "lighthouse: o/r " + tickKey + ": gate: git fetch origin: exit code 128\n"
	if !strings.Contains(errOut.String(), line) {
		t.Errorf("errOut = %q, want %q", errOut.String(), line)
	}
	conn := openStore(t, s.db)
	if slotSkipped(t, conn) {
		t.Error("a failed gate marked the slot skipped")
	}
	if rows := scheduledRows(t, conn); len(rows) != 0 {
		t.Errorf("scheduled runs = %+v, want none", rows)
	}
	if st := tickState(t, conn, "o/r"); st.Fingerprint != "" {
		t.Errorf("tick state = %+v, want none saved: the next tick must gather and gate again", st)
	}

	s.job.GateErr = nil
	s.at(t0.Add(time.Minute))
	s.tick(t)
	if n := gateCalls(s.job); n != 2 {
		t.Errorf("gate calls = %d, want 2", n)
	}
	if rows := scheduledRows(t, conn); len(rows) != 1 || !rows[0].Open {
		t.Errorf("scheduled runs = %+v, want one open run", rows)
	}
}

// (e) While the run is open, a later tick claims nothing and does not gate.
func TestTickScheduledOpenRunBlocksSecondClaim(t *testing.T) {
	s := newScheduledTick(t, "")
	s.tick(t)
	// Stale: a full gather. Within the run's max_runtime of an hour, which would end it.
	s.at(t0.Add(45 * time.Minute))
	out := s.tick(t)
	if !strings.Contains(out, "gathered o/r") {
		t.Fatalf("errOut = %q, want a second gather", out)
	}
	conn := openStore(t, s.db)
	if rows := scheduledRows(t, conn); len(rows) != 1 {
		t.Errorf("scheduled runs = %+v, want one", rows)
	}
	if n := gateCalls(s.job); n != 1 {
		t.Errorf("gate calls = %d, want 1", n)
	}
	if n := len(s.w.attempts()); n != 2 { // the scheduled run and issue 7, once each
		t.Errorf("spawns = %d, want 2", n)
	}
}

// (g) A failed start closes the run as retry; the next tick starts attempt 2 on its own branch;
// when that fails too the run ends failed and the slot is settled.
func TestTickScheduledStartFailureRetriesOnce(t *testing.T) {
	s := newScheduledTick(t, "")
	s.w.spawnErr = fmt.Errorf("target refused: %w", workplace.ErrNotAccepted)
	out := s.tick(t)
	if !strings.Contains(out, "lighthouse: apply o/r: o/r "+tickKey+" scheduled_start: launch not accepted: target refused") {
		t.Errorf("errOut = %q, want the failed start", out)
	}
	conn := openStore(t, s.db)
	rows := scheduledRows(t, conn)
	if len(rows) != 1 || rows[0].Outcome != runs.Retry || rows[0].Attempt != 1 ||
		!strings.HasPrefix(rows[0].Reason, "launch not accepted: target refused") {
		t.Fatalf("after tick 1: runs = %+v, want attempt 1 closed as retry", rows)
	}

	s.at(t0.Add(time.Minute))
	s.tick(t)
	rows = scheduledRows(t, conn)
	if len(rows) != 2 || rows[1].Attempt != 2 || rows[1].Branch != "scheduled/codebase-map-"+tickSlot+"-2" ||
		rows[1].Outcome != runs.Failed {
		t.Fatalf("after tick 2: runs = %+v, want attempt 2 on its own branch, closed failed", rows)
	}
	var scheduledPrepares []string
	s.w.mu.Lock()
	for _, b := range s.w.prepared {
		if strings.HasPrefix(b, "scheduled/") {
			scheduledPrepares = append(scheduledPrepares, b)
		}
	}
	s.w.mu.Unlock()
	if want := []string{"scheduled/codebase-map-" + tickSlot + "-1", "scheduled/codebase-map-" + tickSlot + "-2"}; !slices.Equal(scheduledPrepares, want) {
		t.Errorf("prepared %q, want %q: attempt 2 gets its own workplace", scheduledPrepares, want)
	}

	s.w.spawnErr = nil
	s.at(t0.Add(time.Hour))
	s.tick(t)
	if rows := scheduledRows(t, conn); len(rows) != 2 {
		t.Errorf("after tick 3: runs = %+v, want no third attempt", rows)
	}
	if n := gateCalls(s.job); n != 2 {
		t.Errorf("gate calls = %d, want 2: the settled slot is not gated", n)
	}
}

// (h) A tick that died after its claim left an open row without a workplace or session: the next
// tick neither claims nor spawns for that slot.
func TestTickScheduledDeadClaimIsNotStartedTwice(t *testing.T) {
	s := newScheduledTick(t, "")
	conn := openStore(t, s.db)
	if _, err := runs.Take(context.Background(), conn, runs.Claim{
		Repo: "o/r", Schedule: "codebase-map", SessionID: runs.NewSessionID(), Mode: "start",
		Trigger: runs.TriggerScheduled, TriggerKey: tickKey, Branch: "scheduled/codebase-map-" + tickSlot + "-1",
		Now: t0.Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	s.tick(t)
	if rows := scheduledRows(t, conn); len(rows) != 1 || !rows[0].Open {
		t.Errorf("scheduled runs = %+v, want only the dead tick's open run", rows)
	}
	for _, l := range s.w.attempts() {
		if l.Prompt == "/gsd-map-codebase" {
			t.Errorf("spawned %+v for a slot whose claim is open", l)
		}
	}
	if n := gateCalls(s.job); n != 0 {
		t.Errorf("gate calls = %d, want 0", n)
	}
}

// The slot time breaks the gather-skip: a repo gathered just before 04:00, with nothing changed
// since, is gathered again just after it and its new slot gated.
func TestTickSlotBoundaryRegathers(t *testing.T) {
	s := newScheduledTick(t, "")
	s.srv.BoardNodes = nil // no issue start that changes the fingerprint
	s.job.Verdict = scheduled.Verdict{Skip: "nothing new"}
	s.at(time.Date(2026, 10, 31, 3, 50, 0, 0, time.UTC)) // the slot of 30 October is up
	s.tick(t)
	conn := openStore(t, s.db)
	if st := tickState(t, conn, "o/r"); st.Fingerprint == "" {
		t.Fatalf("tick state = %+v, want the fingerprint saved", st)
	}
	s.at(time.Date(2026, 10, 31, 4, 1, 0, 0, time.UTC))
	out := s.tick(t)
	if !strings.Contains(out, "lighthouse: gathered o/r: 0 items (boundary)\n") {
		t.Errorf("errOut = %q, want a gather on the slot boundary", out)
	}
	if !strings.Contains(out, "lighthouse: o/r "+tickKey+": skipped: nothing new\n") {
		t.Errorf("errOut = %q, want the new slot gated", out)
	}
}

// A schedule naming a job no one registered starts nothing, with a line per tick, but does not
// fail the tick or hold its state: else every tick would exit 1 and gather in full.
func TestTickScheduledUnknownJob(t *testing.T) {
	s := newScheduledTick(t, "")
	s.env.Jobs = func(string) (scheduled.Job, bool) { return nil, false }
	var errOut bytes.Buffer
	if err := tick(context.Background(), s.env, s.db, &errOut); err != nil {
		t.Errorf("tick = %v, want nil: an unknown job is no failure", err)
	}
	if want := "lighthouse: o/r " + tickKey + ": no job map-refresh\n"; !strings.Contains(errOut.String(), want) {
		t.Errorf("errOut = %q, want %q", errOut.String(), want)
	}
	if st := tickState(t, openStore(t, s.db), "o/r"); st.Fingerprint == "" {
		t.Errorf("tick state = %+v, want it saved: no hold on an unknown job", st)
	}
	if rows := scheduledRows(t, openStore(t, s.db)); len(rows) != 0 {
		t.Errorf("scheduled runs = %+v, want none", rows)
	}
}

// Fails if the agent of a scheduled run keeps the token variable of a repo the tick did not
// gather: here o/other names OTHER_TOKEN, which is unset, so its forge does not open.
func TestTickScheduledAgentLosesTheTokenOfEveryRepo(t *testing.T) {
	other := strings.Replace(completeRepo, `repo = "o/r"`, "repo = \"o/other\"\ntoken_env = \"OTHER_TOKEN\"", 1)
	s := newScheduledTick(t, "", other)
	out := s.tick(t)
	if !strings.Contains(out, "skip ") {
		t.Fatalf("errOut = %q, want the second repo skipped (precondition)", out)
	}
	for _, l := range s.w.spawned() {
		if l.Prompt == "/gsd-map-codebase" {
			if !slices.Contains(l.Unset, "OTHER_TOKEN") {
				t.Errorf("Unset = %q, want OTHER_TOKEN of the repo that was not gathered", l.Unset)
			}
			return
		}
	}
	t.Fatalf("spawns = %+v, want the scheduled one", s.w.spawned())
}

// Fails if the token variable of a repo whose config is invalid drops out of the unset list.
func TestTokenEnvsKeepsTheTokenOfAnInvalidRepo(t *testing.T) {
	bad := strings.Replace(completeRepo, `repo = "o/r"`, "repo = \"o/bad\"\ntoken_env = \"BAD_TOKEN\"", 1) +
		"\n[[schedule]]\nname = \"x\"\njob = \"j\"\ncron = \"not a cron\"\n"
	dir := repoDir(t, bad)
	if _, err := repoconfig.Load(dir); err == nil {
		t.Fatal("precondition: the repo config must be invalid")
	}
	good := repoDir(t, strings.Replace(completeRepo, `repo = "o/r"`, "repo = \"o/good\"\ntoken_env = \"GOOD_TOKEN\"", 1))
	got := tokenEnvs(hostconfig.Dispatch{Repos: []string{dir, good}})
	if !slices.Equal(got, []string{"BAD_TOKEN", "GOOD_TOKEN"}) {
		t.Errorf("tokenEnvs = %q, want BAD_TOKEN and GOOD_TOKEN", got)
	}
}

// Classification: wiring. Fails if the real registry lacks the job reflect (the tick then logs
// `no job reflect`) or if the env a job gets in Stage lacks the run history and the home
// directory it needs to tell the kinds of session apart.
func TestScheduledJobGivesReflectItsHistoryAndHome(t *testing.T) {
	if _, ok := scheduled.Lookup("reflect"); !ok {
		t.Fatal("job reflect is not registered")
	}
	s := newScheduledTick(t, "")
	s.env.Getenv = func(k string) string {
		if k == "HOME" {
			return "/home/x"
		}
		return ""
	}
	s.env.Jobs = nil // the real registry
	repo := repoconfig.Repo{Slug: "o/r", Schedules: []repoconfig.Schedule{{Name: "reflect", Job: "reflect"}}}
	if _, err := runs.Take(context.Background(), openStore(t, s.db), runs.Claim{Repo: "o/r", Schedule: "reflect", Mode: "start", Trigger: runs.TriggerScheduled,
		TriggerKey: scheduled.TriggerKey("reflect", "20261118T0100"), SessionID: "sess-1"}); err != nil {
		t.Fatal(err)
	}
	job, jenv, err := scheduledJob(s.env, openStore(t, s.db), map[string]repoconfig.Repo{"o/r": repo})("o/r", "reflect")
	if err != nil || job.Name() != "reflect" {
		t.Fatalf("scheduledJob = %v, %v", job, err)
	}
	if jenv.Home != "/home/x" || len(jenv.History) != 1 || jenv.History[0].SessionID != "sess-1" {
		t.Errorf("env = home %q, history %+v; want the home and the run history", jenv.Home, jenv.History)
	}
}

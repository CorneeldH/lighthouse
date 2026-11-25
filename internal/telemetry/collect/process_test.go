package collect

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/telemetry/transcript"
)

const (
	testRepo = "o/r"
	plansRel = "design/plans"
	known    = "claude-sonnet-5-5"
)

var base = time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.DB.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	return s.DB
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// tline is one transcript line: minutes after base, branch, message id, model, output tokens.
type tline struct {
	min    int
	branch string
	id     string // empty: a user line
	model  string
	out    int
	write  string // file_path of a Write tool use
	ask    string // tool-use id of an AskUserQuestion tool use
	extra  string // spliced into the content blocks of a user line, e.g. a skill text
}

func (l tline) json(sid string) string {
	ts := base.Add(time.Duration(l.min) * time.Minute).Format(time.RFC3339Nano)
	if l.id == "" {
		content := `"hello"`
		if l.extra != "" {
			content = l.extra
		}
		return fmt.Sprintf(`{"type":"user","sessionId":%q,"gitBranch":%q,"timestamp":%q,"message":{"role":"user","content":%s}}`,
			sid, l.branch, ts, content)
	}
	content := `[{"type":"text","text":"x"}]`
	if l.write != "" {
		content = fmt.Sprintf(`[{"type":"tool_use","name":"Write","id":"tu%s","input":{"file_path":%q}}]`, l.id, l.write)
	}
	if l.ask != "" {
		content = fmt.Sprintf(`[{"type":"tool_use","name":"AskUserQuestion","id":%q,"input":{}}]`, l.ask)
	}
	return fmt.Sprintf(`{"type":"assistant","sessionId":%q,"gitBranch":%q,"timestamp":%q,"version":"2.1.0","message":{"id":%q,"model":%q,"content":%s,"usage":{"input_tokens":10,"output_tokens":%d,"cache_read_input_tokens":100,"cache_creation_input_tokens":20}}}`,
		sid, l.branch, ts, l.id, l.model, content, l.out)
}

func source(sid string, mtime time.Time, lines ...tline) Source {
	js := make([]string, len(lines))
	for i, l := range lines {
		js[i] = l.json(sid)
	}
	return Source{
		SessionID: sid, TranscriptPath: "/t/" + sid + ".jsonl",
		Files:    []transcript.File{{Name: sid + ".jsonl", R: strings.NewReader(strings.Join(js, "\n") + "\n")}},
		MTime:    mtime,
		Recorded: base.Add(3 * time.Hour),
	}
}

func opts(dir string, now time.Time) Options {
	return Options{
		Repo: testRepo, DefaultBranch: "main", PlansDir: plansRel, RepoDir: dir, StateDir: dir,
		Now: func() time.Time { return now },
	}
}

func process(t *testing.T, db *sql.DB, src Source, opt Options) Outcome {
	t.Helper()
	out, err := Process(context.Background(), db, src, opt)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	return out
}

// takeRun claims an issue run at base+startMin and, when endMin >= 0, ends it then.
func takeRun(t *testing.T, db *sql.DB, issue int, sid, key string, startMin, endMin int) int64 {
	t.Helper()
	ctx := context.Background()
	r, err := runs.Take(ctx, db, runs.Claim{Repo: testRepo, Issue: issue, SessionID: sid, Mode: "start",
		Trigger: "handoff", TriggerKey: key, Now: base.Add(time.Duration(startMin) * time.Minute)})
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if endMin >= 0 {
		if err := runs.Release(ctx, db, r.ID, runs.Handoff, "", base.Add(time.Duration(endMin)*time.Minute)); err != nil {
			t.Fatalf("Release: %v", err)
		}
	}
	return r.ID
}

func sessionBranchLines() []tline {
	return []tline{
		{min: 0, branch: "issue-7-x"},
		{min: 1, branch: "issue-7-x", id: "m1", model: known, out: 50},
		{min: 2, branch: "issue-7-x", id: "m2", model: "mystery-model", out: 30},
	}
}

func TestProcessSessionWritesRows(t *testing.T) {
	db := openDB(t)
	now := base.Add(5 * time.Hour)
	out := process(t, db, source("s1", now.Add(-10*time.Minute),
		tline{min: 0, branch: "main"},
		tline{min: 1, branch: "main", id: "m1", model: known, out: 50},
		tline{min: 2, branch: "main", id: "m2", model: "other-model", out: 30}), opts(t.TempDir(), now))
	if out.Kind != "session" || out.SessionID == 0 {
		t.Fatalf("outcome %+v", out)
	}
	var reason, srcID string
	if err := db.QueryRow(`SELECT unlinked_reason, source_id FROM sessions WHERE id = ?`, out.SessionID).Scan(&reason, &srcID); err != nil {
		t.Fatal(err)
	}
	if reason != "main_only" || srcID != "transcript:s1" {
		t.Errorf("reason %q, source_id %q", reason, srcID)
	}
	if n := count(t, db, `SELECT count(*) FROM setups WHERE session_id = ? AND source_id = 'transcript:s1' AND source = 'transcript' AND harness = 'claude-code'`, out.SessionID); n != 1 {
		t.Errorf("%d setups rows", n)
	}
	if n := count(t, db, `SELECT count(*) FROM usage WHERE session_id = ? AND source = 'transcript'`, out.SessionID); n != 2 {
		t.Errorf("%d usage rows, want one per model", n)
	}
	var in, outTok, cr, cw int64
	if err := db.QueryRow(`SELECT input_tokens, output_tokens, cache_read_tokens, cache_write_tokens FROM usage
		WHERE session_id = ? AND source_id = 'transcript:s1:`+known+`'`, out.SessionID).Scan(&in, &outTok, &cr, &cw); err != nil {
		t.Fatal(err)
	}
	if in != 10 || outTok != 50 || cr != 100 || cw != 20 {
		t.Errorf("tokens %d %d %d %d", in, outTok, cr, cw)
	}
}

func TestProcessRunMakesNoSession(t *testing.T) {
	db := openDB(t)
	rid := takeRun(t, db, 7, "s1", "start:7", 0, -1)
	out := process(t, db, source("s1", base.Add(time.Minute), sessionBranchLines()...), opts(t.TempDir(), base.Add(time.Hour)))
	if out.Kind != "run" || len(out.RunIDs) != 1 || out.RunIDs[0] != rid {
		t.Fatalf("outcome %+v", out)
	}
	if n := count(t, db, `SELECT count(*) FROM sessions`); n != 0 {
		t.Errorf("%d sessions rows for a run", n)
	}
	if n := count(t, db, `SELECT count(*) FROM setups WHERE run_id = ? AND session_id IS NULL`, rid); n != 1 {
		t.Errorf("%d run setups", n)
	}
	if n := count(t, db, `SELECT count(*) FROM usage WHERE run_id = ?`, rid); n != 2 {
		t.Errorf("%d run usage rows", n)
	}
}

func TestProcessTwoRunsSameSession(t *testing.T) {
	db := openDB(t)
	r1 := takeRun(t, db, 7, "s1", "start:7", 0, 10)
	r2 := takeRun(t, db, 7, "s1", "resume:7", 20, -1)
	lines := []tline{
		{min: 1, id: "a1", model: known, out: 100},
		{min: 5, id: "a2", model: known, out: 200},
		{min: 12, id: "gap", model: known, out: 7}, // between the runs: goes to the earlier run
		{min: 21, id: "b1", model: known, out: 400},
		{min: 25, id: "b2", model: "other-model", out: 800},
	}
	out := process(t, db, source("s1", base.Add(time.Minute), lines...), opts(t.TempDir(), base.Add(time.Hour)))
	if out.Kind != "run" || len(out.RunIDs) != 2 {
		t.Fatalf("outcome %+v", out)
	}
	outputs := func(run int64, model string) int {
		return count(t, db, `SELECT coalesce(sum(output_tokens), 0) FROM usage WHERE run_id = ? AND model = ?`, run, model)
	}
	if got := outputs(r1, known); got != 307 {
		t.Errorf("run 1 output %d, want 307", got)
	}
	if got := outputs(r2, known); got != 400 {
		t.Errorf("run 2 output %d, want 400", got)
	}
	if got := outputs(r2, "other-model"); got != 800 {
		t.Errorf("run 2 other output %d, want 800", got)
	}
	if got := count(t, db, `SELECT sum(output_tokens) FROM usage`); got != 100+200+7+400+800 {
		t.Errorf("sum over both runs %d, want the transcript total 1507", got)
	}
	if n := count(t, db, `SELECT count(DISTINCT source_id) FROM setups`); n != 2 {
		t.Errorf("setups of divided transcript need distinct source ids, got %d", n)
	}
	var sid string
	if err := db.QueryRow(`SELECT source_id FROM setups WHERE run_id = ?`, r2).Scan(&sid); err != nil || sid != fmt.Sprintf("transcript:s1:%d", r2) {
		t.Errorf("setup source_id %q (%v)", sid, err)
	}
}

func TestProcessSecondPassGrown(t *testing.T) {
	db := openDB(t)
	dir := t.TempDir()
	now := base.Add(5 * time.Hour)
	first := sessionBranchLines()
	out1 := process(t, db, source("s1", now.Add(-time.Minute), first...), opts(dir, now))
	counts := func() [3]int {
		return [3]int{count(t, db, `SELECT count(*) FROM sessions`), count(t, db, `SELECT count(*) FROM setups`), count(t, db, `SELECT count(*) FROM usage`)}
	}
	before := counts()
	grown := append(first, tline{min: 3, branch: "issue-7-x", id: "m3", model: known, out: 70})
	src := source("s1", now, grown...)
	src.Recorded = base.Add(4 * time.Hour)
	out2 := process(t, db, src, opts(dir, now))
	if out2.SessionID != out1.SessionID || counts() != before {
		t.Fatalf("grown pass changed row counts: %v -> %v", before, counts())
	}
	if got := count(t, db, `SELECT output_tokens FROM usage WHERE source_id = 'transcript:s1:`+known+`'`); got != 120 {
		t.Errorf("output after growth %d, want 120", got)
	}
	if got := count(t, db, `SELECT count(*) FROM usage WHERE recorded_at = ?`, src.Recorded.UTC().Format(time.RFC3339)); got != 2 {
		t.Errorf("%d usage rows carry the new recorded_at", got)
	}
	// An unchanged transcript read again: counts and values stay.
	process(t, db, source("s1", now, grown...), opts(dir, now))
	if counts() != before {
		t.Errorf("unchanged pass changed counts: %v", counts())
	}
	if got := count(t, db, `SELECT output_tokens FROM usage WHERE source_id = 'transcript:s1:`+known+`'`); got != 120 {
		t.Errorf("output after an unchanged pass %d, want 120", got)
	}
}

func TestProcessNoDuplicateAcrossRunAndSession(t *testing.T) {
	db := openDB(t)
	dir := t.TempDir()
	rid := takeRun(t, db, 7, "s1", "start:7", 0, -1)
	for i := 0; i < 2; i++ {
		process(t, db, source("s1", base.Add(time.Minute), sessionBranchLines()...), opts(dir, base.Add(time.Hour)))
	}
	if n := count(t, db, `SELECT count(*) FROM sessions`); n != 0 {
		t.Errorf("%d sessions rows next to a run", n)
	}
	if n := count(t, db, `SELECT count(*) FROM usage WHERE session_id IS NOT NULL`); n != 0 {
		t.Errorf("%d session usage rows next to a run", n)
	}
	if n := count(t, db, `SELECT count(*) FROM usage WHERE run_id = ?`, rid); n != 2 {
		t.Errorf("%d run usage rows", n)
	}
	// A transcript without a run makes a session and no run rows.
	db2 := openDB(t)
	process(t, db2, source("s2", base.Add(time.Minute), sessionBranchLines()...), opts(dir, base.Add(time.Hour)))
	if n := count(t, db2, `SELECT count(*) FROM usage WHERE run_id IS NOT NULL`) + count(t, db2, `SELECT count(*) FROM setups WHERE run_id IS NOT NULL`); n != 0 {
		t.Errorf("%d run rows for a session", n)
	}
}

func TestProcessRunStartedAfterTranscriptEnd(t *testing.T) {
	db := openDB(t)
	takeRun(t, db, 7, "s1", "start:7", 90, -1)
	out := process(t, db, source("s1", base, sessionBranchLines()...), opts(t.TempDir(), base.Add(3*time.Hour)))
	if out.Kind != "skipped" || out.Reason == "" {
		t.Fatalf("outcome %+v", out)
	}
	if n := count(t, db, `SELECT count(*) FROM sessions`) + count(t, db, `SELECT count(*) FROM setups`); n != 0 {
		t.Errorf("%d rows written for a run that started after the transcript", n)
	}
}

func TestProcessEndedAt(t *testing.T) {
	db := openDB(t)
	dir := t.TempDir()
	now := base.Add(5 * time.Hour)
	ended := func() sql.NullString {
		var s sql.NullString
		if err := db.QueryRow(`SELECT ended_at FROM sessions`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	process(t, db, source("s1", now.Add(-10*time.Minute), sessionBranchLines()...), opts(dir, now))
	if e := ended(); e.Valid {
		t.Errorf("ended_at %q after 10 minutes, want NULL", e.String)
	}
	process(t, db, source("s1", now.Add(-2*time.Hour), sessionBranchLines()...), opts(dir, now))
	if e := ended(); e.String != "2026-11-24T10:02:00Z" {
		t.Errorf("ended_at %v after 2 hours, want the last timestamp", e)
	}
	grown := append(sessionBranchLines(), tline{min: 3, branch: "issue-7-x", id: "m3", model: known, out: 1})
	process(t, db, source("s1", now.Add(-time.Minute), grown...), opts(dir, now))
	if e := ended(); e.Valid {
		t.Errorf("ended_at %q after growth, want NULL again", e.String)
	}
}

func TestProcessSinceLastReopensOpenSession(t *testing.T) {
	db := openDB(t)
	dir := t.TempDir()
	// The first pass reads while the transcript is still fresh (recorded_at after the mtime).
	first := base.Add(2 * time.Hour)
	src := source("s1", first.Add(-time.Minute), sessionBranchLines()...)
	src.Recorded = first
	process(t, db, src, opts(dir, first))
	var ended sql.NullString
	read := func() {
		if err := db.QueryRow(`SELECT ended_at FROM sessions`).Scan(&ended); err != nil {
			t.Fatal(err)
		}
	}
	read()
	if ended.Valid {
		t.Fatalf("ended_at set on a fresh session")
	}
	// An hour after the last write: recorded_at is newer than the mtime, but the open session is revisited.
	now := first.Add(time.Hour)
	o := opts(dir, now)
	o.SinceLast = true
	src = source("s1", first.Add(-time.Minute), sessionBranchLines()...)
	src.Recorded = now
	out := process(t, db, src, o)
	if out.Kind != "session" {
		t.Fatalf("open session was not reprocessed: %+v", out)
	}
	read()
	if !ended.Valid {
		t.Errorf("ended_at still NULL after the hour")
	}
	// Now it is closed: the same call is skipped.
	src = source("s1", first.Add(-time.Minute), sessionBranchLines()...)
	src.Recorded = now.Add(time.Minute)
	if out := process(t, db, src, o); out.Kind != "skipped" {
		t.Errorf("closed session reprocessed: %+v", out)
	}
}

func TestProcessRollbackLeavesNothing(t *testing.T) {
	db := openDB(t)
	// The usage write fails after the session and the setup are written: a trigger raises on the
	// usage insert, so only a rollback of the whole transcript leaves nothing behind.
	if _, err := db.Exec(`CREATE TRIGGER fail_usage BEFORE INSERT ON usage BEGIN SELECT RAISE(ABORT, 'forced'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := Process(context.Background(), db, source("s1", base.Add(time.Minute), sessionBranchLines()...), opts(t.TempDir(), base.Add(time.Hour)))
	if err == nil {
		t.Fatal("no error from the failing usage write")
	}
	for _, table := range []string{"sessions", "setups", "usage", "trajectories"} {
		if n := count(t, db, `SELECT count(*) FROM `+table); n != 0 {
			t.Errorf("%s holds %d rows after a failed transcript", table, n)
		}
	}
}

func TestProcessMissingValuesNull(t *testing.T) {
	db := openDB(t)
	dir := t.TempDir()
	now := base.Add(5 * time.Hour)
	process(t, db, source("s1", now, sessionBranchLines()...), opts(dir, now))
	var sp, lh, pack, lane sql.NullString
	row := func() {
		if err := db.QueryRow(`SELECT superpowers_version, lighthouse_version, pack_version, lane FROM setups`).Scan(&sp, &lh, &pack, &lane); err != nil {
			t.Fatal(err)
		}
	}
	row()
	if sp.Valid || lh.Valid || pack.Valid || lane.Valid {
		t.Errorf("missing values stored: %v %v %v %v", sp, lh, pack, lane)
	}
	if n := count(t, db, `SELECT count(*) FROM setups WHERE superpowers_version = '' OR lighthouse_version = '' OR pack_version = '' OR skills_hash = '' OR effort = ''`); n != 0 {
		t.Errorf("%d empty-string values", n)
	}
	// With a skill load and a sidecar the values arrive.
	skill := `[{"type":"text","text":"Base directory for this skill: /home/u/.claude/plugins/cache/x/superpowers/5.1.0/skills/subagent-driven-development\nbody\nARGUMENTS: 7"}]`
	lines := append(sessionBranchLines(), tline{min: 1, branch: "issue-7-x", extra: skill})
	if err := AppendSidecar(dir, "s1", Observation{At: base, LighthouseVersion: "0.9.1", PackVersion: "0.9.0"}); err != nil {
		t.Fatal(err)
	}
	process(t, db, source("s1", now, lines...), opts(dir, now))
	row()
	if !sp.Valid || sp.String != "5.1.0" || lh.String != "0.9.1" || pack.String != "0.9.0" || lane.String != "sdd" {
		t.Errorf("filled values: %v %v %v %v", sp, lh, pack, lane)
	}
}

func TestProcessUnknownModelCostNull(t *testing.T) {
	db := openDB(t)
	now := base.Add(5 * time.Hour)
	process(t, db, source("s1", now, sessionBranchLines()...), opts(t.TempDir(), now))
	var cost sql.NullFloat64
	var kind sql.NullString
	var output int64
	if err := db.QueryRow(`SELECT cost, cost_kind, output_tokens FROM usage WHERE model = 'mystery-model'`).Scan(&cost, &kind, &output); err != nil {
		t.Fatal(err)
	}
	if cost.Valid || kind.Valid || output != 30 {
		t.Errorf("unknown model: cost %v kind %v output %d", cost, kind, output)
	}
	if err := db.QueryRow(`SELECT cost, cost_kind FROM usage WHERE model = ?`, known).Scan(&cost, &kind); err != nil {
		t.Fatal(err)
	}
	if !cost.Valid || cost.Float64 <= 0 || kind.String != "estimate" {
		t.Errorf("known model: cost %v kind %v", cost, kind)
	}
}

// planRepo makes a clone-like dir with plan files in the working tree.
func writePlan(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, plansRel, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func planOf(t *testing.T, db *sql.DB) (tasks sql.NullInt64, path sql.NullString) {
	t.Helper()
	if err := db.QueryRow(`SELECT plan_tasks, plan_path FROM trajectories WHERE issue = 7`).Scan(&tasks, &path); err != nil {
		t.Fatal(err)
	}
	return tasks, path
}

func TestProcessPlanTasks(t *testing.T) {
	six := "# P\n## Task 1\n## Task 2\n## Task 3\n## Task 4\n## Task 5\n## Task 6\n"
	now := base.Add(5 * time.Hour)

	t.Run("written plan", func(t *testing.T) {
		db, dir := openDB(t), t.TempDir()
		p := writePlan(t, dir, "2026-11-24-issue-7-plan.md", six)
		lines := append(sessionBranchLines(), tline{min: 2, branch: "issue-7-x", id: "w1", model: known, out: 1, write: p})
		process(t, db, source("s1", now, lines...), opts(dir, now))
		tasks, path := planOf(t, db)
		if !tasks.Valid || tasks.Int64 != 6 || path.String != plansRel+"/2026-11-24-issue-7-plan.md" {
			t.Errorf("plan %v %v", tasks, path)
		}
	})
	t.Run("no plan", func(t *testing.T) {
		db, dir := openDB(t), t.TempDir()
		process(t, db, source("s1", now, sessionBranchLines()...), opts(dir, now))
		tasks, path := planOf(t, db)
		if tasks.Valid || path.Valid {
			t.Errorf("plan %v %v, want NULL", tasks, path)
		}
	})
	t.Run("last written wins", func(t *testing.T) {
		db, dir := openDB(t), t.TempDir()
		a := writePlan(t, dir, "a-plan.md", "## Task 1\n")
		b := writePlan(t, dir, "b-plan.md", "## Task 1\n## Task 2\n## Task 3\n")
		lines := append(sessionBranchLines(),
			tline{min: 3, branch: "issue-7-x", id: "w1", model: known, out: 1, write: b},
			tline{min: 4, branch: "issue-7-x", id: "w2", model: known, out: 1, write: a})
		process(t, db, source("s1", now, lines...), opts(dir, now))
		tasks, path := planOf(t, db)
		if tasks.Int64 != 1 || path.String != plansRel+"/a-plan.md" {
			t.Errorf("plan %v %v, want the later write a-plan.md", tasks, path)
		}
	})
	t.Run("name fallback", func(t *testing.T) {
		db, dir := openDB(t), t.TempDir()
		writePlan(t, dir, "2026-11-20-issue-7-other.md", "## Taak 1\n## Taak 2\n")
		writePlan(t, dir, "2026-11-20-issue-8-other.md", "## Taak 1\n")
		process(t, db, source("s1", now, sessionBranchLines()...), opts(dir, now))
		tasks, path := planOf(t, db)
		if tasks.Int64 != 2 || path.String != plansRel+"/2026-11-20-issue-7-other.md" {
			t.Errorf("plan %v %v", tasks, path)
		}
	})
	t.Run("default branch", func(t *testing.T) {
		db, dir := openDB(t), t.TempDir()
		git := func(args ...string) {
			t.Helper()
			cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
			cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		git("init", "-q", "-b", "main")
		p := writePlan(t, dir, "2026-11-24-issue-7-plan.md", six)
		git("add", "-A")
		git("commit", "-q", "-m", "plan")
		git("update-ref", "refs/remotes/origin/main", "HEAD")
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		lines := append(sessionBranchLines(), tline{min: 2, branch: "issue-7-x", id: "w1", model: known, out: 1, write: p})
		process(t, db, source("s1", now, lines...), opts(dir, now))
		if tasks, _ := planOf(t, db); tasks.Int64 != 6 {
			t.Errorf("plan from origin/main: %v", tasks)
		}
	})
}

func TestProcessRawBytesGrowingRun(t *testing.T) {
	db := openDB(t)
	dir := t.TempDir()
	rid := takeRun(t, db, 7, "s1", "start:7", 0, -1) // open run
	o := opts(dir, base.Add(time.Hour))
	o.SinceLast = true
	small := sessionBranchLines()
	process(t, db, source("s1", time.Time{}, small...), o)
	grown := append(small, tline{min: 3, id: "m3", model: known, out: 70})
	out := process(t, db, source("s1", time.Time{}, grown...), o)
	if out.Kind != "run" {
		t.Fatalf("open run with raw bytes skipped: %+v", out)
	}
	if got := count(t, db, `SELECT output_tokens FROM usage WHERE run_id = ? AND model = ?`, rid, known); got != 120 {
		t.Errorf("usage not updated: %d, want 120", got)
	}
	// End the run before the setup was recorded: still reprocessed (recorded_at < ended_at).
	if err := runs.Release(context.Background(), db, rid, runs.Handoff, "", base.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if out := process(t, db, source("s1", time.Time{}, grown...), o); out.Kind != "run" {
		t.Fatalf("run ended after its last pass was skipped: %+v", out)
	}
	// Recorded after the end: now it is skipped.
	src := source("s1", time.Time{}, grown...)
	src.Recorded = base.Add(5 * time.Hour)
	process(t, db, src, o)
	out = process(t, db, source("s1", time.Time{}, grown...), o)
	if out.Kind != "skipped" {
		t.Errorf("closed run with a setup after its end not skipped: %+v", out)
	}
}

func TestProcessSessionKeepsLinkOnRefusal(t *testing.T) {
	db := openDB(t)
	dir := t.TempDir()
	now := base.Add(5 * time.Hour)
	out := process(t, db, source("s1", now, sessionBranchLines()...), opts(dir, now))
	if _, err := db.Exec(`INSERT INTO findings (source, source_id, recorded_at, trajectory_id, session_id, component, finding_key, severity, record_path, record_sum)
		SELECT 'review', 'f1', '2026-11-24T10:00:00Z', trajectory_id, id, 'c', 'k', 'minor', 'p', 's' FROM sessions WHERE id = ?`, out.SessionID); err != nil {
		t.Fatal(err)
	}
	moved := []tline{{min: 0, branch: "issue-9-y"}, {min: 1, branch: "issue-9-y", id: "m1", model: known, out: 50}}
	got, err := Process(context.Background(), db, source("s1", now.Add(time.Minute), moved...), opts(dir, now))
	if err != nil {
		t.Fatalf("refusal is an error: %v", err)
	}
	if got.Kind != "skipped" || got.Reason == "" {
		t.Fatalf("outcome %+v", got)
	}
	var issue int
	if err := db.QueryRow(`SELECT t.issue FROM sessions s JOIN trajectories t ON t.id = s.trajectory_id`).Scan(&issue); err != nil || issue != 7 {
		t.Errorf("session issue %d (%v), want to stay on 7", issue, err)
	}
	if n := count(t, db, `SELECT count(*) FROM trajectories WHERE issue = 9`); n != 0 {
		t.Errorf("refused move left a trajectory 9")
	}
}

// twoTranscripts processes a session that wrote a plan and one that wrote none, both on issue 7,
// in the given order, and returns the plan stored at the end.
func twoTranscripts(t *testing.T, aFirst, withNamed bool) (sql.NullInt64, sql.NullString) {
	t.Helper()
	db, dir := openDB(t), t.TempDir()
	now := base.Add(5 * time.Hour)
	written := writePlan(t, dir, "2026-11-24-design.md", "## Task 1\n## Task 2\n## Task 3\n")
	if withNamed {
		writePlan(t, dir, "2026-11-20-issue-7-other.md", "## Task 1\n")
	}
	a := source("sA", now, append(sessionBranchLines(), tline{min: 3, branch: "issue-7-x", id: "w1", model: known, out: 1, write: written})...)
	b := source("sB", now, sessionBranchLines()...)
	order := []Source{a, b}
	if !aFirst {
		order = []Source{b, a}
	}
	for _, s := range order {
		process(t, db, s, opts(dir, now))
	}
	return planOf(t, db)
}

func TestProcessPlanSurvivesOtherTranscriptOfTrajectory(t *testing.T) {
	for _, aFirst := range []bool{true, false} {
		tasks, path := twoTranscripts(t, aFirst, false)
		if tasks.Int64 != 3 || path.String != plansRel+"/2026-11-24-design.md" {
			t.Errorf("A first %v: plan %v %v, want the plan A wrote", aFirst, tasks, path)
		}
	}
}

func TestProcessNameFallbackDoesNotReplaceWrittenPlan(t *testing.T) {
	for _, aFirst := range []bool{true, false} {
		tasks, path := twoTranscripts(t, aFirst, true)
		if tasks.Int64 != 3 || path.String != plansRel+"/2026-11-24-design.md" {
			t.Errorf("A first %v: plan %v %v, want the written plan over the issue-7- file", aFirst, tasks, path)
		}
	}
}

func TestProcessPlanOutsideRepoIgnored(t *testing.T) {
	db, dir, other := openDB(t), t.TempDir(), t.TempDir()
	now := base.Add(5 * time.Hour)
	foreign := writePlan(t, other, "x.md", "## Task 1\n## Task 2\n")
	lines := append(sessionBranchLines(), tline{min: 3, branch: "issue-7-x", id: "w1", model: known, out: 1, write: foreign})
	process(t, db, source("s1", now, lines...), opts(dir, now))
	if tasks, path := planOf(t, db); tasks.Valid || path.Valid {
		t.Errorf("plan from another repo counted: %v %v", tasks, path)
	}
}

// answerTo is the extra content of a user line that answers the AskUserQuestion with this id.
func answerTo(id string) string {
	return fmt.Sprintf(`[{"type":"tool_result","tool_use_id":%q,"content":"ok"}]`, id)
}

func questionLines(answered bool) []tline {
	ls := []tline{
		{min: 0, branch: "issue-7-x"},
		{min: 1, branch: "issue-7-x", id: "m1", model: known, out: 50, ask: "q1"},
	}
	if answered {
		ls = append(ls, tline{min: 5, branch: "issue-7-x", extra: answerTo("q1")})
	}
	return ls
}

type eventRow struct {
	kind, ref, sourceID, at string
}

func eventsOf(t *testing.T, db *sql.DB) []eventRow {
	t.Helper()
	rows, err := db.Query(`SELECT kind, ref, source_id, at FROM trajectory_events ORDER BY kind DESC, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []eventRow
	for rows.Next() {
		var e eventRow
		if err := rows.Scan(&e.kind, &e.ref, &e.sourceID, &e.at); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestProcessQuestionAndAnswerClosedSession(t *testing.T) {
	db := openDB(t)
	now := base.Add(5 * time.Hour)
	process(t, db, source("s1", now.Add(-2*time.Hour), questionLines(true)...), opts(t.TempDir(), now))
	got := eventsOf(t, db)
	want := []eventRow{
		{"question", "s1:q1", "transcript:s1:q1:question", "2026-11-24T10:01:00Z"},
		{"answer", "s1:q1", "transcript:s1:q1:answer", "2026-11-24T10:05:00Z"},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("events = %+v, want %+v", got, want)
	}
	if n := count(t, db, `SELECT count(*) FROM trajectory_events WHERE session_id IS NULL OR run_id IS NOT NULL`); n != 0 {
		t.Errorf("%d events not owned by the session alone", n)
	}
}

func TestProcessQuestionsSecondPassAddsNothing(t *testing.T) {
	db := openDB(t)
	now := base.Add(5 * time.Hour)
	for i := 0; i < 2; i++ {
		process(t, db, source("s1", now.Add(-2*time.Hour), questionLines(true)...), opts(t.TempDir(), now))
	}
	if n := count(t, db, `SELECT count(*) FROM trajectory_events`); n != 2 {
		t.Errorf("events after two passes = %d, want 2", n)
	}
}

func TestProcessOpenSessionWritesNoEvents(t *testing.T) {
	db := openDB(t)
	now := base.Add(5 * time.Hour)
	process(t, db, source("s1", now.Add(-10*time.Minute), questionLines(true)...), opts(t.TempDir(), now))
	if n := count(t, db, `SELECT count(*) FROM trajectory_events`); n != 0 {
		t.Errorf("open session wrote %d events, want 0", n)
	}
}

func TestProcessRunWritesNoEvents(t *testing.T) {
	db := openDB(t)
	takeRun(t, db, 7, "s1", "start:7", 0, 30)
	now := base.Add(5 * time.Hour)
	process(t, db, source("s1", now.Add(-2*time.Hour), questionLines(true)...), opts(t.TempDir(), now))
	if n := count(t, db, `SELECT count(*) FROM trajectory_events`); n != 0 {
		t.Errorf("run transcript wrote %d events, want 0", n)
	}
}

func TestProcessUnansweredQuestion(t *testing.T) {
	db := openDB(t)
	now := base.Add(5 * time.Hour)
	process(t, db, source("s1", now.Add(-2*time.Hour), questionLines(false)...), opts(t.TempDir(), now))
	got := eventsOf(t, db)
	if len(got) != 1 || got[0].kind != "question" {
		t.Fatalf("events = %+v, want only the question", got)
	}
	// The answer arrives in a later pass: the question stays one row, the answer is added.
	process(t, db, source("s1", now.Add(-time.Hour), questionLines(true)...), opts(t.TempDir(), now))
	if got := eventsOf(t, db); len(got) != 2 || got[1].kind != "answer" {
		t.Errorf("events after the answer = %+v, want question and answer", got)
	}
}

func TestProcessSessionHasEventsKeepsLink(t *testing.T) {
	db := openDB(t)
	now := base.Add(5 * time.Hour)
	process(t, db, source("s1", now.Add(-2*time.Hour), questionLines(true)...), opts(t.TempDir(), now))
	moved := []tline{{min: 0, branch: "issue-9-y"}, {min: 1, branch: "issue-9-y", id: "m1", model: known, out: 50}}
	got, err := Process(context.Background(), db, source("s1", now.Add(-time.Hour), moved...), opts(t.TempDir(), now))
	if err != nil {
		t.Fatalf("refusal is an error: %v", err)
	}
	if got.Kind != "skipped" || got.Reason == "" {
		t.Fatalf("outcome %+v", got)
	}
	var issue int
	if err := db.QueryRow(`SELECT t.issue FROM sessions s JOIN trajectories t ON t.id = s.trajectory_id`).Scan(&issue); err != nil || issue != 7 {
		t.Errorf("session issue %d (%v), want to stay on 7", issue, err)
	}
	if n := count(t, db, `SELECT count(*) FROM trajectories WHERE issue = 9`); n != 0 {
		t.Errorf("refused move left a trajectory 9")
	}
}

// TestProcessScheduledArchitectureScanUsage proves a scheduled architecture-scan run lands in the
// telemetry like any other run: a run row without an issue, tokens and cost per model (#386, AC 7).
func TestProcessScheduledArchitectureScanUsage(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	r, err := runs.Take(ctx, db, runs.Claim{Repo: testRepo, Schedule: "architecture-scan", SessionID: "scan1",
		Mode: "start", Trigger: runs.TriggerScheduled, TriggerKey: "scheduled:architecture-scan:2026-11-24",
		Now: base})
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	lines := []tline{
		{min: 0},
		{min: 1, id: "m1", model: known, out: 50},
		{min: 2, id: "m2", model: known, out: 70},
	}
	out := process(t, db, source("scan1", base.Add(5*time.Minute), lines...), opts(t.TempDir(), base.Add(time.Hour)))
	if out.Kind != "run" || len(out.RunIDs) != 1 || out.RunIDs[0] != r.ID {
		t.Fatalf("outcome %+v", out)
	}
	var in, outTok int
	var cost sql.NullFloat64
	if err := db.QueryRow(`SELECT sum(input_tokens), sum(output_tokens), sum(cost) FROM usage WHERE run_id = ?`, r.ID).
		Scan(&in, &outTok, &cost); err != nil {
		t.Fatalf("usage: %v", err)
	}
	if in != 20 || outTok != 120 || !cost.Valid || cost.Float64 <= 0 {
		t.Errorf("usage in=%d out=%d cost=%v", in, outTok, cost)
	}
}

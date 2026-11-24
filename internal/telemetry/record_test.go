package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
)

var t0 = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	s, err := store.OpenShared(context.Background(), filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatalf("OpenShared: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
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

// takeRun claims an issue run and returns its id.
func takeRun(t *testing.T, db *sql.DB, repo string, issue int, key, mode string) int64 {
	t.Helper()
	r, err := runs.Take(context.Background(), db, runs.Claim{
		Repo: repo, Issue: issue, SessionID: runs.NewSessionID(), Mode: mode,
		Trigger: "handoff", TriggerKey: key, Now: t0,
	})
	if err != nil {
		t.Fatalf("Take %s: %v", key, err)
	}
	return r.ID
}

func session(key string, lines ...Line) SessionRecord {
	return SessionRecord{
		Origin: Origin{Source: "claude-code-transcript", SourceID: key, RecordedAt: t0},
		Repo:   "o/r", Harness: "claude-code", SessionKey: key,
		TranscriptPath: "/t/" + key + ".jsonl", DefaultBranch: "main",
		Lines: lines, StartedAt: t0,
	}
}

// sessionRow is every column of a sessions row, as stored.
type sessionRow struct {
	ID                                               int64
	Source, SourceID, RecordedAt, Repo, Harness, Key string
	TranscriptPath, Branch                           string
	TrajectoryID                                     sql.NullInt64
	LinkBasis, UnlinkedReason                        sql.NullString
	Assignment, StartedAt                            string
	EndedAt                                          sql.NullString
}

func readSession(t *testing.T, db *sql.DB, id int64) sessionRow {
	t.Helper()
	var r sessionRow
	err := db.QueryRow(`SELECT id, source, source_id, recorded_at, repo, harness, session_key, transcript_path,
		branch, trajectory_id, link_basis, unlinked_reason, assignment, started_at, ended_at
		FROM sessions WHERE id = ?`, id).Scan(&r.ID, &r.Source, &r.SourceID, &r.RecordedAt, &r.Repo, &r.Harness,
		&r.Key, &r.TranscriptPath, &r.Branch, &r.TrajectoryID, &r.LinkBasis, &r.UnlinkedReason, &r.Assignment,
		&r.StartedAt, &r.EndedAt)
	if err != nil {
		t.Fatalf("read session %d: %v", id, err)
	}
	return r
}

func trajectoryID(t *testing.T, db *sql.DB, repo string, issue int) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`SELECT id FROM trajectories WHERE repo = ? AND issue = ?`, repo, issue).Scan(&id); err != nil {
		t.Fatalf("trajectory %s#%d: %v", repo, issue, err)
	}
	return id
}

func TestOneTrajectoryAcrossSources(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	first := takeRun(t, db, "o/r", 373, "start:373", "start")
	if err := runs.Release(ctx, db, first, runs.Handoff, "", t0); err != nil {
		t.Fatalf("Release: %v", err)
	}
	second := takeRun(t, db, "o/r", 373, "resume:373", "resume")

	sid, isNew, err := RecordSession(ctx, db, session("s1", Line{Branch: "main"}, Line{Branch: "issue-373-x"}))
	if err != nil || !isNew {
		t.Fatalf("RecordSession = %d, %v, %v; want new", sid, isNew, err)
	}
	if n := count(t, db, `SELECT count(*) FROM trajectories`); n != 1 {
		t.Fatalf("trajectories = %d, want 1", n)
	}
	tid := trajectoryID(t, db, "o/r", 373)
	for _, id := range []int64{first, second} {
		if n := count(t, db, `SELECT count(*) FROM runs WHERE id = ? AND trajectory_id = ?`, id, tid); n != 1 {
			t.Errorf("run %d does not point at trajectory %d", id, tid)
		}
	}
	got := readSession(t, db, sid)
	if !got.TrajectoryID.Valid || got.TrajectoryID.Int64 != tid || got.LinkBasis.String != "branch" ||
		got.Branch != "issue-373-x" || got.UnlinkedReason.Valid {
		t.Errorf("session = %+v; want trajectory %d, basis branch, branch issue-373-x, no reason", got, tid)
	}
}

func TestRecordIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	runID := takeRun(t, db, "o/r", 5, "start:5", "start")
	cost := 1.25

	// Each case offers the same record twice, then once under another source
	// for the same object.
	var sid int64
	cases := []struct {
		name  string
		table string
		first func(o Origin) (int64, bool, error)
	}{
		{"session", "sessions", func(o Origin) (int64, bool, error) {
			s := session("s1", Line{Branch: "issue-5-x"})
			s.Origin = o
			id, n, err := RecordSession(ctx, db, s)
			sid = id
			return id, n, err
		}},
		{"setup run", "setups", func(o Origin) (int64, bool, error) {
			return RecordSetup(ctx, db, SetupRecord{Origin: o, RunID: runID, Model: "opus"})
		}},
		{"setup session", "setups", func(o Origin) (int64, bool, error) {
			return RecordSetup(ctx, db, SetupRecord{Origin: o, SessionID: sid, Model: "opus"})
		}},
		{"usage run", "usage", func(o Origin) (int64, bool, error) {
			return RecordUsage(ctx, db, UsageRecord{Origin: o, RunID: runID, Model: "opus", InputTokens: 10, Cost: &cost, CostKind: "estimate"})
		}},
		{"usage session", "usage", func(o Origin) (int64, bool, error) {
			return RecordUsage(ctx, db, UsageRecord{Origin: o, SessionID: sid, Model: "opus", OutputTokens: 3})
		}},
		{"finding", "findings", func(o Origin) (int64, bool, error) {
			return RecordFinding(ctx, db, FindingRecord{Origin: o, Repo: "o/r", Issue: 5, RunID: runID,
				Component: "plan_review", FindingKey: "r1-f1", RecordPath: "docs/r.md", RecordSum: "abc"})
		}},
	}
	for _, c := range cases {
		before := count(t, db, `SELECT count(*) FROM `+c.table)
		o := Origin{Source: "src-a", SourceID: c.name, RecordedAt: t0}
		id1, new1, err := c.first(o)
		if err != nil || !new1 {
			t.Fatalf("%s first: id %d, new %v, err %v; want new", c.name, id1, new1, err)
		}
		id2, new2, err := c.first(o)
		if err != nil || new2 || id2 != id1 {
			t.Errorf("%s repeat: id %d, new %v, err %v; want id %d, not new", c.name, id2, new2, err, id1)
		}
		if c.table != "findings" { // a finding has no object key: another source is another finding
			other := Origin{Source: "src-b", SourceID: c.name, RecordedAt: t0}
			id3, new3, err := c.first(other)
			if err != nil || new3 || id3 != id1 {
				t.Errorf("%s other source: id %d, new %v, err %v; want id %d, not new", c.name, id3, new3, err, id1)
			}
		}
		if n := count(t, db, `SELECT count(*) FROM `+c.table); n != before+1 {
			t.Errorf("%s: %s grew by %d rows, want 1", c.name, c.table, n-before)
		}
	}
}

func TestSessionGrows(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	early := session("s1", Line{Branch: "main"}, Line{Branch: "main"})
	id, isNew, err := RecordSession(ctx, db, early)
	if err != nil || !isNew {
		t.Fatalf("first: %v, new %v", err, isNew)
	}
	got := readSession(t, db, id)
	if got.UnlinkedReason.String != "main_only" || got.TrajectoryID.Valid {
		t.Fatalf("first: reason %v, trajectory %v; want main_only, NULL", got.UnlinkedReason, got.TrajectoryID)
	}

	later := session("s1", Line{Branch: "main"}, Line{Branch: "main"}, Line{Branch: "issue-9-x"})
	later.Origin = Origin{Source: "claude-code-transcript", SourceID: "s1", RecordedAt: t0.Add(time.Hour)}
	later.TranscriptPath = "/elsewhere.jsonl"
	later.StartedAt = t0.Add(time.Minute)
	end := t0.Add(2 * time.Hour)
	later.EndedAt = &end
	id2, isNew, err := RecordSession(ctx, db, later)
	if err != nil || isNew || id2 != id {
		t.Fatalf("later: id %d, new %v, err %v; want id %d, not new", id2, isNew, err, id)
	}
	got = readSession(t, db, id)
	want := sessionRow{
		ID: id, Source: "claude-code-transcript", SourceID: "s1", RecordedAt: "2026-11-19T11:00:00Z",
		Repo: "o/r", Harness: "claude-code", Key: "s1", TranscriptPath: "/t/s1.jsonl", Branch: "issue-9-x",
		TrajectoryID: sql.NullInt64{Int64: trajectoryID(t, db, "o/r", 9), Valid: true},
		LinkBasis:    sql.NullString{String: "branch", Valid: true},
		Assignment:   "standard", StartedAt: "2026-11-19T10:00:00Z",
		EndedAt: sql.NullString{String: "2026-11-19T12:00:00Z", Valid: true},
	}
	if got != want {
		t.Errorf("session =\n%+v\nwant\n%+v", got, want)
	}
	if n := count(t, db, `SELECT count(*) FROM sessions`); n != 1 {
		t.Errorf("sessions = %d, want 1", n)
	}
}

func TestSessionRepeatSameOffering(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	s := session("s1", Line{Branch: "issue-4-y"}, Line{Branch: "main"})
	end := t0.Add(time.Hour)
	s.EndedAt = &end
	id, _, err := RecordSession(ctx, db, s)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	before := readSession(t, db, id)
	if _, _, err := RecordSession(ctx, db, s); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if after := readSession(t, db, id); after != before {
		t.Errorf("row changed:\n%+v\n->\n%+v", before, after)
	}
}

func TestSessionUnlinkedStored(t *testing.T) {
	tests := []struct {
		name   string
		lines  []Line
		reason string
	}{
		{"only main", []Line{{Branch: "main"}, {Branch: ""}}, "main_only"},
		{"no lines", nil, "no_issue_number"},
		{"fix branch", []Line{{Branch: "fix-thing"}}, "no_issue_branch"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openDB(t)
			id, isNew, err := RecordSession(context.Background(), db, session("s1", tc.lines...))
			if err != nil || !isNew {
				t.Fatalf("RecordSession: %v, new %v", err, isNew)
			}
			got := readSession(t, db, id)
			if got.UnlinkedReason.String != tc.reason || got.TrajectoryID.Valid || got.LinkBasis.Valid || got.Branch != "" {
				t.Errorf("session = %+v; want reason %s, no trajectory, no basis, empty branch", got, tc.reason)
			}
			if n := count(t, db, `SELECT count(*) FROM trajectories`); n != 0 {
				t.Errorf("trajectories = %d, want 0", n)
			}
		})
	}
}

func TestFindingDecisionLater(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	f := FindingRecord{
		Origin: Origin{Source: "plan-review-record", SourceID: "plan:r1-f2", RecordedAt: t0},
		Repo:   "o/r", Issue: 3, Component: "plan_review", FindingKey: "r1-f2", Severity: "major",
		RecordPath: "docs/plan.review.md", RecordSum: "sum1",
	}
	id, isNew, err := RecordFinding(ctx, db, f)
	if err != nil || !isNew {
		t.Fatalf("first: %v, new %v", err, isNew)
	}
	var decision sql.NullString
	if err := db.QueryRow(`SELECT decision FROM findings WHERE id = ?`, id).Scan(&decision); err != nil || decision.Valid {
		t.Fatalf("decision = %v (err %v), want NULL", decision, err)
	}

	f.Decision = "adopted"
	f.RecordSum = "sum2"
	f.Severity = "minor"
	f.RecordPath = "docs/plan2.review.md"
	f.RecordedAt = t0.Add(time.Hour)
	id2, isNew, err := RecordFinding(ctx, db, f)
	if err != nil || isNew || id2 != id {
		t.Fatalf("second: id %d, new %v, err %v; want id %d, not new", id2, isNew, err, id)
	}
	var sev, path, sum, recorded string
	if err := db.QueryRow(`SELECT decision, severity, record_path, record_sum, recorded_at FROM findings WHERE id = ?`, id).
		Scan(&decision, &sev, &path, &sum, &recorded); err != nil {
		t.Fatal(err)
	}
	if decision.String != "adopted" || sev != "minor" || path != "docs/plan2.review.md" || sum != "sum2" ||
		recorded != "2026-11-19T11:00:00Z" {
		t.Errorf("finding = %v %q %q %q %q; want adopted minor docs/plan2.review.md sum2 2026-11-19T11:00:00Z",
			decision, sev, path, sum, recorded)
	}
	if n := count(t, db, `SELECT count(*) FROM findings`); n != 1 {
		t.Errorf("findings = %d, want 1", n)
	}
}

func TestSessionWithFindingsRefusesRetarget(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	s := session("s1", Line{Branch: "issue-9-x"})
	sid, _, err := RecordSession(ctx, db, s)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	if _, _, err := RecordFinding(ctx, db, FindingRecord{
		Origin: Origin{Source: "rec", SourceID: "f1", RecordedAt: t0}, Repo: "o/r", Issue: 9, SessionID: sid,
		Component: "spec_review", FindingKey: "r1-f1", RecordPath: "p", RecordSum: "s",
	}); err != nil {
		t.Fatalf("finding: %v", err)
	}
	before := readSession(t, db, sid)
	trajectories := count(t, db, `SELECT count(*) FROM trajectories`)

	moved := session("s1", Line{Branch: "issue-10-x"}, Line{Branch: "issue-10-x"})
	moved.RecordedAt = t0.Add(time.Hour)
	if _, _, err := RecordSession(ctx, db, moved); !errors.Is(err, ErrSessionHasFindings) {
		t.Errorf("retarget err = %v, want ErrSessionHasFindings", err)
	}
	unlinked := session("s1", Line{Branch: "main"})
	if _, _, err := RecordSession(ctx, db, unlinked); !errors.Is(err, ErrSessionHasFindings) {
		t.Errorf("unlink err = %v, want ErrSessionHasFindings", err)
	}
	if after := readSession(t, db, sid); after != before {
		t.Errorf("row changed:\n%+v\n->\n%+v", before, after)
	}
	if n := count(t, db, `SELECT count(*) FROM trajectories`); n != trajectories {
		t.Errorf("trajectories = %d, want %d (refusal left a trajectory)", n, trajectories)
	}

	id, isNew, err := RecordSession(ctx, db, s)
	if err != nil || isNew || id != sid {
		t.Errorf("same offering: id %d, new %v, err %v; want id %d, not new, no error", id, isNew, err, sid)
	}
}

func TestFindingMustFitTrajectory(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	runA := takeRun(t, db, "o/r", 1, "start:1", "start")
	unlinked, _, err := RecordSession(ctx, db, session("s1", Line{Branch: "main"}))
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	base := FindingRecord{
		Origin: Origin{Source: "rec", SourceID: "f", RecordedAt: t0}, Repo: "o/r",
		Component: "task_review", FindingKey: "k", RecordPath: "p", RecordSum: "s",
	}
	tests := []struct {
		name    string
		mutate  func(f *FindingRecord)
		wantErr error
	}{
		{"run of another issue", func(f *FindingRecord) { f.Issue, f.RunID = 2, runA }, ErrTrajectoryMismatch},
		{"unlinked session", func(f *FindingRecord) { f.Issue, f.SessionID = 1, unlinked }, ErrTrajectoryMismatch},
		{"issue zero", func(f *FindingRecord) { f.Issue = 0 }, nil},
		{"issue negative", func(f *FindingRecord) { f.Issue = -1 }, nil},
		{"run and session", func(f *FindingRecord) { f.Issue, f.RunID, f.SessionID = 1, runA, unlinked }, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			tc.mutate(&f)
			trajectories := count(t, db, `SELECT count(*) FROM trajectories`)
			_, _, err := RecordFinding(ctx, db, f)
			if err == nil {
				t.Fatalf("RecordFinding: no error")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
			if n := count(t, db, `SELECT count(*) FROM trajectories`); n != trajectories {
				t.Errorf("trajectories = %d, want %d", n, trajectories)
			}
			if n := count(t, db, `SELECT count(*) FROM findings`); n != 0 {
				t.Errorf("findings = %d, want 0", n)
			}
		})
	}
	if n := count(t, db, `SELECT count(*) FROM trajectories WHERE issue = 2`); n != 0 {
		t.Errorf("trajectory for issue 2 exists after refusal")
	}
	// The run fits its own trajectory.
	f := base
	f.Issue, f.RunID = 1, runA
	if _, isNew, err := RecordFinding(ctx, db, f); err != nil || !isNew {
		t.Errorf("fitting finding: %v, new %v", err, isNew)
	}
}

func TestRepoSlugCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	t.Run("from trajectories", func(t *testing.T) {
		db := openDB(t)
		takeRun(t, db, "CorneeldH/lighthouse", 1, "start:1", "start")
		if _, _, err := RecordFinding(ctx, db, FindingRecord{
			Origin: Origin{Source: "rec", SourceID: "f", RecordedAt: t0}, Repo: "corneeldh/lighthouse", Issue: 1,
			Component: "c", FindingKey: "k", RecordPath: "p", RecordSum: "s",
		}); err != nil {
			t.Fatalf("RecordFinding: %v", err)
		}
		if n := count(t, db, `SELECT count(*) FROM trajectories`); n != 1 {
			t.Errorf("trajectories = %d, want 1", n)
		}
		if n := count(t, db, `SELECT count(*) FROM trajectories WHERE repo = 'CorneeldH/lighthouse'`); n != 1 {
			t.Errorf("stored spelling CorneeldH/lighthouse lost")
		}
	})
	t.Run("from runs", func(t *testing.T) {
		db := openDB(t)
		// A scheduled run has no trajectory, only its repo spelling.
		if _, err := runs.Take(ctx, db, runs.Claim{
			Repo: "CorneeldH/lighthouse", Schedule: "nightly", SessionID: runs.NewSessionID(), Mode: "start",
			Trigger: runs.TriggerScheduled, TriggerKey: "scheduled:nightly:1", Now: t0,
		}); err != nil {
			t.Fatalf("Take scheduled: %v", err)
		}
		s := session("s1", Line{Branch: "issue-2-x"})
		s.Repo = "corneeldh/LIGHTHOUSE"
		id, _, err := RecordSession(ctx, db, s)
		if err != nil {
			t.Fatalf("RecordSession: %v", err)
		}
		if got := readSession(t, db, id).Repo; got != "CorneeldH/lighthouse" {
			t.Errorf("session repo = %q, want CorneeldH/lighthouse", got)
		}
		if n := count(t, db, `SELECT count(*) FROM trajectories WHERE repo = 'CorneeldH/lighthouse' AND issue = 2`); n != 1 {
			t.Errorf("trajectory not under stored spelling")
		}
	})
	t.Run("exact spelling wins", func(t *testing.T) {
		db := openDB(t)
		takeRun(t, db, "CorneeldH/lighthouse", 1, "start:1", "start")
		takeRun(t, db, "corneeldh/lighthouse", 2, "start:2", "start")
		s := session("s1", Line{Branch: "issue-3-x"})
		s.Repo = "corneeldh/lighthouse"
		id, _, err := RecordSession(ctx, db, s)
		if err != nil {
			t.Fatalf("RecordSession: %v", err)
		}
		if got := readSession(t, db, id).Repo; got != "corneeldh/lighthouse" {
			t.Errorf("session repo = %q, want corneeldh/lighthouse", got)
		}
	})
	t.Run("new repo keeps its own spelling", func(t *testing.T) {
		db := openDB(t)
		s := session("s1", Line{Branch: "issue-2-x"})
		s.Repo = "New/Repo"
		if _, _, err := RecordSession(ctx, db, s); err != nil {
			t.Fatalf("RecordSession: %v", err)
		}
		if n := count(t, db, `SELECT count(*) FROM trajectories WHERE repo = 'New/Repo'`); n != 1 {
			t.Errorf("trajectory for New/Repo missing")
		}
	})
}

func TestSetupAndUsageOneOwner(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	runID := takeRun(t, db, "o/r", 1, "start:1", "start")
	sid, _, err := RecordSession(ctx, db, session("s1"))
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	o := Origin{Source: "src", SourceID: "x", RecordedAt: t0}
	cost := 0.5

	refused := []struct {
		name string
		call func() error
	}{
		{"setup both", func() error {
			_, _, err := RecordSetup(ctx, db, SetupRecord{Origin: o, RunID: runID, SessionID: sid})
			return err
		}},
		{"setup none", func() error { _, _, err := RecordSetup(ctx, db, SetupRecord{Origin: o}); return err }},
		{"usage both", func() error {
			_, _, err := RecordUsage(ctx, db, UsageRecord{Origin: o, RunID: runID, SessionID: sid, Model: "m"})
			return err
		}},
		{"usage none", func() error { _, _, err := RecordUsage(ctx, db, UsageRecord{Origin: o, Model: "m"}); return err }},
		{"cost without kind", func() error {
			_, _, err := RecordUsage(ctx, db, UsageRecord{Origin: o, RunID: runID, Model: "m", Cost: &cost})
			return err
		}},
		{"kind without cost", func() error {
			_, _, err := RecordUsage(ctx, db, UsageRecord{Origin: o, RunID: runID, Model: "m", CostKind: "actual"})
			return err
		}},
	}
	for _, r := range refused {
		if err := r.call(); err == nil {
			t.Errorf("%s: no error", r.name)
		}
	}
	if n := count(t, db, `SELECT (SELECT count(*) FROM setups) + (SELECT count(*) FROM usage)`); n != 0 {
		t.Errorf("refused calls wrote %d rows", n)
	}

	id, isNew, err := RecordUsage(ctx, db, UsageRecord{Origin: o, SessionID: sid, Model: "m", InputTokens: 7})
	if err != nil || !isNew {
		t.Fatalf("usage without cost: %v, new %v", err, isNew)
	}
	var c, kind sql.NullString
	var in int64
	if err := db.QueryRow(`SELECT cost, cost_kind, input_tokens FROM usage WHERE id = ?`, id).Scan(&c, &kind, &in); err != nil {
		t.Fatal(err)
	}
	if c.Valid || kind.Valid || in != 7 {
		t.Errorf("usage = cost %v, kind %v, input %d; want NULL, NULL, 7", c, kind, in)
	}
}

func TestFindingRollsBackTrajectoryOnCheckFailure(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	takeRun(t, db, "o/r", 1, "start:1", "start")
	trajectories := count(t, db, `SELECT count(*) FROM trajectories`)
	// A new trajectory (issue 2) is inserted before the findings CHECK on
	// decision refuses the row; the whole call must leave nothing behind.
	f := FindingRecord{
		Origin: Origin{Source: "rec", SourceID: "f", RecordedAt: t0}, Repo: "o/r", Issue: 2,
		Component: "task_review", FindingKey: "k", Decision: "maybe", RecordPath: "p", RecordSum: "s",
	}
	if _, _, err := RecordFinding(ctx, db, f); err == nil {
		t.Fatalf("RecordFinding with decision %q: no error", f.Decision)
	}
	if n := count(t, db, `SELECT count(*) FROM trajectories`); n != trajectories {
		t.Errorf("trajectories = %d, want %d: insert not rolled back", n, trajectories)
	}
	if n := count(t, db, `SELECT count(*) FROM findings`); n != 0 {
		t.Errorf("findings = %d, want 0", n)
	}
}

func TestFindingSumAndDelete(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	mk := func(sourceID string) FindingRecord {
		return FindingRecord{
			Origin: Origin{Source: "plan-review-record", SourceID: sourceID, RecordedAt: t0},
			Repo:   "o/r", Issue: 3, Component: "plan_review", FindingKey: "f", RecordPath: "p", RecordSum: "sum-" + sourceID,
		}
	}
	for _, id := range []string{"plan/a_b/A1/r1-f1", "plan/a_b/A2/r1-f1", "plan/axb/A1/r1-f1"} {
		if _, _, err := RecordFinding(ctx, db, mk(id)); err != nil {
			t.Fatalf("RecordFinding %s: %v", id, err)
		}
	}
	sum, found, err := FindingSum(ctx, db, "plan-review-record", "plan/a_b/A1/r1-f1")
	if err != nil || !found || sum != "sum-plan/a_b/A1/r1-f1" {
		t.Fatalf("FindingSum = %q, %v, %v", sum, found, err)
	}
	if _, found, err := FindingSum(ctx, db, "plan-review-record", "nope"); err != nil || found {
		t.Errorf("FindingSum unknown = %v, %v; want not found", found, err)
	}
	// "_" is no wildcard: plan/axb/ stays.
	n, err := DeleteFindingsUnder(ctx, db, "plan-review-record", "plan/a_b/", "plan/a_b/A2/")
	if err != nil || n != 1 {
		t.Fatalf("DeleteFindingsUnder keep = %d, %v; want 1", n, err)
	}
	if got := count(t, db, `SELECT count(*) FROM findings`); got != 2 {
		t.Errorf("findings = %d, want 2", got)
	}
	n, err = DeleteFindingsUnder(ctx, db, "plan-review-record", "plan/a_b/", "")
	if err != nil || n != 1 {
		t.Fatalf("DeleteFindingsUnder all = %d, %v; want 1", n, err)
	}
	if got := count(t, db, `SELECT count(*) FROM findings WHERE source_id LIKE 'plan/axb/%'`); got != 1 {
		t.Errorf("axb rows = %d, want 1", got)
	}
	if n, _ := DeleteFindingsUnder(ctx, db, "sdd-ledger", "plan/axb/", ""); n != 0 {
		t.Errorf("other source deleted %d rows", n)
	}
}

func TestRecordSessionRefusesRetargetWithEvents(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	s := session("s1", Line{Branch: "issue-9-x"})
	sid, _, err := RecordSession(ctx, db, s)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	moved := session("s1", Line{Branch: "issue-10-x"}, Line{Branch: "issue-10-x"})
	moved.RecordedAt = t0.Add(time.Hour)

	// Without an event the session may move.
	if _, _, err := RecordSession(ctx, db, moved); err != nil {
		t.Fatalf("retarget without events: %v", err)
	}
	back := session("s1", Line{Branch: "issue-9-x"}, Line{Branch: "issue-9-x"}, Line{Branch: "issue-9-x"})
	back.RecordedAt = t0.Add(2 * time.Hour)
	if _, _, err := RecordSession(ctx, db, back); err != nil {
		t.Fatalf("retarget back: %v", err)
	}

	if _, _, err := RecordEvent(ctx, db, EventRecord{
		Origin: Origin{Source: "events", SourceID: "question:1", RecordedAt: t0},
		Repo:   "o/r", Issue: 9, SessionID: sid, Kind: EventQuestion, Ref: "q1", At: t0,
	}); err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, _, err := RecordSession(ctx, db, moved); !errors.Is(err, ErrSessionHasEvents) {
		t.Errorf("retarget err = %v, want ErrSessionHasEvents", err)
	}
}

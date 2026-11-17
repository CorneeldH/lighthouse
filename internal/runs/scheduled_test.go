package runs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Classification: io (SQLite) and pure logic. These tests fail if Take stops telling a scheduled
// claim from an issue claim (attempt counting, the open-run index per schedule), if Release lets an
// issue run end as retry or failed, if a reader scans issue into an int without coalesce, or if
// SlotSettled or the schedule_slots table settle a slot on the wrong rows.

func scheduledClaim(repo, schedule, slot string) Claim {
	return Claim{
		Repo: repo, Schedule: schedule, SessionID: NewSessionID(), Mode: "start",
		Trigger: "scheduled", TriggerKey: "scheduled:" + schedule + ":" + slot,
		Now: time.Date(2026, 11, 16, 3, 0, 0, 0, time.UTC),
	}
}

func TestTakeScheduledClaimsPerSchedule(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	r, err := Take(ctx, db, scheduledClaim("o/a", "reflect", "20261116T0300"))
	if err != nil {
		t.Fatalf("Take reflect: %v", err)
	}
	if r.Attempt != 1 {
		t.Errorf("Attempt = %d, want 1", r.Attempt)
	}
	if _, err := Take(ctx, db, scheduledClaim("o/a", "reflect", "20261117T0300")); !errors.Is(err, ErrClaimed) {
		t.Errorf("second open run of reflect: error = %v, want ErrClaimed", err)
	}
	if _, err := Take(ctx, db, scheduledClaim("o/a", "map-refresh", "20261116T0300")); err != nil {
		t.Errorf("open run of a second schedule: %v", err)
	}
	if _, err := Take(ctx, db, scheduledClaim("o/b", "reflect", "20261116T0300")); err != nil {
		t.Errorf("open run of reflect in another repo: %v", err)
	}
	// An issue run in the same repo is not held by the scheduled runs.
	if _, err := Take(ctx, db, claim("o/a", 7, "start:7")); err != nil {
		t.Errorf("issue run next to scheduled runs: %v", err)
	}
}

func TestTakeRejectsIssueAndScheduleMix(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	both := scheduledClaim("o/a", "reflect", "x")
	both.Issue = 7
	neither := claim("o/a", 0, "start:0")
	scheduleOnHandoff := claim("o/a", 0, "start:0")
	scheduleOnHandoff.Schedule = "reflect"
	issueOnScheduled := scheduledClaim("o/a", "", "x")
	issueOnScheduled.Issue = 7
	for name, c := range map[string]Claim{
		"both": both, "neither": neither, "schedule on handoff": scheduleOnHandoff, "issue on scheduled": issueOnScheduled,
	} {
		_, err := Take(ctx, db, c)
		if err == nil || !strings.Contains(err.Error(), "runs: claim needs") {
			t.Errorf("%s: Take error = %v, want the claim-shape error", name, err)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM runs`).Scan(&n); err != nil || n != 0 {
		t.Errorf("rows after rejected claims = %d, %v; want 0", n, err)
	}
}

// takeClosed claims c and closes it with o.
func takeClosed(t *testing.T, db *sql.DB, c Claim, o Outcome) Run {
	t.Helper()
	ctx := context.Background()
	r, err := Take(ctx, db, c)
	if err != nil {
		t.Fatalf("Take %s: %v", c.TriggerKey, err)
	}
	if err := Release(ctx, db, r.ID, o, "", c.Now); err != nil {
		t.Fatalf("Release %s as %s: %v", c.TriggerKey, o, err)
	}
	return r
}

func TestTakeScheduledCountsEveryRow(t *testing.T) {
	tests := []struct {
		name    string
		before  []Outcome
		want    int // attempt; 0 is ErrTriggerSpent
		wantErr error
	}{
		{"fresh slot", nil, 1, nil},
		{"after one retry", []Outcome{Retry}, 2, nil},
		{"after done", []Outcome{Done}, 0, ErrTriggerSpent},
		{"after failed", []Outcome{Failed}, 0, ErrTriggerSpent},
		{"after two retries", []Outcome{Retry, Retry}, 0, ErrTriggerSpent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openDB(t)
			for _, o := range tc.before {
				takeClosed(t, db, scheduledClaim("o/a", "reflect", "20261116T0300"), o)
			}
			r, err := Take(context.Background(), db, scheduledClaim("o/a", "reflect", "20261116T0300"))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Take error = %v, want %v", err, tc.wantErr)
			}
			if r.Attempt != tc.want {
				t.Errorf("Attempt = %d, want %d", r.Attempt, tc.want)
			}
		})
	}
}

// An issue run keeps the old rule: only start_failed rows give a further attempt.
func TestTakeIssueRunIgnoresScheduledRule(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	r, err := Take(ctx, db, claim("o/a", 7, "start:7"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Release(ctx, db, r.ID, Done, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := Take(ctx, db, claim("o/a", 7, "start:7")); !errors.Is(err, ErrTriggerSpent) {
		t.Errorf("Take after done: error = %v, want ErrTriggerSpent", err)
	}
}

func TestReleaseScheduledOutcomes(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	for _, o := range []Outcome{Retry, Failed} {
		issue, err := Take(ctx, db, claim("o/a", 7, "start:"+string(o)))
		if err != nil {
			t.Fatal(err)
		}
		err = Release(ctx, db, issue.ID, o, "", time.Now())
		if err == nil || errors.Is(err, ErrNotOpen) || !strings.Contains(err.Error(), "only for a scheduled run") {
			t.Errorf("Release issue run as %s: error = %v, want the scheduled-only error", o, err)
		}
		if err := Release(ctx, db, issue.ID, Done, "", time.Now()); err != nil {
			t.Errorf("issue run not open after the refused %s: %v", o, err)
		}

		sched, err := Take(ctx, db, scheduledClaim("o/a", "reflect", string(o)))
		if err != nil {
			t.Fatal(err)
		}
		if err := Release(ctx, db, sched.ID, o, "why", time.Now()); err != nil {
			t.Errorf("Release scheduled run as %s: %v", o, err)
		}
		var got string
		if err := db.QueryRow(`SELECT outcome FROM runs WHERE id = ?`, sched.ID).Scan(&got); err != nil || got != string(o) {
			t.Errorf("outcome = %q, %v; want %q", got, err, o)
		}
		if err := Release(ctx, db, sched.ID, o, "", time.Now()); !errors.Is(err, ErrNotOpen) {
			t.Errorf("second Release as %s: error = %v, want ErrNotOpen", o, err)
		}
	}
	if err := Release(ctx, db, 999, Retry, "", time.Now()); !errors.Is(err, ErrNotOpen) {
		t.Errorf("Release unknown run as retry: error = %v, want ErrNotOpen", err)
	}
}

func TestScheduledRunColumns(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	c := scheduledClaim("o/a", "reflect", "20261116T0300")
	c.StartSHA, c.Verdict, c.Branch = "abc", `{"mode":"full"}`, "scheduled/reflect-20261116T0300-1"
	r, err := Take(ctx, db, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetWorkplace(ctx, db, r.ID, "ws-1", c.Branch); err != nil {
		t.Fatal(err)
	}
	if err := SetStartSHA(ctx, db, r.ID, "def"); err != nil {
		t.Fatalf("SetStartSHA: %v", err)
	}
	prAt := time.Date(2026, 11, 16, 3, 40, 0, 0, time.UTC)
	if err := SetPRAt(ctx, db, r.ID, prAt); err != nil {
		t.Fatalf("SetPRAt: %v", err)
	}
	issue, err := Take(ctx, db, claim("o/a", 7, "start:7"))
	if err != nil {
		t.Fatal(err)
	}

	open, err := Open(ctx, db, "o/a")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("Open = %+v, want 2 runs", open)
	}
	o := open[0]
	if o.Issue != 0 || o.Schedule != "reflect" || o.StartSHA != "def" || o.Verdict != `{"mode":"full"}` || !o.PRAt.Equal(prAt) {
		t.Errorf("open scheduled run = %+v", o)
	}
	if i := open[1]; i.ID != issue.ID || i.Issue != 7 || i.Schedule != "" || !i.PRAt.IsZero() {
		t.Errorf("open issue run = %+v", i)
	}

	all, err := OpenAll(ctx, db)
	if err != nil {
		t.Fatalf("OpenAll: %v", err)
	}
	if len(all) != 2 || all[0].Schedule != "reflect" || all[0].Issue != 0 {
		t.Errorf("OpenAll = %+v", all)
	}

	briefs, err := OpenBriefs(ctx, db, "o/a")
	if err != nil {
		t.Fatalf("OpenBriefs: %v", err)
	}
	if len(briefs) != 2 || briefs[0].Schedule != "reflect" || briefs[0].Issue != 0 || !briefs[0].PRAt.Equal(prAt) ||
		briefs[1].Schedule != "" || !briefs[1].PRAt.IsZero() {
		t.Errorf("OpenBriefs = %+v", briefs)
	}

	if err := Release(ctx, db, r.ID, Done, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := SetPRAt(ctx, db, r.ID, prAt); !errors.Is(err, ErrNotOpen) {
		t.Errorf("SetPRAt on closed run: error = %v, want ErrNotOpen", err)
	}
	if err := SetStartSHA(ctx, db, r.ID, "x"); !errors.Is(err, ErrNotOpen) {
		t.Errorf("SetStartSHA on closed run: error = %v, want ErrNotOpen", err)
	}

	hist, err := History(ctx, db, "o/a")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	h := hist[0]
	if h.Issue != 0 || h.Schedule != "reflect" || h.StartSHA != "def" || h.Verdict != `{"mode":"full"}` ||
		!h.PRAt.Equal(prAt) || h.Outcome != Done || h.Trigger != "scheduled" {
		t.Errorf("history scheduled row = %+v", h)
	}
	if !hist[1].PRAt.IsZero() || hist[1].Schedule != "" || hist[1].Issue != 7 {
		t.Errorf("history issue row = %+v", hist[1])
	}

	_, last, ok, err := LastOfSession(ctx, db, c.SessionID)
	if err != nil || !ok || last.Schedule != "reflect" {
		t.Errorf("LastOfSession = %+v, %v, %v", last, ok, err)
	}

	refs, err := Workspaces(ctx, db, "o/a")
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	if len(refs) != 1 || refs[0].Issue != 0 || refs[0].Schedule != "reflect" || refs[0].WorkspaceID != "ws-1" {
		t.Errorf("Workspaces = %+v", refs)
	}
}

func TestSlotSettled(t *testing.T) {
	row := func(key string, o Outcome) Row {
		return Row{Trigger: "scheduled", Schedule: "reflect", TriggerKey: key, Outcome: o, Open: o == ""}
	}
	const key = "scheduled:reflect:20261116T0300"
	tests := []struct {
		name string
		rows []Row
		want bool
	}{
		{"no rows", nil, false},
		{"one retry", []Row{row(key, Retry)}, false},
		{"one open", []Row{row(key, "")}, false},
		{"retry and retry", []Row{row(key, Retry), row(key, Retry)}, true},
		{"retry and open", []Row{row(key, Retry), row(key, "")}, true},
		{"failed", []Row{row(key, Failed)}, true},
		{"done", []Row{row(key, Done)}, true},
		{"done of another slot", []Row{row("scheduled:reflect:20261115T0300", Done)}, false},
		{"retries of two other slots", []Row{row("scheduled:reflect:20261115T0300", Retry), row("scheduled:reflect:20261114T0300", Retry)}, false},
		{"done of another schedule", []Row{{Trigger: "scheduled", Schedule: "map", TriggerKey: "scheduled:map:20261116T0300", Outcome: Done}}, false},
	}
	for _, tc := range tests {
		if got := SlotSettled(tc.rows, "reflect", "20261116T0300"); got != tc.want {
			t.Errorf("%s: SlotSettled = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSkipSlot(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	now := time.Date(2026, 11, 16, 3, 0, 0, 0, time.UTC)
	skipped, err := SlotSkipped(ctx, db, "o/a", "reflect", "20261116T0300")
	if err != nil || skipped {
		t.Fatalf("SlotSkipped before SkipSlot = %v, %v; want false", skipped, err)
	}
	if err := SkipSlot(ctx, db, "o/a", "reflect", "20261116T0300", "no new sessions", now); err != nil {
		t.Fatalf("SkipSlot: %v", err)
	}
	if err := SkipSlot(ctx, db, "o/a", "reflect", "20261116T0300", "again", now); err != nil {
		t.Errorf("second SkipSlot of the same slot: %v", err)
	}
	for _, tc := range []struct {
		repo, schedule, slot string
		want                 bool
	}{
		{"o/a", "reflect", "20261116T0300", true},
		{"o/a", "reflect", "20261117T0300", false},
		{"o/a", "map-refresh", "20261116T0300", false},
		{"o/b", "reflect", "20261116T0300", false},
	} {
		got, err := SlotSkipped(ctx, db, tc.repo, tc.schedule, tc.slot)
		if err != nil || got != tc.want {
			t.Errorf("SlotSkipped(%s, %s, %s) = %v, %v; want %v", tc.repo, tc.schedule, tc.slot, got, err, tc.want)
		}
	}
	var reason, at string
	if err := db.QueryRow(`SELECT reason, at FROM schedule_slots WHERE repo = 'o/a'`).Scan(&reason, &at); err != nil {
		t.Fatal(err)
	}
	if reason != "no new sessions" || at != "2026-11-16T03:00:00Z" {
		t.Errorf("slot row = %q at %q; want the first reason at 2026-11-16T03:00:00Z", reason, at)
	}
}

// A scheduled claim must carry the trigger key of its own schedule, with a slot: a key of another
// schedule would let the open-run index and SlotSettled disagree on which slot the run is for.
func TestTakeScheduledKeyNamesItsSchedule(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	for _, key := range []string{
		"scheduled:map-refresh:20261116T0300", // another schedule
		"scheduled:reflect:",                  // no slot
		"scheduled:reflect",                   // no slot separator
		"start:7",                             // an issue key
		"scheduled:reflectx:20261116T0300",    // a schedule that merely starts with the name
	} {
		c := scheduledClaim("o/a", "reflect", "20261116T0300")
		c.TriggerKey = key
		_, err := Take(ctx, db, c)
		if err == nil || !strings.Contains(err.Error(), "trigger key") || errors.Is(err, ErrClaimed) {
			t.Errorf("Take with trigger key %q: error = %v, want the trigger-key error", key, err)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM runs`).Scan(&n); err != nil || n != 0 {
		t.Errorf("rows after rejected claims = %d, %v; want 0", n, err)
	}
}

// A scheduled run ends only as done, failed or retry: the issue outcomes (start_failed, handoff,
// question, parked, returned) would leave its slot with a row SlotSettled does not read as settled
// yet that counts as an attempt, without the run ever being retried on purpose.
func TestReleaseRefusesIssueOutcomesOnScheduledRun(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	for i, o := range []Outcome{StartFailed, Handoff, Question, Parked, Returned} {
		slot := fmt.Sprintf("2026111%dT0300", i)
		r, err := Take(ctx, db, scheduledClaim("o/a", "reflect", slot))
		if err != nil {
			t.Fatal(err)
		}
		err = Release(ctx, db, r.ID, o, "", time.Now())
		if err == nil || errors.Is(err, ErrNotOpen) || !strings.Contains(err.Error(), "only for an issue run") {
			t.Errorf("Release scheduled run as %s: error = %v, want the issue-only error", o, err)
		}
		if err := Release(ctx, db, r.ID, Retry, "", time.Now()); err != nil {
			t.Errorf("scheduled run not open after the refused %s: %v", o, err)
		}
	}
	// Done stays allowed for both kinds.
	r, err := Take(ctx, db, scheduledClaim("o/a", "reflect", "20261120T0300"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Release(ctx, db, r.ID, Done, "", time.Now()); err != nil {
		t.Errorf("Release scheduled run as done: %v", err)
	}
}

func TestSetVerdict(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	r, err := Take(ctx, db, scheduledClaim("o/a", "reflect", "20261116T0300"))
	if err != nil {
		t.Fatal(err)
	}
	if err := SetVerdict(ctx, db, r.ID, `{"v":2}`); err != nil {
		t.Fatalf("SetVerdict: %v", err)
	}
	open, err := Open(ctx, db, "o/a")
	if err != nil || len(open) != 1 || open[0].Verdict != `{"v":2}` {
		t.Errorf("Open = %+v, %v; want the verdict set", open, err)
	}
	if err := Release(ctx, db, r.ID, Retry, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := SetVerdict(ctx, db, r.ID, `{}`); !errors.Is(err, ErrNotOpen) {
		t.Errorf("SetVerdict on closed run: error = %v, want ErrNotOpen", err)
	}
}

package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func timeline(t *testing.T, db *sql.DB, now time.Time, f Filter) TimelineReport {
	t.Helper()
	d, err := Load(context.Background(), db, now)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r, err := Timeline(d, db, f)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	return r
}

func week(t *testing.T, r TimelineReport, start string) Week {
	t.Helper()
	for _, w := range r.Weeks {
		if w.Start == start {
			return w
		}
	}
	t.Fatalf("no week %s in %+v", start, r.Weeks)
	return Week{}
}

func fmtF(p *float64) string {
	if p == nil {
		return "nil"
	}
	b, _ := json.Marshal(*p)
	return string(b)
}

func fmtI(p *int) string {
	if p == nil {
		return "nil"
	}
	b, _ := json.Marshal(*p)
	return string(b)
}

// t0 is Monday 2026-03-02 09:00 UTC.
func TestTimelineWeeksStartOnMondayUTC(t *testing.T) {
	db, f := newFixture(t)
	tid := f.addTrajectory("o/r", 1)
	r := f.addRun(tid, "standard", at(0), at(10))
	sunday := time.Date(2026, 3, 8, 23, 59, 0, 0, time.UTC)
	monday := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	f.addUsageAt(ofRun(r), "opus", 1, fp(1), "estimate", sunday)
	f.addUsageAt(ofRun(r), "sonnet", 1, fp(2), "estimate", monday)

	got := timeline(t, db, monday.Add(time.Hour), Filter{})
	if len(got.Weeks) != 2 {
		t.Fatalf("Weeks = %+v; want 2", got.Weeks)
	}
	if w := week(t, got, "2026-03-02"); fmtF(w.Cost) != "1" {
		t.Errorf("week 2026-03-02 Cost = %s; want 1", fmtF(w.Cost))
	}
	if w := week(t, got, "2026-03-09"); fmtF(w.Cost) != "2" {
		t.Errorf("week 2026-03-09 Cost = %s; want 2", fmtF(w.Cost))
	}
}

func TestTimelineLeadingWeeksNullThenZero(t *testing.T) {
	db, f := newFixture(t)
	tid := f.addTrajectory("o/r", 1)
	r := f.addRun(tid, "standard", at(0), at(10))
	f.addUsageAt(ofRun(r), "opus", 1, fp(3), "estimate", t0)
	f.addQuestion(tid, ofRun(r), t0.Add(14*24*time.Hour))
	f.addRecovery(tid, 7, "recovered", t0.Add(7*24*time.Hour), nil, "fix")

	got := timeline(t, db, t0.Add(21*24*time.Hour), Filter{})
	want := []struct{ start, cost, questions, closed, recovered string }{
		{"2026-03-02", "3", "nil", "nil", "nil"},
		{"2026-03-09", "0", "nil", "1", "1"},
		{"2026-03-16", "0", "1", "0", "0"},
		{"2026-03-23", "0", "0", "0", "0"},
	}
	if len(got.Weeks) != len(want) {
		t.Fatalf("Weeks = %+v; want %d weeks", got.Weeks, len(want))
	}
	for i, w := range want {
		g := got.Weeks[i]
		have := []string{g.Start, fmtF(g.Cost), fmtI(g.Questions), fmtI(g.Closed), fmtI(g.Recovered)}
		if strings.Join(have, " ") != strings.Join([]string{w.start, w.cost, w.questions, w.closed, w.recovered}, " ") {
			t.Errorf("week %d = %v; want %+v", i, have, w)
		}
	}
}

func TestTimelineSkipsUnpricedUsage(t *testing.T) {
	db, f := newFixture(t)
	tid := f.addTrajectory("o/r", 1)
	r := f.addRun(tid, "standard", at(0), at(10))
	f.addUsageAt(ofRun(r), "opus", 1, nil, "", t0)
	f.addQuestion(tid, ofRun(r), t0) // keeps the first week on the timeline
	f.addUsageAt(ofRun(r), "sonnet", 1, fp(2), "estimate", t0.Add(7*24*time.Hour))
	f.addUsageAt(ofRun(r), "haiku", 1, nil, "", t0.Add(7*24*time.Hour))

	got := timeline(t, db, t0.Add(7*24*time.Hour), Filter{})
	if w := week(t, got, "2026-03-02"); w.Cost != nil {
		t.Errorf("week with only an unpriced row: Cost = %s; want nil", fmtF(w.Cost))
	}
	if w := week(t, got, "2026-03-09"); fmtF(w.Cost) != "2" {
		t.Errorf("week 2026-03-09 Cost = %s; want 2", fmtF(w.Cost))
	}
}

func TestTimelineMarkersEmptyNotNil(t *testing.T) {
	db, _ := newFixture(t)
	got := timeline(t, db, t0, Filter{})
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(b), `"markers":[]`) || !strings.Contains(string(b), `"weeks":[]`) {
		t.Errorf("JSON = %s; want markers and weeks as []", b)
	}
}

func TestTimelineFilterByRepo(t *testing.T) {
	db, f := newFixture(t)
	a := f.addTrajectory("o/a", 1)
	ra := f.addRun(a, "standard", at(0), at(10))
	f.addUsageAt(ofRun(ra), "opus", 1, fp(1), "estimate", t0)
	f.addQuestion(a, ofRun(ra), t0)
	b := f.addTrajectory("o/b", 1)
	rb := f.addRun(b, "standard", at(0), at(10))
	f.addUsageAt(ofRun(rb), "opus", 1, fp(10), "estimate", t0)
	f.addQuestion(b, ofRun(rb), t0)
	f.addQuestion(b, ofRun(rb), t0.Add(time.Minute))
	f.addRecovery(b, 3, "none", t0, nil)

	got := timeline(t, db, t0, Filter{Repo: "o/a"})
	w := week(t, got, "2026-03-02")
	if fmtF(w.Cost) != "1" || fmtI(w.Questions) != "1" || w.Closed != nil {
		t.Errorf("filtered week = cost %s, questions %s, closed %s; want 1, 1, nil",
			fmtF(w.Cost), fmtI(w.Questions), fmtI(w.Closed))
	}
}

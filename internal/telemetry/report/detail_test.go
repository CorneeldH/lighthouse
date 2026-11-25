package report

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"testing"
)

func detail(t *testing.T, f *fixture, id int64) Detail {
	t.Helper()
	d, ok, err := LoadDetail(context.Background(), f.db, id, at(100000))
	if err != nil || !ok {
		t.Fatalf("LoadDetail(%d) = ok %v, err %v; want found", id, ok, err)
	}
	return d
}

// jsonKeys returns the sorted keys of v marshalled as a JSON object.
func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("Unmarshal %s: %v", b, err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestLoadDetailListsFindingsWithoutContent(t *testing.T) {
	_, f := newFixture(t)
	tid := f.addTrajectory("o/r", 1, withPlan(4))
	rid, path := f.addReview(tid, "plan_review", at(0), 2)
	f.addFinding(tid, "plan_review", "k1", "high", "adopted", path)
	f.addFinding(tid, "plan_review", "k2", "low", "", path)
	f.addFinding(tid, "task_review", "k3", "medium", "rejected", "/ledger.json")
	f.addRecovery(tid, 9, "recovered", at(60), nil, "fix")

	got := detail(t, f, tid)
	if got.ID != tid || got.Repo != "o/r" || got.Issue != 1 {
		t.Errorf("header = %d %s#%d; want %d o/r#1", got.ID, got.Repo, got.Issue, tid)
	}
	if len(got.Reviews) != 1 {
		t.Fatalf("Reviews = %+v; want 1", got.Reviews)
	}
	rv := got.Reviews[0]
	if rv.ID != rid || rv.Component != "plan_review" || rv.RecordPath != path || rv.FindingCount != 2 || rv.Outcome != "converged" {
		t.Errorf("review = %+v", rv)
	}
	wantFindings := []FindingRow{
		{Component: "plan_review", Key: "k1", Severity: "high", Decision: "adopted", RecordPath: path},
		{Component: "plan_review", Key: "k2", Severity: "low", Decision: "", RecordPath: path},
	}
	if !slices.Equal(rv.Findings, wantFindings) {
		t.Errorf("review findings = %+v; want %+v", rv.Findings, wantFindings)
	}
	ledger := []FindingRow{{Component: "task_review", Key: "k3", Severity: "medium", Decision: "rejected", RecordPath: "/ledger.json"}}
	if !slices.Equal(got.Findings, ledger) {
		t.Errorf("findings without a review = %+v; want %+v", got.Findings, ledger)
	}
	if keys := jsonKeys(t, rv.Findings[0]); !slices.Equal(keys, []string{"component", "decision", "key", "record_path", "severity"}) {
		t.Errorf("finding JSON keys = %v; want only references", keys)
	}
	if len(got.Recoveries) != 1 || got.Recoveries[0].PR != 9 || got.Recoveries[0].Status != "recovered" ||
		len(got.Recoveries[0].Items) != 1 || got.Recoveries[0].Items[0].Kind != "fix" || got.Recoveries[0].Items[0].Ref == "" {
		t.Errorf("Recoveries = %+v; want PR 9 recovered with one fix and its ref", got.Recoveries)
	}
}

func TestLoadDetailQuestionSplit(t *testing.T) {
	_, f := newFixture(t)
	tid := f.addTrajectory("o/r", 1)
	r := f.addRun(tid, "standard", at(0), at(10))
	f.addUsage(ofRun(r), "opus", 100, fp(1.5), "estimate")
	s := f.addSession(tid, "claude-code", "standard", at(20), at(30))
	f.addUsage(ofSession(s), "sonnet", 50, nil, "")
	f.addQuestion(tid, ofRun(r), at(10))
	f.addQuestion(tid, ofRun(r), at(11))
	f.addQuestion(tid, ofSession(s), at(25))
	f.addRecovery(tid, 5, "none", at(40), nil)

	got := detail(t, f, tid)
	if got.Questions != (QuestionCounts{Dispatcher: 2, Transcript: 1}) {
		t.Errorf("Questions = %+v; want 2 dispatcher, 1 transcript", got.Questions)
	}
	if len(got.Events) != 3 || got.Events[0].Kind != "question" || got.Events[0].RunID != r || got.Events[2].SessionID != s {
		t.Errorf("Events = %+v; want three questions in time order", got.Events)
	}
	if len(got.Runs) != 1 || got.Runs[0].ID != r || got.Runs[0].Outcome != "done" || got.Runs[0].Assignment != "standard" ||
		got.Runs[0].EndedAt == nil || len(got.Runs[0].Usage) != 1 || fmtF(got.Runs[0].Usage[0].Cost) != "1.5" {
		t.Errorf("Runs = %+v", got.Runs)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].Harness != "claude-code" || got.Sessions[0].LinkBasis != "branch" ||
		len(got.Sessions[0].Usage) != 1 || got.Sessions[0].Usage[0].Tokens != 50 || got.Sessions[0].Usage[0].Cost != nil {
		t.Errorf("Sessions = %+v", got.Sessions)
	}
	if v := got.ByCause["active"]; v == nil || *v <= 0 {
		t.Errorf("ByCause[active] = %s; want positive seconds", fmtF(v))
	}
}

func TestLoadDetailUnknownIDNotFound(t *testing.T) {
	_, f := newFixture(t)
	f.addTrajectory("o/r", 1)
	_, ok, err := LoadDetail(context.Background(), f.db, 999, at(0))
	if err != nil || ok {
		t.Errorf("LoadDetail(999) = ok %v, err %v; want not found, no error", ok, err)
	}
}

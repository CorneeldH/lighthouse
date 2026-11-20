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

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/protocol"
)

// readForge is a forge.Forge that answers the read methods from fields; every other method
// panics on the nil embedded interface, so a command that reaches for one fails the test.
type readForge struct {
	forge.Forge
	issues     map[int]forge.Issue
	list       []forge.Issue
	change     forge.Change
	blockers   []forge.Blocker
	comments   []protocol.Comment
	milestones []forge.Milestone
	changes    []forge.Change
	linked     []forge.IssueChange
	closer     int
	checks     []forge.Check
	logs       map[int64]string // a job without an entry has no log
	err        error

	listState string // the state ListChanges was asked for
}

func (r *readForge) Checks(context.Context, int) ([]forge.Check, error) { return r.checks, r.err }
func (r *readForge) JobLog(_ context.Context, job int64) (string, error) {
	if log, ok := r.logs[job]; ok {
		return log, nil
	}
	return "", forge.ErrNoJobLog
}

func (r *readForge) Issue(_ context.Context, n int) (forge.Issue, error) {
	if r.err != nil {
		return forge.Issue{}, r.err
	}
	return r.issues[n], nil
}
func (r *readForge) ListIssues(context.Context, forge.IssueFilter) ([]forge.Issue, error) {
	return r.list, r.err
}
func (r *readForge) Change(context.Context, int) (forge.Change, error) { return r.change, r.err }
func (r *readForge) BlockedBy(context.Context, int) ([]forge.Blocker, error) {
	return r.blockers, r.err
}
func (r *readForge) Blocking(context.Context, int) ([]forge.Blocker, error) {
	return r.blockers, r.err
}
func (r *readForge) ListChanges(_ context.Context, state string) ([]forge.Change, error) {
	r.listState = state
	return r.changes, r.err
}
func (r *readForge) IssueChanges(context.Context, int) ([]forge.IssueChange, error) {
	return r.linked, r.err
}
func (r *readForge) Closer(context.Context, int) (int, error) { return r.closer, r.err }
func (r *readForge) Comments(context.Context, int) ([]protocol.Comment, error) {
	return r.comments, r.err
}
func (r *readForge) ChangeComments(context.Context, int) ([]protocol.Comment, error) {
	return r.comments, r.err
}
func (r *readForge) Milestones(context.Context, string) ([]forge.Milestone, error) {
	return r.milestones, r.err
}

// runForge runs `lighthouse forge args...` against f.
func runForge(t *testing.T, f forge.Forge, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return runForgeEnv(&forgeEnv{open: func(context.Context) (forge.Forge, error) { return f, nil }}, args...)
}

func runForgeEnv(env *forgeEnv, args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.AddCommand(newForgeCommandWith(env))
	root.SetArgs(append([]string{"forge"}, args...))
	code = execute(root, &errOut)
	return code, out.String(), errOut.String()
}

func sampleIssue() forge.Issue {
	return forge.Issue{Number: 7, Title: "T", Body: "B", State: "open", URL: "https://x/7",
		Author: "ann", Labels: []string{"bug", "ready"},
		CreatedAt: time.Date(2026, 11, 1, 10, 30, 0, 0, time.UTC),
		Milestone: &forge.Milestone{Number: 3, Title: "M3", Description: "third"}}
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, s)
	}
	return m
}

func TestIssueViewJSONShape(t *testing.T) {
	f := &readForge{issues: map[int]forge.Issue{7: sampleIssue(), 8: {Number: 8, Title: "bare", State: "closed"}}}
	code, out, errOut := runForge(t, f, "issue", "view", "7")
	if code != 0 || errOut != "" {
		t.Fatalf("code %d err %q", code, errOut)
	}
	got := decode(t, out)
	want := map[string]any{
		"number": 7.0, "title": "T", "body": "B", "state": "open", "url": "https://x/7",
		"created_at": "2026-11-01T10:30:00Z", "author": "ann", "closed_at": "", "state_reason": "",
		"labels":    []any{"bug", "ready"},
		"milestone": map[string]any{"number": 3.0, "title": "M3", "description": "third"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
	if _, has := got["comments"]; has {
		t.Error("comments without --comments")
	}
	_, out, _ = runForge(t, f, "issue", "view", "8")
	bare := decode(t, out)
	if m, has := bare["milestone"]; !has || m != nil {
		t.Errorf("milestone = %v (present %v), want null", m, has)
	}
	if l, ok := bare["labels"].([]any); !ok || len(l) != 0 {
		t.Errorf("labels = %#v, want empty list", bare["labels"])
	}
}

func TestIssueViewComments(t *testing.T) {
	f := &readForge{issues: map[int]forge.Issue{7: sampleIssue()},
		comments: []protocol.Comment{{Author: "bob", Body: "hi", CreatedAt: "2026-11-02T09:00:00Z"}}}
	_, out, _ := runForge(t, f, "issue", "view", "7", "--comments")
	want := `[map[author:bob body:hi created_at:2026-11-02T09:00:00Z]]`
	if got := fmt.Sprint(decode(t, out)["comments"]); got != want {
		t.Errorf("comments = %s, want %s", got, want)
	}
}

func TestFieldScalarAndObject(t *testing.T) {
	f := &readForge{issues: map[int]forge.Issue{7: sampleIssue(), 8: {Number: 8}}}
	tests := []struct {
		args     []string
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{[]string{"7", "--field", "milestone.title"}, 0, "M3\n", ""},
		{[]string{"7", "--field", "number"}, 0, "7\n", ""},
		{[]string{"7", "--field", "labels"}, 0, `["bug","ready"]` + "\n", ""},
		{[]string{"8", "--field", "milestone.title"}, 0, "\n", ""},
		{[]string{"7", "--field", "nope"}, 1, "", "unknown field"},
	}
	for _, tt := range tests {
		code, out, errOut := runForge(t, f, append([]string{"issue", "view"}, tt.args...)...)
		if code != tt.wantCode || out != tt.wantOut || !strings.Contains(errOut, tt.wantErr) {
			t.Errorf("%v: code %d out %q err %q, want %d %q %q", tt.args, code, out, errOut, tt.wantCode, tt.wantOut, tt.wantErr)
		}
	}
}

func TestPRViewMerged(t *testing.T) {
	f := &readForge{change: forge.Change{Number: 5, Title: "P", State: "merged", Author: "ann"}}
	code, out, _ := runForge(t, f, "pr", "view", "5")
	got := decode(t, out)
	if code != 0 || got["state"] != "merged" || got["number"] != 5.0 || got["milestone"] != nil {
		t.Errorf("code %d got %v", code, got)
	}
}

func TestForgePRViewRendersMergeFields(t *testing.T) {
	f := &readForge{
		change:   forge.Change{Number: 5, State: "open", Draft: true, Mergeable: "conflicting", HeadRef: "issue-5-x", BaseRef: "main"},
		comments: []protocol.Comment{{Author: "ann", Body: "Mooi"}},
	}
	code, out, _ := runForge(t, f, "pr", "view", "5")
	got := decode(t, out)
	if code != 0 || got["draft"] != true || got["mergeable"] != "conflicting" || got["head_ref"] != "issue-5-x" || got["base_ref"] != "main" {
		t.Errorf("code %d got %v", code, got)
	}
	if _, has := got["comments"]; has {
		t.Errorf("comments present without --comments: %v", got)
	}
	_, out, _ = runForge(t, f, "pr", "view", "5", "--comments")
	cs, _ := decode(t, out)["comments"].([]any)
	if len(cs) != 1 || cs[0].(map[string]any)["body"] != "Mooi" {
		t.Errorf("comments = %v, want one with body Mooi", cs)
	}
}

func TestIssueBlockersShape(t *testing.T) {
	f := &readForge{blockers: []forge.Blocker{
		{Repo: "o/r", Issue: 4, State: "OPEN"},
		{Repo: "o/other", Issue: 9, State: "CLOSED", MergedPR: true},
	}}
	_, out, _ := runForge(t, f, "issue", "blockers", "7")
	want := `[
  {
    "merged_pr": false,
    "number": 4,
    "repo": "o/r",
    "state": "open"
  },
  {
    "merged_pr": true,
    "number": 9,
    "repo": "o/other",
    "state": "closed"
  }
]
`
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestIssueListAllPages(t *testing.T) {
	var all []forge.Issue
	for i := 1; i <= 150; i++ {
		all = append(all, forge.Issue{Number: i, Title: "t"})
	}
	_, out, _ := runForge(t, &readForge{list: all}, "issue", "list", "--field", "number")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 150 || lines[0] != "1" || lines[149] != "150" {
		t.Errorf("%d lines, first %q last %q", len(lines), lines[0], lines[len(lines)-1])
	}
}

func TestMilestoneViewAmbiguousTitleExit2(t *testing.T) {
	f := &readForge{milestones: []forge.Milestone{{Number: 11, Title: "v1"}, {Number: 502, Title: "v1"}, {Number: 12, Title: "v2"}}}
	code, out, errOut := runForge(t, f, "milestone", "view", "v1")
	if code != 2 || out != "" || !strings.Contains(errOut, "11") || !strings.Contains(errOut, "502") {
		t.Errorf("code %d out %q err %q", code, out, errOut)
	}
	code, out, _ = runForge(t, f, "milestone", "view", "v2", "--field", "number")
	if code != 0 || out != "12\n" {
		t.Errorf("unique title: code %d out %q", code, out)
	}
	if code, _, errOut = runForge(t, f, "milestone", "view", "nope"); code != 1 || !strings.Contains(errOut, "nope") {
		t.Errorf("missing title: code %d err %q", code, errOut)
	}
}

func TestForgeErrorExit1KeepsMessage(t *testing.T) {
	f := &readForge{err: errors.New("GET /issues/7: 404 Not Found")}
	code, out, errOut := runForge(t, f, "issue", "view", "7")
	if code != 1 || out != "" || !strings.Contains(errOut, "GET /issues/7: 404 Not Found") {
		t.Errorf("code %d out %q err %q", code, out, errOut)
	}
}

func TestNoMergeSubcommand(t *testing.T) {
	code, out, errOut := runForge(t, &readForge{}, "pr", "merge", "5")
	if code != 2 || out != "" || !strings.Contains(errOut, "merge") {
		t.Errorf("code %d out %q err %q", code, out, errOut)
	}
}

func TestForgeUsageErrorsExit2(t *testing.T) {
	for _, args := range [][]string{{}, {"bogus"}, {"issue"}, {"issue", "view"}, {"issue", "view", "x"},
		{"issue", "view", "7", "--bogus"}, {"issue", "list", "--state", "weird"}} {
		if code, _, _ := runForge(t, &readForge{}, args...); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
}

func forgeProject(t *testing.T) string {
	t.Helper()
	return gateProject(t)
}

func TestMissingConfigExit2(t *testing.T) {
	dir := t.TempDir() // no lighthouse.toml
	env := &forgeEnv{open: forgeOpenFrom(newForgeOpener(func(string) string { return "tok" }, ""), dir)}
	code, out, errOut := runForgeEnv(env, "issue", "view", "7")
	if code != 2 || out != "" || !strings.Contains(errOut, "lighthouse.toml") {
		t.Errorf("code %d out %q err %q", code, out, errOut)
	}
}

func TestOpenerFallsBackToGhAuthToken(t *testing.T) {
	srv := forgetest.New(t)
	srv.Issues = map[int]map[string]any{7: {"number": 7, "title": "From gh", "state": "open"}}
	project := forgeProject(t)
	toml, err := os.ReadFile(filepath.Join(project, "lighthouse.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(toml), "Acme/widgets") {
		t.Fatal("fixture repo changed")
	}
	getenv := func(string) string { return "" } // no GH_TOKEN, no GITHUB_TOKEN
	o := newForgeOpener(getenv, srv.URL())

	code, _, errOut := runForgeEnv(&forgeEnv{open: forgeOpenFrom(o, project)}, "issue", "view", "7")
	if code != 2 || !strings.Contains(errOut, "GH_TOKEN") {
		t.Fatalf("without a token: code %d err %q, want 2 naming GH_TOKEN", code, errOut)
	}

	o.ghToken = func() string { return "gh-token" }
	code, out, errOut := runForgeEnv(&forgeEnv{open: forgeOpenFrom(o, project)}, "issue", "view", "7", "--field", "title")
	if code != 0 || out != "From gh\n" {
		t.Errorf("with gh token: code %d out %q err %q", code, out, errOut)
	}
}

func TestDefaultForgeOpenerUsesGhAuthToken(t *testing.T) {
	o := newDefaultForgeOpener()
	if o.ghToken == nil {
		t.Fatal("the opener of lighthouse forge has no gh auth token fallback")
	}
}

func TestIssueViewClosedAtAndStateReason(t *testing.T) {
	f := &readForge{issues: map[int]forge.Issue{
		8: {Number: 8, State: "closed", StateReason: "not_planned", ClosedAt: time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)}}}
	for field, want := range map[string]string{"state_reason": "not_planned\n", "closed_at": "2026-11-05T08:00:00Z\n"} {
		if code, out, _ := runForge(t, f, "issue", "view", "8", "--field", field); code != 0 || out != want {
			t.Errorf("--field %s: code %d out %q, want %q", field, code, out, want)
		}
	}
}

func TestIssueBlockingShape(t *testing.T) {
	f := &readForge{blockers: []forge.Blocker{{Repo: "o/r", Issue: 12, State: "OPEN"}, {Repo: "o/x", Issue: 3, State: "CLOSED", MergedPR: true}}}
	_, out, _ := runForge(t, f, "issue", "blocking", "7")
	want := `[
  {
    "merged_pr": false,
    "number": 12,
    "repo": "o/r",
    "state": "open"
  },
  {
    "merged_pr": true,
    "number": 3,
    "repo": "o/x",
    "state": "closed"
  }
]
`
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestIssueChangesShape(t *testing.T) {
	linked := []forge.IssueChange{
		{Number: 14, State: "MERGED", HeadRef: "issue-7-x", CrossRepo: false, Repo: "o/r", Body: "Closes #7", Closes: true},
		{Number: 15, State: "OPEN", HeadRef: "fork-br", CrossRepo: true, Repo: "o/r", Body: "see #7"},
	}
	f := &readForge{issues: map[int]forge.Issue{7: {Number: 7, State: "closed"}}, linked: linked, closer: 14}
	_, out, errOut := runForge(t, f, "issue", "changes", "7")
	want := `{
  "changes": [
    {
      "body": "Closes #7",
      "closes": true,
      "cross_repo": false,
      "head_ref": "issue-7-x",
      "number": 14,
      "repo": "o/r",
      "state": "merged"
    },
    {
      "body": "see #7",
      "closes": false,
      "cross_repo": true,
      "head_ref": "fork-br",
      "number": 15,
      "repo": "o/r",
      "state": "open"
    }
  ],
  "closer": 14,
  "state": "closed"
}
`
	if out != want {
		t.Errorf("err %q\ngot\n%s\nwant\n%s", errOut, out, want)
	}
	f = &readForge{issues: map[int]forge.Issue{7: {Number: 7, State: "open"}}}
	_, out, _ = runForge(t, f, "issue", "changes", "7")
	got := decode(t, out)
	if c, has := got["closer"]; !has || c != nil || got["state"] != "open" || fmt.Sprint(got["changes"]) != "[]" {
		t.Errorf("no closer: got %v", got)
	}
}

func TestPRListStates(t *testing.T) {
	merged := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	f := &readForge{changes: []forge.Change{
		{Number: 14, Title: "A", State: "merged", HeadRef: "issue-7-x", MergedAt: merged, Author: "ann"},
		{Number: 15, Title: "B", State: "open", HeadRef: "b"},
	}}
	code, out, errOut := runForge(t, f, "pr", "list", "--state", "merged")
	if code != 0 || f.listState != "merged" {
		t.Fatalf("code %d err %q state %q", code, errOut, f.listState)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) != 2 {
		t.Fatalf("out %q: %v", out, err)
	}
	if got[0]["merged_at"] != "2026-11-03T09:00:00Z" || got[0]["head_ref"] != "issue-7-x" || got[0]["state"] != "merged" {
		t.Errorf("merged: %v", got[0])
	}
	if v, has := got[1]["merged_at"]; !has || v != nil {
		t.Errorf("open merged_at = %v (present %v), want null", v, has)
	}
	for _, k := range []string{"number", "title", "body", "labels", "milestone", "url", "created_at", "author"} {
		if _, has := got[0][k]; !has {
			t.Errorf("missing %q", k)
		}
	}
	if code, _, _ = runForge(t, f, "pr", "list"); code != 0 || f.listState != "" {
		t.Errorf("default: code %d state %q", code, f.listState)
	}
	if code, _, errOut = runForge(t, f, "pr", "list", "--state", "weird"); code != 2 || !strings.Contains(errOut, "weird") {
		t.Errorf("bad state: code %d err %q", code, errOut)
	}
}

func TestMilestoneViewCounts(t *testing.T) {
	f := &readForge{milestones: []forge.Milestone{{Number: 3, Title: "v3", State: "open", OpenIssues: 4, ClosedIssues: 9}}}
	_, out, _ := runForge(t, f, "milestone", "view", "v3")
	got := decode(t, out)
	if got["open_issues"] != 4.0 || got["closed_issues"] != 9.0 {
		t.Errorf("got %v", got)
	}
}

func TestPrChecksEmptyPrintsEmptyArray(t *testing.T) {
	code, out, errOut := runForge(t, &readForge{}, "pr", "checks", "5")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Errorf("code %d stdout %q stderr %q; want exit 0 and exactly []", code, out, errOut)
	}
}

func TestForgePRChecksJSONKeys(t *testing.T) {
	f := &readForge{checks: []forge.Check{{Name: "build", Bucket: "pass", Link: "https://x/1", Job: 7}}}
	code, out, _ := runForge(t, f, "pr", "checks", "5")
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 0 || len(got) != 1 {
		t.Fatalf("code %d out %q err %v", code, out, err)
	}
	if got[0]["name"] != "build" || got[0]["bucket"] != "pass" || got[0]["link"] != "https://x/1" || got[0]["job"] != float64(7) {
		t.Errorf("got %v", got[0])
	}
	_, out, _ = runForge(t, f, "pr", "checks", "5", "--field", "bucket")
	if strings.TrimSpace(out) != "pass" {
		t.Errorf("--field bucket = %q", out)
	}
}

func TestForgePRChecksFailedLog(t *testing.T) {
	f := &readForge{
		checks: []forge.Check{
			{Name: "build", Bucket: "fail", Job: 7},
			{Name: "status-ci", Bucket: "fail"},
			{Name: "lint", Bucket: "pass", Job: 8},
			{Name: "gone", Bucket: "fail", Job: 9},
		},
		logs: map[int64]string{7: "compile error\n", 8: "all fine\n"},
	}
	code, out, errOut := runForge(t, f, "pr", "checks", "5", "--failed-log")
	if code != 0 {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	for _, want := range []string{"--- job 7 build ---\ncompile error\n", "no log for job 0", "no log for job 9"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "all fine") || strings.Contains(out, "lint") {
		t.Errorf("a passing check leaked into the failed log:\n%s", out)
	}
}

func TestForgePRChecksForgeErrorExits1(t *testing.T) {
	code, _, _ := runForge(t, &readForge{err: errors.New("boom")}, "pr", "checks", "5")
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
}

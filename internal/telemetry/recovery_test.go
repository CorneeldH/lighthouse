package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeSource struct {
	merge     Commit
	mergeErr  error
	commits   []Commit
	issues    []BugIssue
	commitErr error
}

func (f fakeSource) MergeCommit(context.Context, int) (Commit, error) {
	return f.merge, f.mergeErr
}

// CommitsAfter deliberately ignores its arguments: MeasureRecovery must filter
// on the window itself.
func (f fakeSource) CommitsAfter(context.Context, time.Time, time.Time) ([]Commit, error) {
	return f.commits, f.commitErr
}

func (f fakeSource) IssuesMentioning(context.Context, int) ([]BugIssue, error) {
	return f.issues, nil
}

func TestMeasureRecovery(t *testing.T) {
	mergedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	day := func(n int) time.Time { return mergedAt.Add(time.Duration(n) * 24 * time.Hour) }
	merge := Commit{SHA: "merge1", Subject: "feat: x (#12)", At: mergedAt, Files: []string{"a/x.go", "b/y.go"}}
	commit := func(sha, subject string, at time.Time, files ...string) Commit {
		return Commit{SHA: sha, Subject: subject, At: at, Files: files}
	}
	bug := func(n int, title, body string, at time.Time, labels ...string) BugIssue {
		return BugIssue{Number: n, Title: title, Body: body, Labels: labels, CreatedAt: at}
	}

	tests := []struct {
		name       string
		commits    []Commit
		issues     []BugIssue
		now        time.Time
		wantStatus RecoveryStatus
		wantItems  []RecoveryItem
	}{
		{
			name:       "fix on shared path, window over",
			commits:    []Commit{commit("f1", "fix(x): repair", day(2), "a/x.go")},
			now:        day(10),
			wantStatus: RecoveryRecovered,
			wantItems:  []RecoveryItem{{Kind: "fix", Ref: "f1", At: day(2)}},
		},
		{
			name:       "fix, window still open",
			commits:    []Commit{commit("f1", "fix(x): repair", day(2), "a/x.go")},
			now:        day(3),
			wantStatus: RecoveryOpen,
			wantItems:  []RecoveryItem{{Kind: "fix", Ref: "f1", At: day(2)}},
		},
		{name: "nothing, open", now: day(3), wantStatus: RecoveryOpen},
		{name: "nothing, closed", now: day(8), wantStatus: RecoveryNone},
		{
			name:       "one second before the window end: open",
			now:        mergedAt.Add(RecoveryWindow - time.Second),
			wantStatus: RecoveryOpen,
		},
		{
			name:       "exactly at the window end: closed",
			now:        mergedAt.Add(RecoveryWindow),
			wantStatus: RecoveryNone,
		},
		{
			name:       "same file name in another directory",
			commits:    []Commit{commit("f1", "fix: repair", day(2), "c/x.go")},
			now:        day(10),
			wantStatus: RecoveryNone,
		},
		{
			name:       "feat commit on shared path",
			commits:    []Commit{commit("f1", "feat: more", day(2), "a/x.go")},
			now:        day(10),
			wantStatus: RecoveryNone,
		},
		{
			name:       "revert on shared path",
			commits:    []Commit{commit("r1", `Revert "feat: x"`, day(2), "b/y.go")},
			now:        day(10),
			wantStatus: RecoveryRecovered,
			wantItems:  []RecoveryItem{{Kind: "revert", Ref: "r1", At: day(2)}},
		},
		{
			name:       "fix exactly at +7d counts",
			commits:    []Commit{commit("f1", "fix: repair", mergedAt.Add(RecoveryWindow), "a/x.go")},
			now:        day(10),
			wantStatus: RecoveryRecovered,
			wantItems:  []RecoveryItem{{Kind: "fix", Ref: "f1", At: mergedAt.Add(RecoveryWindow)}},
		},
		{
			name:       "fix at +7d+1s does not count",
			commits:    []Commit{commit("f1", "fix: repair", mergedAt.Add(RecoveryWindow+time.Second), "a/x.go")},
			now:        day(10),
			wantStatus: RecoveryNone,
		},
		{
			name:       "fix at merge time does not count (window is open at the start)",
			commits:    []Commit{commit("f1", "fix: repair", mergedAt, "a/x.go")},
			now:        day(10),
			wantStatus: RecoveryNone,
		},
		{
			name:       "merge commit itself is excluded",
			commits:    []Commit{commit("merge1", "fix: x (#12)", day(1), "a/x.go")},
			now:        day(10),
			wantStatus: RecoveryNone,
		},
		{
			name:       "bug issue with label Bug and #12",
			issues:     []BugIssue{bug(40, "broken", "caused by #12", day(1), "Bug")},
			now:        day(10),
			wantStatus: RecoveryRecovered,
			wantItems:  []RecoveryItem{{Kind: "bug_issue", Ref: "40", At: day(1)}},
		},
		{
			name:       "bug issue mentioning in title",
			issues:     []BugIssue{bug(41, "regression from #12", "", day(1), "bug")},
			now:        day(10),
			wantStatus: RecoveryRecovered,
			wantItems:  []RecoveryItem{{Kind: "bug_issue", Ref: "41", At: day(1)}},
		},
		{
			name:       "issue without bug label",
			issues:     []BugIssue{bug(40, "x", "#12", day(1), "enhancement")},
			now:        day(10),
			wantStatus: RecoveryNone,
		},
		{
			name:       "issue created before the merge",
			issues:     []BugIssue{bug(40, "x", "#12", day(-1), "bug")},
			now:        day(10),
			wantStatus: RecoveryNone,
		},
		{
			name:       "issue created after the window",
			issues:     []BugIssue{bug(40, "x", "#12", day(8), "bug")},
			now:        day(10),
			wantStatus: RecoveryNone,
		},
		{
			name:       "#120 is not #12",
			issues:     []BugIssue{bug(40, "x", "see #120", day(1), "bug")},
			now:        day(10),
			wantStatus: RecoveryNone,
		},
		{
			name: "items sorted by time then ref",
			commits: []Commit{
				commit("f2", "fix: b", day(3), "a/x.go"),
				commit("f1", "fix: a", day(2), "a/x.go"),
			},
			issues:     []BugIssue{bug(40, "x", "#12", day(2), "bug")},
			now:        day(10),
			wantStatus: RecoveryRecovered,
			wantItems: []RecoveryItem{
				{Kind: "bug_issue", Ref: "40", At: day(2)},
				{Kind: "fix", Ref: "f1", At: day(2)},
				{Kind: "fix", Ref: "f2", At: day(3)},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := fakeSource{merge: merge, commits: tt.commits, issues: tt.issues}
			got, err := MeasureRecovery(context.Background(), src, 12, mergedAt, tt.now)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.PR != 12 || !got.MergedAt.Equal(mergedAt) || !got.WindowEnd.Equal(mergedAt.Add(RecoveryWindow)) {
				t.Errorf("header = %+v", got)
			}
			if len(got.Items) != len(tt.wantItems) {
				t.Fatalf("items = %+v, want %+v", got.Items, tt.wantItems)
			}
			for i, w := range tt.wantItems {
				g := got.Items[i]
				if g.Kind != w.Kind || g.Ref != w.Ref || !g.At.Equal(w.At) {
					t.Errorf("item %d = %+v, want %+v", i, g, w)
				}
			}
		})
	}
}

func TestMeasureRecoveryNoMergeCommit(t *testing.T) {
	src := fakeSource{mergeErr: ErrNoMergeCommit}
	_, err := MeasureRecovery(context.Background(), src, 12, time.Now(), time.Now())
	if !errors.Is(err, ErrNoMergeCommit) {
		t.Fatalf("err = %v, want ErrNoMergeCommit", err)
	}
}

func TestClassifySubject(t *testing.T) {
	tests := map[string]string{
		"fix: a":           "fix",
		"fix(scope): a":    "fix",
		"fix!: a":          "fix",
		"Fix(scope): a":    "fix",
		`Revert "feat: x"`: "revert",
		"revert: x":        "revert",
		"feat: a":          "",
		"fixture: a":       "",
		"prefix: fix: a":   "",
		"docs: fix a typo": "",
		"":                 "",
	}
	for subject, want := range tests {
		if got := classifySubject(subject); got != want {
			t.Errorf("classifySubject(%q) = %q, want %q", subject, got, want)
		}
	}
}

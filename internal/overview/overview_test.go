package overview

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
)

type fakeSource struct {
	issues      []forge.Issue
	listErr     error
	comments    map[int][]protocol.Comment
	failNumbers map[int]error

	mu     sync.Mutex
	called []int
}

func (f *fakeSource) OpenIssues(context.Context) ([]forge.Issue, error) {
	return f.issues, f.listErr
}

func (f *fakeSource) Comments(_ context.Context, number int) ([]protocol.Comment, error) {
	f.mu.Lock()
	f.called = append(f.called, number)
	f.mu.Unlock()
	if err := f.failNumbers[number]; err != nil {
		return nil, err
	}
	return f.comments[number], nil
}

func issue(n int) forge.Issue {
	return forge.Issue{Number: n, Title: fmt.Sprintf("issue %d", n), URL: fmt.Sprintf("https://x/issues/%d", n), Comments: 1}
}

func question(url, at, text string) protocol.Comment {
	return protocol.Comment{Body: "🤖 Question\n\n" + text, CreatedAt: at, URL: url}
}

func returnedComment(at string) []protocol.Comment {
	return []protocol.Comment{{Body: "🤖 Returned\nreason: too big", CreatedAt: at, URL: "https://x/c"}}
}

func TestBuild(t *testing.T) {
	noComments := issue(4)
	noComments.Comments = 0
	src := &fakeSource{
		issues: []forge.Issue{issue(1), issue(2), issue(3), noComments},
		comments: map[int][]protocol.Comment{
			1: {question("https://x/c1", "2026-01-01T10:00:00Z", "Which database?")},
			2: {
				question("https://x/c2", "2026-01-01T10:00:00Z", "Which port?"),
				{Body: "8080", CreatedAt: "2026-01-02T10:00:00Z"},
			},
			3: returnedComment("2026-01-03T10:00:00Z"),
		},
	}

	got, err := Build(context.Background(), src)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(got.Questions) != 1 {
		t.Fatalf("questions = %+v, want one", got.Questions)
	}
	q := got.Questions[0]
	want := protocol.QuestionText(src.comments[1][0].Body)
	if want == "" {
		t.Fatal("fixture question text is empty")
	}
	if q.Number != 1 || q.Title != "issue 1" || q.URL != "https://x/issues/1" ||
		q.Question != want || q.AnswerURL != "https://x/c1" {
		t.Errorf("question = %+v", q)
	}
	if len(got.Returned) != 1 {
		t.Fatalf("returned = %+v, want one", got.Returned)
	}
	if r := got.Returned[0]; r.Number != 3 || r.Reason != "too big" || r.URL != "https://x/issues/3" || r.Title != "issue 3" {
		t.Errorf("returned = %+v", r)
	}
	for _, n := range src.called {
		if n == 4 {
			t.Error("Comments called for the issue with Comments == 0")
		}
	}
}

func TestBuildAnswerURLFallsBackToIssue(t *testing.T) {
	src := &fakeSource{
		issues:   []forge.Issue{issue(1)},
		comments: map[int][]protocol.Comment{1: {question("", "2026-01-01T10:00:00Z", "Why?")}},
	}
	got, err := Build(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if got.Questions[0].AnswerURL != "https://x/issues/1" {
		t.Errorf("AnswerURL = %q", got.Questions[0].AnswerURL)
	}
}

func TestBuildOrder(t *testing.T) {
	src := &fakeSource{
		issues: []forge.Issue{issue(5), issue(3), issue(9), issue(7), issue(2), issue(8)},
		comments: map[int][]protocol.Comment{
			5: {question("c5", "2026-02-01T00:00:00Z", "a")},
			3: {question("c3", "2026-02-01T00:00:00Z", "b")},
			9: {question("c9", "2026-01-01T00:00:00Z", "c")},
			7: returnedComment("2026-03-01T00:00:00Z"),
			2: returnedComment("2026-03-01T00:00:00Z"),
			8: returnedComment("2026-01-01T00:00:00Z"),
		},
	}

	got, err := Build(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	var qs, rs []int
	for _, q := range got.Questions {
		qs = append(qs, q.Number)
	}
	for _, r := range got.Returned {
		rs = append(rs, r.Number)
	}
	if fmt.Sprint(qs) != "[9 3 5]" {
		t.Errorf("question order = %v, want [9 3 5]", qs)
	}
	if fmt.Sprint(rs) != "[8 2 7]" {
		t.Errorf("returned order = %v, want [8 2 7]", rs)
	}
}

func TestBuildFailsWhole(t *testing.T) {
	boom := errors.New("boom")
	src := &fakeSource{comments: map[int][]protocol.Comment{}, failNumbers: map[int]error{37: boom}}
	for n := 1; n <= 50; n++ {
		src.issues = append(src.issues, issue(n))
		src.comments[n] = []protocol.Comment{question("c", "2026-01-01T00:00:00Z", "q")}
	}
	got, err := Build(context.Background(), src)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if got.Questions != nil || got.Returned != nil {
		t.Errorf("overview = %+v, want empty", got)
	}
}

func TestBuildListFails(t *testing.T) {
	boom := errors.New("list down")
	got, err := Build(context.Background(), &fakeSource{listErr: boom})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want list down", err)
	}
	if got.Questions != nil || got.Returned != nil {
		t.Errorf("overview = %+v, want empty", got)
	}
}

func TestBuildEmptyIsNotNil(t *testing.T) {
	got, err := Build(context.Background(), &fakeSource{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Questions == nil || got.Returned == nil {
		t.Errorf("overview = %+v, want non-nil slices", got)
	}
	if len(got.Questions)+len(got.Returned) != 0 {
		t.Errorf("overview = %+v, want empty", got)
	}
}

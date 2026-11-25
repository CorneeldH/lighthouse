package overview

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

type fakeSource struct {
	forge.Forge
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

// answerBy is an answer comment of login, after a question of 2026-01-01.
func answerBy(login, at string) protocol.Comment {
	return protocol.Comment{Author: login, Body: "8080", CreatedAt: at}
}

// trustOnly trusts the listed logins and counts its calls, so a test sees a check per login.
func trustOnly(calls *[]string, logins ...string) Trusted {
	return func(_ context.Context, login string) (bool, error) {
		*calls = append(*calls, login)
		return slices.Contains(logins, login), nil
	}
}

func TestBuildUntrustedAnswerStaysQuestion(t *testing.T) {
	src := &fakeSource{
		issues: []forge.Issue{issue(1)},
		comments: map[int][]protocol.Comment{1: {
			question("https://x/c1", "2026-01-01T10:00:00Z", "Which port?"),
			answerBy("stranger", "2026-01-02T10:00:00Z"),
		}},
	}
	var calls []string
	got, err := Build(context.Background(), src, nil, nil, repoconfig.Labels{}, trustOnly(&calls, "owner"))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(got.Questions) != 1 || got.Questions[0].Number != 1 {
		t.Errorf("questions = %+v, want issue 1 still open: an answer of a stranger answers nothing", got.Questions)
	}
	if !slices.Equal(calls, []string{"stranger"}) {
		t.Errorf("trust asked about %q, want the author of the answer", calls)
	}
}

func TestBuildTrustedAnswerIsAnswered(t *testing.T) {
	src := &fakeSource{
		issues: []forge.Issue{issue(1), issue(2)},
		comments: map[int][]protocol.Comment{
			1: {
				question("https://x/c1", "2026-01-01T10:00:00Z", "Which port?"),
				answerBy("stranger", "2026-01-02T10:00:00Z"),
				answerBy("owner", "2026-01-03T10:00:00Z"),
			},
			2: {
				question("https://x/c2", "2026-01-01T10:00:00Z", "Which host?"),
				answerBy("owner", "2026-01-02T10:00:00Z"),
			},
		},
	}
	var calls []string
	got, err := Build(context.Background(), src, nil, nil, repoconfig.Labels{}, trustOnly(&calls, "owner"))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(got.Questions) != 0 {
		t.Errorf("questions = %+v, want none: a trusted author answered both", got.Questions)
	}
}

func TestBuildTrustFailureFailsBuild(t *testing.T) {
	src := &fakeSource{
		issues: []forge.Issue{issue(1)},
		comments: map[int][]protocol.Comment{1: {
			question("https://x/c1", "2026-01-01T10:00:00Z", "Which port?"),
			answerBy("owner", "2026-01-02T10:00:00Z"),
		}},
	}
	boom := errors.New("role lookup failed")
	_, err := Build(context.Background(), src, nil, nil, repoconfig.Labels{}, func(context.Context, string) (bool, error) { return false, boom })
	if !errors.Is(err, boom) {
		t.Errorf("Build err = %v, want %v", err, boom)
	}
}

func TestBuildAllNewTrustedPerBuild(t *testing.T) {
	src := &fakeSource{
		issues: []forge.Issue{issue(1)},
		comments: map[int][]protocol.Comment{1: {
			question("https://x/c1", "2026-01-01T10:00:00Z", "Which port?"),
			answerBy("stranger", "2026-01-02T10:00:00Z"),
		}},
	}
	made := 0
	repos := []RepoSource{{Slug: "o/a", Src: src, NewTrusted: func() Trusted {
		made++
		return func(context.Context, string) (bool, error) { return false, nil }
	}}}
	for range 2 {
		got, err := BuildAll(context.Background(), repos)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Questions) != 1 || got.Questions[0].Repo != "o/a" {
			t.Errorf("questions = %+v, want the untrusted-answered question of o/a", got.Questions)
		}
	}
	if made != 2 {
		t.Errorf("NewTrusted called %d times, want once per BuildAll", made)
	}
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

	got, err := Build(context.Background(), src, nil, nil, repoconfig.Labels{}, nil)
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
	got, err := Build(context.Background(), src, nil, nil, repoconfig.Labels{}, nil)
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

	got, err := Build(context.Background(), src, nil, nil, repoconfig.Labels{}, nil)
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
	got, err := Build(context.Background(), src, nil, nil, repoconfig.Labels{}, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if got.Questions != nil || got.Returned != nil {
		t.Errorf("overview = %+v, want empty", got)
	}
}

func TestBuildListFails(t *testing.T) {
	boom := errors.New("list down")
	got, err := Build(context.Background(), &fakeSource{listErr: boom}, nil, nil, repoconfig.Labels{}, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want list down", err)
	}
	if got.Questions != nil || got.Returned != nil {
		t.Errorf("overview = %+v, want empty", got)
	}
}

func TestBuildEmptyIsNotNil(t *testing.T) {
	got, err := Build(context.Background(), &fakeSource{}, nil, nil, repoconfig.Labels{}, nil)
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

func TestBuildOnlyController(t *testing.T) {
	// 945 is paia#945 on 2026-10-26: a run that asks an open question outside Controller.
	src := &fakeSource{
		issues: []forge.Issue{issue(945), issue(12), issue(13)},
		comments: map[int][]protocol.Comment{
			945: {question("c945", "2026-10-26T10:00:00Z", "A of B?")},
			12:  returnedComment("2026-10-26T11:00:00Z"),
			13:  {question("c13", "2026-10-26T12:00:00Z", "Welke poort?")},
		},
	}
	got, err := Build(context.Background(), src, map[int]bool{12: true, 13: true}, nil, repoconfig.Labels{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Questions) != 1 || got.Questions[0].Number != 13 {
		t.Errorf("questions = %+v, want only 13", got.Questions)
	}
	if len(got.Returned) != 1 || got.Returned[0].Number != 12 {
		t.Errorf("returned = %+v, want only 12", got.Returned)
	}
	for _, n := range src.called {
		if n == 945 {
			t.Error("Comments called for 945, which is not in Controller")
		}
	}
}

// fakeBoard is a forge whose board read is counted; items and err are what it returns.
type fakeBoard struct {
	forge.Forge
	items []forge.BoardItem
	err   error
	calls atomic.Int32
}

func (f *fakeBoard) BoardItems(_ context.Context) ([]forge.BoardItem, error) {
	f.calls.Add(1)
	return f.items, f.err
}

// boardSource is a fakeSource that also serves a board.
type boardSource struct {
	*fakeSource
	board *fakeBoard
}

func (b boardSource) BoardItems(ctx context.Context) ([]forge.BoardItem, error) {
	return b.board.BoardItems(ctx)
}

// threeIssues has an open question on each of 1, 2 and 3.
func threeIssues() *fakeSource {
	c := func(n int) []protocol.Comment {
		return []protocol.Comment{question(fmt.Sprintf("c%d", n), "2026-01-01T10:00:00Z", "q")}
	}
	return &fakeSource{
		issues:   []forge.Issue{issue(1), issue(2), issue(3)},
		comments: map[int][]protocol.Comment{1: c(1), 2: c(2), 3: c(3)},
	}
}

func threeItems() []forge.BoardItem {
	return []forge.BoardItem{
		{Repo: "o/r", Issue: 1, Status: "Controller"},
		{Repo: "o/r", Issue: 2, Status: "In progress"},
		{Repo: "o/r", Issue: 3, Status: ""},
		{Repo: "x/y", Issue: 1, Status: "Controller"},
	}
}

func TestBuildAllReadsBoardOncePerRepo(t *testing.T) {
	board := &fakeBoard{items: threeItems()}
	got, err := BuildAll(context.Background(), []RepoSource{
		{Slug: "o/r", Src: boardSource{threeIssues(), board}, Board: true, Controller: true}})
	if err != nil {
		t.Fatal(err)
	}
	if n := board.calls.Load(); n != 1 {
		t.Errorf("BoardItems calls = %d, want 1", n)
	}
	if len(got.Questions) != 1 || got.Questions[0].Number != 1 {
		t.Errorf("questions = %+v, want only 1", got.Questions)
	}
	want := []IssueStatus{{"o/r", 1, "Controller"}, {"o/r", 2, "In progress"}, {"o/r", 3, ""}}
	if !slices.Equal(got.Statuses, want) {
		t.Errorf("statuses = %+v, want %+v", got.Statuses, want)
	}
}

func TestBuildAllNoBoardNoBoardItemsCall(t *testing.T) {
	board := &fakeBoard{items: threeItems()}
	got, err := BuildAll(context.Background(), []RepoSource{
		{Slug: "o/r", Src: boardSource{threeIssues(), board}}})
	if err != nil {
		t.Fatal(err)
	}
	if n := board.calls.Load(); n != 0 {
		t.Errorf("BoardItems calls = %d, want 0", n)
	}
	if got.Statuses == nil || len(got.Statuses) != 0 {
		t.Errorf("statuses = %#v, want empty non-nil", got.Statuses)
	}
	if len(got.Questions) != 3 {
		t.Errorf("questions = %+v, want all three", got.Questions)
	}
}

func TestBuildAllBoardWithoutControllerOptionShowsEveryIssue(t *testing.T) {
	board := &fakeBoard{items: threeItems()}
	got, err := BuildAll(context.Background(), []RepoSource{
		{Slug: "o/r", Src: boardSource{threeIssues(), board}, Board: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Questions) != 3 {
		t.Errorf("questions = %+v, want all three", got.Questions)
	}
	if len(got.Statuses) != 3 {
		t.Errorf("statuses = %+v, want three", got.Statuses)
	}
}

func TestBuildAllBoardErrorWithoutControllerOptionKeepsOverview(t *testing.T) {
	board := &fakeBoard{err: errors.New("graphql down")}
	got, err := BuildAll(context.Background(), []RepoSource{
		{Slug: "o/r", Src: boardSource{threeIssues(), board}, Board: true}})
	if err != nil {
		t.Fatalf("err = %v, want the overview without statuses", err)
	}
	if len(got.Questions) != 3 || got.Statuses == nil || len(got.Statuses) != 0 {
		t.Errorf("overview = %+v, want three questions and empty statuses", got)
	}
}

func TestBuildAllBoardErrorWithControllerFails(t *testing.T) {
	boom := errors.New("graphql down")
	board := &fakeBoard{err: boom}
	got, err := BuildAll(context.Background(), []RepoSource{
		{Slug: "o/r", Src: boardSource{threeIssues(), board}, Board: true, Controller: true}})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "o/r") {
		t.Fatalf("err = %v, want o/r: graphql down", err)
	}
	if got.Questions != nil || got.Returned != nil {
		t.Errorf("overview = %+v, want empty", got)
	}
}

// TestBuildAllReturnedOnToDo is #605: since #448 a returned issue moves to To do with a needs_*
// label, so the Returned list must read it there too. 379 is returned on To do with
// 3-needs-hands; 20 is on To do with an open question and a needs_* label (not a returned
// issue, so no question either); 21 is on To do without a needs_* label (a human's own work,
// whose comments are never read); 22 is returned and still on Controller (board write failed).
func TestBuildAllReturnedOnToDo(t *testing.T) {
	l := repoconfig.DefaultLabels()
	withLabel := func(n int, labels ...string) forge.Issue {
		is := issue(n)
		is.Labels = labels
		return is
	}
	src := &fakeSource{
		issues: []forge.Issue{withLabel(379, l.NeedsHands), withLabel(20, l.NeedsDecision), withLabel(21), withLabel(22, l.NeedsHands)},
		comments: map[int][]protocol.Comment{
			379: returnedComment("2026-11-25T08:00:00Z"),
			20:  {question("c20", "2026-11-25T09:00:00Z", "A of B?")},
			21:  returnedComment("2026-11-25T10:00:00Z"),
			22:  returnedComment("2026-11-25T11:00:00Z"),
		},
	}
	board := &fakeBoard{items: []forge.BoardItem{
		{Repo: "o/r", Issue: 379, Status: "To do"},
		{Repo: "o/r", Issue: 20, Status: "To do"},
		{Repo: "o/r", Issue: 21, Status: "To do"},
		{Repo: "o/r", Issue: 22, Status: "Controller"},
	}}
	got, err := BuildAll(context.Background(), []RepoSource{
		{Slug: "o/r", Src: boardSource{src, board}, Board: true, Controller: true, Labels: l}})
	if err != nil {
		t.Fatal(err)
	}
	var nums []int
	for _, r := range got.Returned {
		nums = append(nums, r.Number)
	}
	if !slices.Equal(nums, []int{379, 22}) {
		t.Errorf("returned = %v, want [379 22]", nums)
	}
	if len(got.Questions) != 0 {
		t.Errorf("questions = %+v, want none: 20 is not in Controller", got.Questions)
	}
	if slices.Contains(src.called, 21) {
		t.Error("Comments called for 21, on To do without a needs_* label")
	}
}

func questionSource(n int) *fakeSource {
	return &fakeSource{
		issues:   []forge.Issue{issue(n)},
		comments: map[int][]protocol.Comment{n: {question("c", "2026-01-01T10:00:00Z", "Why?")}},
	}
}

func TestBuildAllSetsRepo(t *testing.T) {
	got, err := BuildAll(context.Background(), []RepoSource{
		{Slug: "o/a", Src: questionSource(1)},
		{Slug: "o/b", Src: questionSource(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Questions) != 2 {
		t.Fatalf("questions = %+v, want two", got.Questions)
	}
	if q := got.Questions[0]; q.Repo != "o/a" || q.Number != 1 {
		t.Errorf("Questions[0] = %+v", q)
	}
	if q := got.Questions[1]; q.Repo != "o/b" || q.Number != 2 {
		t.Errorf("Questions[1] = %+v", q)
	}
}

func TestBuildAllSetsRepoOnReturned(t *testing.T) {
	src := &fakeSource{
		issues:   []forge.Issue{issue(3)},
		comments: map[int][]protocol.Comment{3: returnedComment("2026-01-03T10:00:00Z")},
	}
	got, err := BuildAll(context.Background(), []RepoSource{{Slug: "o/a", Src: src}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Returned) != 1 || got.Returned[0].Repo != "o/a" {
		t.Errorf("returned = %+v", got.Returned)
	}
}

func TestBuildAllFailsWhole(t *testing.T) {
	boom := errors.New("boom")
	got, err := BuildAll(context.Background(), []RepoSource{
		{Slug: "o/a", Src: questionSource(1)},
		{Slug: "o/b", Src: &fakeSource{listErr: boom}},
	})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "o/b") {
		t.Fatalf("err = %v, want boom naming o/b", err)
	}
	if got.Questions != nil || got.Returned != nil {
		t.Errorf("overview = %+v, want empty", got)
	}
}

func TestBuildAllNoRepos(t *testing.T) {
	_, err := BuildAll(context.Background(), nil)
	if !errors.Is(err, forge.ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

// Package forgesuite is the contract every forge.Forge implementation keeps. Each forge runs the
// same cases against a Fixture of its own, on its fake server; only tests import this package.
// Every assert is on a neutral field of forge's types, never on a request or a forge's own IDs.
package forgesuite

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
)

// Fixture is one forge implementation against its fake server. Its board maps the lighthouse
// statuses "To do", "Controller", "On hold" and "Done" and the priorities "P1" and "P2".
// Each method arranges the world before the call under test.
type Fixture interface {
	Forge() forge.Forge
	Repo() string
	Login() string // the token's user
	// AddIssue adds is with its Number, Title, Body, Author, State ("open"/"closed"), Labels,
	// Milestone and ClosedAt.
	AddIssue(t *testing.T, is forge.Issue)
	// AddComment adds a comment by author on issue number and returns its ID.
	AddComment(t *testing.T, number int, author, body string) int64
	// AddAcknowledgement records that login acknowledged comment commentID on issue number.
	AddAcknowledgement(t *testing.T, number int, commentID int64, login string)
	// SetRole gives user a role on the repo; role is a neutral word such as "write".
	SetRole(t *testing.T, user, role string)
	// AddBlocker makes b block issue number.
	AddBlocker(t *testing.T, number int, b forge.Blocker)
	AddOpenPR(t *testing.T, pr forge.OpenPR)
	AddBranchPR(t *testing.T, branch string, pr forge.PR)
	// PutOnBoard puts issue number on the board with a lighthouse status and priority; "" none.
	PutOnBoard(t *testing.T, number int, status, priority string)
	// PutOnBoardUnmapped puts issue number on the board with a status the board config does not
	// map, and returns that status as the forge names it.
	PutOnBoardUnmapped(t *testing.T, number int) (raw string)
	// StatusOf is the last status the forge wrote for issue number, as a lighthouse name; ""
	// when it wrote none.
	StatusOf(t *testing.T, number int) string
	// ChecksOnClosedPRs reports whether BranchPRs carries the checks of a closed or merged PR
	// too. GitHub does; GitLab reads head_pipeline only for an open merge request, the only
	// PR whose checks the dispatcher reads.
	ChecksOnClosedPRs() bool
}

// Case is one contract case.
type Case struct {
	Name string
	Run  func(t *testing.T, f Fixture)
}

// Cases are the contract cases.
var Cases = []Case{
	{"Login", login},
	{"Role", role},
	{"IssueFields", issueFields},
	{"OpenIssuesPages", openIssuesPages},
	{"ClosedInMilestone", closedInMilestone},
	{"BlockedBy", blockedBy},
	{"Comments", comments},
	{"Acknowledge", acknowledge},
	{"BoardStatusAndPriority", boardStatusAndPriority},
	{"IssueItem", issueItem},
	{"IssueItemMissingIssue", issueItemMissingIssue},
	{"AddToBoardThenSetStatus", addToBoardThenSetStatus},
	{"SetStatusUnknownName", setStatusUnknownName},
	{"Pagination", pagination},
	{"PRFields", prFields},
}

// Run runs every case as a subtest, each with a fresh fixture.
func Run(t *testing.T, newFixture func(t *testing.T) Fixture) {
	for _, c := range Cases {
		t.Run(c.Name, func(t *testing.T) { c.Run(t, newFixture(t)) })
	}
}

// page is one more record than a page of 100 holds.
const page = 101

// milestone is the milestone the cases put issues in.
var milestone = forge.Milestone{Number: 3, Title: "M3", State: "open"}

func openIssue(n int) forge.Issue {
	return forge.Issue{Number: n, Title: fmt.Sprintf("Issue %d", n), Body: "body", Author: "ann", State: "open"}
}

func closedIssue(n int, m *forge.Milestone, closedAt time.Time) forge.Issue {
	is := openIssue(n)
	is.State, is.Milestone, is.ClosedAt = "closed", m, closedAt
	return is
}

func login(t *testing.T, f Fixture) {
	got, err := f.Forge().Login(t.Context())
	if err != nil || got != f.Login() {
		t.Errorf("Login = %q, %v; want %q", got, err, f.Login())
	}
}

func role(t *testing.T, f Fixture) {
	f.SetRole(t, "ann", "write")
	if got, err := f.Forge().Role(t.Context(), "ann"); err != nil || got != "write" {
		t.Errorf("Role(ann) = %q, %v; want write", got, err)
	}
	if got, err := f.Forge().Role(t.Context(), "nobody"); err != nil || got != "" {
		t.Errorf("Role(nobody) = %q, %v; want \"\" and no error", got, err)
	}
}

func issueFields(t *testing.T, f Fixture) {
	m := milestone
	f.AddIssue(t, forge.Issue{Number: 7, Title: "Seven", Body: "The body.", Author: "ann", State: "open",
		Labels: []string{"bug", "area:cli"}, Milestone: &m})
	got, err := f.Forge().Issue(t.Context(), 7)
	if err != nil {
		t.Fatalf("Issue(7): %v", err)
	}
	if got.Number != 7 || got.Title != "Seven" || got.Body != "The body." || got.Author != "ann" || got.State != "open" {
		t.Errorf("Issue(7) = %+v; want number 7, title Seven, body, author ann, state open", got)
	}
	if !slices.Equal(got.Labels, []string{"bug", "area:cli"}) {
		t.Errorf("Labels = %v, want [bug area:cli]", got.Labels)
	}
	if got.Milestone == nil || got.Milestone.Number != 3 || got.Milestone.Title != "M3" || got.Milestone.State != "open" {
		t.Errorf("Milestone = %+v, want number 3, title M3, state open", got.Milestone)
	}
	if got.URL == "" {
		t.Error("URL is empty")
	}
}

func openIssuesPages(t *testing.T, f Fixture) {
	for n := 1; n <= page; n++ {
		f.AddIssue(t, openIssue(n))
	}
	f.AddIssue(t, closedIssue(page+1, nil, time.Now().Add(-time.Hour)))
	got, err := f.Forge().OpenIssues(t.Context())
	if err != nil {
		t.Fatalf("OpenIssues: %v", err)
	}
	if len(got) != page {
		t.Errorf("OpenIssues = %d issues, want %d (the closed one left out)", len(got), page)
	}
	for _, is := range got {
		if is.Number == page+1 || is.State != "open" {
			t.Errorf("OpenIssues has #%d in state %q, want only open issues", is.Number, is.State)
		}
	}
}

func closedInMilestone(t *testing.T, f Fixture) {
	now := time.Now().UTC().Truncate(time.Second)
	m, other := milestone, forge.Milestone{Number: 4, Title: "M4", State: "open"}
	f.AddIssue(t, closedIssue(10, &m, now.Add(-time.Hour)))
	f.AddIssue(t, closedIssue(11, &m, now.Add(-48*time.Hour)))
	f.AddIssue(t, closedIssue(12, &other, now.Add(-time.Hour)))
	f.AddIssue(t, forge.Issue{Number: 13, Title: "Open", Author: "ann", State: "open", Milestone: &m})
	got, err := f.Forge().ClosedInMilestone(t.Context(), m, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("ClosedInMilestone: %v", err)
	}
	if len(got) != 1 || got[0].Number != 10 || !got[0].ClosedAt.Equal(now.Add(-time.Hour)) {
		t.Errorf("ClosedInMilestone = %+v; want only #10, closed at %s", got, now.Add(-time.Hour))
	}
}

func blockedBy(t *testing.T, f Fixture) {
	f.AddIssue(t, openIssue(7))
	f.AddBlocker(t, 7, forge.Blocker{Repo: f.Repo(), Issue: 5, State: "OPEN"})
	f.AddBlocker(t, 7, forge.Blocker{Repo: "o/other", Issue: 6, State: "CLOSED"})
	got, err := f.Forge().BlockedBy(t.Context(), 7)
	if err != nil {
		t.Fatalf("BlockedBy(7): %v", err)
	}
	want := []forge.Blocker{{Repo: f.Repo(), Issue: 5, State: "OPEN"}, {Repo: "o/other", Issue: 6, State: "CLOSED"}}
	slices.SortFunc(got, func(a, b forge.Blocker) int { return a.Issue - b.Issue })
	if len(got) != len(want) {
		t.Fatalf("BlockedBy(7) = %+v, want %+v", got, want)
	}
	for i, w := range want {
		if g := got[i]; g.Repo != w.Repo || g.Issue != w.Issue || g.State != w.State {
			t.Errorf("BlockedBy(7)[%d] = %+v, want repo %s, issue %d, state %s", i, g, w.Repo, w.Issue, w.State)
		}
	}
}

// commentByID returns the comment with id in cs.
func commentByID(cs []protocol.Comment, id int64) (protocol.Comment, bool) {
	i := slices.IndexFunc(cs, func(c protocol.Comment) bool { return c.ID == id })
	if i < 0 {
		return protocol.Comment{}, false
	}
	return cs[i], true
}

func comments(t *testing.T, f Fixture) {
	f.AddIssue(t, openIssue(7))
	id := f.AddComment(t, 7, "ann", "first")
	posted, err := f.Forge().CreateComment(t.Context(), 7, "second")
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if posted.ID <= 0 || posted.Author != f.Login() || posted.ID == id {
		t.Errorf("CreateComment = %+v; want a new ID > 0 and author %s", posted, f.Login())
	}
	got, err := f.Forge().Comments(t.Context(), 7)
	if err != nil {
		t.Fatalf("Comments(7): %v", err)
	}
	if c, ok := commentByID(got, id); !ok || c.Author != "ann" || c.Body != "first" {
		t.Errorf("Comments(7) = %+v; want comment %d by ann with body first", got, id)
	}
	if c, ok := commentByID(got, posted.ID); !ok || c.Author != f.Login() || c.Body != "second" {
		t.Errorf("Comments(7) = %+v; want the posted comment %d by %s with body second", got, posted.ID, f.Login())
	}
}

func acknowledge(t *testing.T, f Fixture) {
	f.AddIssue(t, openIssue(7))
	id := f.AddComment(t, 7, "ann", "please look")
	before, err := f.Forge().Comments(t.Context(), 7)
	if err != nil {
		t.Fatalf("Comments(7): %v", err)
	}
	if c, ok := commentByID(before, id); !ok || c.Rockets != 0 {
		t.Fatalf("before Acknowledge: comment %d = %+v, %v; want it with Rockets 0", id, c, ok)
	}
	if err := f.Forge().Acknowledge(t.Context(), 7, id); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	logins, err := f.Forge().Acknowledgers(t.Context(), 7, id)
	if err != nil || !slices.Contains(logins, f.Login()) {
		t.Errorf("Acknowledgers = %v, %v; want %s in it", logins, err, f.Login())
	}
	after, err := f.Forge().Comments(t.Context(), 7)
	if err != nil {
		t.Fatalf("Comments(7): %v", err)
	}
	if c, ok := commentByID(after, id); !ok || c.Rockets != 1 {
		t.Errorf("after Acknowledge: comment %d = %+v, %v; want Rockets 1", id, c, ok)
	}
}

// itemOf returns the board item of issue number in items.
func itemOf(items []forge.BoardItem, number int) (forge.BoardItem, bool) {
	i := slices.IndexFunc(items, func(it forge.BoardItem) bool { return it.Issue == number })
	if i < 0 {
		return forge.BoardItem{}, false
	}
	return items[i], true
}

func boardStatusAndPriority(t *testing.T, f Fixture) {
	for n := 7; n <= 9; n++ {
		f.AddIssue(t, openIssue(n))
	}
	f.PutOnBoard(t, 7, "Controller", "P1")
	f.PutOnBoard(t, 8, "", "")
	raw := f.PutOnBoardUnmapped(t, 9)
	items, err := f.Forge().BoardItems(t.Context())
	if err != nil {
		t.Fatalf("BoardItems: %v", err)
	}
	want := map[int][2]string{7: {"Controller", "P1"}, 8: {"", ""}, 9: {forge.UnknownStatusPrefix + raw, ""}}
	for n, w := range want {
		it, ok := itemOf(items, n)
		if !ok {
			t.Errorf("BoardItems has no item for #%d", n)
			continue
		}
		if it.Status != w[0] || it.Priority != w[1] || it.Repo != f.Repo() || it.ItemID == "" {
			t.Errorf("item #%d = %+v; want Status %q, Priority %q, Repo %s and an ItemID", n, it, w[0], w[1], f.Repo())
		}
	}
}

func issueItem(t *testing.T, f Fixture) {
	f.AddIssue(t, openIssue(7))
	f.PutOnBoard(t, 7, "On hold", "")
	it, ok, err := f.Forge().IssueItem(t.Context(), 7)
	if err != nil || !ok {
		t.Fatalf("IssueItem(7) = %+v, %v, %v; want the item", it, ok, err)
	}
	if it.Status != "On hold" || it.Issue != 7 || it.ItemID == "" {
		t.Errorf("IssueItem(7) = %+v; want Status On hold, Issue 7 and an ItemID", it)
	}
}

// A nonexistent issue is an ErrIssueNotFound error, whatever the forge: ok=false is for an
// issue that exists but is not on the board.
func issueItemMissingIssue(t *testing.T, f Fixture) {
	f.AddIssue(t, openIssue(7))
	_, ok, err := f.Forge().IssueItem(t.Context(), 99)
	if ok || !errors.Is(err, forge.ErrIssueNotFound) {
		t.Errorf("IssueItem(99) = ok %v, err %v; want an error wrapping forge.ErrIssueNotFound", ok, err)
	}
}

func addToBoardThenSetStatus(t *testing.T, f Fixture) {
	f.AddIssue(t, openIssue(7))
	it, err := f.Forge().AddToBoard(t.Context(), 7)
	if err != nil {
		t.Fatalf("AddToBoard(7): %v", err)
	}
	if it.Issue != 7 || it.ItemID == "" {
		t.Errorf("AddToBoard(7) = %+v; want Issue 7 and an ItemID", it)
	}
	if err := f.Forge().SetStatus(t.Context(), it, "Controller"); err != nil {
		t.Fatalf("SetStatus(Controller): %v", err)
	}
	if got := f.StatusOf(t, 7); got != "Controller" {
		t.Errorf("StatusOf(7) = %q, want Controller", got)
	}
}

func setStatusUnknownName(t *testing.T, f Fixture) {
	f.AddIssue(t, openIssue(7))
	f.PutOnBoard(t, 7, "To do", "")
	it, ok, err := f.Forge().IssueItem(t.Context(), 7)
	if err != nil || !ok {
		t.Fatalf("IssueItem(7) = %+v, %v, %v; want the item", it, ok, err)
	}
	if err := f.Forge().SetStatus(t.Context(), it, "Controller"); err != nil {
		t.Fatalf("SetStatus(Controller): %v", err)
	}
	err = f.Forge().SetStatus(t.Context(), it, "Nope")
	if err == nil || !strings.Contains(err.Error(), "Nope") {
		t.Errorf("SetStatus(Nope) = %v; want an error that names Nope", err)
	}
	if got := f.StatusOf(t, 7); got != "Controller" {
		t.Errorf("StatusOf(7) = %q after SetStatus(Nope), want Controller unchanged", got)
	}
}

// once fails t unless got holds every key of want exactly once and nothing else.
func once[K comparable](t *testing.T, what string, got []K, want []K) {
	t.Helper()
	seen := map[K]int{}
	for _, k := range got {
		seen[k]++
	}
	if len(got) != len(want) {
		t.Errorf("%s: %d records, want %d", what, len(got), len(want))
	}
	for _, k := range want {
		if seen[k] != 1 {
			t.Errorf("%s: %v seen %d times, want once", what, k, seen[k])
		}
	}
}

// pagination lists 101 records, one more than a page of 100, through each list method. The
// subtests share one fixture: the parent adds the open issues 1..101 and the closed issue 500
// that is on no GitHub board; each subtest adds only records the others do not list. BranchPRs is
// left out: more than 20 PRs on a branch is an error there, not a page.
func pagination(t *testing.T, f Fixture) {
	ctx := t.Context()
	open := make([]int, 0, page)
	for n := 1; n <= page; n++ {
		f.AddIssue(t, openIssue(n))
		open = append(open, n)
	}
	f.AddIssue(t, closedIssue(500, nil, time.Now().Add(-time.Hour)))

	t.Run("OpenIssues", func(t *testing.T) {
		got, err := f.Forge().OpenIssues(ctx)
		if err != nil {
			t.Fatalf("OpenIssues: %v", err)
		}
		var nums []int
		for _, is := range got {
			nums = append(nums, is.Number)
		}
		once(t, "OpenIssues", nums, open)
	})
	t.Run("ClosedInMilestone", func(t *testing.T) {
		m := forge.Milestone{Number: 9, Title: "M9", State: "open"}
		closedAt := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
		var want []int
		for n := 201; n < 201+page; n++ {
			f.AddIssue(t, closedIssue(n, &m, closedAt))
			want = append(want, n)
		}
		got, err := f.Forge().ClosedInMilestone(ctx, m, closedAt.Add(-24*time.Hour))
		if err != nil {
			t.Fatalf("ClosedInMilestone: %v", err)
		}
		var nums []int
		for _, is := range got {
			nums = append(nums, is.Number)
		}
		once(t, "ClosedInMilestone", nums, want)
	})
	t.Run("Comments", func(t *testing.T) {
		var want []int64
		for i := range page {
			want = append(want, f.AddComment(t, 1, "ann", fmt.Sprintf("comment %d", i)))
		}
		got, err := f.Forge().Comments(ctx, 1)
		if err != nil {
			t.Fatalf("Comments(1): %v", err)
		}
		var ids []int64
		for _, c := range got {
			ids = append(ids, c.ID)
		}
		once(t, "Comments", ids, want)
	})
	t.Run("Acknowledgers", func(t *testing.T) {
		id := f.AddComment(t, 2, "ann", "acknowledge me")
		var want []string
		for i := range page {
			l := fmt.Sprintf("user%03d", i)
			f.AddAcknowledgement(t, 2, id, l)
			want = append(want, l)
		}
		got, err := f.Forge().Acknowledgers(ctx, 2, id)
		if err != nil {
			t.Fatalf("Acknowledgers: %v", err)
		}
		once(t, "Acknowledgers", got, want)
	})
	t.Run("BlockedBy", func(t *testing.T) {
		// Issue 500 is on no board: a board read of blockers stops at a few.
		var want []int
		for n := 1001; n < 1001+page; n++ {
			f.AddBlocker(t, 500, forge.Blocker{Repo: "o/other", Issue: n, State: "CLOSED"})
			want = append(want, n)
		}
		got, err := f.Forge().BlockedBy(ctx, 500)
		if err != nil {
			t.Fatalf("BlockedBy(500): %v", err)
		}
		var nums []int
		for _, b := range got {
			nums = append(nums, b.Issue)
		}
		once(t, "BlockedBy", nums, want)
	})
	t.Run("OpenPRs", func(t *testing.T) {
		var want []int
		for n := 2001; n < 2001+page; n++ {
			f.AddOpenPR(t, forge.OpenPR{Number: n, Title: fmt.Sprintf("PR %d", n), HeadRef: fmt.Sprintf("fix-%d", n)})
			want = append(want, n)
		}
		got, err := f.Forge().OpenPRs(ctx)
		if err != nil {
			t.Fatalf("OpenPRs: %v", err)
		}
		var nums []int
		for _, p := range got {
			nums = append(nums, p.Number)
		}
		once(t, "OpenPRs", nums, want)
	})
	t.Run("BoardItems", func(t *testing.T) {
		for _, n := range open {
			f.PutOnBoard(t, n, "To do", "P2")
		}
		got, err := f.Forge().BoardItems(ctx)
		if err != nil {
			t.Fatalf("BoardItems: %v", err)
		}
		// Only the open items count: on GitLab every issue is on the board, so the closed
		// issues the other subtests add within the window of closed issues are listed too.
		var nums []int
		for _, it := range got {
			if it.IssueState == "OPEN" {
				nums = append(nums, it.Issue)
			}
		}
		once(t, "BoardItems", nums, open)
	})
}

func prFields(t *testing.T, f Fixture) {
	mergedAt := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	f.AddBranchPR(t, "issue-7-x", forge.PR{Number: 12, State: "MERGED", HeadSHA: "abc123", HeadRef: "issue-7-x",
		Checks: "SUCCESS", Mergeable: "MERGEABLE", MergedAt: mergedAt})
	prs, err := f.Forge().BranchPRs(t.Context(), "issue-7-x")
	if err != nil {
		t.Fatalf("BranchPRs: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("BranchPRs = %+v, want one PR", prs)
	}
	p := prs[0]
	wantChecks := ""
	if f.ChecksOnClosedPRs() {
		wantChecks = "SUCCESS"
	}
	if p.Number != 12 || p.State != "MERGED" || p.HeadSHA != "abc123" || p.HeadRef != "issue-7-x" ||
		p.Checks != wantChecks || p.Mergeable != "MERGEABLE" || !p.MergedAt.Equal(mergedAt) || p.Closes {
		t.Errorf("BranchPRs[0] = %+v; want #12 MERGED at %s, head abc123 on issue-7-x, checks %q, MERGEABLE, Closes false", p, mergedAt, wantChecks)
	}

	f.AddOpenPR(t, forge.OpenPR{Number: 13, Title: "Fix it", Body: "Closes #7", HeadRef: "issue-7-x"})
	open, err := f.Forge().OpenPRs(t.Context())
	if err != nil {
		t.Fatalf("OpenPRs: %v", err)
	}
	if len(open) != 1 || open[0] != (forge.OpenPR{Number: 13, Title: "Fix it", Body: "Closes #7", HeadRef: "issue-7-x"}) {
		t.Errorf("OpenPRs = %+v; want #13 Fix it, body Closes #7, head issue-7-x", open)
	}

	// An open PR carries its checks on every forge.
	f.AddBranchPR(t, "issue-8-y", forge.PR{Number: 14, State: "OPEN", HeadSHA: "def456", HeadRef: "issue-8-y",
		Checks: "FAILURE", Mergeable: "CONFLICTING"})
	prs, err = f.Forge().BranchPRs(t.Context(), "issue-8-y")
	if err != nil {
		t.Fatalf("BranchPRs(issue-8-y): %v", err)
	}
	if len(prs) != 1 || prs[0].Number != 14 || prs[0].State != "OPEN" || prs[0].Checks != "FAILURE" || prs[0].Mergeable != "CONFLICTING" {
		t.Errorf("BranchPRs(issue-8-y) = %+v; want #14 OPEN, checks FAILURE, CONFLICTING", prs)
	}
}

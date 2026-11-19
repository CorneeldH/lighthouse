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
	// Milestone, ClosedAt and StateReason.
	AddIssue(t *testing.T, is forge.Issue)
	// SetCloseReason sets why the closed issue number was closed: "completed" or "not_planned".
	SetCloseReason(t *testing.T, number int, reason string)
	// AddMilestone adds m to the repo, also one without issues; State is "open" or "closed".
	AddMilestone(t *testing.T, m forge.Milestone)
	// AddComment adds a comment by author on issue number and returns its ID.
	AddComment(t *testing.T, number int, author, body string) int64
	// AddAcknowledgement records that login acknowledged comment commentID on issue number.
	AddAcknowledgement(t *testing.T, number int, commentID int64, login string)
	// SetRole gives user a role on the repo; role is a neutral word such as "write".
	SetRole(t *testing.T, user, role string)
	// AddBlocker makes b block issue number.
	AddBlocker(t *testing.T, number int, b forge.Blocker)
	// FailBlockingPage2 makes the second page of the blocking list of issue number answer a 500.
	FailBlockingPage2(t *testing.T, number int)
	// AddChange adds a pull or merge request with its Number, Title, State (open, closed or
	// merged), HeadRef and, for a merged one, MergedAt.
	AddChange(t *testing.T, c forge.Change)
	// FailListChangesPage2 makes the second page of the pull or merge request list answer a 500.
	FailListChangesPage2(t *testing.T)
	// AddIssueChange makes pull or merge request c close issue number (c.Closes) or mention it,
	// with c's Number, State, HeadRef, CrossRepo, Repo and Body.
	AddIssueChange(t *testing.T, number int, c forge.IssueChange)
	// FailIssueChangesPage2 makes every second page of the pull or merge requests around issue
	// number fail.
	FailIssueChangesPage2(t *testing.T, number int)
	// FailComments makes posting a comment on issue number answer a 500.
	FailComments(t *testing.T, number int)
	// AddStateEvent records that issue number was "closed" or "reopened", in order; closer is
	// the pull or merge request that closed it, 0 by hand.
	AddStateEvent(t *testing.T, number int, state string, closer int)
	AddOpenPR(t *testing.T, pr forge.OpenPR)
	// AddOpenPRWithStatus adds an open pull request with its Number, Title, URL, Draft, Mergeable
	// and Checks.
	AddOpenPRWithStatus(t *testing.T, pr forge.PRSummary)
	// AddMergedPR adds a merged pull request with its Number, Title, URL and MergedAt.
	AddMergedPR(t *testing.T, pr forge.PRSummary)
	AddBranchPR(t *testing.T, branch string, pr forge.PR)
	// AddMilestonedEvent records that issue number was put in the milestone titled milestoneTitle
	// at at. The event names the milestone by title only; the issue is added with AddIssue.
	AddMilestonedEvent(t *testing.T, number int, milestoneTitle string, at time.Time)
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
	// PRCommentBodies lists the comment bodies on pull request number, oldest first. GitHub
	// keeps them with the issue comments; GitLab keeps a merge request's notes apart.
	PRCommentBodies(t *testing.T, number int) []string
	// ListsClosedIssues reports whether BoardItems also lists closed issues no case put on the
	// board. GitLab does: every issue is on its board, closed ones within a window. GitHub
	// lists only the items of its project.
	ListsClosedIssues() bool
	// SupportsReport reports whether the forge reads the period lists of lighthouse report
	// (OpenPRsWithStatus, MergedPRs, ClosedIssues, MilestonedIssues). GitHub does; GitLab
	// answers ErrReportUnsupported.
	SupportsReport() bool
	// AddNamedMilestone adds an open milestone with title and returns it with the Number the forge
	// uses in the Milestone methods. group makes it a group milestone, where the forge has them;
	// the fixture of a forge without them fails the test.
	AddNamedMilestone(t *testing.T, title string, group bool) forge.Milestone
	// SetLabels sets the label names of the repo.
	SetLabels(t *testing.T, names ...string)
	// UniqueMilestoneTitles reports whether the forge refuses two milestones with one title.
	// GitHub does; GitLab does not, and also shows the milestones of the groups above the project.
	UniqueMilestoneTitles() bool
	// SupportsPlanning reports whether the forge implements Planning; the Planning case is
	// skipped for one that does not yet.
	SupportsPlanning() bool
	// NotPlannedCountsAsDone reports whether Planning counts an issue closed as not planned in
	// Done. GitHub does not; GitLab has no such close reason, so every closed issue is done.
	NotPlannedCountsAsDone() bool
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
	{"Blocking", blocking},
	{"BlockingNone", blockingNone},
	{"BlockingPages", blockingPages},
	{"BlockingSecondPageFails", blockingSecondPageFails},
	{"Planning", planning},
	{"Comments", comments},
	{"Acknowledge", acknowledge},
	{"BoardStatusAndPriority", boardStatusAndPriority},
	{"IssueItem", issueItem},
	{"IssueItemMissingIssue", issueItemMissingIssue},
	{"IssueItemStatusName", issueItemStatusName},
	{"AddToBoardThenSetStatus", addToBoardThenSetStatus},
	{"SetStatusUnknownName", setStatusUnknownName},
	{"Pagination", pagination},
	{"PRFields", prFields},
	{"OpenPRsWithStatus", openPRsWithStatus},
	{"MergedPRs", mergedPRs},
	{"ClosedIssues", closedIssues},
	{"MilestonedIssues", milestonedIssues},
	{"CreateIssueRoundTrip", createIssueRoundTrip},
	{"EditIssue", editIssue},
	{"EditIssueRemoveAbsentLabel", editIssueRemoveAbsentLabel},
	{"ListIssuesStateAndLabel", listIssuesStateAndLabel},
	{"SearchIssues", searchIssues},
	{"CreateAndViewChange", createAndViewChange},
	{"ChangeComment", changeComment},
	{"ListChangesStates", listChangesStates},
	{"ListChangesDefaultsToOpen", listChangesDefaultsToOpen},
	{"ListChangesBogusState", listChangesBogusState},
	{"ListChangesPages", listChangesPages},
	{"ListChangesPage2Fails", listChangesPage2Fails},
	{"IssueChanges", issueChanges},
	{"IssueChangesNone", issueChangesNone},
	{"IssueChangesPages", issueChangesPages},
	{"IssueChangesSecondPageFails", issueChangesSecondPageFails},
	{"Closer", closer},
	{"ReopenIssue", reopenIssue},
	{"ReopenIssueNoComment", reopenIssueNoComment},
	{"ReopenIssueCommentFails", reopenIssueCommentFails},
	{"MilestonesAndLookup", milestonesAndLookup},
	{"MilestoneCountsAndState", milestoneCountsAndState},
	{"MilestoneAmbiguousTitle", milestoneAmbiguousTitle},
	{"LabelsList", labelsList},
	{"AddBlockerThenBlockedBy", addBlockerThenBlockedBy},
	{"IssueLifecycleByMarker", issueLifecycleByMarker},
	{"PRLifecycleByMarker", prLifecycleByMarker},
	{"FindByMarkerNoMatch", findByMarkerNoMatch},
	{"FindByMarkerPages", findByMarkerPages},
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

// blocking: issue 7 blocks the open issue 8 and the closed issue 9; Blocking(7) names both, with
// the state of the blocked issue.
func blocking(t *testing.T, f Fixture) {
	f.AddIssue(t, openIssue(7))
	f.AddIssue(t, openIssue(8))
	f.AddIssue(t, closedIssue(9, nil, time.Now().Add(-time.Hour)))
	f.AddBlocker(t, 8, forge.Blocker{Repo: f.Repo(), Issue: 7, State: "OPEN"})
	f.AddBlocker(t, 9, forge.Blocker{Repo: f.Repo(), Issue: 7, State: "OPEN"})
	got, err := f.Forge().Blocking(t.Context(), 7)
	if err != nil {
		t.Fatalf("Blocking(7): %v", err)
	}
	want := []forge.Blocker{{Repo: f.Repo(), Issue: 8, State: "OPEN"}, {Repo: f.Repo(), Issue: 9, State: "CLOSED"}}
	slices.SortFunc(got, func(a, b forge.Blocker) int { return a.Issue - b.Issue })
	if !slices.Equal(got, want) {
		t.Errorf("Blocking(7) = %+v, want %+v", got, want)
	}
}

// blockingNone: an issue that blocks nothing gives an empty list and no error.
func blockingNone(t *testing.T, f Fixture) {
	f.AddIssue(t, openIssue(7))
	got, err := f.Forge().Blocking(t.Context(), 7)
	if err != nil {
		t.Fatalf("Blocking(7): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Blocking(7) = %+v, want none", got)
	}
}

// addBlocked makes issue 7 block page open issues from 1001 up; page is one more than a REST page.
func addBlocked(t *testing.T, f Fixture) []int {
	t.Helper()
	f.AddIssue(t, openIssue(7))
	var nums []int
	for n := 1001; n < 1001+page; n++ {
		f.AddIssue(t, openIssue(n))
		f.AddBlocker(t, n, forge.Blocker{Repo: f.Repo(), Issue: 7, State: "OPEN"})
		nums = append(nums, n)
	}
	return nums
}

// blockingPages: more blocked issues than one page holds are all returned.
func blockingPages(t *testing.T, f Fixture) {
	want := addBlocked(t, f)
	got, err := f.Forge().Blocking(t.Context(), 7)
	if err != nil {
		t.Fatalf("Blocking(7): %v", err)
	}
	var nums []int
	for _, b := range got {
		nums = append(nums, b.Issue)
	}
	once(t, "Blocking", nums, want)
}

// blockingSecondPageFails: a failing second page is an error, never a list cut short.
func blockingSecondPageFails(t *testing.T, f Fixture) {
	addBlocked(t, f)
	f.FailBlockingPage2(t, 7)
	got, err := f.Forge().Blocking(t.Context(), 7)
	if err == nil {
		t.Fatalf("Blocking(7) = %+v, want an error for the failing second page", got)
	}
}

func planning(t *testing.T, f Fixture) {
	if !f.SupportsPlanning() {
		t.Skip("this forge does not implement Planning yet")
	}
	m := milestone
	f.AddMilestone(t, m)
	f.AddMilestone(t, forge.Milestone{Number: 9, Title: "M9", State: "open"})
	f.AddIssue(t, forge.Issue{Number: 1, Title: "One", Author: "ann", State: "open", Milestone: &m})
	f.AddIssue(t, openIssue(2))
	f.AddIssue(t, closedIssue(3, nil, time.Now().Add(-time.Hour)))
	f.AddIssue(t, closedIssue(4, &m, time.Now().Add(-time.Hour)))
	f.AddIssue(t, closedIssue(5, &m, time.Now().Add(-time.Hour)))
	f.AddIssue(t, closedIssue(6, &m, time.Now().Add(-time.Hour)))
	f.SetCloseReason(t, 4, "completed")
	f.SetCloseReason(t, 5, "completed")
	f.SetCloseReason(t, 6, "not_planned")
	f.AddBlocker(t, 2, forge.Blocker{Repo: f.Repo(), Issue: 1, State: "OPEN"})
	f.AddBlocker(t, 2, forge.Blocker{Repo: "other/repo", Issue: 9, State: "OPEN"})
	f.AddBlocker(t, 2, forge.Blocker{Repo: f.Repo(), Issue: 3, State: "CLOSED"})
	got, err := f.Forge().Planning(t.Context())
	if err != nil {
		t.Fatalf("Planning: %v", err)
	}
	if len(got.Issues) != 2 || got.Issues[0].Number != 1 || got.Issues[1].Number != 2 {
		t.Fatalf("Planning.Issues = %+v, want the open #1 and #2", got.Issues)
	}
	want := []forge.Blocker{{Repo: f.Repo(), Issue: 1, State: "OPEN"}, {Repo: "other/repo", Issue: 9, State: "OPEN"}}
	if b := got.Issues[1].BlockedBy; len(b) != 2 || b[0].Repo != want[0].Repo || b[0].Issue != 1 ||
		b[1].Repo != want[1].Repo || b[1].Issue != 9 || b[0].State != "OPEN" || b[1].State != "OPEN" {
		t.Errorf("Issues[1].BlockedBy = %+v, want %+v (closed #3 left out)", b, want)
	}
	if ms := got.Issues[0].Milestone; ms == nil || ms.Title != "M3" || ms.State != "open" {
		t.Errorf("Issues[0].Milestone = %+v, want M3 open", ms)
	}
	if len(got.Milestones) != 2 {
		t.Fatalf("Planning.Milestones = %+v, want M3 and the empty M9", got.Milestones)
	}
	slices.SortFunc(got.Milestones, func(a, b forge.MilestoneCount) int { return strings.Compare(a.Title, b.Title) })
	wantDone := 2
	if f.NotPlannedCountsAsDone() {
		wantDone = 3
	}
	if c := got.Milestones[0]; c.Title != "M3" || c.Open != 1 || c.Done != wantDone {
		t.Errorf("M3 = %+v, want 1 open and %d done (not-planned #6 counts only where the forge cannot tell)", c, wantDone)
	}
	if c := got.Milestones[1]; c.Title != "M9" || c.Open != 0 || c.Done != 0 {
		t.Errorf("M9 = %+v, want 0 open and 0 done", c)
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
	want := map[int][2]string{7: {"Controller", "P1"}, 8: {"", ""}}
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
	// An unmapped status is "unknown:" plus what the forge identifies it by (GitHub an option
	// ID, GitLab the status name), and its raw name is in StatusName.
	if it, ok := itemOf(items, 9); !ok {
		t.Errorf("BoardItems has no item for #9")
	} else if !strings.HasPrefix(it.Status, forge.UnknownStatusPrefix) || it.Status == forge.UnknownStatusPrefix ||
		it.StatusName != raw || it.Priority != "" {
		t.Errorf("item #9 = %+v; want Status unknown:<id>, StatusName %q and no priority", it, raw)
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

// StatusName is the raw name of the item's status on the board: the name of a mapped status,
// the forge's own name of an unmapped one, and "" without a status.
func issueItemStatusName(t *testing.T, f Fixture) {
	for n := 7; n <= 9; n++ {
		f.AddIssue(t, openIssue(n))
	}
	f.PutOnBoard(t, 7, "To do", "")
	raw := f.PutOnBoardUnmapped(t, 8)
	f.PutOnBoard(t, 9, "", "")
	for n, want := range map[int]string{7: "To do", 8: raw, 9: ""} {
		it, ok, err := f.Forge().IssueItem(t.Context(), n)
		if err != nil || !ok {
			t.Fatalf("IssueItem(%d) = %+v, %v, %v; want the item", n, it, ok, err)
		}
		if it.StatusName != want {
			t.Errorf("IssueItem(%d).StatusName = %q; want %q", n, it.StatusName, want)
		}
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
		// The open issues are listed once each. A forge that lists closed issues too (GitLab:
		// the closed issues the other subtests add, within its window) lists each once, as
		// CLOSED; any other forge lists none.
		var nums []int
		closed := map[int]int{}
		for _, it := range got {
			switch it.IssueState {
			case "OPEN":
				nums = append(nums, it.Issue)
			case "CLOSED":
				closed[it.Issue]++
			default:
				t.Errorf("BoardItems lists issue %d as %q, want OPEN or CLOSED", it.Issue, it.IssueState)
			}
		}
		once(t, "BoardItems", nums, open)
		if len(closed) > 0 && !f.ListsClosedIssues() {
			t.Errorf("BoardItems lists closed issues %v, want only the open issues on the board", closed)
		}
		for n, c := range closed {
			if c > 1 {
				t.Errorf("BoardItems lists closed issue %d %d times, want once", n, c)
			}
		}
	})
}

func prFields(t *testing.T, f Fixture) {
	mergedAt := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	f.AddBranchPR(t, "issue-7-x", forge.PR{Number: 12, State: "MERGED", HeadSHA: "abc123", HeadRef: "issue-7-x",
		BaseRef: "main", Checks: "SUCCESS", Mergeable: "MERGEABLE", MergedAt: mergedAt})
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
	if p.Number != 12 || p.State != "MERGED" || p.HeadSHA != "abc123" || p.HeadRef != "issue-7-x" || p.BaseRef != "main" ||
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

// sorted is a sorted copy of names, so a case compares labels without depending on their order.
func sorted(names []string) []string {
	out := slices.Clone(names)
	slices.Sort(out)
	return out
}

// numbers is the sorted issue numbers of issues.
func numbers(issues []forge.Issue) []int {
	out := make([]int, 0, len(issues))
	for _, is := range issues {
		out = append(out, is.Number)
	}
	slices.Sort(out)
	return out
}

func createIssueRoundTrip(t *testing.T, f Fixture) {
	f.AddNamedMilestone(t, "M3", false)
	created, err := f.Forge().CreateIssue(t.Context(), forge.NewIssue{
		Title: "New one", Body: "Fresh body.", Labels: []string{"bug"}, Milestone: "M3"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if created.Number <= 0 {
		t.Fatalf("CreateIssue = %+v; want a number > 0", created)
	}
	got, err := f.Forge().Issue(t.Context(), created.Number)
	if err != nil {
		t.Fatalf("Issue(%d): %v", created.Number, err)
	}
	if got.Title != "New one" || got.Body != "Fresh body." || got.State != "open" || got.Author != f.Login() {
		t.Errorf("Issue = %+v; want title New one, body Fresh body., state open, author %s", got, f.Login())
	}
	if !slices.Equal(got.Labels, []string{"bug"}) {
		t.Errorf("Labels = %v, want [bug]", got.Labels)
	}
	if got.Milestone == nil || got.Milestone.Title != "M3" {
		t.Errorf("Milestone = %+v, want title M3", got.Milestone)
	}
}

func editIssue(t *testing.T, f Fixture) {
	is := openIssue(7)
	is.Labels = []string{"a", "b"}
	f.AddIssue(t, is)
	title, body := "Renamed", "Rewritten."
	edited, err := f.Forge().EditIssue(t.Context(), 7, forge.IssueEdit{
		Title: &title, Body: &body, AddLabels: []string{"c"}, RemoveLabels: []string{"a"}})
	if err != nil {
		t.Fatalf("EditIssue: %v", err)
	}
	if edited.Title != "Renamed" || edited.Body != "Rewritten." || !slices.Equal(sorted(edited.Labels), []string{"b", "c"}) {
		t.Errorf("EditIssue = %+v; want title Renamed, body Rewritten., labels [b c]", edited)
	}
	got, err := f.Forge().Issue(t.Context(), 7)
	if err != nil {
		t.Fatalf("Issue(7): %v", err)
	}
	if got.Title != "Renamed" || got.Body != "Rewritten." || !slices.Equal(sorted(got.Labels), []string{"b", "c"}) {
		t.Errorf("Issue(7) = %+v; want title Renamed, body Rewritten., labels [b c]", got)
	}
}

// editIssueRemoveAbsentLabel: removing a label the issue does not carry (removed by hand in the
// meantime) is no error; the other labels stay.
func editIssueRemoveAbsentLabel(t *testing.T, f Fixture) {
	is := openIssue(7)
	is.Labels = []string{"b"}
	f.AddIssue(t, is)
	edited, err := f.Forge().EditIssue(t.Context(), 7, forge.IssueEdit{RemoveLabels: []string{"a"}})
	if err != nil {
		t.Fatalf("EditIssue(remove absent label): %v", err)
	}
	if !slices.Equal(edited.Labels, []string{"b"}) {
		t.Errorf("EditIssue = labels %v; want [b]", edited.Labels)
	}
}

func listIssuesStateAndLabel(t *testing.T, f Fixture) {
	a := openIssue(1)
	a.Labels = []string{"bug"}
	b := closedIssue(2, nil, time.Now())
	b.Labels = []string{"bug"}
	c := openIssue(3)
	c.Labels = []string{"doc"}
	for _, is := range []forge.Issue{a, b, c} {
		f.AddIssue(t, is)
	}
	cases := []struct {
		name   string
		filter forge.IssueFilter
		want   []int
	}{
		{"open", forge.IssueFilter{State: "open"}, []int{1, 3}},
		{"open and label", forge.IssueFilter{State: "open", Label: "bug"}, []int{1}},
		{"all and label", forge.IssueFilter{State: "all", Label: "bug"}, []int{1, 2}},
		{"closed", forge.IssueFilter{State: "closed"}, []int{2}},
	}
	for _, c := range cases {
		got, err := f.Forge().ListIssues(t.Context(), c.filter)
		if err != nil {
			t.Errorf("%s: ListIssues: %v", c.name, err)
			continue
		}
		if !slices.Equal(numbers(got), c.want) {
			t.Errorf("%s: ListIssues = %v, want %v", c.name, numbers(got), c.want)
		}
	}
}

func searchIssues(t *testing.T, f Fixture) {
	a := openIssue(1)
	a.Title = "Zebra crossing"
	b := openIssue(2)
	b.Title = "Something else"
	f.AddIssue(t, a)
	f.AddIssue(t, b)
	got, err := f.Forge().SearchIssues(t.Context(), "zebra")
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if !slices.Equal(numbers(got), []int{1}) {
		t.Errorf("SearchIssues(zebra) = %v, want [1]", numbers(got))
	}
}

func createAndViewChange(t *testing.T, f Fixture) {
	created, err := f.Forge().CreateChange(t.Context(), forge.NewPR{
		Title: "My change", Body: "Why.", Head: "feature-x", Base: "main"})
	if err != nil {
		t.Fatalf("CreateChange: %v", err)
	}
	got, err := f.Forge().Change(t.Context(), created.Number)
	if err != nil {
		t.Fatalf("Change(%d): %v", created.Number, err)
	}
	if got.Number != created.Number || got.Title != "My change" || got.Body != "Why." ||
		got.State != "open" || got.HeadRef != "feature-x" || got.BaseRef != "main" {
		t.Errorf("Change = %+v; want title My change, body Why., state open, head feature-x, base main", got)
	}
}

// addThreeChanges adds an open (1), a merged (2) and a closed-not-merged (3) change; 2 merged at
// mergedAt.
func addThreeChanges(t *testing.T, f Fixture, mergedAt time.Time) {
	t.Helper()
	f.AddChange(t, forge.Change{Number: 1, Title: "Open", State: "open", HeadRef: "issue-1-open"})
	f.AddChange(t, forge.Change{Number: 2, Title: "Merged", State: "merged", HeadRef: "issue-2-merged", MergedAt: mergedAt})
	f.AddChange(t, forge.Change{Number: 3, Title: "Closed", State: "closed", HeadRef: "issue-3-closed"})
}

// changeNumbers lists the numbers of cs, in order.
func changeNumbers(cs []forge.Change) []int {
	var nums []int
	for _, c := range cs {
		nums = append(nums, c.Number)
	}
	slices.Sort(nums)
	return nums
}

// listChangesStates: each state names exactly its changes; a closed-not-merged one is not merged;
// the merged one carries State merged, MergedAt and its head branch.
func listChangesStates(t *testing.T, f Fixture) {
	mergedAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	addThreeChanges(t, f, mergedAt)
	for state, want := range map[string][]int{"open": {1}, "merged": {2}, "closed": {3}, "all": {1, 2, 3}} {
		got, err := f.Forge().ListChanges(t.Context(), state)
		if err != nil {
			t.Fatalf("ListChanges(%q): %v", state, err)
		}
		if nums := changeNumbers(got); !slices.Equal(nums, want) {
			t.Errorf("ListChanges(%q) = %v, want %v", state, nums, want)
		}
	}
	merged, err := f.Forge().ListChanges(t.Context(), "merged")
	if err != nil || len(merged) != 1 {
		t.Fatalf("ListChanges(merged) = %+v, %v; want one change", merged, err)
	}
	if c := merged[0]; c.State != "merged" || !c.MergedAt.Equal(mergedAt) || c.HeadRef != "issue-2-merged" {
		t.Errorf("merged change = %+v; want state merged at %s on issue-2-merged", c, mergedAt)
	}
	closed, err := f.Forge().ListChanges(t.Context(), "closed")
	if err != nil || len(closed) != 1 || closed[0].State != "closed" || !closed[0].MergedAt.IsZero() {
		t.Errorf("ListChanges(closed) = %+v, %v; want one closed change with no MergedAt", closed, err)
	}
}

// listChangesDefaultsToOpen: an empty state is open.
func listChangesDefaultsToOpen(t *testing.T, f Fixture) {
	addThreeChanges(t, f, time.Now().Add(-time.Hour))
	got, err := f.Forge().ListChanges(t.Context(), "")
	if err != nil {
		t.Fatalf("ListChanges(\"\"): %v", err)
	}
	if nums := changeNumbers(got); !slices.Equal(nums, []int{1}) {
		t.Errorf("ListChanges(\"\") = %v, want [1]", nums)
	}
}

// listChangesBogusState: an unknown state is an error, never an empty list.
func listChangesBogusState(t *testing.T, f Fixture) {
	got, err := f.Forge().ListChanges(t.Context(), "bogus")
	if err == nil {
		t.Errorf("ListChanges(bogus) = %+v, want an error", got)
	}
}

// listChangesPages: more changes than one page holds are all returned.
func listChangesPages(t *testing.T, f Fixture) {
	var want []int
	for n := 1; n <= page; n++ {
		f.AddChange(t, forge.Change{Number: n, Title: "C", State: "open", HeadRef: fmt.Sprintf("fix-%d", n)})
		want = append(want, n)
	}
	got, err := f.Forge().ListChanges(t.Context(), "open")
	if err != nil {
		t.Fatalf("ListChanges(open): %v", err)
	}
	once(t, "ListChanges", changeNumbers(got), want)
}

// listChangesPage2Fails: a failing second page is an error and no list, never the first page
// alone (which could leave out an open change).
func listChangesPage2Fails(t *testing.T, f Fixture) {
	for n := 1; n <= page; n++ {
		f.AddChange(t, forge.Change{Number: n, Title: "C", State: "open", HeadRef: fmt.Sprintf("fix-%d", n)})
	}
	f.FailListChangesPage2(t)
	got, err := f.Forge().ListChanges(t.Context(), "open")
	if err == nil || got != nil {
		t.Errorf("ListChanges(open) = %d changes, %v; want nil and an error", len(got), err)
	}
}

// issueChanges: issue 5 is closed by the merged PR 12, mentioned by PR 13 (Refs), by PR 15 from a
// fork and by PR 16 of another repository; each comes back once, with its own fields.
func issueChanges(t *testing.T, f Fixture) {
	f.AddIssue(t, closedIssue(5, nil, time.Now().Add(-time.Hour)))
	want := []forge.IssueChange{
		{Number: 12, State: "MERGED", HeadRef: "issue-5-x", Repo: f.Repo(), Body: "Closes #5", Closes: true},
		{Number: 13, State: "OPEN", HeadRef: "fix-y", Repo: f.Repo(), Body: "Refs #5 — part one"},
		{Number: 15, State: "CLOSED", HeadRef: "patch-1", CrossRepo: true, Repo: f.Repo(), Body: "Refs #5"},
		{Number: 16, State: "MERGED", HeadRef: "issue-9-z", Repo: "other/repo", Body: "See " + f.Repo() + "#5"},
	}
	for _, c := range want {
		f.AddIssueChange(t, 5, c)
	}
	got, err := f.Forge().IssueChanges(t.Context(), 5)
	if err != nil {
		t.Fatalf("IssueChanges(5): %v", err)
	}
	slices.SortFunc(got, func(a, b forge.IssueChange) int { return a.Number - b.Number })
	if !slices.Equal(got, want) {
		t.Errorf("IssueChanges(5) =\n%+v\nwant\n%+v", got, want)
	}
}

// issueChangesNone: an issue no pull request names gives an empty list and no error.
func issueChangesNone(t *testing.T, f Fixture) {
	f.AddIssue(t, openIssue(5))
	got, err := f.Forge().IssueChanges(t.Context(), 5)
	if err != nil {
		t.Fatalf("IssueChanges(5): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("IssueChanges(5) = %+v, want none", got)
	}
}

// addIssueChanges gives issue 5 page closing pull requests from 1001 up and page mentioning ones
// from 2001 up, and returns their numbers.
func addIssueChanges(t *testing.T, f Fixture) []int {
	t.Helper()
	f.AddIssue(t, closedIssue(5, nil, time.Now().Add(-time.Hour)))
	var nums []int
	for i := range page {
		for _, c := range []forge.IssueChange{
			{Number: 1001 + i, State: "MERGED", HeadRef: fmt.Sprintf("c-%d", i), Repo: f.Repo(), Closes: true},
			{Number: 2001 + i, State: "OPEN", HeadRef: fmt.Sprintf("m-%d", i), Repo: f.Repo()},
		} {
			f.AddIssueChange(t, 5, c)
			nums = append(nums, c.Number)
		}
	}
	return nums
}

// issueChangesPages: more closing and more mentioning pull requests than one page holds are all
// returned, each once.
func issueChangesPages(t *testing.T, f Fixture) {
	want := addIssueChanges(t, f)
	got, err := f.Forge().IssueChanges(t.Context(), 5)
	if err != nil {
		t.Fatalf("IssueChanges(5): %v", err)
	}
	var nums []int
	for _, c := range got {
		nums = append(nums, c.Number)
	}
	once(t, "IssueChanges", nums, want)
}

// issueChangesSecondPageFails: a failing second page is an error, never a list cut short.
func issueChangesSecondPageFails(t *testing.T, f Fixture) {
	addIssueChanges(t, f)
	f.FailIssueChangesPage2(t, 5)
	got, err := f.Forge().IssueChanges(t.Context(), 5)
	if err == nil {
		t.Fatalf("IssueChanges(5) = %d changes, want an error for the failing second page", len(got))
	}
}

// reopenIssue: the comment lands, then the closed issue is open again.
func reopenIssue(t *testing.T, f Fixture) {
	ctx := t.Context()
	f.AddIssue(t, forge.Issue{Number: 3, Title: "Dicht", Author: "ann", State: "closed"})
	if err := f.Forge().ReopenIssue(ctx, 3, "weer open"); err != nil {
		t.Fatalf("ReopenIssue: %v", err)
	}
	is, err := f.Forge().Issue(ctx, 3)
	if err != nil {
		t.Fatalf("Issue(3): %v", err)
	}
	if is.State != "open" {
		t.Errorf("state after ReopenIssue = %q, want open", is.State)
	}
	cs, err := f.Forge().Comments(ctx, 3)
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(cs) != 1 || cs[0].Body != "weer open" {
		t.Errorf("Comments = %+v, want the one comment %q", cs, "weer open")
	}
}

// reopenIssueNoComment: an empty comment posts nothing.
func reopenIssueNoComment(t *testing.T, f Fixture) {
	ctx := t.Context()
	f.AddIssue(t, forge.Issue{Number: 3, Title: "Dicht", Author: "ann", State: "closed"})
	if err := f.Forge().ReopenIssue(ctx, 3, ""); err != nil {
		t.Fatalf("ReopenIssue: %v", err)
	}
	if is, err := f.Forge().Issue(ctx, 3); err != nil || is.State != "open" {
		t.Errorf("Issue(3) = %+v, %v; want open", is, err)
	}
	cs, err := f.Forge().Comments(ctx, 3)
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(cs) != 0 {
		t.Errorf("Comments = %+v, want none for an empty comment", cs)
	}
}

// reopenIssueCommentFails: a failing comment is an error and the issue stays closed.
func reopenIssueCommentFails(t *testing.T, f Fixture) {
	ctx := t.Context()
	f.AddIssue(t, forge.Issue{Number: 3, Title: "Dicht", Author: "ann", State: "closed"})
	f.FailComments(t, 3)
	if err := f.Forge().ReopenIssue(ctx, 3, "weer open"); err == nil {
		t.Fatal("ReopenIssue = nil, want the error of the failing comment")
	}
	if is, err := f.Forge().Issue(ctx, 3); err != nil || is.State != "closed" {
		t.Errorf("Issue(3) = %+v, %v; want still closed", is, err)
	}
}

// closer: the pull request of the last close wins, also when the issue was reopened since; a
// close by hand is 0, also after an earlier close by a pull request, and so is an issue never
// closed.
func closer(t *testing.T, f Fixture) {
	type event struct {
		state  string
		closer int
	}
	for _, tc := range []struct {
		issue  int
		events []event
		want   int
	}{
		{5, []event{{"closed", 12}}, 12},
		{6, []event{{"closed", 0}}, 0},
		{7, []event{{"closed", 12}, {"reopened", 0}, {"closed", 14}}, 14},
		{8, []event{{"closed", 12}, {"reopened", 0}, {"closed", 0}}, 0},
		{10, []event{{"closed", 12}, {"reopened", 0}}, 12},
		{9, nil, 0},
	} {
		is := closedIssue(tc.issue, nil, time.Now().Add(-time.Hour))
		if tc.events == nil {
			is = openIssue(tc.issue)
		}
		f.AddIssue(t, is)
		for _, e := range tc.events {
			f.AddStateEvent(t, tc.issue, e.state, e.closer)
		}
		got, err := f.Forge().Closer(t.Context(), tc.issue)
		if err != nil || got != tc.want {
			t.Errorf("Closer(%d) = %d, %v; want %d", tc.issue, got, err, tc.want)
		}
	}
}

func changeComment(t *testing.T, f Fixture) {
	created, err := f.Forge().CreateChange(t.Context(), forge.NewPR{Title: "C", Head: "feature-x", Base: "main"})
	if err != nil {
		t.Fatalf("CreateChange: %v", err)
	}
	c, err := f.Forge().CreateChangeComment(t.Context(), created.Number, "Looks good.")
	if err != nil {
		t.Fatalf("CreateChangeComment: %v", err)
	}
	if c.ID <= 0 || c.Body != "Looks good." || c.Author != f.Login() {
		t.Errorf("CreateChangeComment = %+v; want an ID > 0, body Looks good., author %s", c, f.Login())
	}
}

func milestonesAndLookup(t *testing.T, f Fixture) {
	m1 := f.AddNamedMilestone(t, "M1", false)
	f.AddNamedMilestone(t, "M2", false)
	open, err := f.Forge().Milestones(t.Context(), "open")
	if err != nil {
		t.Fatalf("Milestones(open): %v", err)
	}
	titles := make([]string, 0, len(open))
	for _, m := range open {
		titles = append(titles, m.Title)
	}
	if !slices.Equal(sorted(titles), []string{"M1", "M2"}) {
		t.Errorf("Milestones(open) titles = %v, want [M1 M2]", titles)
	}
	if closed, err := f.Forge().Milestones(t.Context(), "closed"); err != nil || len(closed) != 0 {
		t.Errorf("Milestones(closed) = %+v, %v; want none", closed, err)
	}
	got, err := f.Forge().MilestoneByNumber(t.Context(), m1.Number)
	if err != nil || got.Title != "M1" || got.Number != m1.Number {
		t.Errorf("MilestoneByNumber(%d) = %+v, %v; want M1", m1.Number, got, err)
	}
	created, err := f.Forge().CreateMilestone(t.Context(), "M9", "Nine.")
	if err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	if created.Number <= 0 || created.Title != "M9" || created.Description != "Nine." || created.State != "open" {
		t.Errorf("CreateMilestone = %+v; want title M9, description Nine., state open", created)
	}
	title, desc := "M9b", "Still nine."
	edited, err := f.Forge().EditMilestone(t.Context(), created.Number, &title, &desc, nil)
	if err != nil {
		t.Fatalf("EditMilestone: %v", err)
	}
	if edited.Title != "M9b" || edited.Description != "Still nine." {
		t.Errorf("EditMilestone = %+v; want title M9b, description Still nine.", edited)
	}
	again, err := f.Forge().MilestoneByNumber(t.Context(), created.Number)
	if err != nil || again.Title != "M9b" || again.Description != "Still nine." {
		t.Errorf("MilestoneByNumber after edit = %+v, %v; want M9b", again, err)
	}
}

// Milestones and MilestoneByNumber count the open and closed issues of a milestone, and
// EditMilestone closes it in the same call that renames it; a nil state leaves the state alone and
// a state other than open or closed is refused.
func milestoneCountsAndState(t *testing.T, f Fixture) {
	m := f.AddNamedMilestone(t, "Count", false)
	f.AddNamedMilestone(t, "Other", false)
	for n := 1; n <= 5; n++ {
		is := openIssue(n)
		if n > 2 {
			is = closedIssue(n, nil, time.Now().Add(-time.Hour))
		}
		is.Milestone = &m
		f.AddIssue(t, is)
	}
	f.AddIssue(t, openIssue(6)) // no milestone: counts for none
	list, err := f.Forge().Milestones(t.Context(), "open")
	if err != nil {
		t.Fatalf("Milestones(open): %v", err)
	}
	for _, got := range list {
		want := [2]int{0, 0}
		if got.Title == "Count" {
			want = [2]int{2, 3}
		}
		if [2]int{got.OpenIssues, got.ClosedIssues} != want {
			t.Errorf("Milestones: %s counts = %d open, %d closed; want %v", got.Title, got.OpenIssues, got.ClosedIssues, want)
		}
	}
	one, err := f.Forge().MilestoneByNumber(t.Context(), m.Number)
	if err != nil || one.OpenIssues != 2 || one.ClosedIssues != 3 {
		t.Errorf("MilestoneByNumber = %+v, %v; want 2 open, 3 closed", one, err)
	}
	title, closed := "Counted", "closed"
	edited, err := f.Forge().EditMilestone(t.Context(), m.Number, &title, nil, &closed)
	if err != nil || edited.State != "closed" || edited.Title != "Counted" {
		t.Fatalf("EditMilestone(title, closed) = %+v, %v; want state closed, title Counted", edited, err)
	}
	again, err := f.Forge().MilestoneByNumber(t.Context(), m.Number)
	if err != nil || again.State != "closed" || again.Title != "Counted" {
		t.Errorf("MilestoneByNumber after close = %+v, %v; want closed, Counted", again, err)
	}
	kept, err := f.Forge().EditMilestone(t.Context(), m.Number, nil, nil, nil)
	if err != nil || kept.State != "closed" {
		t.Errorf("EditMilestone(nil state) = %+v, %v; want state closed kept", kept, err)
	}
	open := "open"
	if reopened, err := f.Forge().EditMilestone(t.Context(), m.Number, nil, nil, &open); err != nil || reopened.State != "open" {
		t.Errorf("EditMilestone(open) = %+v, %v; want state open", reopened, err)
	}
	bad := "archived"
	if _, err := f.Forge().EditMilestone(t.Context(), m.Number, nil, nil, &bad); err == nil {
		t.Errorf("EditMilestone(state archived) = nil error; want a refusal")
	}
}

// A title that fits two milestones is refused with both numbers, never guessed. Only where the
// forge allows two milestones with one title.
func milestoneAmbiguousTitle(t *testing.T, f Fixture) {
	if f.UniqueMilestoneTitles() {
		t.Skip("the forge refuses two milestones with one title")
	}
	a := f.AddNamedMilestone(t, "Dup", false)
	b := f.AddNamedMilestone(t, "Dup", true)
	_, err := f.Forge().CreateIssue(t.Context(), forge.NewIssue{Title: "T", Milestone: "Dup"})
	var amb *forge.AmbiguousMilestone
	if !errors.As(err, &amb) {
		t.Fatalf("CreateIssue error = %v; want *forge.AmbiguousMilestone", err)
	}
	if amb.Title != "Dup" || !slices.Equal(sorted2(amb.Numbers), sorted2([]int{a.Number, b.Number})) {
		t.Errorf("AmbiguousMilestone = %+v; want title Dup and numbers %d, %d", amb, a.Number, b.Number)
	}
}

func sorted2(n []int) []int {
	out := slices.Clone(n)
	slices.Sort(out)
	return out
}

func labelsList(t *testing.T, f Fixture) {
	f.SetLabels(t, "bug", "area:cli")
	got, err := f.Forge().Labels(t.Context())
	if err != nil || !slices.Equal(sorted(got), []string{"area:cli", "bug"}) {
		t.Errorf("Labels = %v, %v; want [area:cli bug]", got, err)
	}
}

func addBlockerThenBlockedBy(t *testing.T, f Fixture) {
	f.AddIssue(t, openIssue(7))
	f.AddIssue(t, openIssue(8))
	if err := f.Forge().AddBlocker(t.Context(), 7, 8); err != nil {
		t.Fatalf("AddBlocker(7, 8): %v", err)
	}
	got, err := f.Forge().BlockedBy(t.Context(), 7)
	if err != nil {
		t.Fatalf("BlockedBy(7): %v", err)
	}
	if len(got) != 1 || got[0].Issue != 8 || got[0].State != "OPEN" || got[0].Repo != f.Repo() {
		t.Errorf("BlockedBy(7) = %+v; want one blocker #8, state OPEN, repo %s", got, f.Repo())
	}
}

// find returns the one item FindByMarker finds for marker; it fails on none or more.
func find(t *testing.T, f Fixture, marker string) forge.Item {
	t.Helper()
	got, err := f.Forge().FindByMarker(t.Context(), marker)
	if err != nil {
		t.Fatalf("FindByMarker(%q): %v", marker, err)
	}
	if len(got) != 1 {
		t.Fatalf("FindByMarker(%q) = %+v, want exactly one item", marker, got)
	}
	return got[0]
}

func issueLifecycleByMarker(t *testing.T, f Fixture) {
	ctx := t.Context()
	const marker = "<!-- lighthouse:pin skill-a -->"
	f.AddIssue(t, forge.Issue{Number: 1, Title: "Other", Body: "no marker", Author: "ann", State: "open"})
	is, err := f.Forge().CreateIssue(ctx, forge.NewIssue{Title: "Pin behind", Body: marker + "\nfirst body"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if is.Number == 0 || is.Number == 1 || is.Title != "Pin behind" || is.State != "open" {
		t.Fatalf("CreateIssue = %+v; want a new open issue titled Pin behind", is)
	}
	it := find(t, f, marker)
	if it.Number != is.Number || it.IsPR || it.State != "open" || it.Body != marker+"\nfirst body" || it.Author != f.Login() {
		t.Errorf("found %+v; want open issue #%d by %s, not a PR, with its full body", it, is.Number, f.Login())
	}

	if err := f.Forge().UpdateIssueBody(ctx, is.Number, marker+"\nsecond body"); err != nil {
		t.Fatalf("UpdateIssueBody: %v", err)
	}
	if it := find(t, f, marker); it.Body != marker+"\nsecond body" || it.State != "open" {
		t.Errorf("after update found %+v; want body %q, still open", it, marker+"\nsecond body")
	}

	if err := f.Forge().CloseIssue(ctx, is.Number, "Bijgewerkt, dicht"); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	if it := find(t, f, marker); it.Number != is.Number || it.State != "closed" {
		t.Errorf("after close found %+v; want #%d closed, still findable", it, is.Number)
	}
	cs, err := f.Forge().Comments(ctx, is.Number)
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(cs) != 1 || cs[0].Body != "Bijgewerkt, dicht" {
		t.Errorf("Comments after close = %+v; want the closing comment", cs)
	}
}

func prLifecycleByMarker(t *testing.T, f Fixture) {
	ctx := t.Context()
	const marker = "<!-- lighthouse:pin pr -->"
	pr, err := f.Forge().CreatePR(ctx, forge.NewPR{Title: "Pin bump", Body: marker + "\nbody", Head: "pins/x", Base: "main", Draft: true})
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}
	if pr.Number == 0 || !pr.Draft || pr.State != "OPEN" || pr.HeadRef != "pins/x" {
		t.Fatalf("CreatePR = %+v; want an open draft PR on pins/x", pr)
	}
	it := find(t, f, marker)
	if it.Number != pr.Number || !it.IsPR || it.State != "open" || it.HeadRef != "pins/x" || it.Body != marker+"\nbody" || it.Author != f.Login() {
		t.Errorf("found %+v; want open PR #%d by %s on pins/x with its full body", it, pr.Number, f.Login())
	}

	if err := f.Forge().UpdatePRBody(ctx, pr.Number, marker+"\nnew"); err != nil {
		t.Fatalf("UpdatePRBody: %v", err)
	}
	if it := find(t, f, marker); it.Body != marker+"\nnew" {
		t.Errorf("after update body = %q, want %q", it.Body, marker+"\nnew")
	}

	if err := f.Forge().ClosePR(ctx, pr.Number, "Vervangen"); err != nil {
		t.Fatalf("ClosePR: %v", err)
	}
	it = find(t, f, marker)
	if it.Number != pr.Number || !it.IsPR || it.State != "closed" || it.HeadRef != "pins/x" {
		t.Errorf("after close found %+v; want PR #%d closed on pins/x, still findable", it, pr.Number)
	}
	if got := f.PRCommentBodies(t, pr.Number); len(got) != 1 || got[0] != "Vervangen" {
		t.Errorf("comments after ClosePR = %q; want the closing comment", got)
	}
}

func findByMarkerNoMatch(t *testing.T, f Fixture) {
	f.AddIssue(t, forge.Issue{Number: 1, Title: "A", Body: "plain", Author: "ann", State: "open"})
	f.AddIssue(t, forge.Issue{Number: 2, Title: "B", Body: "<!-- other -->", Author: "ann", State: "closed"})
	got, err := f.Forge().FindByMarker(t.Context(), "<!-- lighthouse:pin none -->")
	if err != nil {
		t.Fatalf("FindByMarker: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("FindByMarker without a hit = %+v, want none", got)
	}
}

// findByMarkerPages puts the only hit on the second page of the issues list.
func findByMarkerPages(t *testing.T, f Fixture) {
	const marker = "<!-- lighthouse:pin paged -->"
	for n := 1; n <= page; n++ {
		is := forge.Issue{Number: n, Title: fmt.Sprintf("Issue %d", n), Author: "ann", State: "open"}
		if n == page {
			is.Body = marker
		}
		f.AddIssue(t, is)
	}
	if it := find(t, f, marker); it.Number != page || it.Body != marker || it.Author != "ann" {
		t.Errorf("found %+v; want #%d by ann from the second page", it, page)
	}
}

func openPRsWithStatus(t *testing.T, f Fixture) {
	if !f.SupportsReport() {
		t.Skip("forge has no period report")
	}
	f.AddOpenPRWithStatus(t, forge.PRSummary{Number: 52, Title: "Red", URL: "https://example.test/pull/52", Mergeable: "MERGEABLE", Checks: "FAILURE"})
	f.AddOpenPRWithStatus(t, forge.PRSummary{Number: 51, Title: "Conflict", URL: "https://example.test/pull/51", Mergeable: "CONFLICTING", Checks: "SUCCESS"})
	f.AddOpenPRWithStatus(t, forge.PRSummary{Number: 50, Title: "Draft", URL: "https://example.test/pull/50", Draft: true, Mergeable: "UNKNOWN"})
	got, err := f.Forge().OpenPRsWithStatus(t.Context())
	if err != nil {
		t.Fatalf("OpenPRsWithStatus: %v", err)
	}
	want := []forge.PRSummary{
		{Number: 50, Title: "Draft", URL: "https://example.test/pull/50", Draft: true, Mergeable: "UNKNOWN"},
		{Number: 51, Title: "Conflict", URL: "https://example.test/pull/51", Mergeable: "CONFLICTING", Checks: "SUCCESS"},
		{Number: 52, Title: "Red", URL: "https://example.test/pull/52", Mergeable: "MERGEABLE", Checks: "FAILURE"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("OpenPRsWithStatus = %+v; want %+v", got, want)
	}
}

func mergedPRs(t *testing.T, f Fixture) {
	if !f.SupportsReport() {
		t.Skip("forge has no period report")
	}
	since := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)
	f.AddMergedPR(t, forge.PRSummary{Number: 21, Title: "Later", URL: "https://example.test/pull/21", MergedAt: since.Add(15 * time.Hour)})
	f.AddMergedPR(t, forge.PRSummary{Number: 20, Title: "Earlier", URL: "https://example.test/pull/20", MergedAt: since.Add(2 * time.Hour)})
	f.AddMergedPR(t, forge.PRSummary{Number: 19, Title: "Outside", URL: "https://example.test/pull/19", MergedAt: since.Add(-time.Hour)})
	got, err := f.Forge().MergedPRs(t.Context(), since, until)
	if err != nil {
		t.Fatalf("MergedPRs: %v", err)
	}
	if len(got) != 2 || got[0].Number != 20 || got[1].Number != 21 {
		t.Fatalf("MergedPRs = %+v; want #20 then #21", got)
	}
	if got[0].Title != "Earlier" || got[0].URL == "" || got[0].Mergeable != "" || !got[0].MergedAt.Equal(since.Add(2*time.Hour)) {
		t.Errorf("MergedPRs[0] = %+v; want title, URL, MergedAt and no Mergeable", got[0])
	}
}

func closedIssues(t *testing.T, f Fixture) {
	if !f.SupportsReport() {
		t.Skip("forge has no period report")
	}
	since := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)
	done := closedIssue(30, nil, since.Add(3*time.Hour))
	done.StateReason = "completed"
	dropped := closedIssue(31, nil, since.Add(4*time.Hour))
	dropped.StateReason = "not_planned"
	outside := closedIssue(32, nil, since.Add(-2*time.Hour))
	outside.StateReason = "completed"
	f.AddIssue(t, done)
	f.AddIssue(t, dropped)
	f.AddIssue(t, outside)
	got, err := f.Forge().ClosedIssues(t.Context(), since, until)
	if err != nil {
		t.Fatalf("ClosedIssues: %v", err)
	}
	if len(got) != 1 || got[0].Number != 30 || got[0].StateReason != "completed" {
		t.Errorf("ClosedIssues = %+v; want only #30", got)
	}
}

func milestonedIssues(t *testing.T, f Fixture) {
	if !f.SupportsReport() {
		t.Skip("forge has no period report")
	}
	since := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)
	f.AddIssue(t, withMilestone(openIssue(40), &milestone))
	f.AddMilestonedEvent(t, 40, "M3", since.Add(5*time.Hour))
	f.AddIssue(t, withMilestone(openIssue(41), &milestone))
	f.AddMilestonedEvent(t, 41, "M3", since.Add(-48*time.Hour))
	f.AddIssue(t, closedIssue(42, &milestone, since.Add(6*time.Hour)))
	f.AddMilestonedEvent(t, 42, "M3", since.Add(2*time.Hour))
	got, err := f.Forge().MilestonedIssues(t.Context(), since, until)
	if err != nil {
		t.Fatalf("MilestonedIssues: %v", err)
	}
	if len(got) != 1 || got[0].Issue.Number != 40 || got[0].Milestone != milestone || !got[0].At.Equal(since.Add(5*time.Hour)) {
		t.Errorf("MilestonedIssues = %+v; want only #40 in M3 at 05:00", got)
	}
}

func withMilestone(is forge.Issue, m *forge.Milestone) forge.Issue {
	is.Milestone = m
	return is
}

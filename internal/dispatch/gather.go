package dispatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// dispatchStatuses are the board statuses whose items the dispatcher always looks at:
// Controller, and On hold as a read-only remnant. The tick no longer sets On hold, but still
// unparks an issue that stands there. A To do item counts only with an own 🤖 Parked
// (Pre.ToDo).
var dispatchStatuses = map[string]bool{statusController: true, statusOnHold: true}

// InController reports per issue whether the controller holds it in pre: a dispatch status, or
// To do with the tick's own 🤖 Parked as last protocol comment (Pre.ToDo). An issue without a
// status is not in the controller.
func InController(pre Pre) func(issue int) bool {
	return func(issue int) bool {
		status := pre.Statuses[issue]
		return dispatchStatuses[status] || status == statusToDo && pre.ToDo[issue].OwnParked
	}
}

// ToDoMark is what the precheck knows of one open To do item without a needs_* or
// blocked_external label: whether its last protocol comment (Rebased does not count) is a
// 🤖 Parked of the token login, read from its comments when the issue had IssueUpdatedAt.
type ToDoMark struct {
	IssueUpdatedAt time.Time `json:"issue_updated_at"`
	OwnParked      bool      `json:"own_parked"`
}

// ToDoProbe is what Prepare needs to find the To do items the tick parked itself. Known are the
// marks of the previous precheck (TickState.ToDo): an item whose IssueUpdatedAt still equals
// its mark is not read again, since a new comment moves IssueUpdatedAt. TokenLogin is called
// only when an item must be read, Trust only for a protocol comment of another author.
type ToDoProbe struct {
	Known      map[int]ToDoMark
	TokenLogin func() (string, error)
	Trust      *Trust
}

// SameToDo reports whether two sets of marks are equal, comparing times as instants.
func SameToDo(a, b map[int]ToDoMark) bool {
	return maps.EqualFunc(a, b, func(x, y ToDoMark) bool {
		return x.OwnParked == y.OwnParked && x.IssueUpdatedAt.Equal(y.IssueUpdatedAt)
	})
}

// MergedHead is the head SHA of the merged PR of prs with the latest MergedAt, leaving out a
// PR from another repository; "" when none is merged.
func MergedHead(prs []forge.PR) string {
	var last forge.PR
	for _, p := range prs {
		if p.State == "MERGED" && !p.CrossRepo && (last.HeadSHA == "" || p.MergedAt.After(last.MergedAt)) {
			last = p
		}
	}
	return last.HeadSHA
}

// Pre is the first half of a gather: the board fields and PRs of the items, without comments.
type Pre struct {
	Repo  repoconfig.Repo
	Items []Item // board fields and PRs; no comments
	// Statuses is the Status name of every board item of the repo, in any status; an issue
	// without an item is missing. The fingerprint does not read it.
	Statuses map[int]string
	OpenRuns []runs.OpenRun
	// ScheduledPRs are the PRs of the branch of each open scheduled run, by branch.
	ScheduledPRs map[string][]forge.PR
	Stands       map[int64]workplace.Stand // per open run id, set by the caller; missing = not measurable
	Runs         map[int64]RunState        // per open run id, set by the caller; missing = not measured
	// Markers are the controller markers whose session's last run is in this repo; the
	// fingerprint and the boundaries of the precheck read them.
	Markers []park.Marker
	// Unreadable are the sessions with a marker file that does not read (#513).
	Unreadable []string
	// PauseUntil is the end of the active pause flag; zero is no pause. It is in the fingerprint,
	// so a pause that ends makes the next tick gather.
	PauseUntil time.Time
	// ToDo is the mark of every open To do item of the repo without a needs_* or
	// blocked_external label (toDoCandidate); the caller saves it for the next precheck.
	ToDo map[int]ToDoMark
}

// Prepare reads the board of repo and keeps the issues of repo.Slug whose Status is
// Controller or On hold, that are To do with the tick's own 🤖 Parked as last protocol comment,
// or that have an open run in open, sorted by issue. The To do items are found through probe
// (markToDo): only an item that changed since probe.Known costs a read of its comments.
// branches is per issue the branch of its last run (IssueBranches),
// open or closed: such an item also gets the PRs of that branch, merged with its closing PRs,
// so a merged PR that does not close the issue stays visible after its run closed. Each open
// scheduled run with a branch gets the PRs of that branch in ScheduledPRs: it has no board item.
func Prepare(ctx context.Context, f forge.Forge, repo repoconfig.Repo, open []runs.OpenRun, branches map[int]string,
	probe ToDoProbe) (Pre, error) {
	if repo.Board == nil {
		return Pre{}, fmt.Errorf("dispatch: %s: missing [forge.board]", repo.Slug)
	}
	board, err := f.BoardItems(ctx)
	if err != nil {
		return Pre{}, err
	}
	// An open run is closed only through its board item. A forge whose BoardItems lists a window
	// (GitLab: open issues and recently closed ones) can leave out the issue of an open run, so
	// that issue is read on its own: one IssueItem call per open run outside the list, none on
	// GitHub, where every issue of a run is on the board. An issue that does not exist, or
	// whose item is archived, stays out.
	onBoard := make(map[int]bool, len(board))
	for _, bi := range board {
		if bi.Repo == repo.Slug {
			onBoard[bi.Issue] = true
		}
	}
	for _, r := range open {
		if r.Issue <= 0 || onBoard[r.Issue] {
			continue
		}
		bi, ok, err := f.IssueItem(ctx, r.Issue)
		if errors.Is(err, forge.ErrIssueNotFound) {
			continue // a deleted issue: its run stays as it was, the tick goes on
		}
		if err != nil {
			return Pre{}, err
		}
		if ok && !bi.Archived { // BoardItems lists no archived item, so neither does this
			board = append(board, bi)
			onBoard[r.Issue] = true
		}
	}
	// The status of every board item, before the dispatch filter: a blocker is usually not a
	// dispatch item.
	type ref struct {
		repo  string
		issue int
	}
	boardStatus := make(map[ref]string, len(board))
	statuses := map[int]string{}
	for _, bi := range board {
		if bi.Repo != "" && bi.Issue > 0 {
			status := known(bi.Status)
			boardStatus[ref{bi.Repo, bi.Issue}] = status
			if bi.Repo == repo.Slug {
				statuses[bi.Issue] = status
			}
		}
	}
	// An issue with an open run stays in, whatever its status: only settle closes that run,
	// and close-issue moves the item to Done before the tick sees the run end.
	hasRun := make(map[int]bool, len(open))
	for _, r := range open {
		if r.Schedule == "" { // a scheduled run has no issue
			hasRun[r.Issue] = true
		}
	}
	toDo, err := markToDo(ctx, f, repo, board, probe)
	if err != nil {
		return Pre{}, err
	}
	var items []Item
	for _, bi := range board {
		status := known(bi.Status)
		held := dispatchStatuses[status] || hasRun[bi.Issue] || status == statusToDo && toDo[bi.Issue].OwnParked
		if bi.Repo != repo.Slug || bi.Issue <= 0 || !held {
			continue
		}
		it := Item{
			Issue: bi.Issue, Title: bi.Title, State: bi.IssueState, ItemID: bi.ItemID,
			Status: status, Priority: bi.Priority,
			ItemUpdatedAt: bi.ItemUpdatedAt, IssueUpdatedAt: bi.IssueUpdatedAt,
			Labels:    bi.Labels,
			BlockedBy: withStatus(bi.BlockedBy, func(bl forge.Blocker) string { return boardStatus[ref{bl.Repo, bl.Issue}] }),
			PRs:       bi.ClosingPRs,
		}
		if br := branches[bi.Issue]; br != "" {
			onBranch, err := f.BranchPRs(ctx, br)
			if err != nil {
				return Pre{}, err
			}
			it.PRs = mergePRs(bi.ClosingPRs, onBranch)
		}
		slices.SortFunc(it.PRs, func(x, y forge.PR) int { return x.Number - y.Number })
		items = append(items, it)
	}
	slices.SortFunc(items, func(x, y Item) int { return x.Issue - y.Issue })
	scheduledPRs, err := scheduledBranchPRs(ctx, f, open)
	if err != nil {
		return Pre{}, err
	}
	return Pre{Repo: repo, Items: items, Statuses: statuses, OpenRuns: open, ScheduledPRs: scheduledPRs, ToDo: toDo}, nil
}

// toDoCandidate reports whether bi is an open To do item of repo that the tick may have parked:
// one without a needs_* or blocked_external label, which a human holds (heldByHuman).
func toDoCandidate(bi forge.BoardItem, repo repoconfig.Repo) bool {
	return bi.Repo == repo.Slug && bi.Issue > 0 && known(bi.Status) == statusToDo && bi.IssueState == "OPEN" &&
		!heldByHuman(bi.Labels, repo.Labels)
}

// markToDo is the mark of every toDoCandidate on board. A mark of probe.Known with the same
// IssueUpdatedAt stands without a request; any other candidate costs one Comments call, so an
// unchanged board costs none. A zero IssueUpdatedAt is always read: it proves nothing.
func markToDo(ctx context.Context, f forge.Forge, repo repoconfig.Repo, board []forge.BoardItem, probe ToDoProbe) (map[int]ToDoMark, error) {
	out := map[int]ToDoMark{}
	login := ""
	for _, bi := range board {
		if !toDoCandidate(bi, repo) {
			continue
		}
		if m, ok := probe.Known[bi.Issue]; ok && !bi.IssueUpdatedAt.IsZero() && m.IssueUpdatedAt.Equal(bi.IssueUpdatedAt) {
			out[bi.Issue] = m
			continue
		}
		if login == "" {
			if probe.TokenLogin == nil || probe.Trust == nil {
				return nil, fmt.Errorf("dispatch: %s#%d: no token login or trust to read a To do item", repo.Slug, bi.Issue)
			}
			var err error
			if login, err = probe.TokenLogin(); err != nil {
				return nil, err
			}
		}
		own, err := readOwnParked(ctx, f, bi.Issue, login, probe.Trust)
		if err != nil {
			return nil, err
		}
		out[bi.Issue] = ToDoMark{IssueUpdatedAt: bi.IssueUpdatedAt.UTC(), OwnParked: own}
	}
	return out, nil
}

// readOwnParked reads the comments of issue and reports whether its last protocol comment is a
// 🤖 Parked of login, as decide would see it (ownParkedBy): a protocol comment of an untrusted
// author drops out, as in Complete. Only a protocol comment of another author costs a trust check.
func readOwnParked(ctx context.Context, f forge.Forge, issue int, login string, trust *Trust) (bool, error) {
	all, err := f.Comments(ctx, issue)
	if err != nil {
		return false, err
	}
	var kept []Comment
	for _, pc := range all {
		if _, ok := protocol.ParseHeader(pc.Body); !ok {
			continue
		}
		if pc.Author != login {
			ok, err := trust.Trusted(ctx, pc.Author)
			if err != nil {
				return false, err
			}
			if !ok {
				continue
			}
		}
		kept = append(kept, Comment{ID: pc.ID, Author: pc.Author, Body: pc.Body, CreatedAt: pc.CreatedAt})
	}
	return ownParkedBy(headers(Item{Comments: kept}), login), nil
}

// known is status, or "" for a status option that [forge.board] status_options does not list:
// the dispatcher knows no such status, and its option ID is no text for a comment.
func known(status string) string {
	if strings.HasPrefix(status, forge.UnknownStatusPrefix) {
		return ""
	}
	return status
}

// withStatus returns a copy of blockers with Status set by status; the input stays untouched.
// The fingerprint ignores Status: it is text for the comment only.
func withStatus(blockers []forge.Blocker, status func(forge.Blocker) string) []forge.Blocker {
	if blockers == nil {
		return nil
	}
	out := slices.Clone(blockers)
	for i := range out {
		out[i].Status = status(out[i])
	}
	return out
}

// mergePRs joins the closing PRs and the PRs of a branch, one per number. A PR in both keeps
// the closing version, so Closes stays true.
func mergePRs(closing, onBranch []forge.PR) []forge.PR {
	out := slices.Clone(closing)
	for _, p := range onBranch {
		if !slices.ContainsFunc(out, func(q forge.PR) bool { return q.Number == p.Number }) {
			out = append(out, p)
		}
	}
	return out
}

// Complete reads the comments of every prepared item and returns the snapshot. Comments of an
// untrusted author drop out; a dropped 🤖-comment gets one line in log. A comment's Rocket is
// set only when tokenLogin put a 🚀 on it. Any failed request fails the whole gather. Every
// list in the snapshot is non-nil, so its JSON has [] where a list is empty, never null.
// For an item with an open run, every merge-ready PR gets its roborev state from the PR's
// comments, on GitHub only; the PRs of Pre stay untouched.
func Complete(ctx context.Context, f forge.Forge, pre Pre, trust *Trust, tokenLogin string, log io.Writer, now time.Time) (Snapshot, error) {
	items := make([]Item, 0, len(pre.Items))
	hasRun := make(map[int]bool, len(pre.OpenRuns))
	for _, r := range pre.OpenRuns {
		if r.Schedule == "" { // a scheduled run has no issue
			hasRun[r.Issue] = true
		}
	}
	for _, it := range pre.Items {
		if hasRun[it.Issue] {
			it.PRs = slices.Clone(it.PRs)
			for i, pr := range it.PRs {
				// On GitHub a PR is an issue too, so its comments are those of its number. On
				// GitLab a merge request has its own numbering, and the number would read the
				// notes of another issue: the roborev state stays zero there.
				if !mergeCandidate(pr) || f.Kind() != "github" {
					continue
				}
				prc, err := f.Comments(ctx, pr.Number)
				if err != nil {
					return Snapshot{}, err
				}
				if it.PRs[i].Roborev, err = roborevState(ctx, prc, trust); err != nil {
					return Snapshot{}, err
				}
			}
		}
		all, err := f.Comments(ctx, it.Issue)
		if err != nil {
			return Snapshot{}, err
		}
		it.BlockedBy = nonNil(it.BlockedBy)
		it.PRs = nonNil(it.PRs)
		it.Comments = []Comment{}
		for _, pc := range all {
			ok, err := trust.Trusted(ctx, pc.Author)
			if err != nil {
				return Snapshot{}, err
			}
			if !ok {
				if strings.HasPrefix(strings.TrimSpace(pc.Body), protocol.Robot) {
					_, _ = fmt.Fprintf(log, "lighthouse: %s#%d: ignored 🤖-comment %s by %s: role not in answer_permissions\n",
						pre.Repo.Slug, it.Issue, pc.URL, pc.Author)
				}
				continue
			}
			rocket, err := tokenRocket(ctx, f, it.Issue, pc, tokenLogin)
			if err != nil {
				return Snapshot{}, err
			}
			it.Comments = append(it.Comments, Comment{ID: pc.ID, Author: pc.Author, Body: pc.Body,
				CreatedAt: pc.CreatedAt, URL: pc.URL, Rocket: rocket})
		}
		it.PRs = withPushedSHA(it.PRs, it.Comments)
		items = append(items, it)
	}
	scheduledPRs, err := withRoborev(ctx, f, pre.ScheduledPRs, trust)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Version:      SnapshotVersion,
		Repo:         pre.Repo.Slug,
		TokenLogin:   tokenLogin,
		GatheredAt:   now.UTC(),
		Items:        items,
		OpenRuns:     nonNil(pre.OpenRuns),
		ScheduledPRs: scheduledPRs,
	}, nil
}

// withPushedSHA returns a copy of prs where each PR carries the SHA of the newest pusher comment
// in comments (trusted ones only) whose branch is that PR's head ref. A push of another branch
// never counts: it says nothing about this PR's workflows. The SHA only routes the merge to the
// pusher; the pusher's merge takes the SHA it checks from its own push rows, never from a comment.
func withPushedSHA(prs []forge.PR, comments []Comment) []forge.PR {
	latest := map[string]string{}
	for _, c := range comments {
		if p, ok := protocol.ParsePushed(c.Body); ok {
			latest[p.Branch] = p.SHA
		}
	}
	out := slices.Clone(prs)
	for i := range out {
		out[i].PushedSHA = latest[out[i].HeadRef]
	}
	return out
}

// nonNil returns s, or an empty slice when s is nil.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// tokenRocket reports whether tokenLogin put a 🚀 on comment pc of issue. A comment without any
// 🚀 costs no request.
func tokenRocket(ctx context.Context, f forge.Forge, issue int, pc protocol.Comment, tokenLogin string) (bool, error) {
	if pc.Rockets == 0 {
		return false, nil
	}
	logins, err := f.Acknowledgers(ctx, issue, pc.ID)
	if err != nil {
		return false, err
	}
	return slices.Contains(logins, tokenLogin), nil
}

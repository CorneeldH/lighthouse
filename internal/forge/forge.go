package forge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// ErrIssueNotFound means an issue does not exist: IssueItem wraps it, on every forge. It is
// not "the issue is not on the board", which is IssueItem's ok=false.
var ErrIssueNotFound = errors.New("issue not found")

// ErrReportUnsupported is the answer of a forge that cannot yet list the periods of lighthouse report.
var ErrReportUnsupported = errors.New("lighthouse report is not supported on this forge yet")

// ErrNoJobLog means a job has no log (any more): JobLog wraps it, on every forge.
var ErrNoJobLog = errors.New("no log for job")

// Forge is one repository on a forge: its issues, comments, acknowledgements, pull requests
// and board. A status is an option name of [forge.board] status_options, never an ID.
type Forge interface {
	// Kind is "github" or "gitlab".
	Kind() string
	Login(ctx context.Context) (string, error)
	// Role is the repository role of user; "" for a user the repo does not know.
	Role(ctx context.Context, user string) (string, error)
	Issue(ctx context.Context, number int) (Issue, error)
	OpenIssues(ctx context.Context) ([]Issue, error)
	ClosedInMilestone(ctx context.Context, m Milestone, since time.Time) ([]Issue, error)
	BlockedBy(ctx context.Context, number int) ([]Blocker, error)
	// Blocking lists the issues that issue number blocks, with their own state. Every page is
	// followed; a failing page is an error, never a shorter list. MergedPR stays false.
	Blocking(ctx context.Context, number int) ([]Blocker, error)
	// Planning reads the open issues with their milestone and open blockers, and the issue
	// counts of the open milestones, for the prioritisation view.
	Planning(ctx context.Context) (Planning, error)
	// PrioInputs reads what the priority weight needs besides the blockers: the milestone of
	// every open issue and the open milestones with their description. It reads no closed
	// issues and no blockers, so it can run on every tick.
	PrioInputs(ctx context.Context) (PrioInputs, error)
	Comments(ctx context.Context, number int) ([]protocol.Comment, error)
	CreateComment(ctx context.Context, number int, body string) (protocol.Comment, error)
	// Acknowledge marks comment commentID on issue number as seen by the token's user.
	Acknowledge(ctx context.Context, number int, commentID int64) error
	// Acknowledgers lists the logins that marked comment commentID on issue number as seen.
	Acknowledgers(ctx context.Context, number int, commentID int64) ([]string, error)
	// MergePR squash-merges pull request number, only while its head is still sha. It fails
	// with ErrHeadMoved when the head moved, and with *MergeRefused when the forge declines.
	MergePR(ctx context.Context, number int, sha string) error
	// DeleteBranch deletes branch; a branch that is already gone is not an error.
	DeleteBranch(ctx context.Context, branch string) error
	OpenPRs(ctx context.Context) ([]OpenPR, error)
	// OpenPRsWithStatus lists the open pull requests with draft, mergeable and checks, by number.
	OpenPRsWithStatus(ctx context.Context) ([]PRSummary, error)
	// MergedPRs lists the pull requests that merged in [since, until), oldest merge first.
	MergedPRs(ctx context.Context, since, until time.Time) ([]PRSummary, error)
	// ClosedIssues lists the issues closed as done in [since, until), oldest close first;
	// an issue closed for another reason than completed (not planned, duplicate) is left out.
	ClosedIssues(ctx context.Context, since, until time.Time) ([]Issue, error)
	// MilestonedIssues lists the open issues put in their current milestone in [since, until),
	// by milestone title and then number; At is the last such moment.
	MilestonedIssues(ctx context.Context, since, until time.Time) ([]MilestonedIssue, error)
	BranchPRs(ctx context.Context, branch string) ([]PR, error)
	// PullHeads lists every pull request or merge request of the repo, in any state, over all
	// pages: its number, state, merge time and head branch.
	PullHeads(ctx context.Context) ([]PullHead, error)
	// IssueClosingHeads lists the pull requests that close issue number or refer to it, as the
	// forge reports them. It filters nothing: a PR that is not merged, from a fork, from another
	// repo or without a mention in its body is returned too, for the caller to judge.
	IssueClosingHeads(ctx context.Context, number int) ([]ClosingHead, error)
	BoardItems(ctx context.Context) ([]BoardItem, error)
	// IssueItem reads the board item of issue number. ok is false for an issue that exists but
	// is not on the board. An issue that does not exist is an error wrapping ErrIssueNotFound.
	IssueItem(ctx context.Context, number int) (item BoardItem, ok bool, err error)
	AddToBoard(ctx context.Context, number int) (BoardItem, error)
	SetStatus(ctx context.Context, item BoardItem, status string) error

	// The methods below are the wrapper (`lighthouse forge`). A milestone is given by title;
	// Milestone.Number is the number the forge uses in a path (on GitLab the global id).

	// CreateIssue opens an issue and returns it as the forge stored it.
	CreateIssue(ctx context.Context, in NewIssue) (Issue, error)
	EditIssue(ctx context.Context, number int, e IssueEdit) (Issue, error)
	ListIssues(ctx context.Context, f IssueFilter) ([]Issue, error)
	SearchIssues(ctx context.Context, text string) ([]Issue, error)
	CreateChange(ctx context.Context, in NewPR) (Change, error)
	Change(ctx context.Context, number int) (Change, error)
	// ListChanges lists the pull or merge requests in state "open", "merged", "closed" (closed
	// without a merge) or "all"; "" is open, any other value an error. HeadRef is filled and
	// MergedAt set for a merged one. Every page is followed; a failing page is an error.
	ListChanges(ctx context.Context, state string) ([]Change, error)
	// IssueChanges lists the pull or merge requests that close issue number (Closes true) or
	// mention it, each once, the closing ones first. Every page is followed; a failing page is
	// an error, never a shorter list.
	IssueChanges(ctx context.Context, number int) ([]IssueChange, error)
	// Closer is the number of the pull or merge request that closed issue number the last time;
	// 0 when none did, also for an issue closed by hand or never closed.
	Closer(ctx context.Context, number int) (int, error)
	// ChangeComments lists the comments of pull request number: on GitLab the notes of the merge
	// request without system notes.
	ChangeComments(ctx context.Context, number int) ([]protocol.Comment, error)
	// Checks lists the CI checks of pull request number, newest pipeline only on GitLab. No checks
	// is an empty list, not an error.
	Checks(ctx context.Context, number int) ([]Check, error)
	// JobLog is the raw log of a check's job; an error wrapping ErrNoJobLog when there is none.
	JobLog(ctx context.Context, job int64) (string, error)
	CreateChangeComment(ctx context.Context, number int, body string) (protocol.Comment, error)
	// Milestones lists the milestones in state "open", "closed" or "all"; "" is open.
	Milestones(ctx context.Context, state string) ([]Milestone, error)
	MilestoneByNumber(ctx context.Context, number int) (Milestone, error)
	CreateMilestone(ctx context.Context, title, description string) (Milestone, error)
	EditMilestone(ctx context.Context, number int, title, description, state *string) (Milestone, error)
	Labels(ctx context.Context) ([]string, error)
	// CreateLabel creates the label name with color, six hex digits without a "#". A label that
	// already exists is no error.
	CreateLabel(ctx context.Context, name, color string) error
	// DeleteLabel deletes the label name from the repository, and so from every issue that carries
	// it. A label that does not exist is no error.
	DeleteLabel(ctx context.Context, name string) error
	// CommitKnown reports whether the forge knows the commit sha; a 404 or 422 is false, nil,
	// any other failure is returned.
	CommitKnown(ctx context.Context, sha string) (bool, error)
	// BranchContains reports whether branch on the forge holds sha in its history. A branch that
	// does not exist is false, nil.
	BranchContains(ctx context.Context, branch, sha string) (bool, error)
	// AddBlocker makes issue blocker block issue number.
	AddBlocker(ctx context.Context, number, blocker int) error
	// API sends r to the forge's REST API and returns the raw response body.
	API(ctx context.Context, r APIRequest) ([]byte, error)

	// The methods below are the writes of the pins lane.

	UpdateIssueBody(ctx context.Context, number int, body string) error
	// CloseIssue posts comment (when not empty) and then closes the issue.
	CloseIssue(ctx context.Context, number int, comment string) error
	// ReopenIssue posts comment (when not empty) and then reopens the issue. A failing comment
	// is returned and leaves the issue closed.
	ReopenIssue(ctx context.Context, number int, comment string) error
	// CreatePR opens a pull request; with NewPR.Draft it is a draft.
	CreatePR(ctx context.Context, pr NewPR) (PR, error)
	UpdatePRBody(ctx context.Context, number int, body string) error
	// ClosePR posts comment (when not empty) and then closes the pull request unmerged.
	ClosePR(ctx context.Context, number int, comment string) error
	// FindByMarker lists the issues and pull requests, open and closed, whose body contains
	// marker. No hit is an empty list, not an error; an empty marker is an error.
	FindByMarker(ctx context.Context, marker string) ([]Item, error)
}

var _ Forge = (*GitHub)(nil)
var _ Forge = (*GitLab)(nil)

// Config is what Open needs to reach one repository.
type Config struct {
	Kind, Repo, Host, Token string
	// BaseURL is the API root; "" is the default of Kind. Trailing slashes are dropped.
	BaseURL string
	// Board is the [forge.board] of the repo; nil makes every board method fail.
	Board *repoconfig.Board
	// HTTP is the client for every request; nil is one with a 30 s timeout.
	HTTP *http.Client
	// Cache stores the answers of conditional GETs (GitHub only); nil: no cache.
	Cache CacheStore
	// CacheLog receives the one line a cache error produces per transport; nil discards it.
	CacheLog io.Writer
}

// Open returns the forge of cfg.Kind ("" is github).
func Open(cfg Config) (Forge, error) {
	switch cfg.Kind {
	case "", "github":
		c := New(cfg.Repo, cfg.Token)
		if cfg.BaseURL != "" {
			c.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
		}
		if cfg.HTTP != nil {
			c.HTTP = cfg.HTTP
		}
		c.Board = cfg.Board
		if cfg.Cache != nil {
			c.cache = cfg.Cache
			c.HTTP = &http.Client{Timeout: c.HTTP.Timeout, Transport: newCachingTransport(c.HTTP.Transport, cfg.Cache, cfg.CacheLog)}
		}
		return c, nil
	case "gitlab":
		return newGitLab(cfg), nil
	default:
		return nil, fmt.Errorf("forge: kind %q is not github or gitlab", cfg.Kind)
	}
}

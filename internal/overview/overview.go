// Package overview builds the lists of open questions and returned issues from the forge.
package overview

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// maxConcurrent caps the comment fetches that run at the same time.
const maxConcurrent = 8

// Source is where the open issues and their comments come from; *forge.Client satisfies it.
type Source interface {
	OpenIssues(ctx context.Context) ([]forge.Issue, error)
	Comments(ctx context.Context, number int) ([]protocol.Comment, error)
}

// Filter returns the numbers of the issues the overview may show. A nil Filter shows every
// open issue, as without [dispatch].
type Filter func(ctx context.Context) (map[int]bool, error)

// BoardReader lists the items of a Projects v2 board; *forge.Client satisfies it.
type BoardReader interface {
	BoardItems(ctx context.Context, projectID string) ([]forge.BoardItem, error)
}

// ControllerIssues is the Filter of the issues of slug whose Status on board b is the option
// b.StatusOptions["Controller"]. The caller makes sure that option is set: an empty option ID
// would match every item without a Status.
func ControllerIssues(r BoardReader, slug string, b repoconfig.Board) Filter {
	controller := b.StatusOptions["Controller"]
	return func(ctx context.Context) (map[int]bool, error) {
		items, err := r.BoardItems(ctx, b.ProjectID)
		if err != nil {
			return nil, err
		}
		in := map[int]bool{}
		for _, it := range items {
			if it.Repo == slug && it.SingleSelect[b.StatusFieldID] == controller {
				in[it.Issue] = true
			}
		}
		return in, nil
	}
}

// Question is an open question on an issue.
type Question struct {
	// Repo is the slug of the repository the issue belongs to; set by BuildAll.
	Repo     string `json:"repo"`
	Number   int    `json:"number"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Question string `json:"question"`
	// AnswerURL is the URL of the question comment, where the answer goes; the issue URL
	// when the comment has none.
	AnswerURL string `json:"answer_url"`
}

// Returned is an issue that was handed back, with the reason from its header.
type Returned struct {
	// Repo is the slug of the repository the issue belongs to; set by BuildAll.
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Reason string `json:"reason"`
}

// Overview holds both lists; neither is ever nil.
type Overview struct {
	Questions []Question
	Returned  []Returned
}

// RepoSource is one repository for BuildAll: its slug, where its issues come from and its
// controller filter (nil shows every open issue).
type RepoSource struct {
	Slug         string
	Src          Source
	InController Filter
}

// BuildAll runs Build per repo, in the order given, and sets Repo on every item. The first
// failure fails the whole overview and names its repo; with no repos it returns
// forge.ErrNotConfigured.
func BuildAll(ctx context.Context, repos []RepoSource) (Overview, error) {
	if len(repos) == 0 {
		return Overview{}, forge.ErrNotConfigured
	}
	all := Overview{Questions: []Question{}, Returned: []Returned{}}
	for _, r := range repos {
		o, err := Build(ctx, r.Src, r.InController)
		if err != nil {
			return Overview{}, fmt.Errorf("%s: %w", r.Slug, err)
		}
		for _, q := range o.Questions {
			q.Repo = r.Slug
			all.Questions = append(all.Questions, q)
		}
		for _, x := range o.Returned {
			x.Repo = r.Slug
			all.Returned = append(all.Returned, x)
		}
	}
	return all, nil
}

// entry is one result with the sort key of the comment that put it on the list.
type entry[T any] struct {
	number    int
	createdAt string
	item      T
}

// Build lists the open questions and returned issues, oldest header comment first. With a
// non-nil inController only the issues it names count, and no comments are read for the
// others. Any failure fails the whole build: a partial list would hide issues.
func Build(ctx context.Context, src Source, inController Filter) (Overview, error) {
	issues, err := src.OpenIssues(ctx)
	if err != nil {
		return Overview{}, err
	}
	if inController != nil {
		in, err := inController(ctx)
		if err != nil {
			return Overview{}, err
		}
		// A new slice: the source may keep the one it returned.
		kept := make([]forge.Issue, 0, len(issues))
		for _, issue := range issues {
			if in[issue.Number] {
				kept = append(kept, issue)
			}
		}
		issues = kept
	}
	statuses, err := fetchStatuses(ctx, src, issues)
	if err != nil {
		return Overview{}, err
	}

	var questions []entry[Question]
	var returned []entry[Returned]
	for i, issue := range issues {
		status := statuses[i]
		switch status.State {
		case protocol.StateQuestion:
			answerURL := status.Comment.URL
			if answerURL == "" {
				answerURL = issue.URL
			}
			questions = append(questions, entry[Question]{issue.Number, status.Comment.CreatedAt, Question{
				Number:    issue.Number,
				Title:     issue.Title,
				URL:       issue.URL,
				Question:  protocol.QuestionText(status.Comment.Body),
				AnswerURL: answerURL,
			}})
		case protocol.StateReturned:
			returned = append(returned, entry[Returned]{issue.Number, status.Comment.CreatedAt, Returned{
				Number: issue.Number,
				Title:  issue.Title,
				URL:    issue.URL,
				Reason: status.Header.Fields["reason"],
			}})
		}
	}

	return Overview{Questions: sorted(questions), Returned: sorted(returned)}, nil
}

// sorted orders oldest comment first, lowest issue number on a tie, and never returns nil.
// RFC 3339 UTC timestamps, as GitHub sends them, sort correctly as strings.
func sorted[T any](entries []entry[T]) []T {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.createdAt != b.createdAt {
			return a.createdAt < b.createdAt
		}
		return a.number < b.number
	})
	out := make([]T, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.item)
	}
	return out
}

// fetchStatuses classifies the comments of every issue that has any, at most maxConcurrent
// at a time. The first error cancels the rest and is the one returned.
func fetchStatuses(ctx context.Context, src Source, issues []forge.Issue) ([]protocol.Status, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	statuses := make([]protocol.Status, len(issues))
	slots := make(chan struct{}, maxConcurrent)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
loop:
	for i, issue := range issues {
		if issue.Comments == 0 {
			continue
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		wg.Go(func() {
			defer func() { <-slots }()
			comments, err := src.Comments(ctx, issue.Number)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
				return
			}
			statuses[i] = protocol.Classify(comments)
		})
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	// The caller's context ended before every issue was fetched.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return statuses, nil
}

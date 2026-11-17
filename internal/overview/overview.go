// Package overview builds the lists of open questions and returned issues from the forge.
package overview

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
)

// maxConcurrent caps the comment fetches that run at the same time.
const maxConcurrent = 8

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

// IssueStatus is the board Status of one issue; "" when it has none.
type IssueStatus struct {
	Repo   string `json:"repo"`
	Issue  int    `json:"issue"`
	Status string `json:"status"`
}

// Overview holds the lists; none is ever nil.
type Overview struct {
	Questions []Question
	Returned  []Returned
	// Statuses is the board Status of every issue of the repos with Board set.
	Statuses []IssueStatus
}

// RepoSource is one repository for BuildAll: its slug and where its issues come from. Board
// reads the board once for the statuses; Controller, which needs Board, also narrows the
// questions and returned issues to the issues in Status Controller. Without Controller every
// open issue counts, as without [dispatch].
type RepoSource struct {
	Slug       string
	Src        forge.Forge
	Board      bool
	Controller bool
}

// BuildAll runs Build per repo, in the order given, and sets Repo on every item. The first
// failure fails the whole overview and names its repo; with no repos it returns
// forge.ErrNotConfigured. The board is read once per repo with Board set. A failing read fails
// the overview only with Controller set: without it the overview never needed the board, so the
// repo just has no statuses.
func BuildAll(ctx context.Context, repos []RepoSource) (Overview, error) {
	if len(repos) == 0 {
		return Overview{}, forge.ErrNotConfigured
	}
	all := Overview{Questions: []Question{}, Returned: []Returned{}, Statuses: []IssueStatus{}}
	for _, r := range repos {
		var statuses []IssueStatus
		var in map[int]bool
		if r.Board {
			items, err := r.Src.BoardItems(ctx)
			switch {
			case err != nil && r.Controller:
				return Overview{}, fmt.Errorf("%s: %w", r.Slug, err)
			case err == nil:
				if r.Controller {
					in = map[int]bool{}
				}
				for _, it := range items {
					if it.Repo != r.Slug {
						continue
					}
					statuses = append(statuses, IssueStatus{Repo: r.Slug, Issue: it.Issue, Status: it.Status})
					if r.Controller && it.Status == "Controller" {
						in[it.Issue] = true
					}
				}
			}
		}
		o, err := Build(ctx, r.Src, in)
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
		all.Statuses = append(all.Statuses, statuses...)
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
// non-nil in only the issues it names count, and no comments are read for the
// others. Any failure fails the whole build: a partial list would hide issues.
func Build(ctx context.Context, src forge.Forge, in map[int]bool) (Overview, error) {
	issues, err := src.OpenIssues(ctx)
	if err != nil {
		return Overview{}, err
	}
	if in != nil {
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
func fetchStatuses(ctx context.Context, src forge.Forge, issues []forge.Issue) ([]protocol.Status, error) {
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

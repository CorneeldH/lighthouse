// Package overview builds the lists of open questions and returned issues from the forge.
package overview

import (
	"context"
	"sort"
	"sync"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
)

// maxConcurrent caps the comment fetches that run at the same time.
const maxConcurrent = 8

// Source is where the open issues and their comments come from; *forge.Client satisfies it.
type Source interface {
	OpenIssues(ctx context.Context) ([]forge.Issue, error)
	Comments(ctx context.Context, number int) ([]protocol.Comment, error)
}

// Question is an open question on an issue.
type Question struct {
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

// entry is one result with the sort key of the comment that put it on the list.
type entry[T any] struct {
	number    int
	createdAt string
	item      T
}

// Build lists the open questions and returned issues, oldest header comment first. Any
// failure fails the whole build: a partial list would hide issues.
func Build(ctx context.Context, src Source) (Overview, error) {
	issues, err := src.OpenIssues(ctx)
	if err != nil {
		return Overview{}, err
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

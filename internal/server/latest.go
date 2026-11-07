package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/CorneeldH/lighthouse/internal/overview"
)

type latestCommentInput struct {
	Repo  string `query:"repo" required:"true" doc:"Slug of a served repo, owner/name"`
	Issue int    `query:"issue" required:"true" minimum:"1"`
}

type redirectOutput struct {
	Status   int
	Location string `header:"Location"`
}

// registerLatestComment adds the endpoint the UI links an issue to: it redirects to the newest
// comment of the issue, read from the forge at click time, so the link is never older than the
// click. Without comments, or when the forge read fails, it redirects to the issue page. Only
// the served repos redirect; any other repo is a 404, so the server is no open redirect.
func registerLatestComment(api huma.API, repos []overview.RepoSource) {
	huma.Get(api, "/api/latest-comment", func(ctx context.Context, in *latestCommentInput) (*redirectOutput, error) {
		for _, r := range repos {
			if r.Slug == in.Repo {
				return &redirectOutput{Status: http.StatusFound, Location: latestCommentURL(ctx, r, in.Issue)}, nil
			}
		}
		return nil, huma.Error404NotFound(fmt.Sprintf("repo %s is not served", in.Repo))
	}, func(op *huma.Operation) {
		op.OperationID = "get-latest-comment"
		op.DefaultStatus = http.StatusFound // the description names 302, not huma's 204 for a bodiless output
	})
}

// latestCommentURL is the URL of the last comment of issue, or the issue page when it has none
// or the comments cannot be read.
func latestCommentURL(ctx context.Context, r overview.RepoSource, issue int) string {
	comments, err := r.Src.Comments(ctx, issue)
	if err == nil && len(comments) > 0 {
		if u := comments[len(comments)-1].URL; u != "" {
			return u
		}
	}
	return fmt.Sprintf("https://github.com/%s/issues/%d", r.Slug, issue)
}

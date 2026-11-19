package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"
	"strconv"
)

// glJobBucket maps a job status to a bucket; a failed job that may fail passes.
func glJobBucket(status string, allowFailure bool) string {
	switch status {
	case "success":
		return "pass"
	case "failed":
		if allowFailure {
			return "pass"
		}
		return "fail"
	case "skipped", "manual":
		return "skipping"
	case "canceled":
		return "cancel"
	default: // created, waiting_for_resource, preparing, pending, running, scheduled
		return "pending"
	}
}

// Checks reads the jobs of the newest pipeline of merge request number. A merge request without
// a pipeline has no checks.
func (g *GitLab) Checks(ctx context.Context, number int) ([]Check, error) {
	if err := g.configured(); err != nil {
		return nil, err
	}
	pipelines, err := getAll[struct {
		ID int `json:"id"`
	}](ctx, g, g.projectPath("/merge_requests/"+strconv.Itoa(number)+"/pipelines"), nil)
	if err != nil {
		return nil, err
	}
	out := []Check{}
	newest := 0
	for _, p := range pipelines {
		newest = max(newest, p.ID)
	}
	if newest == 0 {
		return out, nil
	}
	jobs, err := getAll[struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		Status       string `json:"status"`
		AllowFailure bool   `json:"allow_failure"`
		WebURL       string `json:"web_url"`
	}](ctx, g, g.projectPath("/pipelines/"+strconv.Itoa(newest)+"/jobs"), nil)
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		out = append(out, Check{Name: j.Name, Bucket: glJobBucket(j.Status, j.AllowFailure), Link: j.WebURL, Job: j.ID})
	}
	return out, nil
}

// JobLog reads the trace of job.
func (g *GitLab) JobLog(ctx context.Context, job int64) (string, error) {
	if err := g.configured(); err != nil {
		return "", err
	}
	var raw []byte
	if _, err := g.do(ctx, http.MethodGet, g.projectPath("/jobs/"+strconv.FormatInt(job, 10)+"/trace"), neturl.Values{}, nil, &raw); err != nil {
		var se *httpStatusError
		if errors.As(err, &se) && se.Code == http.StatusNotFound {
			return "", fmt.Errorf("%w %d", ErrNoJobLog, job)
		}
		return "", err
	}
	return string(raw), nil
}

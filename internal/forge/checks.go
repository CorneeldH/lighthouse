package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// Check is one CI check of a pull request. Bucket is pass, fail, pending, skipping or cancel.
// Job is the ID JobLog reads, 0 when the check has no log (a commit status).
type Check struct {
	Name, Bucket, Link string
	Job                int64
}

// ghCheckBucket maps a check-run to a bucket; a run that is not completed is pending.
func ghCheckBucket(status, conclusion string) string {
	if status != "completed" {
		return "pending"
	}
	switch conclusion {
	case "success", "neutral":
		return "pass"
	case "skipped":
		return "skipping"
	case "cancelled":
		return "cancel"
	default: // failure, timed_out, action_required, startup_failure, stale
		return "fail"
	}
}

// ghStatusBucket maps a commit status state to a bucket.
func ghStatusBucket(state string) string {
	switch state {
	case "success":
		return "pass"
	case "failure", "error":
		return "fail"
	default:
		return "pending"
	}
}

// Checks reads the check-runs and the commit statuses of the head of pull request number, every
// page. Check-runs come first, then statuses.
func (c *GitHub) Checks(ctx context.Context, number int) ([]Check, error) {
	p, err := c.pullHead(ctx, number)
	if err != nil {
		return nil, err
	}
	out := []Check{}
	url := c.repoURL("/commits/"+p+"/check-runs") + "?per_page=100"
	for url != "" {
		var page struct {
			Runs []struct {
				ID         int64  `json:"id"`
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				HTMLURL    string `json:"html_url"`
			} `json:"check_runs"`
		}
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, r := range page.Runs {
			out = append(out, Check{Name: r.Name, Bucket: ghCheckBucket(r.Status, r.Conclusion), Link: r.HTMLURL, Job: r.ID})
		}
		url = next
	}
	url = c.repoURL("/commits/"+p+"/status") + "?per_page=100"
	for url != "" {
		var page struct {
			Statuses []struct {
				Context   string `json:"context"`
				State     string `json:"state"`
				TargetURL string `json:"target_url"`
			} `json:"statuses"`
		}
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, s := range page.Statuses {
			out = append(out, Check{Name: s.Context, Bucket: ghStatusBucket(s.State), Link: s.TargetURL})
		}
		url = next
	}
	return out, nil
}

// pullHead is the head sha of pull request number.
func (c *GitHub) pullHead(ctx context.Context, number int) (string, error) {
	if err := c.configured(); err != nil {
		return "", err
	}
	var p struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if _, err := c.do(ctx, http.MethodGet, c.repoURL("/pulls/"+strconv.Itoa(number)), nil, &p); err != nil {
		return "", err
	}
	if p.Head.SHA == "" {
		return "", fmt.Errorf("forge: pull request #%d has no head sha", number)
	}
	return p.Head.SHA, nil
}

// JobLog reads the log of a check-run's job; the redirect to the log storage is followed.
func (c *GitHub) JobLog(ctx context.Context, job int64) (string, error) {
	if err := c.configured(); err != nil {
		return "", err
	}
	var raw []byte
	if _, err := c.do(ctx, http.MethodGet, c.repoURL("/actions/jobs/"+strconv.FormatInt(job, 10)+"/logs"), nil, &raw); err != nil {
		var se *httpStatusError
		if errors.As(err, &se) && se.Code == http.StatusNotFound {
			return "", fmt.Errorf("%w %d", ErrNoJobLog, job)
		}
		return "", err
	}
	return string(raw), nil
}

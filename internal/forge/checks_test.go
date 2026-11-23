package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// checksServer fakes the GitHub endpoints Checks reads for pull request 5 with head abc.
func checksServer(t *testing.T, runs []map[string]any, statuses []map[string]any) *GitHub {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/pulls/5", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"number":5,"head":{"ref":"b","sha":"abc"}}`)
	})
	mux.HandleFunc("GET /repos/o/r/commits/abc/check-runs", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, map[string]any{"total_count": len(runs), "check_runs": runs})
	})
	mux.HandleFunc("GET /repos/o/r/commits/abc/status", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, map[string]any{"state": "pending", "statuses": statuses})
	})
	mux.HandleFunc("GET /repos/o/r/actions/jobs/9/logs", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "line 1\nline 2\n")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := New("o/r", "secret-token")
	c.BaseURL = srv.URL
	return c
}

func TestGitHubChecksBuckets(t *testing.T) {
	type row struct{ status, conclusion, bucket string }
	rows := []row{
		{"completed", "success", "pass"}, {"completed", "neutral", "pass"},
		{"completed", "failure", "fail"}, {"completed", "timed_out", "fail"},
		{"completed", "action_required", "fail"}, {"completed", "startup_failure", "fail"},
		{"in_progress", "", "pending"}, {"queued", "", "pending"},
		{"completed", "skipped", "skipping"}, {"completed", "cancelled", "cancel"},
	}
	var runs []map[string]any
	for i, r := range rows {
		run := map[string]any{"id": 100 + i, "name": fmt.Sprintf("run-%d", i), "status": r.status, "html_url": fmt.Sprintf("https://x/run/%d", i)}
		if r.conclusion != "" {
			run["conclusion"] = r.conclusion
		}
		runs = append(runs, run)
	}
	statuses := []map[string]any{
		{"context": "s-ok", "state": "success", "target_url": "https://x/s1"},
		{"context": "s-failure", "state": "failure", "target_url": "https://x/s2"},
		{"context": "s-error", "state": "error", "target_url": "https://x/s3"},
		{"context": "s-pending", "state": "pending", "target_url": "https://x/s4"},
	}
	got, err := checksServer(t, runs, statuses).Checks(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	var want []Check
	for i, r := range rows {
		want = append(want, Check{Name: fmt.Sprintf("run-%d", i), Bucket: r.bucket, Link: fmt.Sprintf("https://x/run/%d", i), Job: int64(100 + i)})
	}
	want = append(want,
		Check{Name: "s-ok", Bucket: "pass", Link: "https://x/s1"},
		Check{Name: "s-failure", Bucket: "fail", Link: "https://x/s2"},
		Check{Name: "s-error", Bucket: "fail", Link: "https://x/s3"},
		Check{Name: "s-pending", Bucket: "pending", Link: "https://x/s4"})
	if !slices.Equal(got, want) {
		t.Errorf("Checks =\n%+v\nwant\n%+v", got, want)
	}
}

func TestChecksEmptyWithoutRuns(t *testing.T) {
	got, err := checksServer(t, nil, nil).Checks(context.Background(), 5)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("GitHub Checks = %#v, %v; want empty non-nil slice", got, err)
	}
}

func TestGitHubJobLog(t *testing.T) {
	c := checksServer(t, nil, nil)
	log, err := c.JobLog(context.Background(), 9)
	if err != nil || log != "line 1\nline 2\n" {
		t.Errorf("JobLog(9) = %q, %v", log, err)
	}
	if _, err := c.JobLog(context.Background(), 10); !errors.Is(err, ErrNoJobLog) {
		t.Errorf("JobLog(10) err = %v, want ErrNoJobLog", err)
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Error(err)
	}
}

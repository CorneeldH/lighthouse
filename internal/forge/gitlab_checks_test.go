package forge

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestGitLabChecksBuckets(t *testing.T) {
	srv, g := glClient(t)
	srv.AddMR(map[string]any{"iid": 3, "state": "opened"})
	srv.AddMRPipeline(3, 40) // an older pipeline: its jobs must not show
	srv.AddJob(40, map[string]any{"id": 1, "name": "old", "status": "failed"})
	srv.AddMRPipeline(3, 41)
	rows := []struct {
		status string
		allow  bool
		bucket string
	}{
		{"success", false, "pass"}, {"failed", true, "pass"}, {"failed", false, "fail"},
		{"created", false, "pending"}, {"waiting_for_resource", false, "pending"}, {"preparing", false, "pending"},
		{"pending", false, "pending"}, {"running", false, "pending"}, {"scheduled", false, "pending"},
		{"skipped", false, "skipping"}, {"manual", false, "skipping"}, {"canceled", false, "cancel"},
	}
	var want []Check
	for i, r := range rows {
		id := 200 + i
		name := "job-" + r.status
		if r.allow {
			name += "-allowed"
		}
		srv.AddJob(41, map[string]any{"id": id, "name": name, "status": r.status, "allow_failure": r.allow, "web_url": "https://gl/j/" + name})
		want = append(want, Check{Name: name, Bucket: r.bucket, Link: "https://gl/j/" + name, Job: int64(id)})
	}
	got, err := g.Checks(context.Background(), 3)
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("Checks = %+v, %v\nwant %+v", got, err, want)
	}
}

func TestGitLabChecksEmptyWithoutPipelines(t *testing.T) {
	srv, g := glClient(t)
	srv.AddMR(map[string]any{"iid": 3, "state": "opened"})
	got, err := g.Checks(context.Background(), 3)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("Checks = %#v, %v; want empty non-nil slice", got, err)
	}
}

func TestGitLabJobLog(t *testing.T) {
	srv, g := glClient(t)
	srv.SetTrace(7, "boom\n")
	if log, err := g.JobLog(context.Background(), 7); err != nil || log != "boom\n" {
		t.Errorf("JobLog(7) = %q, %v", log, err)
	}
	if _, err := g.JobLog(context.Background(), 8); !errors.Is(err, ErrNoJobLog) {
		t.Errorf("JobLog(8) err = %v, want ErrNoJobLog", err)
	}
}

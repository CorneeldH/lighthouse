package archscan

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
)

// Classification: io, as check_test.go. The regression surface is what Finish posts: a new
// scan issue, or one comment and one body update of the open one, and nothing at all on the
// routes that must not post; and that the result file is gone after every route.

func bindLabels(t *testing.T) *Job {
	t.Helper()
	j, err := scheduled.Bind(New(), repoconfig.Schedule{Name: "scan", Job: Name, Params: map[string]any{"labels": []any{"2-area:core"}}})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	return j.(*Job)
}

func finish(t *testing.T, r *rig) error {
	t.Helper()
	return bindLabels(t).Finish(context.Background(), r.scanEnv(), r.repo, scheduled.Verdict{RunID: testRun})
}

func labelsOf(it map[string]any) []string {
	var out []string
	ls, _ := it["labels"].([]any)
	for _, l := range ls {
		if m, ok := l.(map[string]any); ok {
			out = append(out, fmt.Sprint(m["name"]))
		} else {
			out = append(out, fmt.Sprint(l))
		}
	}
	return out
}

func commentsOn(t *testing.T, r *rig, n int) []string {
	t.Helper()
	cs, err := r.env().Forge.Comments(context.Background(), n)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range cs {
		out = append(out, c.Body)
	}
	return out
}

func liveBody(t *testing.T, r *rig, n int) string {
	t.Helper()
	is, err := r.env().Forge.Issue(context.Background(), n)
	if err != nil {
		t.Fatal(err)
	}
	return is.Body
}

// postPaths are "METHOD path" of every write the forge got.
func postPaths(r *rig) []string {
	var out []string
	for _, p := range r.srv.Posts {
		out = append(out, p.Method+" "+p.Path)
	}
	return out
}

func TestFinishCreatesIssue(t *testing.T) {
	// Fails if Finish without an open scan issue creates none, uses the key needs_decision or
	// another label than labels.needs_decision of the repo, drops params.labels, or writes
	// another marker than scans 1 with this run, the base and today, or a warning on a first scan.
	r := scanRig(t)
	head := gitIn(t, r.repo, nil, "rev-parse", "HEAD")
	doc := resultDoc(candidate("A", "a", false), "")
	r.writeResult(doc)
	if err := finish(t, r); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if got := postPaths(r); !slices.Equal(got, []string{"POST /repos/o/r/issues"}) {
		t.Fatalf("writes = %v, want one issue create", got)
	}
	var created map[string]any
	for _, it := range r.srv.Issues {
		created = it
	}
	if got := labelsOf(created); !slices.Equal(got, []string{"3-needs-decision", "2-area:core"}) {
		t.Fatalf("labels = %v, want [3-needs-decision 2-area:core]", got)
	}
	body := fmt.Sprint(created["body"])
	m, ok := scheduled.ParseMarker(body)
	want := scheduled.Marker{Kind: Name, Version: 1, Run: testRun, Base: head, First: "2026-11-22", Scans: 1}
	if !ok || m != want {
		t.Fatalf("marker = %+v (%v), want %+v", m, ok, want)
	}
	lines := strings.SplitN(body, "\n", 3)
	if lines[1] != "" || !strings.HasSuffix(body, doc) {
		t.Fatalf("body = %q, want an empty line 2 and the result below the header", body)
	}
	if title := fmt.Sprint(created["title"]); !strings.Contains(title, "Kandidaten") { // lighthouse: allow-language
		t.Fatalf("title = %q, want the heading scan_candidates in the repo language", title)
	}
	if resultExists(t, r) {
		t.Fatal("result file still there")
	}
}

func TestFinishUpdatesWithComment(t *testing.T) {
	// Fails if Finish on an open scan issue opens a second one, posts no comment with the old
	// body, writes the body before the comment, does not raise scans or keep first, or puts the
	// warning anywhere but on line 2 or not from the headings.
	r := scanRig(t)
	old := r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduled.Marker{Run: "scan-20261001T0600", Base: "abc", First: "2026-08-01", Scans: 1},
		resultDoc(candidate("A", "a", false), ""))
	doc := resultDoc(candidate("B", "b", false), "")
	r.writeResult(doc)
	if err := finish(t, r); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if got := postPaths(r); !slices.Equal(got, []string{"POST /repos/o/r/issues/7/comments", "PATCH /repos/o/r/issues/7"}) {
		t.Fatalf("writes = %v, want the comment, then the body", got)
	}
	cs := commentsOn(t, r, 7)
	if len(cs) != 1 || cs[0] != "Scan van 2026-10-01\n\n"+old { // lighthouse: allow-language
		t.Fatalf("comments = %q, want one with the date of the old scan and the old body", cs)
	}
	body := liveBody(t, r, 7)
	m, _ := scheduled.ParseMarker(body)
	if m.Scans != 2 || m.First != "2026-08-01" || m.Run != testRun {
		t.Fatalf("marker = %+v, want scans 2, first 2026-08-01, run %s", m, testRun)
	}
	lines := strings.SplitN(body, "\n", 3)
	if lines[1] != "Onbeslist sinds 2026-08-01, 2 scans" { // lighthouse: allow-language
		t.Fatalf("line 2 = %q", lines[1])
	}
	if !strings.HasSuffix(body, doc) {
		t.Fatalf("body = %q, want the new result", body)
	}
	if len(r.srv.Issues) != 1 {
		t.Fatalf("%d issues, want 1", len(r.srv.Issues))
	}
}

func TestFinishRepeatedPostsOnce(t *testing.T) {
	// Fails if a repeated Finish of the same run posts again: a second issue, or a second
	// comment and body update.
	t.Run("update", func(t *testing.T) {
		r := scanRig(t)
		r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduled.Marker{Run: "scan-20261001T0600", Base: "abc", First: "2026-08-01", Scans: 1}, "x")
		for range 2 {
			r.writeResult(resultDoc(candidate("B", "b", false), ""))
			if err := finish(t, r); err != nil {
				t.Fatalf("Finish: %v", err)
			}
			if resultExists(t, r) {
				t.Fatal("result file still there")
			}
		}
		if len(r.srv.Issues) != 1 || len(commentsOn(t, r, 7)) != 1 || len(r.srv.Posts) != 2 {
			t.Fatalf("%d issues, %d comments, writes %v; want 1, 1 and two writes", len(r.srv.Issues), len(commentsOn(t, r, 7)), postPaths(r))
		}
	})
	t.Run("result already removed", func(t *testing.T) {
		// A repeated Postcheck after Finish published: the result file is gone, and that is fine.
		r := scanRig(t)
		r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduled.Marker{Run: "scan-20261001T0600", Base: "abc", First: "2026-08-01", Scans: 1}, "x")
		r.writeResult(resultDoc(candidate("B", "b", false), ""))
		for range 2 {
			if err := finish(t, r); err != nil {
				t.Fatalf("Finish: %v", err)
			}
		}
		if len(commentsOn(t, r, 7)) != 1 || len(r.srv.Posts) != 2 {
			t.Fatalf("writes %v, want one comment and one body update", postPaths(r))
		}
	})
	t.Run("create", func(t *testing.T) {
		r := scanRig(t)
		for range 2 {
			r.writeResult(resultDoc(candidate("B", "b", false), ""))
			if err := finish(t, r); err != nil {
				t.Fatalf("Finish: %v", err)
			}
		}
		if len(r.srv.Issues) != 1 || len(r.srv.Posts) != 1 {
			t.Fatalf("%d issues, writes %v; want one create", len(r.srv.Issues), postPaths(r))
		}
	})
}

func TestFinishKeepsCheckboxSetAfterCheck(t *testing.T) {
	// Fails if Finish writes the result as Check saw it and loses a box the user ticked on the
	// forge between Check and Finish.
	r := scanRig(t)
	open := r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduled.Marker{Run: "scan-20261001T0600", Base: "abc", First: "2026-08-01", Scans: 1},
		resultDoc(candidate("A", "a", false)+candidate("B", "b", false), ""))
	r.writeResult(resultDoc(candidate("A", "a", false)+candidate("B", "b", false), ""))
	checkPasses(t, r)
	env := r.scanEnv()
	tickB := strings.Replace(open, "(`b`)\n\n- [ ] Afgewezen", "(`b`)\n\n- [x] Afgewezen", 1) // lighthouse: allow-language
	if err := env.Forge.UpdateIssueBody(context.Background(), 7, tickB); err != nil {
		t.Fatal(err)
	}
	if err := finish(t, r); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	body := liveBody(t, r, 7)
	if !strings.Contains(body, "(`b`)\n\n- [x] Afgewezen") || !strings.Contains(body, "(`a`)\n\n- [ ] Afgewezen") { // lighthouse: allow-language
		t.Fatalf("body = %q, want b ticked and a open", body)
	}
	if m, _ := scheduled.ParseMarker(body); m.Run != testRun {
		t.Fatalf("body not updated: %q", body)
	}
}

func TestFinishNoCandidatesNoIssue(t *testing.T) {
	// Fails if Finish opens a scan issue for a scan without candidates.
	r := scanRig(t)
	r.writeResult(resultDoc("", ""))
	if err := finish(t, r); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if len(r.srv.Posts) != 0 || len(r.srv.Issues) != 0 {
		t.Fatalf("writes %v, %d issues; want none", postPaths(r), len(r.srv.Issues))
	}
	if resultExists(t, r) {
		t.Fatal("result file still there")
	}
}

func TestFinishOnlyRejectedCarriedOverPostsNothing(t *testing.T) {
	// Fails if a carried-over rejection counts as a candidate: without an open scan issue it
	// stays in the last closed one and nothing is posted.
	r := scanRig(t)
	r.seedScan(3, "closed", "2026-05-01T05:00:00Z", "2026-10-01T05:00:00Z", scheduled.Marker{Run: "scan-20260501T0500", Base: "abc", First: "2026-05-01", Scans: 1},
		resultDoc(candidate("A", "a", true), ""))
	r.writeResult(resultDoc(candidate("A", "a", true), ""))
	if err := finish(t, r); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if len(r.srv.Posts) != 0 {
		t.Fatalf("writes %v, want none", postPaths(r))
	}
}

func TestFinishAlwaysRemovesResultFile(t *testing.T) {
	// Fails if any route of Finish, the early exits and the errors included, leaves the result
	// file: Postcheck would then commit and merge it.
	for name, tc := range map[string]struct {
		setup   func(r *rig)
		job     func(t *testing.T) *Job
		noRun   bool
		wantErr bool
	}{
		"unbound":   {job: func(*testing.T) *Job { return New().(*Job) }, wantErr: true},
		"no forge":  {setup: func(r *rig) { r.noForge = true }, wantErr: true},
		"no run id": {noRun: true, wantErr: true},
		"already published": {setup: func(r *rig) {
			r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduled.Marker{Run: testRun, Base: "abc", First: "2026-08-01", Scans: 1}, "x")
		}},
		"nothing to post": {setup: func(r *rig) { r.writeResult(resultDoc("", "")) }},
		"create":          {},
		"update": {setup: func(r *rig) {
			r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduled.Marker{Run: "scan-20261001T0600", Base: "abc", First: "2026-08-01", Scans: 1}, "x")
		}},
		"invalid result": {setup: func(r *rig) { r.writeResult("## Kandidaten\n") }, wantErr: true}, // lighthouse: allow-language
		"forge error": {setup: func(r *rig) {
			r.srv.Fail = map[string]int{"POST /repos/o/r/issues": 500}
		}, wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			r := scanRig(t)
			r.writeResult(resultDoc(candidate("A", "a", false), ""))
			if tc.setup != nil {
				tc.setup(r)
			}
			j := bindLabels(t)
			if tc.job != nil {
				j = tc.job(t)
			}
			v := scheduled.Verdict{RunID: testRun}
			if tc.noRun {
				v = scheduled.Verdict{}
			}
			err := j.Finish(context.Background(), r.scanEnv(), r.repo, v)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Finish = %v, want error %v", err, tc.wantErr)
			}
			if resultExists(t, r) {
				t.Fatal("result file still there")
			}
		})
	}
}

func TestFinishCommentErrorFailsRunWithoutPost(t *testing.T) {
	// Fails if a failed comment does not fail Finish, or the body is still rewritten.
	r := scanRig(t)
	old := r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduled.Marker{Run: "scan-20261001T0600", Base: "abc", First: "2026-08-01", Scans: 1}, "x")
	r.srv.Fail = map[string]int{"POST /repos/o/r/issues/7/comments": 500}
	r.writeResult(resultDoc(candidate("B", "b", false), ""))
	if err := finish(t, r); err == nil {
		t.Fatal("Finish = nil after a failed comment")
	}
	if got := liveBody(t, r, 7); got != old {
		t.Fatalf("body changed after a failed comment: %q", got)
	}
	if n := len(commentsOn(t, r, 7)); n != 0 || len(r.srv.Issues) != 1 {
		t.Fatalf("%d comments, %d issues; want 0 and 1", n, len(r.srv.Issues))
	}
}

func TestFinishBodyErrorFailsRunWithOneComment(t *testing.T) {
	// Fails if a failed body update does not fail Finish, or leaves another number of comments
	// than one.
	r := scanRig(t)
	old := r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduled.Marker{Run: "scan-20261001T0600", Base: "abc", First: "2026-08-01", Scans: 1}, "x")
	r.srv.Fail = map[string]int{"PATCH /repos/o/r/issues/7": 500}
	r.writeResult(resultDoc(candidate("B", "b", false), ""))
	if err := finish(t, r); err == nil {
		t.Fatal("Finish = nil after a failed body update")
	}
	if got := liveBody(t, r, 7); got != old {
		t.Fatalf("body changed: %q", got)
	}
	if n := len(commentsOn(t, r, 7)); n != 1 {
		t.Fatalf("%d comments, want 1", n)
	}
}

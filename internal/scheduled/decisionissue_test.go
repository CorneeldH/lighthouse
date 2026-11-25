package scheduled

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
)

const (
	testKind  = "architecture-scan"
	testLabel = "needs_decision"
	botLogin  = "lh-bot"
)

type diRig struct {
	t   *testing.T
	srv *forgetest.Server
	f   forge.Forge
}

func newDIRig(t *testing.T) *diRig {
	t.Helper()
	srv := forgetest.New(t)
	srv.Login = botLogin
	srv.Issues = map[int]map[string]any{}
	c := forge.New("o/r", "secret-token")
	c.BaseURL = srv.URL()
	return &diRig{t: t, srv: srv, f: c}
}

// seed stores an issue with body, label, state and author.
func (r *diRig) seed(n int, author, state, created, closed, body string, labels ...string) {
	var ls []any
	for _, l := range labels {
		ls = append(ls, map[string]any{"name": l})
	}
	it := map[string]any{"number": n, "title": "t", "state": state, "body": body, "labels": ls,
		"created_at": created, "user": map[string]any{"login": author}}
	if closed != "" {
		it["closed_at"] = closed
	}
	r.srv.Issues[n] = it
}

func (r *diRig) posts(method, suffix string) []forgetest.Post {
	var out []forgetest.Post
	for _, p := range r.srv.Posts {
		if p.Method == method && strings.HasSuffix(p.Path, suffix) {
			out = append(out, p)
		}
	}
	return out
}

func testMarker(run string, scans int) Marker {
	return Marker{Kind: testKind, Version: 1, Run: run, Base: "abc123", First: "2026-11-01", Scans: scans}
}

func warn(first string, scans int) string {
	return fmt.Sprintf("WARNING since %s, scan %d", first, scans)
}

func basePublish(run, body string) Publish {
	return Publish{
		Kind: testKind, Title: "Architecture scan", Label: testLabel, ExtraLabels: []string{"architecture"},
		Warning: warn, Run: run, Base: "def456", Now: time.Date(2026, 11, 25, 5, 0, 0, 0, time.UTC),
		Body: body, MaxBody: 60000, CommentTitle: "Previous scan",
	}
}

// existing seeds an open scanissue 7 whose body is marker + warning + content.
func (r *diRig) existing(run string, scans int, content string) string {
	body := testMarker(run, scans).String() + "\n" + warn("2026-11-01", scans) + "\n\n" + content
	r.seed(7, botLogin, "open", "2026-11-01T05:00:00Z", "", body, testLabel)
	return body
}

// Classification: pure function. Fails if a marker field is dropped or renamed in the JSON.
func TestMarkerRoundTrip(t *testing.T) {
	m := testMarker("20261125T050000Z", 3)
	line := m.String()
	want := `<!-- lighthouse: {"kind":"architecture-scan","version":1,"run":"20261125T050000Z","base":"abc123","first":"2026-11-01","scans":3} -->`
	if line != want {
		t.Fatalf("String = %q, want %q", line, want)
	}
	got, ok := ParseMarker(line + "\nrest")
	if !ok || got != m {
		t.Fatalf("ParseMarker = %+v, %v; want %+v", got, ok, m)
	}
	if _, ok := ParseMarker(strings.Replace(line+"\n", "\n", "\r\n", 1)); !ok {
		t.Error("a CRLF line ending must parse")
	}
}

// Classification: pure function. Fails if the parser looks beyond line 1.
func TestParseMarkerOnlyLineOne(t *testing.T) {
	line := testMarker("r", 1).String()
	for name, body := range map[string]string{
		"line two": "intro\n" + line,
		"empty":    "",
		"no json":  "<!-- lighthouse: nope -->",
		"other":    "<!-- other: {} -->",
	} {
		if _, ok := ParseMarker(body); ok {
			t.Errorf("%s: parsed a marker from %q", name, body)
		}
	}
}

// Classification: forge interaction. Fails if the author, marker or label check is dropped.
func TestFindScanIssueIgnoresForeign(t *testing.T) {
	r := newDIRig(t)
	good := testMarker("a", 1).String() + "\nw"
	r.seed(1, "someone-else", "open", "2026-11-01T05:00:00Z", "", good, testLabel)
	r.seed(2, botLogin, "open", "2026-11-01T05:00:00Z", "", "no marker here", testLabel)
	r.seed(3, botLogin, "open", "2026-11-01T05:00:00Z", "", good, "other-label")
	r.seed(4, botLogin, "open", "2026-11-01T05:00:00Z", "", testMarker("a", 1).String()[:10]+"\n"+good, testLabel)
	r.seed(5, botLogin, "open", "2026-11-01T05:00:00Z", "", strings.Replace(good, testKind, "other-kind", 1), testLabel)
	got, err := FindDecisionIssue(context.Background(), r.f, testKind, testLabel, "open")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want none", got, err)
	}
	r.seed(6, botLogin, "open", "2026-11-01T05:00:00Z", "", good, testLabel)
	got, err = FindDecisionIssue(context.Background(), r.f, testKind, testLabel, "open")
	if err != nil || len(got) != 1 || got[0].Number != 6 {
		t.Fatalf("got %v, %v; want issue 6", got, err)
	}
}

// Classification: forge interaction. Fails if the order is by number or newest first, or if the
// state is ignored.
func TestFindDecisionIssueOldestFirst(t *testing.T) {
	r := newDIRig(t)
	b := testMarker("a", 1).String() + "\nw"
	r.seed(5, botLogin, "open", "2026-11-01T05:00:00Z", "", b, testLabel)
	r.seed(3, botLogin, "open", "2026-11-10T05:00:00Z", "", b, testLabel)
	r.seed(8, botLogin, "closed", "2026-10-01T05:00:00Z", "2026-10-05T05:00:00Z", b, testLabel)
	r.seed(9, botLogin, "closed", "2026-10-02T05:00:00Z", "2026-10-20T05:00:00Z", b, testLabel)
	open, err := FindDecisionIssue(context.Background(), r.f, testKind, testLabel, "open")
	if err != nil {
		t.Fatal(err)
	}
	var nums []int
	for _, i := range open {
		nums = append(nums, i.Number)
	}
	if !slices.Equal(nums, []int{5, 3}) {
		t.Errorf("open = %v, want [5 3] (oldest first)", nums)
	}
	closed, err := FindDecisionIssue(context.Background(), r.f, testKind, testLabel, "closed")
	if err != nil || len(closed) != 2 {
		t.Fatalf("closed = %v, %v", closed, err)
	}
	if closed[0].Number != 8 || !closed[1].ClosedAt.After(closed[0].ClosedAt) {
		t.Errorf("closed = %v; want both closed issues with ClosedAt filled", closed)
	}
	if _, err := FindDecisionIssue(context.Background(), r.f, testKind, testLabel, "all"); err == nil {
		t.Error("state all must be refused")
	}
}

// Classification: forge write. Fails if the labels, marker or warning line are not in the created issue.
func TestPublishCreatesIssueWithLabels(t *testing.T) {
	r := newDIRig(t)
	r.srv.Labels = []string{testLabel, "architecture"}
	iss, err := PublishDecisionIssue(context.Background(), r.f, basePublish("run1", "content"))
	if err != nil {
		t.Fatal(err)
	}
	ps := r.posts("POST", "/repos/o/r/issues")
	if len(ps) != 1 {
		t.Fatalf("created %d issues, want 1", len(ps))
	}
	body := fmt.Sprint(ps[0].Body["body"])
	lines := strings.Split(body, "\n")
	m, ok := ParseMarker(body)
	want := Marker{Kind: testKind, Version: 1, Run: "run1", Base: "def456", First: "2026-11-25", Scans: 1}
	if !ok || m != want {
		t.Errorf("marker = %+v, %v; want %+v", m, ok, want)
	}
	if lines[1] != warn("2026-11-25", 1) || !strings.HasSuffix(body, "content") {
		t.Errorf("body = %q", body)
	}
	got := fmt.Sprint(ps[0].Body["labels"])
	if !strings.Contains(got, testLabel) || !strings.Contains(got, "architecture") || ps[0].Body["title"] != "Architecture scan" {
		t.Errorf("post = %v", ps[0].Body)
	}
	if iss.Number == 0 {
		t.Error("created issue not returned")
	}
}

// Classification: forge write. Fails if a repeat of the same run posts a comment or edits the body.
func TestPublishSameRunPostsNothing(t *testing.T) {
	r := newDIRig(t)
	r.existing("run1", 1, "old")
	iss, err := PublishDecisionIssue(context.Background(), r.f, basePublish("run1", "new"))
	if err != nil {
		t.Fatal(err)
	}
	if iss.Number != 7 {
		t.Errorf("returned issue %d, want 7", iss.Number)
	}
	if len(r.srv.Posts) != 0 {
		t.Errorf("posts = %v, want none", r.srv.Posts)
	}
}

// Classification: forge write. Fails if the comment follows the body update, if scans is not
// incremented, if first changes or if line 2 is not the new warning.
func TestPublishUpdateCommentBeforeBody(t *testing.T) {
	r := newDIRig(t)
	old := r.existing("run1", 1, "old content")
	if _, err := PublishDecisionIssue(context.Background(), r.f, basePublish("run2", "new content")); err != nil {
		t.Fatal(err)
	}
	var seq []string
	for _, p := range r.srv.Posts {
		seq = append(seq, p.Method+" "+p.Path)
	}
	if want := []string{"POST /repos/o/r/issues/7/comments", "PATCH /repos/o/r/issues/7"}; !slices.Equal(seq, want) {
		t.Fatalf("writes = %v, want %v", seq, want)
	}
	comment := fmt.Sprint(r.srv.Posts[0].Body["body"])
	if !strings.Contains(comment, old) || !strings.Contains(comment, "Previous scan") {
		t.Errorf("comment = %q, want the old body and the title", comment)
	}
	body := fmt.Sprint(r.srv.Posts[1].Body["body"])
	m, ok := ParseMarker(body)
	if !ok || m.Scans != 2 || m.First != "2026-11-01" || m.Run != "run2" || m.Base != "def456" {
		t.Errorf("marker = %+v, %v", m, ok)
	}
	if got := strings.Split(body, "\n")[1]; got != warn("2026-11-01", 2) {
		t.Errorf("line 2 = %q", got)
	}
	if !strings.HasSuffix(body, "new content") || strings.Contains(body, "old content") {
		t.Errorf("body = %q", body)
	}
}

// Classification: forge write. Fails if Merge gets the stored body instead of the live one, or
// if its result is not what is written.
func TestPublishMergeAppliedToLiveBody(t *testing.T) {
	r := newDIRig(t)
	r.existing("run1", 1, "edited by a human")
	var seen string
	p := basePublish("run2", "unused")
	p.Merge = func(live string) string { seen = live; return "merged: " + live }
	if _, err := PublishDecisionIssue(context.Background(), r.f, p); err != nil {
		t.Fatal(err)
	}
	if seen != "edited by a human" {
		t.Errorf("Merge saw %q, want the live content without the header", seen)
	}
	if body := fmt.Sprint(r.srv.Posts[1].Body["body"]); !strings.HasSuffix(body, "merged: edited by a human") {
		t.Errorf("body = %q", body)
	}
}

// Classification: forge write. Fails if the length check is dropped or runs after a write.
func TestPublishBodyTooLong(t *testing.T) {
	for name, existing := range map[string]bool{"update": true, "create": false} {
		t.Run(name, func(t *testing.T) {
			r := newDIRig(t)
			if existing {
				r.existing("run1", 1, "old")
			}
			p := basePublish("run2", strings.Repeat("x", 500))
			p.MaxBody = 300
			if _, err := PublishDecisionIssue(context.Background(), r.f, p); err == nil {
				t.Fatal("want an error")
			}
			if len(r.srv.Posts) != 0 {
				t.Errorf("writes = %v, want none", r.srv.Posts)
			}
		})
	}
}

// Classification: forge write. Fails if the body is updated although the comment failed.
func TestPublishCommentFailsLeavesBody(t *testing.T) {
	r := newDIRig(t)
	r.existing("run1", 1, "old")
	r.srv.Fail = map[string]int{"POST /repos/o/r/issues/7/comments": 500}
	if _, err := PublishDecisionIssue(context.Background(), r.f, basePublish("run2", "new")); err == nil {
		t.Fatal("want an error")
	}
	if got := r.posts("PATCH", "/issues/7"); len(got) != 0 {
		t.Errorf("body updated after a failed comment: %v", got)
	}
}

// Classification: forge write. Fails if a failing body update is swallowed or the comment repeated.
func TestPublishBodyUpdateFailsAfterComment(t *testing.T) {
	r := newDIRig(t)
	r.existing("run1", 1, "old")
	r.srv.Fail = map[string]int{"PATCH /repos/o/r/issues/7": 500}
	_, err := PublishDecisionIssue(context.Background(), r.f, basePublish("run2", "new"))
	if err == nil {
		t.Fatal("want an error")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected %v", err)
	}
	if n := len(r.posts("POST", "/issues/7/comments")); n != 1 {
		t.Errorf("%d comments, want exactly 1", n)
	}
}

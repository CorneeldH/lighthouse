package forge_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/gitlabtest"
)

func TestOpenGitLabBaseURL(t *testing.T) {
	for _, tc := range []struct{ name, host, baseURL, want string }{
		{"no host is gitlab.com", "", "", "https://gitlab.com/api/v4"},
		{"self-hosted host", "git.example.nl", "", "https://git.example.nl/api/v4"},
		{"trailing slash dropped", "", "http://x/api/v4/", "http://x/api/v4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := forge.Open(forge.Config{Kind: "gitlab", Repo: "g/p", Token: "t", Host: tc.host, BaseURL: tc.baseURL})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			gl, ok := f.(*forge.GitLab)
			if !ok || gl.Kind() != "gitlab" || gl.BaseURL != tc.want {
				t.Errorf("Open = %T %+v; want a *GitLab of kind gitlab with BaseURL %s", f, f, tc.want)
			}
		})
	}
}

// gitlabOn opens the client on a fresh fake whose project is path.
func gitlabOn(t *testing.T, path string) (*gitlabtest.Server, forge.Forge) {
	t.Helper()
	srv := gitlabtest.New(t)
	srv.Login = "lh-bot"
	srv.Project = path
	f, err := forge.Open(forge.Config{Kind: "gitlab", Repo: path, Token: "secret-token", BaseURL: srv.URL()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return srv, f
}

func TestGitLabEncodesSubgroupPath(t *testing.T) {
	srv, f := gitlabOn(t, "g/sub/p")
	srv.AddIssue(map[string]any{"iid": 7, "state": "opened", "title": "Seven"})
	if _, err := f.Issue(t.Context(), 7); err != nil {
		t.Fatalf("Issue(7): %v", err)
	}
	if _, err := f.Comments(t.Context(), 7); err != nil {
		t.Fatalf("Comments(7): %v", err)
	}
	for _, r := range srv.Requests() {
		if strings.Contains(r, "/projects/g/sub/p") || !strings.Contains(r, "/projects/g%2Fsub%2Fp/") {
			t.Errorf("request %q: want the project path as g%%2Fsub%%2Fp", r)
		}
	}
}

func TestGitLabAsksForTheLargestPage(t *testing.T) {
	srv, f := gitlabOn(t, "g/p")
	if _, err := f.OpenIssues(t.Context()); err != nil {
		t.Fatalf("OpenIssues: %v", err)
	}
	m := forge.Milestone{Number: 3, Title: "M 3", State: "open"}
	since := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if _, err := f.ClosedInMilestone(t.Context(), m, since); err != nil {
		t.Fatalf("ClosedInMilestone: %v", err)
	}
	want := []string{
		"GET /api/v4/projects/g%2Fp/issues?per_page=100&state=opened",
		"GET /api/v4/projects/g%2Fp/issues?milestone=M+3&per_page=100&state=closed&updated_after=2026-11-01T00%3A00%3A00Z",
	}
	if got := srv.Requests(); !slices.Equal(got, want) {
		t.Errorf("requests = %q; want %q", got, want)
	}
}

func TestGitLabSendsTheTokenInPrivateToken(t *testing.T) {
	// The fake answers 401 to a request without PRIVATE-TOKEN, so a client that sent
	// Authorization instead would fail here.
	_, f := gitlabOn(t, "g/p")
	got, err := f.Login(t.Context())
	if err != nil || got != "lh-bot" {
		t.Errorf("Login = %q, %v; want lh-bot", got, err)
	}
}

func TestGitLabCommentsSkipSystemNotesAndCountRockets(t *testing.T) {
	srv, f := gitlabOn(t, "g/p")
	srv.AddIssue(map[string]any{"iid": 7, "state": "opened", "web_url": "https://gitlab.example/g/p/-/issues/7"})
	srv.AddNote(7, "ann", "added label", true)
	id := srv.AddNote(7, "ann", "real", false)
	srv.AddAward(7, id, "rocket", "bob")
	srv.AddAward(7, id, "thumbsup", "cat")
	srv.AddAward(7, id, "rocket", "dan")
	got, err := f.Comments(t.Context(), 7)
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(got) != 1 || got[0].ID != id || got[0].Rockets != 2 || got[0].Author != "ann" || got[0].Body != "real" {
		t.Errorf("Comments = %+v; want one comment %d by ann with 2 rockets", got, id)
	}
	// The notes list has no URL: the client builds it from the API root, which is <web root>/api/v4.
	web := strings.TrimSuffix(srv.URL(), "/api/v4")
	if want := fmt.Sprintf("%s/g/p/-/issues/7#note_%d", web, id); got[0].URL != want {
		t.Errorf("URL = %q; want %q", got[0].URL, want)
	}
}

func TestGitLabRoleMapsAccessLevelsAndCachesTheUserID(t *testing.T) {
	srv, f := gitlabOn(t, "g/p")
	for user, level := range map[string]int{"guest": 10, "planner": 15, "reporter": 20, "dev": 30, "maint": 40, "owner": 50} {
		srv.SetMember(user, level)
	}
	want := map[string]string{"guest": "read", "planner": "triage", "reporter": "triage", "dev": "write", "maint": "maintain", "owner": "admin"}
	for user, role := range want {
		if got, err := f.Role(t.Context(), user); err != nil || got != role {
			t.Errorf("Role(%s) = %q, %v; want %s", user, got, err, role)
		}
	}
	before := len(srv.Requests())
	if _, err := f.Role(t.Context(), "dev"); err != nil {
		t.Fatal(err)
	}
	reqs := srv.Requests()[before:]
	if len(reqs) != 1 || !strings.Contains(reqs[0], "/members/all/") {
		t.Errorf("second Role(dev) requests = %q; want only the membership call, the user ID cached", reqs)
	}
}

func TestGitLabStatesAndMilestone(t *testing.T) {
	srv, f := gitlabOn(t, "g/p")
	srv.AddIssue(map[string]any{
		"iid": 7, "id": 9007, "state": "closed", "title": "T", "description": "D", "user_notes_count": 4,
		"web_url": "https://x/7", "author": map[string]any{"username": "ann"}, "labels": []string{"a"},
		"updated_at": "2026-11-02T10:00:00Z", "closed_at": "2026-11-01T10:00:00Z",
		"milestone": map[string]any{"id": 55, "iid": 2, "title": "M", "state": "closed", "description": "md"},
	})
	got, err := f.Issue(t.Context(), 7)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if got.State != "closed" || got.Comments != 4 || got.URL != "https://x/7" || got.Author != "ann" ||
		got.UpdatedAt.IsZero() || got.ClosedAt.IsZero() ||
		got.Milestone == nil || *got.Milestone != (forge.Milestone{Number: 55, Title: "M", State: "closed", Description: "md"}) {
		t.Errorf("Issue = %+v; want state closed, 4 comments, the milestone with the global id 55", got)
	}
}

func TestGitLabUnconfigured(t *testing.T) {
	f, err := forge.Open(forge.Config{Kind: "gitlab", Repo: "g/p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Login(t.Context()); err == nil || err.Error() != forge.ErrNotConfigured.Error() {
		t.Errorf("Login without a token = %v; want ErrNotConfigured", err)
	}
}

func TestGitLabTokenDoesNotFollowARedirectToAnotherHost(t *testing.T) {
	var leaked []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = append(leaked, r.Header.Get("PRIVATE-TOKEN"))
		_, _ = w.Write([]byte(`{"username":"evil"}`))
	}))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	}))
	defer origin.Close()
	f, err := forge.Open(forge.Config{Kind: "gitlab", Repo: "g/p", Token: "secret-token", BaseURL: origin.URL + "/api/v4"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.Login(t.Context()); err == nil {
		t.Errorf("Login = %q, nil; want an error for the redirect to another host", got)
	}
	if len(leaked) != 0 {
		t.Errorf("the other host received requests with tokens %q; want none", leaked)
	}
}

func TestGitLabCommentsCountRocketsOnEveryUserNote(t *testing.T) {
	srv, f := gitlabOn(t, "g/p")
	srv.AddIssue(map[string]any{"iid": 7, "state": "opened"})
	sys := srv.AddNote(7, "ann", "added label", true)
	plain := srv.AddNote(7, "ann", "my answer, no header", false)
	header := srv.AddNote(7, "lh-bot", "<!-- lighthouse:handoff -->\nstate", false)
	srv.AddAward(7, plain, "rocket", "bob")
	srv.AddAward(7, header, "rocket", "bob")
	srv.AddAward(7, header, "rocket", "cat")
	srv.AddAward(7, sys, "rocket", "bob")
	got, err := f.Comments(t.Context(), 7)
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	rockets := map[int64]int{}
	for _, c := range got {
		rockets[c.ID] = c.Rockets
	}
	if len(got) != 2 || rockets[plain] != 1 || rockets[header] != 2 {
		t.Errorf("rockets = %v; want the plain note on 1 and the lighthouse note on 2, and no system note", rockets)
	}
	path := fmt.Sprintf("/notes/%d/award_emoji", sys)
	for _, r := range srv.Requests() {
		if strings.Contains(r, path) {
			t.Errorf("request %q: a system note must cost no award_emoji call", r)
		}
	}
}

func TestGitLabAcknowledgeTwiceAndAcknowledgers(t *testing.T) {
	srv, f := gitlabOn(t, "g/p")
	srv.AddIssue(map[string]any{"iid": 7, "state": "opened"})
	id := srv.AddNote(7, "ann", "look", false)
	srv.AddAward(7, id, "thumbsup", "dan")
	srv.AddAward(7, id, "rocket", "bob")
	for range 2 {
		if err := f.Acknowledge(t.Context(), 7, id); err != nil {
			t.Fatalf("Acknowledge: %v", err)
		}
	}
	got, err := f.Acknowledgers(t.Context(), 7, id)
	if err != nil || !slices.Equal(got, []string{"bob", "lh-bot"}) {
		t.Errorf("Acknowledgers = %v, %v; want [bob lh-bot]: only rockets, one per user", got, err)
	}
	if !slices.Contains(srv.Requests(), fmt.Sprintf("POST /api/v4/projects/g%%2Fp/issues/7/notes/%d/award_emoji?name=rocket", id)) {
		t.Errorf("requests = %q; want the award POSTed with name=rocket", srv.Requests())
	}
}

func TestGitLabAcknowledgeIgnoresAnExistingAwardButNotOtherErrors(t *testing.T) {
	for _, tc := range []struct {
		code    int
		wantErr bool
	}{{http.StatusConflict, false}, {http.StatusNotFound, false}, {http.StatusForbidden, true}} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.code)
		}))
		f, err := forge.Open(forge.Config{Kind: "gitlab", Repo: "g/p", Token: "t", BaseURL: ts.URL})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Acknowledge(t.Context(), 7, 5); (err != nil) != tc.wantErr {
			t.Errorf("Acknowledge on %d: err = %v; want error %v", tc.code, err, tc.wantErr)
		}
		ts.Close()
	}
}

func TestGitLabBlockedByReadsBlockingLinksAndMergedPRs(t *testing.T) {
	srv, f := gitlabOn(t, "g/p")
	srv.AddIssue(map[string]any{"iid": 7, "state": "opened"})
	link := func(iid int, state, typ, full string) {
		srv.AddLink(7, map[string]any{"iid": iid, "state": state, "link_type": typ, "references": map[string]any{"full": full}})
	}
	link(1, "opened", "is_blocked_by", "g/p#1")
	link(2, "closed", "is_blocked_by", "g/p#2")
	link(3, "closed", "is_blocked_by", "o/sub/other#3")
	link(4, "opened", "blocks", "g/p#4")
	link(5, "opened", "relates_to", "g/p#5")
	srv.AddClosedBy("g/p", 2, map[string]any{"iid": 20, "state": "closed"})
	srv.AddClosedBy("g/p", 2, map[string]any{"iid": 21, "state": "merged"})
	srv.AddClosedBy("o/sub/other", 3, map[string]any{"iid": 30, "state": "closed"})
	got, err := f.BlockedBy(t.Context(), 7)
	if err != nil {
		t.Fatalf("BlockedBy: %v", err)
	}
	want := []forge.Blocker{
		{Repo: "g/p", Issue: 1, State: "OPEN"},
		{Repo: "g/p", Issue: 2, State: "CLOSED", MergedPR: true},
		{Repo: "o/sub/other", Issue: 3, State: "CLOSED"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("BlockedBy = %+v; want %+v", got, want)
	}
	for _, r := range srv.Requests() {
		if strings.Contains(r, "/issues/1/closed_by") {
			t.Errorf("request %q: an open blocker has no closing MR to look up", r)
		}
	}
}

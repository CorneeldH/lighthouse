package forgetest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
)

// The REST side of the server, driven by the real client the dispatcher tests use.

func client(srv *forgetest.Server) *forge.Client {
	c := forge.New("o/r", "secret-token")
	c.BaseURL = srv.URL()
	return c
}

func TestLoginAndComments(t *testing.T) {
	srv := forgetest.New(t)
	srv.Login = "lh-bot"
	srv.Comments = map[int][]map[string]any{7: {
		{"id": 1, "body": "🤖 Vraag", "created_at": "2026-10-31T10:00:00Z", "html_url": "u1", "user": map[string]any{"login": "owner"}},
		{"id": 2, "body": "ja", "user": nil, "reactions": map[string]any{"rocket": 3}},
	}}
	srv.Rockets = map[int64][]string{1: {"someone", "lh-bot"}}
	c := client(srv)
	login, err := c.Login(context.Background())
	if err != nil || login != "lh-bot" {
		t.Fatalf("Login = %q, %v; want lh-bot", login, err)
	}
	got, err := c.Comments(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 1 || got[0].Author != "owner" || got[0].Body != "🤖 Vraag" || got[0].URL != "u1" ||
		got[0].Rockets != 2 || got[1].Author != "" || got[1].Rockets != 3 {
		t.Fatalf("Comments = %+v; want owner's with 2 rockets (from Rockets), then a deleted user's with 3 (its own)", got)
	}
	if srv.Count("/user") != 1 || srv.Count("/repos/o/r/issues/7/comments") != 1 {
		t.Fatalf("counts /user %d, comments %d; want 1 and 1", srv.Count("/user"), srv.Count("/repos/o/r/issues/7/comments"))
	}
}

func TestRocketsPageAt100(t *testing.T) {
	srv := forgetest.New(t)
	logins := make([]string, 101)
	for i := range logins {
		logins[i] = fmt.Sprintf("u%d", i)
	}
	srv.Rockets = map[int64][]string{42: logins}
	got, err := client(srv).RocketLogins(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 101 || got[0] != "u0" || got[100] != "u100" {
		t.Fatalf("got %d logins; want 101 from u0 to u100", len(got))
	}
	if n := srv.Count("/repos/o/r/issues/comments/42/reactions"); n != 2 {
		t.Fatalf("reaction requests = %d, want 2 (pages of 100)", n)
	}
}

func TestRoles(t *testing.T) {
	srv := forgetest.New(t)
	srv.Roles = map[string]string{"ann": "maintain"}
	c := client(srv)
	if role, err := c.RoleName(context.Background(), "ann"); err != nil || role != "maintain" {
		t.Fatalf("RoleName(ann) = %q, %v; want maintain", role, err)
	}
	if role, err := c.RoleName(context.Background(), "nobody"); err != nil || role != "" {
		t.Fatalf("RoleName(nobody) = %q, %v; want \"\" and no error (404)", role, err)
	}
	srv.RoleStatus = 502
	if _, err := c.RoleName(context.Background(), "ann"); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("RoleName with RoleStatus 502: err = %v, want a 502 error", err)
	}
	if n := srv.Count("/repos/o/r/collaborators/ann/permission"); n != 2 {
		t.Fatalf("permission requests for ann = %d, want 2", n)
	}
}

func TestPostedCommentsAndRockets(t *testing.T) {
	srv := forgetest.New(t)
	srv.Login = "lh-bot"
	srv.Now = func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.FixedZone("CET", 3600)) }
	srv.Comments = map[int][]map[string]any{7: {{"id": 40, "body": "Hoi", "user": map[string]any{"login": "owner"}}}}
	c := client(srv)
	posted, err := c.CreateComment(context.Background(), 7, "🤖 Gestart")
	if err != nil {
		t.Fatal(err)
	}
	if posted.ID != 41 || posted.Author != "lh-bot" || posted.Body != "🤖 Gestart" || posted.CreatedAt != "2026-10-31T11:00:00Z" ||
		posted.URL != "https://github.com/o/r/issues/7#issuecomment-41" {
		t.Fatalf("CreateComment = %+v; want id 41 by lh-bot at 11:00 UTC", posted)
	}
	if err := c.AddRocket(context.Background(), 40); err != nil {
		t.Fatal(err)
	}
	got, err := c.Comments(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Rockets != 1 || got[1].ID != 41 || got[1].Body != "🤖 Gestart" || got[1].Author != "lh-bot" {
		t.Fatalf("Comments = %+v; want the old one with a rocket, then the posted one", got)
	}
	if logins, err := c.RocketLogins(context.Background(), 40); err != nil || len(logins) != 1 || logins[0] != "lh-bot" {
		t.Fatalf("RocketLogins(40) = %v, %v; want [lh-bot]", logins, err)
	}
	if got := srv.IssueComments(7); len(got) != 2 || got[1]["body"] != "🤖 Gestart" {
		t.Fatalf("IssueComments(7) = %v; want the posted comment last", got)
	}
}

func TestFailStatus(t *testing.T) {
	srv := forgetest.New(t)
	srv.Fail = map[string]int{"POST /repos/o/r/issues/7/comments": 500}
	c := client(srv)
	if _, err := c.CreateComment(context.Background(), 7, "x"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("CreateComment with Fail 500: err = %v, want a 500 error", err)
	}
	if _, err := c.CreateComment(context.Background(), 8, "x"); err != nil {
		t.Fatalf("CreateComment on issue 8 = %v, want no error: Fail names issue 7 only", err)
	}
	if n := len(srv.IssueComments(7)); n != 0 {
		t.Fatalf("issue 7 holds %d comments after a failed post, want 0", n)
	}
}

func TestIssue(t *testing.T) {
	srv := forgetest.New(t)
	srv.Issues = map[int]map[string]any{7: {"number": 7, "title": "Bouw", "html_url": "https://github.com/o/r/issues/7"}}
	get := func(n int) (int, map[string]any) {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/repos/o/r/issues/%d", srv.URL(), n), nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer x")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, body
	}
	if code, body := get(7); code != 200 || body["title"] != "Bouw" {
		t.Fatalf("GET issue 7 = %d %v; want 200 with its title", code, body)
	}
	if code, _ := get(8); code != 404 {
		t.Fatalf("GET issue 8 = %d; want 404", code)
	}
}

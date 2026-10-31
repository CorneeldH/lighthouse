package forgetest_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

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

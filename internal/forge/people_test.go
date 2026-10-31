package forge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// peopleServer serves handler and returns a client pointed at it.
func peopleServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New("o/r", "secret-token")
	c.BaseURL = srv.URL
	return c
}

func TestLogin(t *testing.T) {
	c := peopleServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/user" {
			t.Errorf("request = %s %s, want GET /user", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"login":"lh-bot","id":1}`)
	})
	got, err := c.Login(context.Background())
	if err != nil || got != "lh-bot" {
		t.Fatalf("Login = %q, %v; want lh-bot", got, err)
	}
}

func TestRocketLoginsFollowsPages(t *testing.T) {
	var srvURL string
	c := peopleServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues/comments/42/reactions" || r.URL.Query().Get("content") != "rocket" {
			t.Errorf("request = %s, want rocket reactions of comment 42", r.URL)
		}
		if r.URL.Query().Get("page") == "2" {
			_, _ = io.WriteString(w, `[{"user":{"login":"late"}}]`)
			return
		}
		if r.URL.Query().Get("per_page") != "100" {
			t.Errorf("per_page = %q, want 100", r.URL.Query().Get("per_page"))
		}
		items := make([]string, 100)
		for i := range items {
			items[i] = fmt.Sprintf(`{"user":{"login":"u%d"}}`, i)
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s%s&page=2>; rel="next"`, srvURL, r.URL.RequestURI()))
		_, _ = io.WriteString(w, "["+strings.Join(items, ",")+"]")
	})
	srvURL = c.BaseURL
	got, err := c.RocketLogins(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 101 || got[0] != "u0" || got[100] != "late" {
		t.Fatalf("got %d logins; want 101 from u0 to late", len(got))
	}
}

func TestRoleName(t *testing.T) {
	cases := []struct {
		name, user string
		status     int
		body       string
		want       string
		wantErr    string
	}{
		{"maintain", "alice", 200, `{"permission":"write","role_name":"maintain"}`, "maintain", ""},
		{"stranger with read", "carol", 200, `{"permission":"read","role_name":"read"}`, "read", ""},
		{"unknown login", "nobody", 404, `{"message":"Not Found"}`, "", ""},
		{"server error", "alice", 500, `{"message":"boom"}`, "", "500"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := peopleServer(t, func(w http.ResponseWriter, r *http.Request) {
				if want := "/repos/o/r/collaborators/" + tc.user + "/permission"; r.URL.Path != want {
					t.Errorf("path = %s, want %s", r.URL.Path, want)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			got, err := c.RoleName(context.Background(), tc.user)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want it to contain %s", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("RoleName = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestRoleNameEscapesUser(t *testing.T) {
	c := peopleServer(t, func(w http.ResponseWriter, r *http.Request) {
		if want := "/repos/o/r/collaborators/a%2Fb/permission"; r.URL.EscapedPath() != want {
			t.Errorf("escaped path = %s, want %s", r.URL.EscapedPath(), want)
		}
		_, _ = io.WriteString(w, `{"role_name":"read"}`)
	})
	if _, err := c.RoleName(context.Background(), "a/b"); err != nil {
		t.Fatal(err)
	}
}

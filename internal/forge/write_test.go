package forge

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// writeServer answers every request with status and body after checking the wire contract:
// POST, path, JSON body and the fixed headers.
func writeServer(t *testing.T, wantPath, wantBody string, status int, resp string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != wantPath {
			t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
		}
		raw, _ := io.ReadAll(r.Body)
		if strings.TrimSpace(string(raw)) != wantBody {
			t.Errorf("body = %s, want %s", raw, wantBody)
		}
		for k, v := range map[string]string{
			"Content-Type":         "application/json",
			"Authorization":        "Bearer secret-token",
			"Accept":               "application/vnd.github+json",
			"X-GitHub-Api-Version": "2022-11-28",
		} {
			if got := r.Header.Get(k); got != v {
				t.Errorf("header %s = %q, want %q", k, got, v)
			}
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	c := New("o/r", "secret-token")
	c.BaseURL = srv.URL
	return c
}

func TestCreateCommentPosts(t *testing.T) {
	c := writeServer(t, "/repos/o/r/issues/7/comments", `{"body":"🤖 Vraag"}`, 201,
		`{"id":42,"html_url":"u","created_at":"2026-10-31T10:00:00Z","body":"🤖 Vraag","user":{"login":"lh-bot"}}`)
	got, err := c.CreateComment(context.Background(), 7, "🤖 Vraag")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 42 || got.Author != "lh-bot" || got.URL != "u" ||
		got.CreatedAt != "2026-10-31T10:00:00Z" || got.Body != "🤖 Vraag" {
		t.Fatalf("got %+v", got)
	}
}

func TestAddRocket(t *testing.T) {
	const path = "/repos/o/r/issues/comments/42/reactions"
	for _, status := range []int{200, 201} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c := writeServer(t, path, `{"content":"rocket"}`, status, `{"id":1,"content":"rocket"}`)
			if err := c.AddRocket(context.Background(), 42); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("forbidden", func(t *testing.T) {
		c := writeServer(t, path, `{"content":"rocket"}`, 403, `{"message":"Resource not accessible"}`)
		err := c.AddRocket(context.Background(), 42)
		if err == nil {
			t.Fatal("want error")
		}
		for _, want := range []string{"POST", "403", "Resource not accessible"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q lacks %q", err, want)
			}
		}
		if strings.Contains(err.Error(), "secret-token") {
			t.Errorf("error leaks token: %q", err)
		}
	})
}

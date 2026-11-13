package forge

import (
	"context"
	"encoding/json"
	"errors"
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

// mergeServer answers a PUT merge request with status, header and resp after checking the wire
// contract: method, path, the squash body and the fixed headers.
func mergeServer(t *testing.T, status int, header map[string]string, resp string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/repos/o/r/pulls/12/merge" {
			t.Errorf("path = %s, want /repos/o/r/pulls/12/merge", r.URL.Path)
		}
		var got map[string]string
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("body: %v", err)
		}
		if len(got) != 2 || got["sha"] != "abc1234" || got["merge_method"] != "squash" {
			t.Errorf("body = %v, want sha abc1234 and merge_method squash", got)
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
		for k, v := range header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	c := New("o/r", "secret-token")
	c.BaseURL = srv.URL
	return c
}

func TestMergePR(t *testing.T) {
	const (
		kindNil = iota
		kindRefused
		kindHeadMoved
		kindOther
	)
	tests := []struct {
		name    string
		status  int
		header  map[string]string
		resp    string
		kind    int
		message string
	}{
		{"merged", 200, nil, `{"merged":true,"message":"Pull Request successfully merged"}`, kindNil, ""},
		{"200 not merged", 200, nil, `{"merged":false,"message":"x"}`, kindRefused, "x"},
		{"head changed", 409, nil, `{"message":"Head branch was modified. Review and try the merge again."}`, kindHeadMoved, ""},
		{"base changed", 405, nil, `{"message":"Base branch was modified. Review and try the merge again."}`, kindHeadMoved, ""},
		{"not mergeable", 405, nil, `{"message":"Pull Request is not mergeable"}`, kindRefused, "Pull Request is not mergeable"},
		{"403 no limit", 403, nil, `{"message":"Resource not accessible by personal access token"}`, kindRefused, "Resource not accessible by personal access token"},
		{"403 rate limit header", 403, map[string]string{"x-ratelimit-remaining": "0"}, `{"message":"Forbidden"}`, kindOther, ""},
		{"403 retry-after", 403, map[string]string{"retry-after": "60"}, `{"message":"Forbidden"}`, kindOther, ""},
		{"403 rate limit message", 403, nil, `{"message":"API rate limit exceeded for user"}`, kindOther, ""},
		{"422", 422, nil, `{"message":"Validation failed"}`, kindOther, ""},
		{"500", 500, nil, `{"message":"boom"}`, kindOther, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := mergeServer(t, tt.status, tt.header, tt.resp)
			err := c.MergePR(context.Background(), 12, "abc1234")
			var mr *MergeRefused
			switch tt.kind {
			case kindNil:
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
			case kindRefused:
				if !errors.As(err, &mr) || mr.Message != tt.message {
					t.Fatalf("err = %v, want MergeRefused %q", err, tt.message)
				}
			case kindHeadMoved:
				if !errors.Is(err, ErrHeadMoved) {
					t.Fatalf("err = %v, want ErrHeadMoved", err)
				}
			case kindOther:
				if err == nil || errors.As(err, &mr) || errors.Is(err, ErrHeadMoved) {
					t.Fatalf("err = %v, want a plain error", err)
				}
			}
		})
	}
}

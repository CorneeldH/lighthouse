package forge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// scheduleServer records every request and answers by "METHOD path" from replies; an unknown
// request fails the test. It returns the client and a pointer to the recorded requests.
type recorded struct{ method, path, body, auth string }

func scheduleServer(t *testing.T, replies map[string]struct {
	status int
	resp   string
}) (*GitHub, *[]recorded) {
	t.Helper()
	var reqs []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		reqs = append(reqs, recorded{r.Method, r.URL.EscapedPath(), strings.TrimSpace(string(raw)), r.Header.Get("Authorization")})
		rep, ok := replies[r.Method+" "+r.URL.EscapedPath()]
		if !ok {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(rep.status)
		_, _ = io.WriteString(w, rep.resp)
	}))
	t.Cleanup(srv.Close)
	c := New("o/r", "secret-token")
	c.BaseURL = srv.URL
	return c, &reqs
}

type reply = struct {
	status int
	resp   string
}

func TestCreatePRDefaultBase(t *testing.T) {
	c, reqs := scheduleServer(t, map[string]reply{
		"GET /repos/o/r":        {200, `{"default_branch":"trunk"}`},
		"POST /repos/o/r/pulls": {201, `{"number":77,"html_url":"https://github.com/o/r/pull/77","state":"open","head":{"ref":"lh/run","sha":"abc"}}`},
	})
	pr, err := c.CreatePR(context.Background(), NewPR{Head: "lh/run", Title: "Titel", Body: "Tekst"})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 77 || pr.URL != "https://github.com/o/r/pull/77" || pr.HeadRef != "lh/run" || pr.State != "OPEN" || pr.HeadSHA != "abc" {
		t.Fatalf("pr = %+v", pr)
	}
	var post *recorded
	for i := range *reqs {
		if (*reqs)[i].method == "POST" {
			post = &(*reqs)[i]
		}
	}
	if post == nil {
		t.Fatal("no POST")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(post.body), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"head": "lh/run", "base": "trunk", "title": "Titel", "body": "Tekst", "draft": false}
	if len(got) != len(want) {
		t.Fatalf("body = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("body[%s] = %q, want %q", k, got[k], v)
		}
	}
	if post.auth != "Bearer secret-token" {
		t.Errorf("auth = %q", post.auth)
	}
	// the default branch is fetched once per client
	if _, err := c.CreatePR(context.Background(), NewPR{Head: "lh/run2", Title: "T", Body: "B"}); err != nil {
		t.Fatal(err)
	}
	gets := 0
	for _, r := range *reqs {
		if r.method == "GET" {
			gets++
		}
	}
	if gets != 1 {
		t.Errorf("GET repo count = %d, want 1", gets)
	}
}

func TestCreatePRDefaultBaseErrors(t *testing.T) {
	t.Run("no default branch", func(t *testing.T) {
		c, reqs := scheduleServer(t, map[string]reply{"GET /repos/o/r": {200, `{}`}})
		_, err := c.CreatePR(context.Background(), NewPR{Head: "b", Title: "t", Body: "x"})
		if err == nil || !strings.Contains(err.Error(), "default branch") {
			t.Fatalf("err = %v, want default branch error", err)
		}
		for _, r := range *reqs {
			if r.method == "POST" {
				t.Fatal("must not POST without a base")
			}
		}
	})
	t.Run("422", func(t *testing.T) {
		c, _ := scheduleServer(t, map[string]reply{
			"GET /repos/o/r":        {200, `{"default_branch":"main"}`},
			"POST /repos/o/r/pulls": {422, `{"message":"No commits between main and b"}`},
		})
		_, err := c.CreatePR(context.Background(), NewPR{Head: "b", Title: "t", Body: "x"})
		if err == nil || !strings.Contains(err.Error(), "No commits between main and b") || !strings.Contains(err.Error(), "422") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestDeleteBranch(t *testing.T) {
	const path = "DELETE /repos/o/r/git/refs/heads/lh/run-1"
	tests := []struct {
		name    string
		status  int
		resp    string
		wantErr []string // nil: no error
	}{
		{"deleted", 204, "", nil},
		{"gone 404", 404, `{"message":"Not Found"}`, nil},
		{"gone 422", 422, `{"message":"Reference does not exist"}`, nil},
		{"other 422", 422, `{"message":"Validation failed"}`, []string{"422", "Validation failed"}},
		{"forbidden", 403, `{"message":"Resource not accessible by integration"}`, []string{"403", "Resource not accessible by integration"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, reqs := scheduleServer(t, map[string]reply{path: {tt.status, tt.resp}})
			err := c.DeleteBranch(context.Background(), "lh/run-1")
			if len(*reqs) != 1 {
				t.Fatalf("requests = %+v", *reqs)
			}
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("want error")
			}
			for _, w := range tt.wantErr {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
		})
	}
}

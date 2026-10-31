package kennforge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

func tokenFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestClientSendsBearer(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := New(srv.URL, tokenFile(t, "tok1\n"))
	var out map[string]any
	if err := c.do(context.Background(), "GET", "/x", nil, 0, &out); err != nil {
		t.Fatal(err)
	}
	if got != "Bearer tok1" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestClientRereadsToken(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	tf := tokenFile(t, "tok1")
	c := New(srv.URL, tf)
	if err := c.do(context.Background(), "GET", "/x", nil, 0, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tf, []byte("tok2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.do(context.Background(), "GET", "/x", nil, 0, nil); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "Bearer tok1" || got[1] != "Bearer tok2" {
		t.Fatalf("got %v", got)
	}
}

func TestClientEmptyTokenSendsNothing(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n.Add(1) }))
	defer srv.Close()
	for name, tf := range map[string]string{
		"empty":   tokenFile(t, "  \n"),
		"missing": filepath.Join(t.TempDir(), "nope"),
	} {
		err := New(srv.URL, tf).do(context.Background(), "GET", "/x", nil, 0, nil)
		if !errors.Is(err, workplace.ErrNotAccepted) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if n.Load() != 0 {
		t.Fatalf("server saw %d requests", n.Load())
	}
}

func TestClient5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	err := New(srv.URL, tokenFile(t, "t")).do(context.Background(), "POST", "/x", map[string]string{"a": "b"}, 0, nil)
	if !errors.Is(err, workplace.ErrNotAccepted) {
		t.Fatalf("not ErrNotAccepted: %v", err)
	}
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 503 {
		t.Fatalf("StatusError = %v", se)
	}
}

func TestClient4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()
	err := New(srv.URL, tokenFile(t, "t")).do(context.Background(), "GET", "/x", nil, 0, nil)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 404 {
		t.Fatalf("err = %v", err)
	}
	if errors.Is(err, workplace.ErrNotAccepted) || errors.Is(err, workplace.ErrUncertain) {
		t.Fatalf("4xx must be neither sentinel: %v", err)
	}
}

func TestClientConnRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	err := New(url, tokenFile(t, "t")).do(context.Background(), "GET", "/x", nil, 0, nil)
	if !errors.Is(err, workplace.ErrNotAccepted) {
		t.Fatalf("err = %v", err)
	}
}

func TestClientTimeoutUncertain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()
	err := New(srv.URL, tokenFile(t, "t")).do(context.Background(), "GET", "/x", nil, 50*time.Millisecond, nil)
	if !errors.Is(err, workplace.ErrUncertain) {
		t.Fatalf("err = %v", err)
	}
}

func TestClientNonJSONUncertain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>"))
	}))
	defer srv.Close()
	var out map[string]any
	err := New(srv.URL, tokenFile(t, "t")).do(context.Background(), "GET", "/x", nil, 0, &out)
	if !errors.Is(err, workplace.ErrUncertain) {
		t.Fatalf("err = %v", err)
	}
}

func TestClientEmptyResponseLeavesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	out := map[string]any{"keep": 1}
	if err := New(srv.URL, tokenFile(t, "t")).do(context.Background(), "DELETE", "/x", nil, 0, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("out = %v", out)
	}
}

func TestClientDoesNotFollowRedirect(t *testing.T) {
	var n atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n.Add(1) }))
	defer second.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL, http.StatusFound)
	}))
	defer first.Close()
	err := New(first.URL, tokenFile(t, "t")).do(context.Background(), "GET", "/x", nil, 0, nil)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 302 {
		t.Fatalf("err = %v", err)
	}
	if n.Load() != 0 {
		t.Fatalf("redirect followed")
	}
}

func TestClientNoProxyNoClientTimeout(t *testing.T) {
	c := New("http://x", "f").HTTP
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil {
		t.Fatalf("transport = %#v", c.Transport)
	}
	if c.Timeout != 0 {
		t.Fatalf("Timeout = %v", c.Timeout)
	}
}

func TestClientLongTimeoutHonoured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if err := New(srv.URL, tokenFile(t, "t")).do(context.Background(), "GET", "/x", nil, time.Second, nil); err != nil {
		t.Fatal(err)
	}
}

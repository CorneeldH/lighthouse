package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func getWithHost(h http.Handler, host, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.URL.Path = path
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHostAllowed(t *testing.T) {
	h := New(testSite(), fakeReport)
	for _, host := range []string{
		"127.0.0.1:8127", "localhost:8127", "LOCALHOST", "[::1]:8127", "[::1]",
		"100.64.0.1", "box.tail1234.ts.net", "box.tail1234.ts.net.", "BOX.Tail1234.TS.NET:443",
	} {
		t.Run(host, func(t *testing.T) {
			if rec := getWithHost(h, host, "/api/health"); rec.Code != 200 {
				t.Errorf("status = %d, want 200", rec.Code)
			}
		})
	}
}

func TestHostRefused(t *testing.T) {
	h := New(testSite(), fakeReport)
	for _, host := range []string{
		"evil.example.com", "127.0.0.1.evil.com", "localhost.evil.com",
		"ts.net", "evilts.net", "ts.net.evil.com", "", "evil.example.com:8127",
	} {
		for _, path := range []string{"/api/health", "/"} {
			t.Run(host+path, func(t *testing.T) {
				rec := getWithHost(h, host, path)
				if rec.Code != 403 {
					t.Fatalf("status = %d, want 403", rec.Code)
				}
				body := rec.Body.String()
				for _, leak := range []string{"version", "placeholder"} {
					if strings.Contains(body, leak) {
						t.Errorf("body %q contains %q", body, leak)
					}
				}
			})
		}
	}
}

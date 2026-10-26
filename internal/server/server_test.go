package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/CorneeldH/lighthouse/internal/usage"
	"github.com/CorneeldH/lighthouse/internal/version"
	"github.com/CorneeldH/lighthouse/internal/web"
)

func testSite() fs.FS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<p>placeholder</p>")},
		"assets/app.js": {Data: []byte("js")},
	}
}

func intp(n int) *int { return &n }

// fakeReport is a fixed report: Claude measured, Codex not.
func fakeReport(context.Context) usage.Report {
	notMeasurable := usage.WindowReport{State: usage.StateNotMeasurable, Reason: "no codex"}
	return usage.Report{
		Claude: usage.SourceReport{
			Session: usage.WindowReport{State: usage.StateOK, Pct: intp(99), AgeS: intp(5)},
			Week:    usage.WindowReport{State: usage.StateStale, Pct: intp(48), AgeS: intp(1000)},
		},
		Codex: usage.SourceReport{Session: notMeasurable, Week: notMeasurable},
	}
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.URL.Path = path
	req.Host = "127.0.0.1" // New refuses any other Host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRoutes(t *testing.T) {
	h := New(testSite(), fakeReport)
	tests := []struct {
		path    string
		status  []int
		contain string
		absent  string
	}{
		{"/api/onbekend", []int{404}, "", "placeholder"},
		{"/", []int{200}, "placeholder", ""},
		{"/runs/42", []int{200}, "placeholder", ""},
		{"/assets/app.js", []int{200}, "js", "placeholder"},
		{"/assets/", []int{200}, "placeholder", ""},
		{"/api/usage", []int{200}, "", "placeholder"},
		{"/api/docs", []int{200}, "", "placeholder"},
		{"/../../etc/passwd", []int{200, 301, 307, 404}, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			rec := get(h, tc.path)
			ok := false
			for _, s := range tc.status {
				ok = ok || rec.Code == s
			}
			if !ok {
				t.Fatalf("status = %d, want one of %v", rec.Code, tc.status)
			}
			body := rec.Body.String()
			if tc.contain != "" && !strings.Contains(body, tc.contain) {
				t.Errorf("body %q lacks %q", body, tc.contain)
			}
			if tc.absent != "" && strings.Contains(body, tc.absent) {
				t.Errorf("body %q contains %q", body, tc.absent)
			}
			if rec.Code == 301 || rec.Code == 307 {
				loc := rec.Header().Get("Location")
				if !strings.HasPrefix(loc, "/") || strings.Contains(loc, "..") {
					t.Errorf("Location = %q", loc)
				}
			}
			if rec.Code == 200 && tc.path == "/../../etc/passwd" && !strings.Contains(rec.Body.String(), "placeholder") {
				t.Errorf("200 without placeholder: %q", rec.Body.String())
			}
		})
	}
}

func TestHealth(t *testing.T) {
	rec := get(New(testSite(), fakeReport), "/api/health")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 1 || body["version"] != version.String() {
		t.Errorf("body = %v, want only version=%q", body, version.String())
	}
}

func TestUsage(t *testing.T) {
	rec := get(New(testSite(), fakeReport), "/api/usage")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	cs := body["claude"]["session"]
	if cs["state"] != "ok" || cs["pct"] != float64(99) {
		t.Errorf("claude.session = %v, want state ok, pct 99", cs)
	}
	cw := body["codex"]["week"]
	if cw["state"] != "not_measurable" {
		t.Errorf("codex.week.state = %v", cw["state"])
	}
	for _, k := range []string{"pct", "age_s"} {
		if v, ok := cw[k]; !ok || v != nil {
			t.Errorf("codex.week.%s = %v (present %v), want present null", k, v, ok)
		}
	}
}

func TestOpenAPI(t *testing.T) {
	h := New(testSite(), fakeReport)
	rec := get(h, "/api/openapi.json")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var doc struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, p := range []string{"/api/health", "/api/usage"} {
		if _, ok := doc.Paths[p]; !ok {
			t.Errorf("paths lack %s: %v", p, doc.Paths)
		}
	}
	if rec := get(h, "/api/openapi.yaml"); rec.Code != 200 {
		t.Errorf("openapi.yaml status = %d", rec.Code)
	}
}

func TestSPAEscapes(t *testing.T) {
	h := spa(testSite())
	for _, p := range []string{"/../../etc/passwd", "/assets/../../etc/passwd", "/assets/"} {
		rec := get(h, p)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "placeholder") {
			t.Errorf("%s: status %d body %q", p, rec.Code, rec.Body.String())
		}
	}
}

func TestEmbeddedSiteHasIndex(t *testing.T) {
	if _, err := fs.ReadFile(web.FS(), "index.html"); err != nil {
		t.Fatal(err)
	}
}

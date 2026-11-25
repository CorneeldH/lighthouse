package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/telemetry/report"
)

// fakeTelemetry serves canned reports and records the filter, repo and id of each call.
type fakeTelemetry struct {
	mu         sync.Mutex
	filters    map[string]report.Filter
	repo       string
	components report.ComponentsReport
	timeline   report.TimelineReport
	rows       []report.ListRow
	detail     map[int64]report.Detail
	coverage   report.CoverageReport
	err        error
}

func (f *fakeTelemetry) note(op string, flt report.Filter) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.filters == nil {
		f.filters = map[string]report.Filter{}
	}
	f.filters[op] = flt
}

func (f *fakeTelemetry) Components(_ context.Context, flt report.Filter) (report.ComponentsReport, error) {
	f.note("components", flt)
	return f.components, f.err
}

func (f *fakeTelemetry) Timeline(_ context.Context, flt report.Filter) (report.TimelineReport, error) {
	f.note("timeline", flt)
	return f.timeline, f.err
}

func (f *fakeTelemetry) Trajectories(_ context.Context, flt report.Filter) ([]report.ListRow, error) {
	f.note("trajectories", flt)
	return f.rows, f.err
}

func (f *fakeTelemetry) Detail(_ context.Context, id int64) (report.Detail, bool, error) {
	if f.err != nil {
		return report.Detail{}, false, f.err
	}
	d, ok := f.detail[id]
	return d, ok, nil
}

func (f *fakeTelemetry) Coverage(_ context.Context, repo string) (report.CoverageReport, error) {
	f.mu.Lock()
	f.repo = repo
	f.mu.Unlock()
	return f.coverage, f.err
}

func telemetryHandler(r TelemetryReader) http.Handler {
	d := testDeps(stubSource{}, nil)
	d.Telemetry = r
	return New(testSite(), d)
}

// getURL is get with a query string.
func getURL(h http.Handler, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host = "127.0.0.1"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTelemetryEndpointsReturnJSONShape(t *testing.T) {
	cost := 1.5
	fake := &fakeTelemetry{
		components: report.ComponentsReport{Blocks: []report.Block{{Name: "lane", Kind: "values", LabelNote: "n", Groups: []report.Group{}}}},
		rows:       []report.ListRow{{ID: 7, Repo: "o/r", Issue: 3, Size: "small", Components: []string{}, Cost: &cost, CostKind: "api"}},
		detail:     map[int64]report.Detail{7: {ID: 7, Repo: "o/r", Issue: 3}},
	}
	h := telemetryHandler(fake)

	rec := getURL(h, "/api/telemetry/components")
	if rec.Code != 200 {
		t.Fatalf("components status = %d, body %s", rec.Code, rec.Body)
	}
	body := decode(t, rec.Body.Bytes())
	if !strings.Contains(string(body["blocks"]), `"label_note":"n"`) {
		t.Errorf("blocks = %s, want snake_case label_note", body["blocks"])
	}
	var opts map[string]json.RawMessage
	_ = json.Unmarshal(body["options"], &opts)
	if string(opts["pr_sizes"]) != "[]" || string(opts["repos"]) != "[]" {
		t.Errorf("options = %s, want empty lists as []", body["options"])
	}

	rec = getURL(h, "/api/telemetry/timeline")
	body = decode(t, rec.Body.Bytes())
	if rec.Code != 200 || string(body["weeks"]) != "[]" || string(body["markers"]) != "[]" {
		t.Errorf("timeline = %d %s, want 200 with weeks and markers []", rec.Code, rec.Body)
	}

	rec = getURL(h, "/api/telemetry/trajectories")
	body = decode(t, rec.Body.Bytes())
	if rec.Code != 200 || !strings.Contains(string(body["trajectories"]), `"cost_kind":"api"`) {
		t.Errorf("trajectories = %d %s, want 200 with snake_case cost_kind", rec.Code, rec.Body)
	}

	rec = getURL(h, "/api/telemetry/trajectories/7")
	body = decode(t, rec.Body.Bytes())
	if rec.Code != 200 || string(body["id"]) != "7" || string(body["repo"]) != `"o/r"` {
		t.Errorf("detail = %d %s, want 200 for trajectory 7", rec.Code, rec.Body)
	}
	for _, k := range []string{"runs", "sessions", "reviews", "findings", "recoveries", "events"} {
		if string(body[k]) != "[]" {
			t.Errorf("detail %s = %s, want []", k, body[k])
		}
	}

	rec = getURL(h, "/api/telemetry/coverage")
	body = decode(t, rec.Body.Bytes())
	if rec.Code != 200 || string(body["lines"]) != "[]" || string(body["not_migrated"]) != "false" {
		t.Errorf("coverage = %d %s, want 200 with lines [] and not_migrated false", rec.Code, rec.Body)
	}
}

func TestTelemetryEmptyTrajectoriesAreArray(t *testing.T) {
	rec := getURL(telemetryHandler(&fakeTelemetry{}), "/api/telemetry/trajectories")
	if body := decode(t, rec.Body.Bytes()); string(body["trajectories"]) != "[]" {
		t.Errorf("trajectories = %s, want []", body["trajectories"])
	}
	rec = getURL(telemetryHandler(&fakeTelemetry{}), "/api/telemetry/components")
	if body := decode(t, rec.Body.Bytes()); string(body["blocks"]) != "[]" {
		t.Errorf("blocks = %s, want []", body["blocks"])
	}
}

func TestTelemetryFiltersAreParsedOnEveryEndpoint(t *testing.T) {
	fake := &fakeTelemetry{}
	h := telemetryHandler(fake)
	const q = "?repo=a&route=2&size=small&pr_size=below&area=x&model=m&harness=h"
	full := report.Filter{Repo: "a", Route: "2", Size: "small", PRSize: "below", Area: "x", Model: "m", Harness: "h"}
	for _, path := range []string{
		"/api/telemetry/components" + q,
		"/api/telemetry/trajectories" + q + "&component=plan_review",
		"/api/telemetry/timeline?repo=a",
		"/api/telemetry/coverage?repo=a",
	} {
		if rec := getURL(h, path); rec.Code != 200 {
			t.Fatalf("%s status = %d, body %s", path, rec.Code, rec.Body)
		}
	}
	if got := fake.filters["components"]; got != full {
		t.Errorf("components filter = %+v, want %+v", got, full)
	}
	withComponent := full
	withComponent.Component = "plan_review"
	if got := fake.filters["trajectories"]; got != withComponent {
		t.Errorf("trajectories filter = %+v, want %+v", got, withComponent)
	}
	if got := fake.filters["timeline"]; got != (report.Filter{Repo: "a"}) {
		t.Errorf("timeline filter = %+v, want repo a only", got)
	}
	if fake.repo != "a" {
		t.Errorf("coverage repo = %q, want a", fake.repo)
	}
}

func TestTelemetryTrajectoryNotFound(t *testing.T) {
	rec := getURL(telemetryHandler(&fakeTelemetry{}), "/api/telemetry/trajectories/99")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404, body %s", rec.Code, rec.Body)
	}
}

func TestTelemetryReadErrorIs500(t *testing.T) {
	h := telemetryHandler(&fakeTelemetry{err: errors.New("disk")})
	for _, path := range []string{
		"/api/telemetry/components", "/api/telemetry/timeline", "/api/telemetry/trajectories",
		"/api/telemetry/trajectories/1", "/api/telemetry/coverage",
	} {
		if rec := getURL(h, path); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s status = %d, want 500", path, rec.Code)
		}
	}
}

func TestTelemetryUnmigratedDatabaseIsCoverageNotError(t *testing.T) {
	s, err := store.OpenShared(context.Background(), filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// A trajectory exists, so only the missing table can make the detail a 404.
	if _, err := s.DB.Exec(`INSERT INTO trajectories (id, repo, issue, created_at) VALUES (1, 'o/r', 1, '2026-03-02T09:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`DROP TABLE review_runs`); err != nil {
		t.Fatal(err)
	}
	h := telemetryHandler(report.NewReader(s.DB, time.Now))

	rec := getURL(h, "/api/telemetry/coverage")
	body := decode(t, rec.Body.Bytes())
	if rec.Code != 200 || string(body["not_migrated"]) != "true" {
		t.Errorf("coverage = %d %s, want 200 with not_migrated true", rec.Code, rec.Body)
	}
	for _, c := range []struct{ path, key string }{
		{"/api/telemetry/components", "blocks"},
		{"/api/telemetry/timeline", "weeks"},
		{"/api/telemetry/trajectories", "trajectories"},
	} {
		rec := getURL(h, c.path)
		body := decode(t, rec.Body.Bytes())
		if rec.Code != 200 || string(body[c.key]) != "[]" {
			t.Errorf("%s = %d %s, want 200 with %s []", c.path, rec.Code, rec.Body, c.key)
		}
	}
	if rec := getURL(h, "/api/telemetry/trajectories/1"); rec.Code != http.StatusNotFound {
		t.Errorf("detail status = %d, want 404 on an unmigrated database, body %s", rec.Code, rec.Body)
	}
}

func TestTelemetryEndpointsAbsentWithoutReader(t *testing.T) {
	h := telemetryHandler(nil)
	for _, path := range []string{
		"/api/telemetry/components", "/api/telemetry/timeline", "/api/telemetry/trajectories",
		"/api/telemetry/trajectories/1", "/api/telemetry/coverage",
	} {
		if rec := getURL(h, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", path, rec.Code)
		}
	}
	rec := get(h, "/api/openapi.json")
	if strings.Contains(rec.Body.String(), "/api/telemetry") {
		t.Error("openapi.json lists telemetry endpoints without a reader")
	}
}

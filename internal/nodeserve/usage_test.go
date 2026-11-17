package nodeserve

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/park"
)

const (
	markerSession = "5f2a8c1e-3b7d-4e9a-a6c0-1d4b8e2f7c95"
	pauseContent  = "2026-11-16T18:00:00+01:00\n"
	alarmContent  = "{\"schema\":1,\"window\":\"five_hour\",\"reset_at\":\"2026-11-16T19:00:00Z\"}\n"
)

func strPtr(s string) *string { return &s }

func readOrMissing(t *testing.T, path string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b), true
}

func TestUsagePutWritesPauseAndAlarmAtomically(t *testing.T) {
	f := newFixture(t, nil, nodeToken)
	// An old flag is replaced, not appended to.
	if err := os.MkdirAll(filepath.Dir(f.h.d.PauseFile), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Dir(f.h.d.PauseFile), "PAUSE", "2026-11-15T09:00:00+01:00\nmore old bytes\n")

	rec := send(f.h, http.MethodPut, nodeapi.PathUsage, nodeapi.UsageRequest{Pause: strPtr(pauseContent), Alarm: strPtr(alarmContent)})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if got, _ := readOrMissing(t, f.h.d.PauseFile); got != pauseContent {
		t.Errorf("PAUSE = %q, want %q", got, pauseContent)
	}
	if got, _ := readOrMissing(t, f.h.d.AlarmMirror); got != alarmContent {
		t.Errorf("alarm mirror = %q, want %q", got, alarmContent)
	}
	entries, err := os.ReadDir(filepath.Dir(f.h.d.PauseFile))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"PAUSE", "usage-window.alarm"}; !slices.Equal(names, want) {
		t.Errorf("controller dir = %q, want only %q (no temp file left)", names, want)
	}
}

func TestUsagePutNilRemovesFiles(t *testing.T) {
	f := newFixture(t, nil, nodeToken)
	if rec := send(f.h, http.MethodPut, nodeapi.PathUsage, nodeapi.UsageRequest{Pause: strPtr(pauseContent), Alarm: strPtr(alarmContent)}); rec.Code != http.StatusNoContent {
		t.Fatalf("setup: status = %d: %s", rec.Code, rec.Body.String())
	}

	// Only the pause cleared: the alarm stays.
	rec := send(f.h, http.MethodPut, nodeapi.PathUsage, map[string]any{"pause": nil, "alarm": alarmContent})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if _, ok := readOrMissing(t, f.h.d.PauseFile); ok {
		t.Error("PAUSE still there after pause null")
	}
	if got, _ := readOrMissing(t, f.h.d.AlarmMirror); got != alarmContent {
		t.Errorf("alarm mirror = %q, want %q", got, alarmContent)
	}

	// Both absent; a file that is already gone is no error.
	for i := range 2 {
		rec = send(f.h, http.MethodPut, nodeapi.PathUsage, nodeapi.UsageRequest{})
		if rec.Code != http.StatusNoContent {
			t.Fatalf("round %d: status = %d, want 204: %s", i, rec.Code, rec.Body.String())
		}
	}
	if _, ok := readOrMissing(t, f.h.d.AlarmMirror); ok {
		t.Error("alarm mirror still there after alarm absent")
	}
}

// writeMarker writes m as <dir>/<name>.json.
func writeMarker(t *testing.T, dir, name string, m park.Marker) {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, name+".json", string(b))
}

func nodeMarker(sid string) park.Marker {
	return park.Marker{
		SessionID: sid, Cwd: "/srv/work/repo", Window: "five_hour",
		ResetAt: "2026-11-16T19:00:00Z", CreatedAt: "2026-11-16T14:12:00Z",
		Source: "usage-gate", Controller: true,
	}
}

func TestMarkersListsNodeMarkers(t *testing.T) {
	f := newFixture(t, nil, nodeToken)
	dir := f.h.d.MarkerDir

	// No marker dir yet: an empty list, not null.
	rec := call(f.h, nodeapi.PathMarkers, bearer)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("without markers = %s, want []", got)
	}

	writeMarker(t, dir, markerSession, nodeMarker(markerSession))
	writeMarker(t, dir, otherSession, nodeMarker(otherSession))
	writeMarker(t, dir, "a1b2", nodeMarker(markerSession)) // file name differs from session_id
	writeFile(t, dir, "notes.txt", "not a marker")

	rec = call(f.h, nodeapi.PathMarkers, bearer)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got []park.Marker
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, rec.Body.String())
	}
	want := []park.Marker{nodeMarker(markerSession), nodeMarker(otherSession)}
	if !slices.Equal(got, want) {
		t.Errorf("markers = %+v, want %+v", got, want)
	}
}

func markerPath(session, key string) string {
	return nodeapi.Prefix + "/markers/" + session + "?" + url.Values{"key": {key}}.Encode()
}

func TestMarkerDeleteHonoursKey(t *testing.T) {
	f := newFixture(t, nil, nodeToken)
	dir := f.h.d.MarkerDir
	writeMarker(t, dir, markerSession, nodeMarker(markerSession))
	file := filepath.Join(dir, markerSession+".json")

	// The guard parked the session again since: another key leaves the marker.
	rec := send(f.h, http.MethodDelete, markerPath(markerSession, "usage:"+markerSession+"@2026-11-16T09:00:00Z:2026-11-16T14:00:00Z"), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("wrong key: status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if _, ok := readOrMissing(t, file); !ok {
		t.Fatal("marker removed with a wrong key")
	}

	rec = send(f.h, http.MethodDelete, markerPath(markerSession, "usage:"+markerSession+"@2026-11-16T14:12:00Z:2026-11-16T19:00:00Z"), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("right key: status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if _, ok := readOrMissing(t, file); ok {
		t.Error("marker still there after its key")
	}

	// Already gone: no error.
	rec = send(f.h, http.MethodDelete, markerPath(markerSession, "usage:"+markerSession+"@2026-11-16T14:12:00Z:2026-11-16T19:00:00Z"), nil)
	if rec.Code != http.StatusNoContent {
		t.Errorf("missing marker: status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
}

func TestMarkerDeleteBadRequest(t *testing.T) {
	f := newFixture(t, nil, nodeToken)
	dir := f.h.d.MarkerDir
	writeMarker(t, dir, markerSession, nodeMarker(markerSession))
	for name, path := range map[string]string{
		"unsafe session": markerPath("a.b", "usage:x"),
		"leading dash":   markerPath("-v", "usage:x"),
		"no key":         nodeapi.Prefix + "/markers/" + markerSession,
	} {
		rec := send(f.h, http.MethodDelete, path, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", name, rec.Code, rec.Body.String())
		}
	}
	if _, ok := readOrMissing(t, filepath.Join(dir, markerSession+".json")); !ok {
		t.Error("marker removed by a bad request")
	}
}

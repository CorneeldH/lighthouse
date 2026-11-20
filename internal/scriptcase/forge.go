package scriptcase

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// ForgeCall is one scripted request to the forge API (`forge_calls` of a case): the method and
// the request URI (path and query, exactly as the client sends them) it must have, and what it
// answers. The Python runner (tests/pack/_testkit.py, forge_replay) reads the same file and
// replays the same calls, so one fixture proves the Python harness and the Go command alike.
type ForgeCall struct {
	Method string `json:"method"`
	// Path is the request URI, query included.
	Path string `json:"path"`
	// RequestBody, when set, is the JSON the request must carry; compared as decoded JSON.
	RequestBody json.RawMessage `json:"request_body,omitempty"`
	Status      int             `json:"status"`
	// Body is the JSON the response carries; empty is no body. "{forge_url}" in Body and in a
	// header value is the base URL of the replay server, for a Link header.
	Body    json.RawMessage   `json:"body,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// ReplayForge serves calls in order from an httptest server and returns its URL, to hand a
// forge client as its base URL. A request that is not the next call, and one after the list ran
// out, fails t and is answered 599; calls left at the end of the test fail it too.
func ReplayForge(t testing.TB, calls []ForgeCall) string {
	t.Helper()
	var (
		mu   sync.Mutex
		next int
	)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		got := r.Method + " " + r.URL.RequestURI()
		if next >= len(calls) {
			t.Errorf("forge replay: unexpected request %s: all %d scripted calls were used", got, len(calls))
			w.WriteHeader(599)
			return
		}
		want := calls[next]
		if wantLine := want.Method + " " + want.Path; wantLine != got {
			t.Errorf("forge replay: call %d: expected %s, got %s", next+1, wantLine, got)
			w.WriteHeader(599)
			return
		}
		if len(want.RequestBody) > 0 {
			raw, _ := io.ReadAll(r.Body)
			var wantBody, gotBody any
			if err := json.Unmarshal(want.RequestBody, &wantBody); err != nil {
				t.Errorf("forge replay: call %d: request_body is not JSON: %v", next+1, err)
			}
			if err := json.Unmarshal(raw, &gotBody); err != nil || !reflect.DeepEqual(wantBody, gotBody) {
				t.Errorf("forge replay: call %d %s: expected request body %s, got %s", next+1, got, want.RequestBody, raw)
				w.WriteHeader(599)
				return
			}
		}
		next++
		for k, v := range want.Headers {
			w.Header().Set(k, strings.ReplaceAll(v, "{forge_url}", srv.URL))
		}
		if len(want.Body) > 0 {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(want.Status)
		_, _ = w.Write([]byte(strings.ReplaceAll(string(want.Body), "{forge_url}", srv.URL)))
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if next < len(calls) {
			t.Errorf("forge replay: %d of %d scripted calls were not made; next expected: %s %s",
				len(calls)-next, len(calls), calls[next].Method, calls[next].Path)
		}
	})
	return srv.URL
}

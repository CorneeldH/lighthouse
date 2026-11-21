package nodeserve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

const (
	launchSession = "3c9e1f4a-7b2d-4e8f-a1c6-5d0b9e2f7a14"
	launchID      = "ws-81"
)

func send(h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Authorization", bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func launchPath(id string) string { return nodeapi.Prefix + "/workspaces/" + id + "/launch" }

func launchRequest(mode string) nodeapi.LaunchRequest {
	return nodeapi.LaunchRequest{
		Repo: "owner/repo", Issue: 42, SessionID: launchSession, Mode: mode,
		Profile: "default", Prompt: "/lighthouse-start-issue 42",
		Instructions: "runinstructies van de tick\n",
	}
}

func decodeCarrier(t *testing.T, rec *httptest.ResponseRecorder) workplace.Carrier {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var c workplace.Carrier
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatalf("answer is not a carrier: %v: %s", err, rec.Body.String())
	}
	return c
}

// argAfter returns the argument that follows flag in args, and whether flag is there.
func argAfter(args []string, flag string) (string, bool) {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return "", false
	}
	return args[i+1], true
}

func TestLaunchWritesCarrierAtNodeStateDir(t *testing.T) {
	f := newFixture(t, map[string]string{}, nodeToken)
	req := launchRequest("start")
	req.MCPConfig = "/srv/node/mcp/default.json"
	got := decodeCarrier(t, send(f.h, http.MethodPut, launchPath(launchID), req))

	state := f.h.d.StateDir
	path := filepath.Join(state, "carriers", launchID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("carrier not at %s: %v", path, err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("carrier = %v, %v; want mode 0600", info, err)
		}
	}
	var onDisk workplace.Carrier
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(onDisk.Args, got.Args) || onDisk.SessionID != launchSession {
		t.Errorf("answered carrier %+v, on disk %+v", got, onDisk)
	}
	if onDisk.Repo != "owner/repo" || onDisk.Issue != 42 || onDisk.Mode != "start" || onDisk.Profile != "default" {
		t.Errorf("carrier fields = %+v", onDisk)
	}
	if s, ok := argAfter(onDisk.Args, "--session-id"); !ok || s != launchSession {
		t.Errorf("--session-id = %q, %v; want %s", s, ok, launchSession)
	}
	if m, ok := argAfter(onDisk.Args, "--mcp-config"); !ok || m != "/srv/node/mcp/default.json" {
		t.Errorf("--mcp-config = %q, %v; want the node's path unchanged", m, ok)
	}
	instr, ok := argAfter(onDisk.Args, "--append-system-prompt-file")
	if want := filepath.Join(state, "runs", launchSession, "instructions.md"); !ok || instr != want {
		t.Errorf("--append-system-prompt-file = %q, %v; want %s", instr, ok, want)
	}
	if text, err := os.ReadFile(instr); err != nil || string(text) != "runinstructies van de tick\n" {
		t.Errorf("instructions = %q, %v", text, err)
	}
	if onDisk.Env["LIGHTHOUSE_STATE_DIR"] != state {
		t.Errorf("LIGHTHOUSE_STATE_DIR = %q, want %q", onDisk.Env["LIGHTHOUSE_STATE_DIR"], state)
	}
	// Never under a node subdirectory: on the node itself, this is self.
	entries, err := os.ReadDir(filepath.Join(state, "carriers"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("carriers/ holds directory %s", e.Name())
		}
	}
}

func TestLaunchResumeUsesResumeFlag(t *testing.T) {
	f := newFixture(t, map[string]string{}, nodeToken)
	c := decodeCarrier(t, send(f.h, http.MethodPut, launchPath(launchID), launchRequest("resume")))
	if s, ok := argAfter(c.Args, "--resume"); !ok || s != launchSession {
		t.Errorf("--resume = %q, %v; want %s", s, ok, launchSession)
	}
	if slices.Contains(c.Args, "--session-id") {
		t.Errorf("resume carrier has --session-id: %v", c.Args)
	}
}

func TestLaunchRejectsNonUUIDSession(t *testing.T) {
	f := newFixture(t, map[string]string{}, nodeToken)
	for _, session := range []string{"not-a-uuid", "../" + launchSession, strings.ToUpper(launchSession)} {
		req := launchRequest("start")
		req.SessionID = session
		rec := send(f.h, http.MethodPut, launchPath(launchID), req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400: %s", session, rec.Code, rec.Body.String())
		}
	}
	// Nothing was written: the check comes before any handling.
	if _, err := os.Stat(f.h.d.StateDir); !os.IsNotExist(err) {
		t.Errorf("state dir exists after rejected launches: %v", err)
	}
}

func TestLaunchRejectsBadRequestBeforeWriting(t *testing.T) {
	f := newFixture(t, map[string]string{}, nodeToken)
	cases := map[string]func(*nodeapi.LaunchRequest){
		"unknown mode":     func(r *nodeapi.LaunchRequest) { r.Mode = "restart" },
		"relative mcp":     func(r *nodeapi.LaunchRequest) { r.MCPConfig = "mcp.json" },
		"dash prompt":      func(r *nodeapi.LaunchRequest) { r.Prompt = "-x" },
		"no instructions":  func(r *nodeapi.LaunchRequest) { r.Instructions = "" },
		"unsafe workspace": nil,
	}
	for name, mutate := range cases {
		req := launchRequest("start")
		id := launchID
		if mutate != nil {
			mutate(&req)
		} else {
			id = ".hidden"
		}
		rec := send(f.h, http.MethodPut, launchPath(id), req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", name, rec.Code, rec.Body.String())
		}
	}
	if _, err := os.Stat(f.h.d.StateDir); !os.IsNotExist(err) {
		t.Errorf("state dir exists after rejected launches: %v", err)
	}
}

func TestLaunchWorksWhenForgeForgotWorkspace(t *testing.T) {
	f := newFixture(t, map[string]string{}, nodeToken) // the forge knows no workspace
	decodeCarrier(t, send(f.h, http.MethodPut, launchPath(launchID), launchRequest("start")))
	if rec := send(f.h, http.MethodDelete, carrierPath(launchID), nil); rec.Code != http.StatusNoContent {
		t.Errorf("carrier delete: status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if n := f.forge.hits.Load(); n != 0 {
		t.Errorf("forge requests = %d, want 0", n)
	}
	if n := f.runner.calls.Load(); n != 0 {
		t.Errorf("runner calls = %d, want 0", n)
	}
}

func carrierPath(id string) string { return nodeapi.Prefix + "/workspaces/" + id + "/carrier" }

func TestCarrierDeleteIsIdempotent(t *testing.T) {
	f := newFixture(t, map[string]string{}, nodeToken)
	decodeCarrier(t, send(f.h, http.MethodPut, launchPath(launchID), launchRequest("start")))
	// A carrier of another workspace stays.
	decodeCarrier(t, send(f.h, http.MethodPut, launchPath("ws-82"), launchRequest("start")))
	for i := range 2 {
		if rec := send(f.h, http.MethodDelete, carrierPath(launchID), nil); rec.Code != http.StatusNoContent {
			t.Errorf("delete %d: status = %d, want 204: %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if _, err := os.Stat(filepath.Join(f.h.d.StateDir, "carriers", launchID+".json")); !os.IsNotExist(err) {
		t.Errorf("carrier still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.h.d.StateDir, "carriers", "ws-82.json")); err != nil {
		t.Errorf("carrier of ws-82 gone: %v", err)
	}
	if rec := send(f.h, http.MethodDelete, carrierPath(".hidden"), nil); rec.Code != http.StatusBadRequest {
		t.Errorf("unsafe id: status = %d, want 400", rec.Code)
	}
}

func permissionPath(session string) string {
	return nodeapi.Prefix + "/runs/" + session + "/permission"
}

func TestPermissionReturnsRawBytes(t *testing.T) {
	f := newFixture(t, map[string]string{}, nodeToken)
	dir := filepath.Join(f.h.d.StateDir, "runs", launchSession)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Raw: not the parsed note, so a version the node does not know passes through unchanged.
	note := `{"version": 9, "at": "2026-11-16T10:00:00Z", "extra": [1,2]}` + "\n"
	writeFile(t, dir, "permission.json", note)
	rec := send(f.h, http.MethodGet, permissionPath(launchSession), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != note {
		t.Errorf("body = %q, want %q", rec.Body.String(), note)
	}
}

func TestPermissionMissingIs404(t *testing.T) {
	f := newFixture(t, map[string]string{}, nodeToken)
	rec := send(f.h, http.MethodGet, permissionPath(launchSession), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if p := problem(t, rec); p.Code != nodeapi.CodeNoPermission {
		t.Errorf("code = %q, want %q", p.Code, nodeapi.CodeNoPermission)
	}
}

func TestPermissionUnsafeSessionIs400(t *testing.T) {
	f := newFixture(t, map[string]string{}, nodeToken)
	// A note outside runs/ that a path trick would reach.
	if err := os.MkdirAll(f.h.d.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, f.h.d.StateDir, "permission.json", `{"version":1}`)
	for _, session := range []string{"a.b", "a_b", "a%20b", "%2e%2e"} {
		rec := send(f.h, http.MethodGet, permissionPath(session), nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400: %s", session, rec.Code, rec.Body.String())
		}
	}
}

func postFailedPath(session string) string {
	return nodeapi.Prefix + "/runs/" + session + "/post-failed"
}

func TestPostFailedRoute(t *testing.T) {
	f := newFixture(t, map[string]string{}, nodeToken)
	t.Run("raw bytes", func(t *testing.T) {
		dir := filepath.Join(f.h.d.StateDir, "runs", launchSession)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		note := `{"version": 9, "extra": [1]}` + "\n"
		writeFile(t, dir, "post-failed.json", note)
		mtime := time.Date(2026, 11, 19, 8, 30, 0, 250_000_000, time.UTC)
		if err := os.Chtimes(filepath.Join(dir, "post-failed.json"), mtime, mtime); err != nil {
			t.Fatal(err)
		}
		rec := send(f.h, http.MethodGet, postFailedPath(launchSession), nil)
		if rec.Code != http.StatusOK || rec.Body.String() != note {
			t.Errorf("status %d body %q, want 200 and %q", rec.Code, rec.Body.String(), note)
		}
		if got := rec.Header().Get(nodeapi.HeaderModTime); got != "2026-11-19T08:30:00.25Z" {
			t.Errorf("%s = %q, want the file's mtime 2026-11-19T08:30:00.25Z", nodeapi.HeaderModTime, got)
		}
	})
	t.Run("missing is 404 no_post_failed", func(t *testing.T) {
		rec := send(f.h, http.MethodGet, postFailedPath("0b3e1c9a-1111-4222-8333-444455556666"), nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
		}
		if p := problem(t, rec); p.Code != nodeapi.CodeNoPostFailed {
			t.Errorf("code = %q, want %q", p.Code, nodeapi.CodeNoPostFailed)
		}
	})
	t.Run("unsafe session is 400", func(t *testing.T) {
		for _, session := range []string{"a.b", "a_b", "a%20b", "%2e%2e"} {
			rec := send(f.h, http.MethodGet, postFailedPath(session), nil)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%q: status = %d, want 400: %s", session, rec.Code, rec.Body.String())
			}
		}
	})
}

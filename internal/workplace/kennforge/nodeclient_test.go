package kennforge

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// fakeNode is a node-API in the shape nodeserve answers: the bearer check of its handler, GET
// info with Version, and problem+json for every error. Routes are added per test.
type fakeNode struct {
	srv       *httptest.Server
	mux       *http.ServeMux
	token     string
	infoCalls atomic.Int32
	version   int
	lastAuth  atomic.Value // string: the Authorization header of the last request
}

func randomID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func newFakeNode(t *testing.T) *fakeNode {
	t.Helper()
	n := &fakeNode{mux: http.NewServeMux(), token: randomID(t), version: nodeapi.Version}
	n.mux.HandleFunc("GET "+nodeapi.PathInfo, func(w http.ResponseWriter, _ *http.Request) {
		n.infoCalls.Add(1)
		writeNode(w, http.StatusOK, nodeapi.Info{Version: n.version, Binary: "v0.0.0-test", Platform: "linux/amd64"})
	})
	n.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.lastAuth.Store(r.Header.Get("Authorization"))
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			writeNode(w, http.StatusUnauthorized, nodeapi.Problem{Status: 401, Detail: "missing bearer token"})
			return
		}
		if got != n.token {
			writeNode(w, http.StatusForbidden, nodeapi.Problem{Status: 403, Detail: "wrong bearer token"})
			return
		}
		n.mux.ServeHTTP(w, r)
	}))
	t.Cleanup(n.srv.Close)
	return n
}

// client is a NodeClient on the node with its own token in a token file.
func (n *fakeNode) client(t *testing.T) *NodeClient {
	t.Helper()
	tok := filepath.Join(t.TempDir(), "node-token")
	if err := os.WriteFile(tok, []byte(n.token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return NewNodeClient(n.srv.URL, tok)
}

func writeNode(w http.ResponseWriter, status int, v any) {
	if status >= 300 {
		w.Header().Set("Content-Type", "application/problem+json")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestInfoVersionMismatchIsErrorWithBothVersions(t *testing.T) {
	n := newFakeNode(t)
	n.version = 7
	var launched atomic.Int32
	n.mux.HandleFunc("PUT "+nodeapi.PathLaunch, func(w http.ResponseWriter, _ *http.Request) {
		launched.Add(1)
		writeNode(w, http.StatusOK, workplace.Carrier{})
	})
	c := n.client(t)

	_, err := c.Info(context.Background())
	if err == nil {
		t.Fatal("Info of a node with API version 7: no error")
	}
	if !strings.Contains(err.Error(), "version 7") || !strings.Contains(err.Error(), "version 1") {
		t.Errorf("error %q does not carry both versions 7 and 1", err)
	}
	_, err = c.Launch(context.Background(), randomID(t), nodeapi.LaunchRequest{SessionID: randomID(t)})
	if !errors.Is(err, workplace.ErrNotAccepted) {
		t.Errorf("Launch on a node of another API version: %v, want ErrNotAccepted", err)
	}
	if launched.Load() != 0 {
		t.Errorf("launch reached a node of another API version %d times", launched.Load())
	}
}

func TestInfoAskedOncePerClient(t *testing.T) {
	a, b := newFakeNode(t), newFakeNode(t)
	for _, n := range []*fakeNode{a, b} {
		n.mux.HandleFunc("DELETE "+nodeapi.PathCarrier, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	}
	ca, cb := a.client(t), b.client(t)
	for range 3 {
		if err := ca.DeleteCarrier(context.Background(), randomID(t)); err != nil {
			t.Fatalf("DeleteCarrier on node a: %v", err)
		}
	}
	if err := cb.DeleteCarrier(context.Background(), randomID(t)); err != nil {
		t.Fatalf("DeleteCarrier on node b: %v", err)
	}
	if got := a.infoCalls.Load(); got != 1 {
		t.Errorf("info asked %d times of node a by one client, want 1", got)
	}
	if got := b.infoCalls.Load(); got != 1 {
		t.Errorf("info asked %d times of node b, want 1", got)
	}
	// A new client (the next tick) asks again.
	if _, err := a.client(t).Info(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := a.infoCalls.Load(); got != 2 {
		t.Errorf("info asked %d times of node a after a new client, want 2", got)
	}
}

func TestTokenReadPerRequest(t *testing.T) {
	n := newFakeNode(t)
	c := n.client(t)
	if _, err := c.Info(context.Background()); err != nil {
		t.Fatalf("Info with the right token: %v", err)
	}
	if got := n.lastAuth.Load(); got != "Bearer "+n.token {
		t.Errorf("Authorization %q, want the token of the file", got)
	}
	n.mux.HandleFunc("DELETE "+nodeapi.PathCarrier, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	other := randomID(t)
	if err := os.WriteFile(c.TokenFile, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	err := c.DeleteCarrier(context.Background(), randomID(t))
	if got := n.lastAuth.Load(); got != "Bearer "+other {
		t.Errorf("Authorization %q after the file changed, want %q", got, "Bearer "+other)
	}
	var ne *NodeError
	if !errors.As(err, &ne) || ne.Status != http.StatusForbidden {
		t.Fatalf("DeleteCarrier with a wrong token: %v, want a *NodeError 403", err)
	}
	if !strings.Contains(err.Error(), n.srv.URL) {
		t.Errorf("auth error %q does not name the node %s", err, n.srv.URL)
	}
	if !errors.Is(err, workplace.ErrNotMeasurable) {
		t.Errorf("auth error %v is not ErrNotMeasurable", err)
	}
}

func TestMissingTokenFileIsError(t *testing.T) {
	n := newFakeNode(t)
	for name, write := range map[string]func(path string) error{
		"missing": func(string) error { return nil },
		"empty":   func(p string) error { return os.WriteFile(p, []byte(" \n"), 0o600) },
	} {
		t.Run(name, func(t *testing.T) {
			tok := filepath.Join(t.TempDir(), "node-token")
			if err := write(tok); err != nil {
				t.Fatal(err)
			}
			c := NewNodeClient(n.srv.URL, tok)
			before := n.infoCalls.Load()
			_, err := c.Info(context.Background())
			if err == nil {
				t.Fatal("Info with a token file that is " + name + ": no error")
			}
			if n.infoCalls.Load() != before {
				t.Error("the request reached the node without a token")
			}
			if !errors.Is(err, workplace.ErrNotMeasurable) {
				t.Errorf("%v is not ErrNotMeasurable", err)
			}
		})
	}
}

func TestOwnWork409BecomesOwnWorkError(t *testing.T) {
	n := newFakeNode(t)
	id := randomID(t)
	var got nodeapi.FollowRequest
	n.mux.HandleFunc("POST "+nodeapi.PathFollow, func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != id {
			t.Errorf("follow for %q, want %q", r.PathValue("id"), id)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		writeNode(w, http.StatusConflict, nodeapi.OwnWork{
			Status: 409, Code: nodeapi.CodeOwnWork, Detail: "own work",
			Workspace: workplace.Workspace{ID: id, Path: "/node/wt/" + id, Branch: "issue-9-x"},
			Commits:   2, Dirty: true,
		})
	})
	err := n.client(t).Follow(context.Background(), id, "issue-9-x", true)
	if got != (nodeapi.FollowRequest{Branch: "issue-9-x", Created: true}) {
		t.Errorf("follow body %+v", got)
	}
	var own *workplace.OwnWorkError
	if !errors.As(err, &own) {
		t.Fatalf("Follow on 409: %v, want *workplace.OwnWorkError", err)
	}
	want := workplace.OwnWorkError{Workspace: workplace.Workspace{ID: id, Path: "/node/wt/" + id, Branch: "issue-9-x"}, Commits: 2, Dirty: true}
	if *own != want {
		t.Errorf("OwnWorkError %+v, want %+v", *own, want)
	}
}

func TestFollowNoContentIsSuccess(t *testing.T) {
	n := newFakeNode(t)
	n.mux.HandleFunc("POST "+nodeapi.PathFollow, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	if err := n.client(t).Follow(context.Background(), randomID(t), "main", false); err != nil {
		t.Errorf("Follow answered 204: %v", err)
	}
}

func TestWorkspaceGoneCodeMaps(t *testing.T) {
	n := newFakeNode(t)
	n.mux.HandleFunc("POST "+nodeapi.PathKill, func(w http.ResponseWriter, _ *http.Request) {
		writeNode(w, http.StatusNotFound, nodeapi.Problem{Status: 404, Code: nodeapi.CodeWorkspaceGone, Detail: "gone"})
	})
	err := n.client(t).Kill(context.Background(), randomID(t), "default", []string{randomID(t)})
	var ne *NodeError
	if !errors.As(err, &ne) {
		t.Fatalf("Kill on 404 workspace_gone: %v, want *NodeError", err)
	}
	if ne.Status != http.StatusNotFound || ne.Code != nodeapi.CodeWorkspaceGone {
		t.Errorf("NodeError %+v, want 404 %s", ne, nodeapi.CodeWorkspaceGone)
	}
	if errors.Is(err, workplace.ErrGone) {
		t.Error("the client made workspace_gone ErrGone itself; the adapter decides with the hub")
	}
}

func TestUnreachableIsNotGone(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	tok := filepath.Join(t.TempDir(), "node-token")
	if err := os.WriteFile(tok, []byte(randomID(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	c := NewNodeClient("http://"+addr, tok)
	_, err = c.Sessions(context.Background(), randomID(t))
	if err == nil {
		t.Fatal("Sessions on an unreachable node: no error")
	}
	if errors.Is(err, workplace.ErrGone) {
		t.Errorf("unreachable is ErrGone: %v", err)
	}
	if !errors.Is(err, workplace.ErrNotMeasurable) {
		t.Errorf("unreachable is not ErrNotMeasurable: %v", err)
	}
	if !errors.Is(err, workplace.ErrNotAccepted) {
		t.Errorf("a refused dial is not ErrNotAccepted: %v", err)
	}
}

func TestServerErrorIsNotMeasurable(t *testing.T) {
	n := newFakeNode(t)
	n.mux.HandleFunc("GET "+nodeapi.PathGit, func(w http.ResponseWriter, _ *http.Request) {
		writeNode(w, http.StatusInternalServerError, nodeapi.Problem{Status: 500, Detail: "git failed"})
	})
	dirty, unpushed, err := n.client(t).Git(context.Background(), randomID(t), "")
	if err == nil || dirty || unpushed {
		t.Fatalf("Git on 500: %v %v %v, want an error", dirty, unpushed, err)
	}
	if errors.Is(err, workplace.ErrGone) || !errors.Is(err, workplace.ErrNotMeasurable) {
		t.Errorf("500: %v, want ErrNotMeasurable and never ErrGone", err)
	}
}

func TestGitSendsMergedHeadAndDecodes(t *testing.T) {
	n := newFakeNode(t)
	id, head := randomID(t), strings.Repeat("ab", 20)
	n.mux.HandleFunc("GET "+nodeapi.PathGit, func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != id || r.URL.Query().Get("merged_head") != head {
			t.Errorf("git for %q merged_head %q", r.PathValue("id"), r.URL.Query().Get("merged_head"))
		}
		writeNode(w, http.StatusOK, nodeapi.GitState{Dirty: false, Unpushed: true})
	})
	dirty, unpushed, err := n.client(t).Git(context.Background(), id, head)
	if err != nil || dirty || !unpushed {
		t.Errorf("Git: dirty %v unpushed %v err %v, want false true nil", dirty, unpushed, err)
	}
}

func TestPermission404IsNil(t *testing.T) {
	n := newFakeNode(t)
	sess := randomID(t)
	n.mux.HandleFunc("GET "+nodeapi.PathPermission, func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("session") == sess {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tool":"Bash"}`))
			return
		}
		writeNode(w, http.StatusNotFound, nodeapi.Problem{Status: 404, Code: nodeapi.CodeNoPermission, Detail: "none"})
	})
	c := n.client(t)
	data, err := c.Permission(context.Background(), randomID(t))
	if err != nil || data != nil {
		t.Errorf("Permission on 404: %q %v, want nil nil", data, err)
	}
	data, err = c.Permission(context.Background(), sess)
	if err != nil || string(data) != `{"tool":"Bash"}` {
		t.Errorf("Permission: %q %v, want the raw bytes", data, err)
	}
}

func TestHTTPScheme(t *testing.T) {
	var hit atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit.Add(1) }))
	t.Cleanup(other.Close)
	n := newFakeNode(t)
	n.mux.HandleFunc("GET "+nodeapi.PathMarkers, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	})
	_, err := n.client(t).Markers(context.Background())
	if err == nil {
		t.Fatal("Markers answered with a redirect: no error")
	}
	if hit.Load() != 0 {
		t.Error("the client followed a redirect to another server")
	}
}

func TestTranscriptReadsTarWithPAX(t *testing.T) {
	n := newFakeNode(t)
	id, sess := randomID(t), randomID(t)
	var query string
	n.mux.HandleFunc("GET "+nodeapi.PathTranscript, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		if r.PathValue("session") != sess {
			writeNode(w, http.StatusNotFound, nodeapi.Problem{Status: 404, Code: nodeapi.CodeNoTranscript, Detail: "none"})
			return
		}
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		for _, m := range []struct {
			name, data, size, from string
		}{
			{sess + ".jsonl", "line2\n", "12", "6"},
			{sess + "/subagents/a.jsonl", "all\n", "4", "0"},
		} {
			_ = tw.WriteHeader(&tar.Header{
				Name: m.name, Mode: 0o600, Size: int64(len(m.data)), Typeflag: tar.TypeReg, Format: tar.FormatPAX,
				PAXRecords: map[string]string{nodeapi.PAXSize: m.size, nodeapi.PAXFrom: m.from},
			})
			_, _ = tw.Write([]byte(m.data))
		}
		_ = tw.Close()
		w.Header().Set("Content-Type", "application/x-tar")
		_, _ = w.Write(buf.Bytes())
	})
	c := n.client(t)
	got, err := c.Transcript(context.Background(), id, sess, "default",
		map[string]int64{sess + ".jsonl": 6, sess + "/subagents/a.jsonl": 9})
	if err != nil {
		t.Fatal(err)
	}
	want := []workplace.FileDelta{
		{Name: sess + ".jsonl", Size: 12, From: 6, Data: []byte("line2\n")},
		{Name: sess + "/subagents/a.jsonl", Size: 4, From: 0, Data: []byte("all\n")},
	}
	if len(got) != len(want) {
		t.Fatalf("deltas %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].Size != want[i].Size || got[i].From != want[i].From || !bytes.Equal(got[i].Data, want[i].Data) {
			t.Errorf("delta %d %+v, want %+v", i, got[i], want[i])
		}
	}
	// Files sorted by name: "." sorts before "/".
	wantQuery := "from=" + sess + ".jsonl%3A6%2C" + sess + "%2Fsubagents%2Fa.jsonl%3A9&profile=default"
	if query != wantQuery {
		t.Errorf("query %q, want %q", query, wantQuery)
	}

	_, err = c.Transcript(context.Background(), id, randomID(t), "default", nil)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("no_transcript: %v, want fs.ErrNotExist", err)
	}
	var ne *NodeError
	if !errors.As(err, &ne) || ne.Code != nodeapi.CodeNoTranscript {
		t.Errorf("no_transcript: %v, want a *NodeError with code %s", err, nodeapi.CodeNoTranscript)
	}
}

func TestTranscriptMemberWithoutPAXIsError(t *testing.T) {
	n := newFakeNode(t)
	n.mux.HandleFunc("GET "+nodeapi.PathTranscript, func(w http.ResponseWriter, _ *http.Request) {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		_ = tw.WriteHeader(&tar.Header{Name: "x.jsonl", Mode: 0o600, Size: 1, Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte("x"))
		_ = tw.Close()
		_, _ = w.Write(buf.Bytes())
	})
	_, err := n.client(t).Transcript(context.Background(), randomID(t), randomID(t), "default", nil)
	if !errors.Is(err, workplace.ErrNotMeasurable) {
		t.Errorf("member without PAX records: %v, want ErrNotMeasurable", err)
	}
}

func TestSessionsDecodes(t *testing.T) {
	n := newFakeNode(t)
	id, sid := randomID(t), randomID(t)
	n.mux.HandleFunc("GET "+nodeapi.PathSessions, func(w http.ResponseWriter, _ *http.Request) {
		writeNode(w, http.StatusOK, []nodeapi.Session{
			{Key: "k1", State: "running", SessionID: sid, UpdatedAt: "2026-11-15T10:00:00Z"},
			{Key: "k2", State: "idle", UpdatedAt: ""},
		})
	})
	got, err := n.client(t).Sessions(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("sessions %+v", got)
	}
	if got[0].Key != "k1" || got[0].WorkspaceID != id || got[0].SessionID != sid || got[0].State != "running" ||
		got[0].UpdatedAt.UTC().Format("2006-01-02T15:04:05Z") != "2026-11-15T10:00:00Z" {
		t.Errorf("session 0 %+v", got[0])
	}
	if got[1].WorkspaceID != id || !got[1].UpdatedAt.IsZero() {
		t.Errorf("session 1 %+v, want workspace %s and zero UpdatedAt", got[1], id)
	}
}

func TestRelocate422IsError(t *testing.T) {
	n := newFakeNode(t)
	n.mux.HandleFunc("POST "+nodeapi.PathRelocate, func(w http.ResponseWriter, r *http.Request) {
		var in nodeapi.Relocation
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Command == "bad" {
			writeNode(w, http.StatusUnprocessableEntity, nodeapi.Problem{Status: 422, Detail: "cannot relocate"})
			return
		}
		writeNode(w, http.StatusOK, nodeapi.Relocation{Command: "cd /node/wt && " + in.Command})
	})
	c := n.client(t)
	got, err := c.Relocate(context.Background(), randomID(t), "claude")
	if err != nil || got != "cd /node/wt && claude" {
		t.Errorf("Relocate: %q %v", got, err)
	}
	if _, err := c.Relocate(context.Background(), randomID(t), "bad"); err == nil || !strings.Contains(err.Error(), "cannot relocate") {
		t.Errorf("Relocate on 422: %v, want an error with the detail", err)
	}
}

package nodeserve

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/nodeapi"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

const transcriptSession = "3e7a1c52-8b4d-4f6e-a0c9-5d2b7e8f1a36"

// transcriptPath is GET transcript of session in workspace id with the raw query values.
func transcriptPath(id, session string, q url.Values) string {
	return nodeapi.Prefix + "/workspaces/" + id + "/transcript/" + session + "?" + q.Encode()
}

// writeTranscript writes name, relative to the project dir of worktree under home, with content.
func writeTranscript(t *testing.T, home, worktree, name, content string) {
	t.Helper()
	p := filepath.Join(workplace.ProjectDir(home, worktree), filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

type tarMember struct {
	size, from string
	data       string
}

func readTar(t *testing.T, r io.Reader) map[string]tarMember {
	t.Helper()
	got := map[string]tarMember{}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return got
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		got[h.Name] = tarMember{size: h.PAXRecords["LIGHTHOUSE.size"], from: h.PAXRecords["LIGHTHOUSE.from"], data: string(data)}
	}
}

func TestTranscriptRouteTarHeadersCarrySizeAndFrom(t *testing.T) {
	ws := gitRepo(t)
	f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)
	main := transcriptSession + ".jsonl"
	sub := transcriptSession + "/subagents/agent-a1.jsonl"
	writeTranscript(t, f.h.d.Home, ws, main, "0123456789\n")
	writeTranscript(t, f.h.d.Home, ws, sub, "abc\n")
	writeTranscript(t, f.h.d.Home, ws, "9d8c7b6a-5f4e-4d3c-8b2a-1f0e9d8c7b6a.jsonl", "another session\n")

	rec := call(f.h, transcriptPath("ws-1", transcriptSession, url.Values{
		"profile": {"default"},
		"from":    {main + ":4," + sub + ":40"},
	}), bearer)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-tar" {
		t.Errorf("Content-Type = %q, want application/x-tar", ct)
	}
	got := readTar(t, rec.Body)
	want := map[string]tarMember{
		main: {size: "11", from: "4", data: "456789\n"},
		sub:  {size: "4", from: "0", data: "abc\n"}, // shorter than 40: the whole file
	}
	if len(got) != len(want) {
		t.Errorf("members = %v, want only %s and %s", got, main, sub)
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %+v, want %+v", name, got[name], w)
		}
	}
}

func TestTranscriptRouteNoFileIs404NoTranscript(t *testing.T) {
	cases := map[string]func(t *testing.T, home, ws string){
		"no project dir": func(*testing.T, string, string) {},
		"no main transcript": func(t *testing.T, home, ws string) {
			writeTranscript(t, home, ws, transcriptSession+"/subagents/agent-a1.jsonl", "abc\n")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			ws := gitRepo(t)
			f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)
			setup(t, f.h.d.Home, ws)
			rec := call(f.h, transcriptPath("ws-1", transcriptSession, url.Values{"profile": {"default"}}), bearer)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
			}
			if p := problem(t, rec); p.Code != nodeapi.CodeNoTranscript {
				t.Errorf("code = %q, want %q", p.Code, nodeapi.CodeNoTranscript)
			}
		})
	}
}

func TestTranscriptRouteGoneWorkspaceIs404WorkspaceGone(t *testing.T) {
	f := newFixture(t, nil, nodeToken)
	rec := call(f.h, transcriptPath("ws-9", transcriptSession, url.Values{"profile": {"default"}}), bearer)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if p := problem(t, rec); p.Code != nodeapi.CodeWorkspaceGone {
		t.Errorf("code = %q, want %q", p.Code, nodeapi.CodeWorkspaceGone)
	}
}

func TestTranscriptRouteUnknownProfileIs400(t *testing.T) {
	ws := gitRepo(t)
	f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)
	writeTranscript(t, f.h.d.Home, ws, transcriptSession+".jsonl", "x\n")
	for _, profile := range []string{"", "software", "Default"} {
		rec := call(f.h, transcriptPath("ws-1", transcriptSession, url.Values{"profile": {profile}}), bearer)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("profile %q: status = %d, want 400: %s", profile, rec.Code, rec.Body.String())
		}
	}
	if n := f.forge.hits.Load(); n != 0 {
		t.Errorf("forge hits = %d, want none before the profile check", n)
	}
	if n := f.runner.calls.Load(); n != 0 {
		t.Errorf("runner calls = %d, want none before the profile check", n)
	}
}

func TestTranscriptRouteBadRequestBeforeAnyCall(t *testing.T) {
	ws := gitRepo(t)
	f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)
	main := transcriptSession + ".jsonl"
	cases := map[string]string{
		"unsafe session": transcriptPath("ws-1", "a.b", url.Values{"profile": {"default"}}),
		"offset no int":  transcriptPath("ws-1", transcriptSession, url.Values{"profile": {"default"}, "from": {main + ":x"}}),
		"negative":       transcriptPath("ws-1", transcriptSession, url.Values{"profile": {"default"}, "from": {main + ":-1"}}),
		"no colon":       transcriptPath("ws-1", transcriptSession, url.Values{"profile": {"default"}, "from": {main}}),
		"local id":       transcriptPath("local-1", transcriptSession, url.Values{"profile": {"default"}}),
	}
	for name, path := range cases {
		rec := call(f.h, path, bearer)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", name, rec.Code, rec.Body.String())
		}
	}
	if n := f.forge.hits.Load(); n != 0 {
		t.Errorf("forge hits = %d, want none", n)
	}
}

// With the software profile the route reads in the devcontainers, never the host transcript:
// no container holds the session here, so it is 404 no_transcript although the host has one.
func TestTranscriptRouteSoftwareReadsTheContainer(t *testing.T) {
	ws := gitRepo(t)
	f := newFixture(t, map[string]string{"ws-1": ws}, nodeToken)
	f.h.d.Profiles["software"] = true
	writeTranscript(t, f.h.d.Home, ws, transcriptSession+".jsonl", "host bytes\n")
	var podman atomic.Int32
	f.h.adapter.Run = func(ctx context.Context, timeout time.Duration, name string, args ...string) (string, int, error) {
		if name == "podman" {
			podman.Add(1)
			return "", 0, nil // podman ps: no devcontainer
		}
		return workplace.ExecRunner()(ctx, timeout, name, args...)
	}

	rec := call(f.h, transcriptPath("ws-1", transcriptSession, url.Values{"profile": {"software"}}), bearer)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if p := problem(t, rec); p.Code != nodeapi.CodeNoTranscript {
		t.Errorf("code = %q, want %q", p.Code, nodeapi.CodeNoTranscript)
	}
	if podman.Load() == 0 {
		t.Error("podman never ran, want the software branch to look in the devcontainers")
	}
	if f.h.adapter.Profile != "" {
		t.Errorf("shared adapter profile = %q, want it untouched", f.h.adapter.Profile)
	}
}

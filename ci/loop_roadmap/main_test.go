package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakeGH struct {
	open      map[int]string
	closed    map[int]bool
	touched   map[string]bool
	openErr   error
	closedErr error
	openCalls int
	repos     []string
}

func (f *fakeGH) OpenLoopIssues(repo string) (map[int]string, error) {
	f.openCalls++
	f.repos = append(f.repos, repo)
	out := map[int]string{}
	for k, v := range f.open {
		out[k] = v
	}
	return out, f.openErr
}

func (f *fakeGH) ClosedLoopIssues(repo string, pr int) (map[int]bool, error) {
	if pr != 5 {
		return nil, errors.New("unexpected PR")
	}
	return f.closed, f.closedErr
}

func (f *fakeGH) TouchedFiles(string, int) (map[string]bool, error) { return f.touched, nil }

func writeMap(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "docs", "site", "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "loop.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

const planned7 = `{"chips":[{"id":"b","status":"planned","issue":7}],"handoffs":[]}`
const works = `{"chips":[{"id":"b","status":"works"}],"handoffs":[]}`

func runIt(t *testing.T, args []string, root string, gh ghClient) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, func(string) string { return "" }, gh, root, &out, &errb)
	return code, out.String(), errb.String()
}

func TestRunMainMode(t *testing.T) {
	tests := []struct {
		name     string
		mapJSON  string
		gh       *fakeGH
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{"ok", planned7, &fakeGH{open: map[int]string{7: ""}}, 0,
			"loop roadmap: 1 open loop issues, planned items all open, milestoned issues all on the map\n", ""},
		{"verdict", works, &fakeGH{open: map[int]string{9: "M2"}}, 1,
			"#9: open issue labelled loop on milestone M2 is not on the map (see docs/developer/labels.md)\n", ""},
		{"broken map", `{not json`, &fakeGH{}, 2, "", "COULD NOT CHECK:"},
		{"chip without id", `{"chips":[{"status":"works"}],"handoffs":[]}`, &fakeGH{}, 2, "", "COULD NOT CHECK:"},
		{"gh failure", works, &fakeGH{openErr: errors.New("boom")}, 2, "", "COULD NOT CHECK: boom\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errb := runIt(t, nil, writeMap(t, tt.mapJSON), tt.gh)
			if code != tt.wantCode || out != tt.wantOut || !strings.HasPrefix(errb, tt.wantErr) || (tt.wantErr == "" && errb != "") {
				t.Errorf("code=%d out=%q err=%q; want code=%d out=%q err prefix %q", code, out, errb, tt.wantCode, tt.wantOut, tt.wantErr)
			}
		})
	}
}

func TestRunMissingMapIsCouldNotCheck(t *testing.T) {
	code, out, errb := runIt(t, nil, t.TempDir(), &fakeGH{})
	if code != 2 || out != "" || !strings.HasPrefix(errb, "COULD NOT CHECK:") {
		t.Errorf("code=%d out=%q err=%q", code, out, errb)
	}
}

func TestRunPRMode(t *testing.T) {
	mapTouched := map[string]bool{"docs/site/data/loop.json": true}
	tests := []struct {
		name         string
		mapJSON      string
		gh           *fakeGH
		wantCode     int
		wantOut      string
		wantErr      string
		wantOpenCall int
	}{
		{"closing no loop issue, nothing touched", planned7,
			&fakeGH{closed: map[int]bool{}, touched: map[string]bool{"ci/x.py": true}}, 0,
			"loop roadmap: PR #5 closes no loop issue\n", "", 0},
		{"touching the map also runs the main check", planned7,
			&fakeGH{closed: map[int]bool{}, touched: mapTouched, open: map[int]string{9: ""}}, 1,
			"b: #7 is not an open issue labelled loop (see docs/developer/labels.md)\n", "", 1},
		{"touching a Go file of the command also runs the main check", planned7,
			&fakeGH{closed: map[int]bool{}, touched: map[string]bool{"ci/loop_roadmap/roadmap.go": true}, open: map[int]string{9: ""}}, 1,
			"b: #7 is not an open issue labelled loop (see docs/developer/labels.md)\n", "", 1},
		{"touching the Python script does not", planned7,
			&fakeGH{closed: map[int]bool{}, touched: map[string]bool{"ci/loop_roadmap.py": true}}, 0,
			"loop roadmap: PR #5 closes no loop issue\n", "", 0},
		{"verdict", planned7,
			&fakeGH{closed: map[int]bool{7: true}, touched: mapTouched, open: map[int]string{7: ""}}, 1,
			"b: still planned, but this PR closes #7; set status works and drop issue (see docs/developer/labels.md)\n" +
				"b: #7 is not an open issue labelled loop (see docs/developer/labels.md)\n", "", 1},
		{"closing a milestoned issue is green", works,
			&fakeGH{closed: map[int]bool{7: true}, touched: mapTouched, open: map[int]string{7: "M2"}}, 0,
			"loop roadmap: PR #5 closes 1 loop issues, map in step\n", "", 1},
		{"gh failure", works, &fakeGH{closedErr: errors.New("boom")}, 2, "", "COULD NOT CHECK: boom\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errb := runIt(t, []string{"--pr", "5"}, writeMap(t, tt.mapJSON), tt.gh)
			if code != tt.wantCode || out != tt.wantOut || errb != tt.wantErr || tt.gh.openCalls != tt.wantOpenCall {
				t.Errorf("code=%d out=%q err=%q openCalls=%d; want code=%d out=%q err=%q openCalls=%d",
					code, out, errb, tt.gh.openCalls, tt.wantCode, tt.wantOut, tt.wantErr, tt.wantOpenCall)
			}
		})
	}
}

func TestRunRepoFromEnvWithDefault(t *testing.T) {
	root := writeMap(t, works)
	for env, want := range map[string]string{"": "CorneeldH/lighthouse", "o/r": "o/r"} {
		gh := &fakeGH{}
		var out, errb bytes.Buffer
		run(nil, func(k string) string {
			if k == "GITHUB_REPOSITORY" {
				return env
			}
			return ""
		}, gh, root, &out, &errb)
		if !reflect.DeepEqual(gh.repos, []string{want}) {
			t.Errorf("env %q: repos = %v, want [%s]", env, gh.repos, want)
		}
	}
}

func TestRunUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"extra"}, {"--bogus"}, {"--pr", "x"}} {
		code, out, errb := runIt(t, args, writeMap(t, works), &fakeGH{})
		if code != 2 || out != "" || errb == "" {
			t.Errorf("args %v: code=%d out=%q err=%q", args, code, out, errb)
		}
	}
}

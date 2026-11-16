package pins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

const checkToml = `
[pins.superpowers]
kind = "plugin"
repo = "obra/superpowers"
version = "7.0.2"

[pins.mytool]
kind = "tool"
repo = "o/mytool"
version = "0.5.1"
`

func repoWithToml(t *testing.T, toml string) string {
	t.Helper()
	root := t.TempDir()
	if toml != "" {
		if err := os.WriteFile(filepath.Join(root, "lighthouse.toml"), []byte(toml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// failingGit and failingReleases fail the test on any call: a local-only run must not reach them.
type failingGit struct{ t *testing.T }

func (f failingGit) Clone(context.Context, string) (Clone, error) {
	f.t.Error("GitSource.Clone called")
	return nil, errors.New("network")
}

type failingReleases struct{ t *testing.T }

func (f failingReleases) Releases(context.Context, string) ([]forge.Release, error) {
	f.t.Error("ReleaseReader.Releases called")
	return nil, errors.New("network")
}

func localEnv(t *testing.T, root, installedVersion string) Env {
	t.Helper()
	installed := writeInstalled(t, map[string][]pluginEntry{
		"superpowers@m": {{Scope: "user", Version: installedVersion}},
	})
	return Env{
		Root: root, InstalledPlugins: installed,
		Git: failingGit{t}, Releases: failingReleases{t},
		Run: fakeRunner("mytool 0.5.1\n", nil),
	}
}

func byKey(rs []PinResult, key string) PinResult {
	for _, r := range rs {
		if r.Key == key {
			return r
		}
	}
	return PinResult{}
}

// Fails when Check merges the two measurements into one status.
func TestCheck_statusesNeverMerged(t *testing.T) {
	root := repoWithToml(t, checkToml)
	env := localEnv(t, root, "7.0.2")
	env.Releases = fakeReleases{err: errors.New("boom")}

	rs := Check(context.Background(), env, CheckOptions{})
	tool := byKey(rs, "mytool")
	if tool.Upstream == nil || tool.Upstream.Status != StatusError {
		t.Fatalf("upstream = %+v, want error", tool.Upstream)
	}
	if tool.Local == nil || tool.Local.Status != StatusCurrent {
		t.Fatalf("local = %+v, want current", tool.Local)
	}
	var text bytes.Buffer
	RenderText(&text, rs)
	for _, want := range []string{"mytool upstream error ", "mytool lokaal current "} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text lacks %q:\n%s", want, text.String())
		}
	}
	var js bytes.Buffer
	if err := RenderJSON(&js, rs); err != nil {
		t.Fatal(err)
	}
	var got []struct {
		Key             string
		Upstream, Local *struct{ Status string }
	}
	if err := json.Unmarshal(js.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range got {
		if o.Key == "mytool" {
			found = true
			if o.Upstream == nil || o.Upstream.Status != "error" || o.Local == nil || o.Local.Status != "current" {
				t.Errorf("json mytool = %+v %+v, want upstream error and local current", o.Upstream, o.Local)
			}
		}
	}
	if !found {
		t.Errorf("mytool missing from json:\n%s", js.String())
	}
	if !tool.HasError() {
		t.Error("HasError false with an upstream error")
	}
}

// Fails when the local measure touches the network.
func TestCheck_localOnlyMakesNoNetworkCalls(t *testing.T) {
	root := repoWithToml(t, checkToml)
	// A skill pin must not be cloned either.
	writeFile(t, root, "pack/core/skills/s/claude/SKILL.md", skillDoc(upstreamMeta("https://github.com/o/r", strings.Repeat("a", 40), "skills/s")))
	env := localEnv(t, root, "7.0.1")

	rs := Check(context.Background(), env, CheckOptions{Local: true})
	if len(rs) != 2 {
		t.Fatalf("got %d results, want 2 (the skill pin has no local result): %+v", len(rs), rs)
	}
	if sp := byKey(rs, "superpowers"); sp.Upstream != nil || sp.Local == nil || sp.Local.Status != StatusBehind {
		t.Errorf("superpowers = %+v, want only a behind local result", sp)
	}
}

// Review Focus 2: no [pins] section and no lighthouse.toml are not a failure.
func TestCheck_noPinsExitsZero(t *testing.T) {
	for name, toml := range map[string]string{"no section": "[project]\nlanguage = \"nl\"\n", "no file": ""} {
		t.Run(name, func(t *testing.T) {
			root := repoWithToml(t, toml)
			env := localEnv(t, root, "7.0.2")
			rs := Check(context.Background(), env, CheckOptions{Local: true})
			for _, r := range rs {
				if r.HasError() {
					t.Errorf("%s: %+v", r.Key, r)
				}
			}
			if len(rs) != 0 {
				t.Errorf("got %d results, want none", len(rs))
			}
		})
	}
}

// Fails when a pin that did not load, or a lighthouse.toml that does not parse, is swallowed.
func TestCheck_brokenPinIsError(t *testing.T) {
	root := repoWithToml(t, "[pins.nover]\nkind = \"tool\"\nrepo = \"o/r\"\n")
	rs := Check(context.Background(), localEnv(t, root, "7.0.2"), CheckOptions{Local: true})
	if p := byKey(rs, "nover"); p.Local == nil || p.Local.Status != StatusError {
		t.Errorf("pin without version = %+v, want local error", p)
	}
	root = repoWithToml(t, "[pins\n")
	rs = Check(context.Background(), localEnv(t, root, "7.0.2"), CheckOptions{Local: true})
	if len(rs) != 1 || !rs[0].HasError() {
		t.Errorf("unparsable toml = %+v, want one error", rs)
	}
}

func TestRenderJSON_shape(t *testing.T) {
	rs := []PinResult{
		{Key: "a", Kind: "tool", Upstream: &Result{Status: StatusBehind, Detail: "d"}},
		{Key: "b", Kind: "skill", Upstream: &Result{Status: StatusCurrent}, Local: &Result{Status: StatusAhead, Detail: "x"}},
	}
	var buf bytes.Buffer
	if err := RenderJSON(&buf, rs); err != nil {
		t.Fatal(err)
	}
	var got []map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d objects", len(got))
	}
	if _, ok := got[0]["local"]; ok {
		t.Error("a measurement that did not run must be absent")
	}
	if _, ok := got[1]["upstream"]; !ok {
		t.Error("upstream missing")
	}
	var up struct{ Status, Detail string }
	if err := json.Unmarshal(got[1]["local"], &up); err != nil || up.Status != "ahead" || up.Detail != "x" {
		t.Errorf("local = %s (%v)", got[1]["local"], err)
	}
	for _, k := range []string{"key", "kind"} {
		if _, ok := got[0][k]; !ok {
			t.Errorf("%s missing", k)
		}
	}
	if strings.Contains(buf.String(), "Commits") || strings.Contains(buf.String(), "Releases") {
		t.Errorf("json leaks lane-only fields: %s", buf.String())
	}
}

func TestDrift_dropsCurrent(t *testing.T) {
	rs := []PinResult{
		{Key: "a", Kind: "tool", Local: &Result{Status: StatusCurrent}},
		{Key: "b", Kind: "tool", Local: &Result{Status: StatusBehind, Detail: "d"}},
	}
	got := Drift(rs)
	if len(got) != 1 || got[0].Key != "b" {
		t.Errorf("Drift = %+v", got)
	}
}

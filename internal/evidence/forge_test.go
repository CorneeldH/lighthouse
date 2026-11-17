//go:build !windows

package evidence

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/prbody"
)

func lh(args ...string) []string { return append([]string{"lighthouse", "forge"}, args...) }

func TestCheckLighthouseReadForms(t *testing.T) {
	for _, args := range [][]string{
		lh("issue", "view", "12"),
		lh("issue", "view", "12", "--comments"),
		lh("issue", "view", "12", "--field", "milestone.title"),
		lh("issue", "view", "12", "--field=milestone.title"),
		lh("issue", "list"),
		lh("issue", "list", "--state", "closed", "--label", "bug", "--milestone", "v1", "--field", "0.number"),
		lh("issue", "search", "needle", "--field", "0.title"),
		lh("issue", "blockers", "7", "--field", "0.state"),
		lh("pr", "view", "9", "--field", "state"),
		lh("milestone", "list"),
		lh("milestone", "list", "--state", "all", "--field", "0.title"),
		lh("milestone", "view", "v1", "--field", "number"),
		lh("label", "list"),
		lh("label", "list", "--field", "0"),
		lh("api", "repos/x/y/issues/1"),
		lh("api", "-X", "GET", "repos/x/y"),
		lh("api", "repos/x/y", "-X", "GET"),
	} {
		if got := Check(args, "tests"); got != "" {
			t.Errorf("Check(%q) = %q, want allowed", args, got)
		}
	}
}

func TestCheckLighthouseDeniesWrites(t *testing.T) {
	for _, args := range [][]string{
		lh("issue", "create", "--title", "t", "--body-file", "f"),
		lh("issue", "edit", "1", "--title", "t"),
		lh("issue", "comment", "1", "--body-file", "f"),
		lh("issue", "block", "1", "--by", "2"),
		lh("issue", "status", "1", "Done"),
		lh("pr", "create", "--title", "t", "--body-file", "f"),
		lh("pr", "comment", "1", "--body-file", "f"),
		lh("milestone", "create", "--title", "t"),
		lh("milestone", "edit", "v1", "--title", "t"),
	} {
		if got := Check(args, "tests"); got == "" {
			t.Errorf("Check(%q) allowed a write form", args)
		}
	}
}

func TestCheckLighthouseAPIGetOnly(t *testing.T) {
	for _, args := range [][]string{
		lh("api", "repos/x", "-X", "POST"),
		lh("api", "-X", "POST", "repos/x"),
		lh("api", "-XPOST", "repos/x"),
		lh("api", "-XGET", "repos/x"),
		lh("api", "-X", "get", "repos/x"),
		lh("api", "--method", "POST", "repos/x"),
		lh("api", "repos/x", "-f", "a=b"),
		lh("api", "repos/x", "-F", "a=b"),
		lh("api", "repos/x", "-fa=b"),
		lh("api", "-X", "GET", "repos/x", "-f", "a=b"),
		lh("api", "graphql"),
		lh("api", "graphql", "-F", "query={viewer{login}}"),
		lh("api", "graphql", "-f", "query=mutation{x}"),
		lh("api"),
		lh("api", "a", "b"),
		lh("api", "-X", "GET"),
		lh("api", "-X", "GET", "-X", "GET", "repos/x"),
		lh("api", "https://evil.example/x"),
		lh("api", "//evil.example/x"),
		lh("api", "--", "repos/x"),
		lh("api", "--jq", ".", "repos/x"),
	} {
		if got := Check(args, "tests"); got == "" {
			t.Errorf("Check(%q) allowed", args)
		}
	}
}

func TestCheckLighthouseOtherSubcommand(t *testing.T) {
	for _, args := range [][]string{
		{"lighthouse"},
		{"lighthouse", "evidence", "run"},
		{"lighthouse", "serve"},
		{"lighthouse", "--version"},
		{"lighthouse", "-C", "x", "forge", "issue", "view", "1"},
		{"lighthouse", "forge"},
		lh("merge", "1"),
		lh("issue"),
		lh("issue", "frob", "1"),
		lh("issue", "view"),
		lh("issue", "view", "1", "2"),
		lh("issue", "view", "1", "--body-file", "f"),
		lh("issue", "view", "1", "--repo", "x/y"),
		lh("issue", "view", "1", "--comments=false"),
		lh("issue", "view", "1", "-h"),
		lh("issue", "view", "1", "--field"),
		lh("issue", "view", "1", "--field="),
		lh("issue", "view", "1", "--"),
		lh("issue", "list", "5"),
		lh("issue", "list", "--add-label", "x"),
		lh("issue", "search"),
		lh("issue", "blockers", "1", "--by", "2"),
		lh("pr", "view", "1", "--comments"),
		lh("pr", "list"),
		lh("milestone", "list", "--label", "x"),
		lh("milestone", "view"),
		lh("label", "list", "x"),
		lh("label", "create", "x"),
		lh("--help"),
	} {
		if got := Check(args, "tests"); got == "" {
			t.Errorf("Check(%q) allowed", args)
		}
	}
}

const lhForgeStub = "#!/bin/sh\ntouch \"$LH_MARKER\"\ncase \"$*\" in\n  \"forge api user\") [ -n \"$LH_PROBE_FAIL\" ] && exit 1; echo u; exit 0 ;;\nesac\necho \"${LH_STUB_OUT:-y}\"\n"

func TestRunRowForgeUnreachableIsUnmeasurable(t *testing.T) {
	withConfig(t, "")
	marker := filepath.Join(t.TempDir(), "lh-called")
	t.Setenv("LH_MARKER", marker)
	stubBin(t, map[string]string{"lighthouse": lhForgeStub})
	o, d := runOne(t, "`lighthouse forge issue view 1`", "y", t.TempDir(), false, defaultTimeout)
	want(t, o, d, Unmeasurable, "forge not reachable")
	if exists(marker) {
		t.Error("lighthouse ran although the forge is not reachable")
	}
}

func TestRunsForgeCountsForProbe(t *testing.T) {
	withConfig(t, "")
	row := func(cmd, res string) prbody.Row { return prbody.Row{AC: "1", Command: cmd, Result: res} }
	for _, tc := range []struct {
		name     string
		row      prbody.Row
		beforePR bool
		want     bool
	}{
		{"forge row", row("`lighthouse forge issue view 1`", "x"), false, true},
		{"forge row before PR", row("`lighthouse forge pr view 1`", "`na PR: x`"), true, false},
		{"forge write is invalid", row("`lighthouse forge issue create --title t`", "x"), false, false},
		{"other lighthouse", row("`lighthouse evidence run`", "x"), false, false},
		{"grep", row("`grep -F a f`", "x"), false, false},
	} {
		got, err := runsGH(tc.row, tc.beforePR)
		if err != nil || got != tc.want {
			t.Errorf("%s: runsGH = %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
}

func runForgeBody(t *testing.T, config, command string, probeFail bool) (int, string, string) {
	t.Helper()
	withConfig(t, config)
	dir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "lh-called")
	t.Setenv("LH_MARKER", marker)
	if probeFail {
		t.Setenv("LH_PROBE_FAIL", "1")
	}
	ghMarker := filepath.Join(t.TempDir(), "gh-called")
	t.Setenv("GH_MARKER", ghMarker)
	stubBin(t, map[string]string{"lighthouse": lhForgeStub, "gh": ghStub})
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	writeFile(t, bodyFile, body(5, []row3{{"1. een", command, "y"}}), 0o644)
	var out bytes.Buffer
	rc := Run(5, bodyFile, dir, false, &out)
	return rc, out.String(), ghMarker
}

func TestRunForgeRowOnGitLabProbesThroughForgeAPI(t *testing.T) {
	t.Setenv("LH_STUB_OUT", "y")
	rc, out, gh := runForgeGitLab(t, false)
	if rc != 0 || !strings.Contains(out, "| 1. een | holds |") {
		t.Errorf("rc %d, stdout:\n%s", rc, out)
	}
	if exists(gh) {
		t.Error("gh was used on a GitLab repo")
	}
	rc, out, _ = runForgeGitLab(t, true)
	if rc != 4 || !strings.Contains(out, "| 1. een | not measurable | forge not reachable |") {
		t.Errorf("probe failure: rc %d, stdout:\n%s", rc, out)
	}
}

func runForgeGitLab(t *testing.T, probeFail bool) (int, string, string) {
	t.Helper()
	useHeadings(t)
	path := filepath.Join(t.TempDir(), "lighthouse.toml")
	writeFile(t, path, "[forge]\nkind = \"gitlab\"\nrepo = \"acme/widgets\"\n[project]\nlanguage = \"en\"\n", 0o644)
	t.Setenv("LIGHTHOUSE_TOML", path)
	marker := filepath.Join(t.TempDir(), "lh-called")
	t.Setenv("LH_MARKER", marker)
	t.Setenv("LH_PROBE_FAIL", map[bool]string{true: "1", false: ""}[probeFail])
	ghMarker := filepath.Join(t.TempDir(), "gh-called")
	t.Setenv("GH_MARKER", ghMarker)
	stubBin(t, map[string]string{"lighthouse": lhForgeStub, "gh": ghStub})
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	writeFile(t, bodyFile, body(5, []row3{{"1. een", "`lighthouse forge issue view 1`", "y"}}), 0o644)
	var out bytes.Buffer
	return Run(5, bodyFile, t.TempDir(), false, &out), out.String(), ghMarker
}

func TestRunForgeRowOnGitHubProbesThroughGH(t *testing.T) {
	rc, out, gh := runForgeBody(t, "[project]\nlanguage = \"en\"\n", "`lighthouse forge issue view 1`", true)
	if rc != 0 || !strings.Contains(out, "| 1. een | holds |") {
		t.Errorf("rc %d, stdout:\n%s", rc, out)
	}
	if !exists(gh) {
		t.Error("the GitHub probe did not go through gh")
	}
}

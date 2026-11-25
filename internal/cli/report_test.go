package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/testpath"
)

// reportZone is the local time of the clock; no machine runs in it, so a bound built in
// time.Local instead of the zone of the clock shows.
var reportZone = time.FixedZone("UTC+13", 13*3600)

// reportNow is Monday 16 November 2026, in ISO week 2026-W47.
var reportNow = time.Date(2026, 11, 16, 9, 30, 0, 0, reportZone)

// reportFake is the forge of one report run: every call of the four report sources is counted,
// the period bounds are kept, and err fails OpenPRsWithStatus, the last source Build reads.
type reportFake struct {
	forge.Forge
	title        string
	err          error
	calls        int
	since, until time.Time
}

func (f *reportFake) Kind() string { return "github" }

func (f *reportFake) MergedPRs(_ context.Context, since, until time.Time) ([]forge.PRSummary, error) {
	f.calls++
	f.since, f.until = since, until
	return []forge.PRSummary{{Number: 7, Title: f.title, URL: "https://github.com/o/r/pull/7", MergedAt: since}}, nil
}

func (f *reportFake) ClosedIssues(context.Context, time.Time, time.Time) ([]forge.Issue, error) {
	f.calls++
	return nil, nil
}

func (f *reportFake) MilestonedIssues(context.Context, time.Time, time.Time) ([]forge.MilestonedIssue, error) {
	f.calls++
	return nil, nil
}

func (f *reportFake) OpenPRsWithStatus(context.Context) ([]forge.PRSummary, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return nil, nil
}

// reportRig is an env around root whose Open hands out fake and counts the opens.
type reportRig struct {
	env   reportEnv
	fake  *reportFake
	opens int
	cfg   forge.Config
}

func newReportRig(root string) *reportRig {
	r := &reportRig{fake: &reportFake{title: "Samengevoegd"}}
	r.env = reportEnv{
		Getenv:   func(k string) string { return map[string]string{"GH_TOKEN": "tok"}[k] },
		Now:      func() time.Time { return reportNow },
		RepoRoot: func() (string, error) { return root, nil },
		GhToken:  func() string { return "" },
		Open: func(cfg forge.Config) (forge.Forge, error) {
			r.opens++
			r.cfg = cfg
			return r.fake, nil
		},
	}
	return r
}

// runReport runs `lighthouse report args` and returns the exit code, stdout and stderr.
func runReport(env reportEnv, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newReportCommand(env))
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append([]string{"report"}, args...))
	return execute(root, &errOut), out.String(), errOut.String()
}

const reportOn = "[forge]\nrepo = \"o/r\"\n\n[report]\nenabled = true\n"

func TestReportDisabledPrintsOneLineAndExits0(t *testing.T) {
	for name, toml := range map[string]string{
		"no [report]":   "[forge]\nrepo = \"o/r\"\n",
		"enabled false": "[forge]\nrepo = \"o/r\"\n\n[report]\nenabled = false\n",
		"no [forge]":    "[project]\nlanguage = \"nl\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			rig := newReportRig(repoDir(t, toml))
			code, out, errOut := runReport(rig.env, "week")
			if code != 0 {
				t.Fatalf("exit %d, stderr %q; want 0", code, errOut)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if strings.Count(errOut, "\n") != 1 || !strings.Contains(errOut, "[report] enabled = true") {
				t.Errorf("stderr = %q, want one line naming [report] enabled = true", errOut)
			}
			if rig.opens != 0 || rig.fake.calls != 0 {
				t.Errorf("opens = %d, calls = %d; want no forge", rig.opens, rig.fake.calls)
			}
		})
	}
}

func TestReportErrKinds(t *testing.T) {
	for name, tc := range map[string]struct{ report, key string }{
		"type error":         {"enabled = true\nsink = 1\n", "report.sink"},
		"unknown sink":       {"enabled = true\nsink = \"wiki\"\n", "report.sink"},
		"file without dir":   {"enabled = true\nsink = \"file\"\n", "report.dir"},
		"dir outside repo":   {"enabled = true\nsink = \"file\"\ndir = \"../elders\"\n", "report.dir"},
		"absolute dir":       {"enabled = true\nsink = \"file\"\ndir = '" + testpath.Abs("tmp", "verslagen") + "'\n", "report.dir"},
		"enabled not a bool": {"enabled = \"true\"\n", "report.enabled"},
	} {
		t.Run(name, func(t *testing.T) {
			root := repoDir(t, "[forge]\nrepo = \"o/r\"\n\n[report]\n"+tc.report)
			rig := newReportRig(root)
			code, out, errOut := runReport(rig.env, "week")
			if code == 0 {
				t.Fatalf("exit 0, want an error; stderr %q", errOut)
			}
			if !strings.Contains(errOut, tc.key) {
				t.Errorf("stderr = %q, want it to name %s", errOut, tc.key)
			}
			if out != "" || rig.opens != 0 {
				t.Errorf("stdout = %q, opens = %d; want nothing", out, rig.opens)
			}
		})
	}
}

func TestReportStdout(t *testing.T) {
	for name, tc := range map[string]struct{ project, title string }{
		"nl":      {"\n[project]\nlanguage = \"nl\"\n", "# Verslag week 2026-W47 · o/r\n"},
		"default": {"", "# Report week 2026-W47 · o/r\n"},
	} {
		t.Run(name, func(t *testing.T) {
			root := repoDir(t, reportOn+tc.project)
			rig := newReportRig(root)
			code, out, errOut := runReport(rig.env, "week")
			if code != 0 {
				t.Fatalf("exit %d, stderr %q", code, errOut)
			}
			if !strings.HasPrefix(out, tc.title) {
				t.Errorf("stdout starts %q, want %q", firstLine(out), tc.title)
			}
			if !strings.Contains(out, "- [#7](https://github.com/o/r/pull/7) Samengevoegd\n") {
				t.Errorf("stdout lacks the merged PR:\n%s", out)
			}
			if rig.cfg.Repo != "o/r" || rig.cfg.Token != "tok" {
				t.Errorf("opened %q with token %q, want o/r with tok", rig.cfg.Repo, rig.cfg.Token)
			}
			if entries, _ := os.ReadDir(root); len(entries) != 1 {
				t.Errorf("repo holds %d entries, want only lighthouse.toml", len(entries))
			}
		})
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

const reportFileSink = reportOn + "sink = \"file\"\ndir = \"verslagen\"\n"

func TestReportFileSink(t *testing.T) {
	for _, tc := range []struct{ kind, path, title string }{
		{"week", "verslagen/week/2026-W47.md", "# Report week 2026-W47 · o/r\n"},
		{"day", "verslagen/day/2026-11-16.md", "# Report day 2026-11-16 · o/r\n"},
		{"month", "verslagen/month/2026-11.md", "# Report month 2026-11 · o/r\n"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			root := repoDir(t, reportFileSink)
			rig := newReportRig(root)
			rig.fake.title = "Eerste"
			if code, _, errOut := runReport(rig.env, tc.kind); code != 0 {
				t.Fatalf("first run: exit %d, stderr %q", code, errOut)
			}
			rig.fake.title = "Tweede"
			code, out, errOut := runReport(rig.env, tc.kind)
			if code != 0 {
				t.Fatalf("second run: exit %d, stderr %q", code, errOut)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty with sink file", out)
			}
			got, err := os.ReadFile(filepath.Join(root, tc.path))
			if err != nil {
				t.Fatalf("report file: %v", err)
			}
			if !strings.HasPrefix(string(got), tc.title) || !strings.Contains(string(got), "Tweede") || strings.Contains(string(got), "Eerste") {
				t.Errorf("file holds\n%s\nwant the second run under %q", got, tc.title)
			}
			entries, err := os.ReadDir(filepath.Dir(filepath.Join(root, tc.path)))
			if err != nil || len(entries) != 1 {
				t.Errorf("report dir holds %v (%v), want only %s", entries, err, filepath.Base(tc.path))
			}
		})
	}
}

func TestReportDateFlag(t *testing.T) {
	rig := newReportRig(repoDir(t, reportOn))
	code, out, errOut := runReport(rig.env, "--date", "2024-12-30", "week")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if want := "# Report week 2025-W01 · o/r"; firstLine(out) != want {
		t.Errorf("title = %q, want %q", firstLine(out), want)
	}
	since, until := time.Date(2024, 12, 30, 0, 0, 0, 0, reportZone), time.Date(2025, 1, 6, 0, 0, 0, 0, reportZone)
	if !rig.fake.since.Equal(since) || !rig.fake.until.Equal(until) {
		t.Errorf("period = [%v, %v), want [%v, %v)", rig.fake.since, rig.fake.until, since, until)
	}
}

func TestReportBadDateAndKind(t *testing.T) {
	for name, args := range map[string][]string{
		"month 13":     {"--date", "2026-13-40", "week"},
		"word as date": {"--date", "morgen", "week"},
		"year":         {"year"},
		"no argument":  {},
		"two periods":  {"week", "day"},
		"unknown flag": {"--since", "2026-11-01", "week"},
	} {
		// Usage comes first: a disabled repo, where a later check would exit 0, still exits 2.
		for state, toml := range map[string]string{"on": reportOn, "off": "[forge]\nrepo = \"o/r\"\n"} {
			t.Run(name+"/"+state, func(t *testing.T) {
				rig := newReportRig(repoDir(t, toml))
				code, out, errOut := runReport(rig.env, args...)
				if code != 2 {
					t.Errorf("exit %d, stderr %q; want 2", code, errOut)
				}
				if out != "" || rig.opens != 0 {
					t.Errorf("stdout = %q, opens = %d; want nothing", out, rig.opens)
				}
			})
		}
	}
}

func TestReportForgeErrorWritesNothing(t *testing.T) {
	for name, toml := range map[string]string{"stdout": reportOn, "file": reportFileSink} {
		t.Run(name, func(t *testing.T) {
			root := repoDir(t, toml)
			rig := newReportRig(root)
			rig.fake.err = errors.New("GraphQL: rate limited")
			code, out, errOut := runReport(rig.env, "week")
			if code == 0 {
				t.Fatal("exit 0, want an error")
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if !strings.Contains(errOut, "rate limited") {
				t.Errorf("stderr = %q, want the forge error", errOut)
			}
			if _, err := os.Stat(filepath.Join(root, "verslagen")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("verslagen/ exists (%v), want nothing written", err)
			}
		})
	}
}

func TestReportRepoFlagAndRootOverride(t *testing.T) {
	rootDir := repoDir(t, reportOn)
	flagDir := repoDir(t, reportOn+"\n[project]\nlanguage = \"nl\"\n")
	rig := newReportRig(rootDir)
	_, out, errOut := runReport(rig.env, "--repo", flagDir, "week")
	if !strings.HasPrefix(out, "# Verslag week") {
		t.Errorf("with --repo: stdout starts %q (stderr %q), want the nl report of --repo", firstLine(out), errOut)
	}
	_, out, errOut = runReport(rig.env, "week")
	if !strings.HasPrefix(out, "# Report week") {
		t.Errorf("without --repo: stdout starts %q (stderr %q), want the en report of RepoRoot", firstLine(out), errOut)
	}
}

func TestReportRepoRootError(t *testing.T) {
	rig := newReportRig("")
	rig.env.RepoRoot = func() (string, error) { return "", errors.New("repository root not found: not a git repository") }
	code, _, errOut := runReport(rig.env, "week")
	if code == 0 || !strings.Contains(errOut, "repository root not found") {
		t.Errorf("exit %d, stderr %q; want an error naming the repo root", code, errOut)
	}
}

func TestReportDisabledWithBadSink(t *testing.T) {
	rig := newReportRig(repoDir(t, "[forge]\nrepo = \"o/r\"\n\n[report]\nenabled = false\nsink = \"wiki\"\n"))
	code, _, errOut := runReport(rig.env, "week")
	if code == 0 || !strings.Contains(errOut, "report.sink") {
		t.Errorf("exit %d, stderr %q; want an error naming report.sink", code, errOut)
	}
	if rig.opens != 0 {
		t.Errorf("opens = %d, want 0", rig.opens)
	}
}

func TestReportMissingToml(t *testing.T) {
	rig := newReportRig(repoDir(t, ""))
	code, out, errOut := runReport(rig.env, "week")
	if code == 0 || !strings.Contains(errOut, "lighthouse.toml: file is missing") {
		t.Errorf("exit %d, stderr %q; want an error that lighthouse.toml is missing", code, errOut)
	}
	if out != "" || rig.opens != 0 {
		t.Errorf("stdout = %q, opens = %d; want nothing", out, rig.opens)
	}
}

func TestReportTokenFallsBackToGh(t *testing.T) {
	for name, tc := range map[string]struct {
		gh       string
		wantCode int
	}{
		"gh has a token":  {"gh-tok", 0},
		"no token at all": {"", 1},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newReportRig(repoDir(t, reportOn))
			rig.env.Getenv = func(string) string { return "" }
			rig.env.GhToken = func() string { return tc.gh }
			code, _, errOut := runReport(rig.env, "week")
			if code != tc.wantCode {
				t.Fatalf("exit %d, stderr %q; want %d", code, errOut, tc.wantCode)
			}
			if tc.wantCode == 0 && (rig.opens != 1 || rig.cfg.Token != "gh-tok") {
				t.Errorf("opens = %d with token %q, want 1 with gh-tok", rig.opens, rig.cfg.Token)
			}
			if tc.wantCode != 0 && (rig.opens != 0 || !strings.Contains(errOut, "GH_TOKEN")) {
				t.Errorf("opens = %d, stderr %q; want no open and a message naming GH_TOKEN", rig.opens, errOut)
			}
		})
	}
}

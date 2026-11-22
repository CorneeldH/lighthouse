package worktreeaudit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/procrun"
)

// fakeForge answers the two reads of worktree-audit; every other method of the interface panics
// on the nil embedded Forge, which a test would notice.
type fakeForge struct {
	forge.Forge
	pulls      []forge.PullHead
	pullsErr   error
	closing    []forge.ClosingHead
	closingErr error
	asked      []int // the issue numbers IssueClosingHeads was asked for
}

func (f *fakeForge) PullHeads(context.Context) ([]forge.PullHead, error) { return f.pulls, f.pullsErr }

func (f *fakeForge) IssueClosingHeads(_ context.Context, nr int) ([]forge.ClosingHead, error) {
	f.asked = append(f.asked, nr)
	return f.closing, f.closingErr
}

func opens(f forge.Forge) func() (forge.Forge, error) {
	return func() (forge.Forge, error) { return f, nil }
}

// The twins of the tests of tests/pack/test_worktree_audit.py that are no data: processes, time,
// the filesystem and the git calls of the probe and of collect.

// gitFake answers a git call by its argv; nil is a failed call. It records every call.
type gitFake struct {
	answer func(args []string) *string
	calls  [][]string
}

func (f *gitFake) run(_ context.Context, _ string, _ *string, name string, args ...string) (procrun.Result, error) {
	if name != "git" {
		return procrun.Result{}, errors.New("unexpected " + name)
	}
	f.calls = append(f.calls, append([]string(nil), args...))
	out := f.answer(args)
	if out == nil {
		return procrun.Result{Code: 128}, nil
	}
	return procrun.Result{Stdout: *out}, nil
}

func (f *gitFake) called(args ...string) bool {
	for _, c := range f.calls {
		if reflect.DeepEqual(c, args) {
			return true
		}
	}
	return false
}

func probeWith(policy Policy, answer func([]string) *string, ref string, skip bool) (BranchProbe, *gitFake) {
	f := &gitFake{answer: answer}
	c := &collector{ctx: context.Background(), env: Env{Run: f.run}, policy: policy}
	return c.probe(nil, "/repo", ref, skip), f
}

var mainPolicy = Policy{DefaultBranch: "main"}

// twin of test_ref_is_used_never_the_bare_name
func TestProbeMeasuresTheRefNotTheBareName(t *testing.T) {
	p, f := probeWith(mainPolicy, func(args []string) *string {
		switch args[0] {
		case "rev-list":
			return ptr("0\n")
		case "diff":
			return ptr("")
		}
		return nil
	}, "origin/feat-x", false)
	if p.Unpushed == nil || *p.Unpushed != 0 {
		t.Fatalf("unpushed = %v, want 0", p.Unpushed)
	}
	for _, want := range [][]string{
		{"rev-list", "--count", "origin/feat-x", "--not", "--remotes"},
		{"diff", "--name-only", "main...origin/feat-x"},
	} {
		if !f.called(want...) {
			t.Errorf("no call %v in %v", want, f.calls)
		}
	}
	for _, c := range f.calls {
		for _, arg := range c {
			if arg == "feat-x" {
				t.Errorf("call %v uses the bare name", c)
			}
		}
	}
}

// twin of test_skip_content_leaves_unique_files_none_and_skips_the_diff
func TestProbeSkipContentMeasuresNothingOfTheContent(t *testing.T) {
	p, f := probeWith(mainPolicy, func(args []string) *string {
		if args[0] == "rev-list" {
			return ptr("0\n")
		}
		return ptr("")
	}, "feat-merged", true)
	if p.UniqueFiles != nil || p.OwnCommits != nil {
		t.Fatalf("unique %v own %v: skipped content must stay unmeasured (nil), never empty", p.UniqueFiles, p.OwnCommits)
	}
	for _, c := range f.calls {
		if c[0] == "diff" || strings.Contains(strings.Join(c, " "), "main..feat-merged") {
			t.Errorf("content call %v despite skip", c)
		}
	}
}

// twin of test_own_commits_are_counted_against_main_on_the_ref
func TestProbeCountsOwnCommitsAgainstMainOnTheRef(t *testing.T) {
	p, _ := probeWith(mainPolicy, func(args []string) *string {
		switch {
		case reflect.DeepEqual(args, []string{"rev-list", "--count", "main..origin/feat-x"}):
			return ptr("4\n")
		case args[0] == "rev-list":
			return ptr("0\n")
		}
		return ptr("")
	}, "origin/feat-x", false)
	if p.OwnCommits == nil || *p.OwnCommits != 4 {
		t.Fatalf("own commits = %v, want 4", p.OwnCommits)
	}
}

// twin of test_failed_own_commit_count_stays_unknown
func TestProbeFailedOwnCommitCountStaysUnknown(t *testing.T) {
	p, _ := probeWith(mainPolicy, func(args []string) *string {
		switch {
		case args[0] == "rev-list" && args[1] == "--count" && strings.HasPrefix(args[2], "main.."):
			return nil
		case args[0] == "rev-list":
			return ptr("0\n")
		}
		return ptr("")
	}, "feat-x", false)
	if p.OwnCommits != nil {
		t.Fatalf("own commits = %d, want unknown", *p.OwnCommits)
	}
}

// twin of test_content_check_measures_against_the_default_branch
func TestProbeMeasuresAgainstTheDefaultBranch(t *testing.T) {
	cfg, err := config.Parse([]byte("[forge]\ndefault_branch = \"trunk\"\n"), "p.toml")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := PolicyFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, f := probeWith(policy, func(args []string) *string {
		if args[0] == "rev-list" {
			return ptr("0\n")
		}
		return ptr("")
	}, "feat-x", false)
	for _, want := range [][]string{{"rev-list", "--count", "trunk..feat-x"}, {"diff", "--name-only", "trunk...feat-x"}} {
		if !f.called(want...) {
			t.Errorf("no call %v in %v", want, f.calls)
		}
	}
}

// A non-reading git verb never reaches Run; Collect fails with the refusal instead.
func TestRefusedVerbNeverRuns(t *testing.T) {
	c := &collector{ctx: context.Background(), env: Env{Run: func(context.Context, string, *string, string, ...string) (procrun.Result, error) {
		t.Fatal("a refused verb reached Run")
		return procrun.Result{}, nil
	}}}
	if out := c.git("", "branch", "-D", "x"); out != nil {
		t.Fatalf("git = %q, want nil", *out)
	}
	if c.refused == nil || c.refused.Error() != "git branch: read-only commands only" {
		t.Fatalf("refused = %v", c.refused)
	}
}

// collectEnv is an Env over a git fake, a config text, an empty home and a stderr buffer.
func collectEnv(t *testing.T, f *gitFake, toml string) (Env, *bytes.Buffer) {
	t.Helper()
	var stderr bytes.Buffer
	return Env{
		Run: f.run,
		Config: func() (*config.Config, error) {
			return config.Parse([]byte(toml), "lighthouse.toml")
		},
		Alive:  func(int) *bool { return ptr(false) },
		Home:   t.TempDir(),
		Stderr: &stderr,
	}, &stderr
}

func failsWith(t *testing.T, err error, code int, message string) {
	t.Helper()
	var f *Failure
	if !errors.As(err, &f) || f.Code != code || !strings.Contains(f.Message, message) {
		t.Fatalf("err = %v, want a Failure %d with %q", err, code, message)
	}
}

// twin of test_worktree_list_failure_is_reported_by_name
func TestCollectNamesTheFailedWorktreeList(t *testing.T) {
	f := &gitFake{answer: func(args []string) *string {
		if args[0] == "worktree" {
			return nil
		}
		return ptr("")
	}}
	env, _ := collectEnv(t, f, "")
	_, err := Collect(context.Background(), env, "/repo")
	failsWith(t, err, 1, "git worktree list --porcelain failed — cannot read the repo.")
}

// twin of test_local_branch_listing_failure_is_reported_by_name
func TestCollectNamesTheFailedLocalBranchListing(t *testing.T) {
	f := &gitFake{answer: func(args []string) *string {
		if reflect.DeepEqual(args, []string{"branch", "--format=%(refname:short)"}) {
			return nil
		}
		return ptr("")
	}}
	env, _ := collectEnv(t, f, "")
	_, err := Collect(context.Background(), env, "/repo")
	failsWith(t, err, 1, "git branch --format=%(refname:short) failed — cannot read the repo.")
}

// twin of test_remote_branch_listing_failure_is_reported_by_name
func TestCollectNamesTheFailedRemoteBranchListing(t *testing.T) {
	f := &gitFake{answer: func(args []string) *string {
		switch {
		case reflect.DeepEqual(args, []string{"branch", "--format=%(refname:short)"}):
			return ptr("main\n")
		case args[0] == "branch" && args[1] == "-r":
			return nil
		}
		return ptr("")
	}}
	env, _ := collectEnv(t, f, "")
	_, err := Collect(context.Background(), env, "/repo")
	failsWith(t, err, 1, "git branch -r --format=%(refname:short) failed — cannot read the repo.")
}

func mainOnly(args []string) *string {
	if args[0] == "worktree" {
		return ptr("worktree /repo\nbranch refs/heads/main\n")
	}
	return ptr("")
}

// twin of test_broken_config_stops_collect_loudly
func TestCollectStopsOnABrokenConfig(t *testing.T) {
	env, _ := collectEnv(t, &gitFake{answer: mainOnly}, "[worktree_audit]\nprotected_refs = \"x\"\n")
	_, err := Collect(context.Background(), env, "/repo")
	failsWith(t, err, 2, "worktree-audit: ")
	failsWith(t, err, 2, "protected_refs")
}

// twin of test_without_forge_repo_collect_says_it_skips_the_pr_lookup
// twin of test_without_forge_repo_the_pr_lookup_is_unknown
func TestCollectWithoutForgeRepoSkipsTheLookup(t *testing.T) {
	f := &gitFake{answer: func(args []string) *string {
		if args[0] == "branch" && len(args) == 2 {
			return ptr("main\nfeat-x\n")
		}
		return mainOnly(args)
	}}
	env, stderr := collectEnv(t, f, "[project]\nlanguage = \"en\"\n")
	audit, err := Collect(context.Background(), env, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	want := "worktree-audit: PR lookup skipped — [forge] repo is not set, so every PR state is unknown\n"
	if stderr.String() != want {
		t.Fatalf("stderr %q, want %q", stderr.String(), want)
	}
	if audit.Repo != "" {
		t.Fatalf("repo %q, want none", audit.Repo)
	}
	for _, r := range audit.Rows {
		if r.PRLookupOK {
			t.Errorf("row %s: PR lookup ok without a lookup", r.Name)
		}
	}
	env.Forge = func() (forge.Forge, error) {
		t.Error("the forge was opened without a repo")
		return nil, errors.New("unexpected")
	}
	if _, ok := IssuePRHeads(context.Background(), env, "", 9); ok {
		t.Error("IssuePRHeads without a repo must fail, not find nothing")
	}
}

// twin of test_run_gh_issue_pr_heads_argv: the lookup runs on the forge and fails on every error.
func TestIssuePRHeadsAsksTheForgeAndFailsOnEveryError(t *testing.T) {
	for _, tt := range []struct {
		name  string
		env   Env
		heads []string
		ok    bool
	}{
		{"found nothing", Env{Forge: opens(&fakeForge{})}, []string{}, true},
		{"forge read fails", Env{Forge: opens(&fakeForge{closingErr: errors.New("boom")})}, nil, false},
		{"forge cannot be opened", Env{Forge: func() (forge.Forge, error) { return nil, errors.New("no token") }}, nil, false},
		{"no forge at all", Env{}, nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			heads, ok := IssuePRHeads(context.Background(), tt.env, "o/r", 9)
			if ok != tt.ok || !reflect.DeepEqual(heads, tt.heads) {
				t.Errorf("IssuePRHeads = %v %v, want %v %v", heads, ok, tt.heads, tt.ok)
			}
		})
	}
}

// A merged PR from a fork, one from another repo, an unmerged one and one that only mentions the
// issue are no merged head of it; a closing PR and a `Refs #nr` PR are.
func TestIssuePRHeadsKeepsOnlyTheMergedHeadsOfTheIssue(t *testing.T) {
	ff := &fakeForge{closing: []forge.ClosingHead{
		{HeadRef: "issue-9-a", Repo: "o/r", Merged: true, Closes: true},
		{HeadRef: "issue-9-fork", Repo: "o/r", Merged: true, Closes: true, CrossRepo: true},
		{HeadRef: "issue-9-other", Repo: "x/fork", Merged: true, Closes: true},
		{HeadRef: "issue-9-open", Repo: "o/r", Closes: true},
		{HeadRef: "issue-9-refs", Repo: "o/r", Merged: true, Body: "Refs #9 — deel 1"},
		{HeadRef: "issue-9-forkrefs", Repo: "o/r", Merged: true, CrossRepo: true, Body: "Refs #9"},
		{HeadRef: "issue-9-mention", Repo: "o/r", Merged: true, Body: "zie #9"},
		{HeadRef: "issue-9-nine3", Repo: "o/r", Merged: true, Body: "Refs #93"},
		{HeadRef: "issue-9-refs", Repo: "o/r", Merged: true, Body: "Refs #9"},
	}}

	heads, ok := IssuePRHeads(context.Background(), Env{Forge: opens(ff)}, "o/r", 9)

	if want := []string{"issue-9-a", "issue-9-refs"}; !ok || !reflect.DeepEqual(heads, want) {
		t.Errorf("IssuePRHeads = %v %v, want %v true", heads, ok, want)
	}
	if !reflect.DeepEqual(ff.asked, []int{9}) {
		t.Errorf("asked the forge for issues %v, want [9]", ff.asked)
	}
}

// twin of test_latest_transcript_picks_the_newest_jsonl
func TestLatestTranscriptPicksTheNewest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects")
	dir := filepath.Join(root, EncodeProjectDir("/repo/wt"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, at := range map[string]int64{"1111.jsonl": 1_000_000, "2222.jsonl": 2_000_000, "3333.json": 3_000_000} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, time.Unix(at, 0), time.Unix(at, 0)); err != nil {
			t.Fatal(err)
		}
	}
	got, ok := latestTranscript(root, "/repo/wt")
	if !ok || got != (Transcript{MTime: 2_000_000, SessionID: "2222"}) {
		t.Fatalf("latest = %+v %v, want 2222 at 2000000", got, ok)
	}
}

// twin of test_missing_project_dir_gives_none
func TestLatestTranscriptOfAMissingDirIsNone(t *testing.T) {
	if got, ok := latestTranscript(t.TempDir(), "/repo/nooit-geopend"); ok {
		t.Fatalf("latest = %+v, want none", got)
	}
}

// Review focus 5: no `~/.claude/sessions` is no sessions, not an error.
func TestMissingSessionsDirIsNoSessions(t *testing.T) {
	if got := loadSessions(filepath.Join(t.TempDir(), "missing")); len(got) != 0 {
		t.Fatalf("sessions = %v, want none", got)
	}
}

// Review focus 5: a session file that is no JSON object is skipped; the others still count.
func TestBrokenSessionJSONIsSkipped(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{
		"a.json":       `{"pid": 11, "sessionId": "a", "cwd": "/wt/a"}`,
		"b.json":       `{"pid": 12, "sessionId": "b", "cwd": `,
		"c.json":       `[1, 2]`,
		"d.json":       `{"pid": 14, "sessionId": "d", "cwd": "/wt/d"}`,
		"e.txt":        `{"pid": 15, "sessionId": "e", "cwd": "/wt/e"}`,
		"dir.json/x":   ``,
		".hidden.json": `{"pid": 16, "sessionId": "h", "cwd": "/wt/h"}`,
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]SessionInfo{
		"/wt/a": {11, "a", "/wt/a"},
		"/wt/d": {14, "d", "/wt/d"},
		"/wt/h": {16, "h", "/wt/h"},
	}
	if got := loadSessions(dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("sessions = %v, want %v", got, want)
	}
}

// Review focus 5: a session pid that cannot be measured keeps the row live, and a live row is
// never `cleanup: yes`, however merged its PR.
func TestUnmeasurablePidBlocksCleanup(t *testing.T) {
	wt := "/repo/.claude/worktrees/issue-9-x"
	unmeasurable := func(int) *bool { return nil }
	rows := BuildRows(Inputs{
		Worktrees:     []WorktreeInfo{{Path: wt, Branch: ptr("issue-9-x")}},
		LocalBranches: []string{"issue-9-x"},
		Pulls:         map[string]PullInfo{"issue-9-x": {Number: 9, State: "merged"}},
		Probes:        map[string]BranchProbe{"issue-9-x": {Dirty: ptr(false), Unpushed: ptr(0)}},
		Sessions:      map[string]SessionInfo{wt: {PID: 1 << 40, SessionID: "s", Cwd: wt}},
		PRLookupOK:    true,
	}, PidAlive)
	if len(rows) != 1 || !rows[0].Live {
		t.Fatalf("rows = %+v, want one live row", rows)
	}
	text, ok := View{Policy: mainPolicy, Alive: unmeasurable}.Only(rows, "issue-9-x", "/elders")
	if ok || !strings.HasSuffix(text, "cleanup: no — ⛔ running session\n") {
		t.Fatalf("Only = %q %v, want cleanup: no", text, ok)
	}
}

// Review focus 5: a session record whose pid or session id has an odd type is kept, never
// dropped; a pid that cannot be signalled is unmeasurable, so the row is live and never
// `cleanup: yes` (Python: a huge pid is an OverflowError, None; `true` is pid 1; a garbage string
// stops the whole run, which here becomes a blocking session).
func TestOddSessionRecordsBlockCleanup(t *testing.T) {
	wt := "/repo/.claude/worktrees/issue-9-x"
	for _, record := range []string{
		`{"pid": 1e20, "sessionId": "s", "cwd": "` + wt + `"}`,
		`{"pid": "x", "sessionId": "s", "cwd": "` + wt + `"}`,
		`{"pid": true, "sessionId": "s", "cwd": "` + wt + `"}`,
		`{"pid": 99999999999, "sessionId": 42, "cwd": "` + wt + `"}`,
	} {
		t.Run(record, func(t *testing.T) {
			var v any
			if err := json.Unmarshal([]byte(record), &v); err != nil {
				t.Fatal(err)
			}
			sessions := ParseSessions([]any{v})
			if len(sessions) != 1 {
				t.Fatalf("sessions = %v, want the record kept", sessions)
			}
			rows := BuildRows(Inputs{
				Worktrees:     []WorktreeInfo{{Path: wt, Branch: ptr("issue-9-x")}},
				LocalBranches: []string{"issue-9-x"},
				Pulls:         map[string]PullInfo{"issue-9-x": {Number: 9, State: "merged"}},
				Probes:        map[string]BranchProbe{"issue-9-x": {Dirty: ptr(false), Unpushed: ptr(0)}},
				Sessions:      sessions,
				PRLookupOK:    true,
			}, PidAlive)
			text, ok := View{Policy: mainPolicy, Alive: PidAlive}.Only(rows, "issue-9-x", "/elders")
			if ok || !strings.HasSuffix(text, "cleanup: no — ⛔ running session\n") {
				t.Fatalf("Only = %q %v, want cleanup: no — running session", text, ok)
			}
		})
	}
}

func TestSessionPIDLikePython(t *testing.T) {
	for _, tt := range []struct {
		in   any
		want int
	}{{float64(4242), 4242}, {12.7, 12}, {"77", 77}, {true, 1}, {1e20, unmeasurablePID}, {"x", unmeasurablePID}} {
		if got := sessionPID(tt.in); got != tt.want {
			t.Errorf("sessionPID(%v) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// twin of test_is_live_only_false_is_dead
func TestIsLiveOnlyFalseIsDead(t *testing.T) {
	if !isLive(nil) || !isLive(ptr(true)) || isLive(ptr(false)) {
		t.Fatal("only a measured false is dead")
	}
}

// fnmatch against Python's fnmatch.fnmatch: for each pattern, the names it matched (generated
// with Python 3.12 and checked in as literals).
func TestFnmatchMatchesPython(t *testing.T) {
	names := []string{"entire/a/b", "entire/", "abc", "ax", "dx", "b", "-", "]", "a", "[", "a[", "e", "d", "y", "w",
		"sub/x.lock", "aXbYc", "\\", "^", "\\x", "[:x:]]", ":]", "release-1", "release-10", "xa", "xb", "xz", "", "\n"}
	for _, tt := range []struct {
		pattern string
		hits    []string
	}{
		{"entire/*", []string{"entire/a/b", "entire/"}},
		{"*", names},
		{"a?c", []string{"abc"}},
		{"[abc]x", []string{"ax"}},
		{"[!abc]x", []string{"dx", "\\x"}},
		{"[a-c]", []string{"b", "a"}},
		{"[!a-c]", []string{"-", "]", "[", "e", "d", "y", "w", "\\", "^", "\n"}},
		{"[z-a]", []string{}},
		{"[!z-a]", []string{"b", "-", "]", "a", "[", "e", "d", "y", "w", "\\", "^", "\n"}},
		{"[a-]", []string{"-", "a"}},
		{"[]a]", []string{"]", "a"}},
		{"[!]a]", []string{"b", "-", "[", "e", "d", "y", "w", "\\", "^", "\n"}},
		{"[", []string{"["}},
		{"a[", []string{"a["}},
		{"[a-c-e]", []string{"b", "-", "a", "e"}},
		{"[a-cx-z]", []string{"b", "a", "y"}},
		{"*.lock", []string{"sub/x.lock"}},
		{"a*b*c", []string{"abc", "aXbYc"}},
		{"[\\]", []string{"\\"}},
		{"[^a]", []string{"a", "^"}},
		{"\\*", []string{"\\", "\\x"}},
		{"[[:x:]]", []string{":]"}},
		{"release-?", []string{"release-1"}},
		{"x[az-ab]", []string{"xa", "xb"}},
	} {
		got := []string{}
		for _, n := range names {
			if fnmatch(n, tt.pattern) {
				got = append(got, n)
			}
		}
		if !reflect.DeepEqual(got, tt.hits) {
			t.Errorf("fnmatch %q: got %q, want %q", tt.pattern, got, tt.hits)
		}
	}
}

// Python stopped the whole run on a pull it could not read; skipping an open PR would let a merged
// one win.
func TestParsePullsFailsOnAPullItCannotRead(t *testing.T) {
	for name, pull := range map[string]string{
		"number is no integer": `{"number":"x","state":"open","merged_at":null,"head":{"ref":"a"}}`,
		"number is missing":    `{"state":"open","merged_at":null,"head":{"ref":"a"}}`,
		"head is null":         `{"number":1,"state":"open","merged_at":null,"head":null}`,
		"state is unknown":     `{"number":1,"state":"draft","merged_at":null,"head":{"ref":"a"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var item any
			if err := json.Unmarshal([]byte(pull), &item); err != nil {
				t.Fatal(err)
			}
			if got, err := ParsePulls([]any{item}); err == nil {
				t.Fatalf("ParsePulls = %v, want an error", got)
			}
		})
	}
}

// A forge that cannot be read makes every PR state unknown, never "no PR": a branch whose PR is
// merged must not look like one without a PR.
func TestCollectTreatsAForgeFailureAsAnUnknownPR(t *testing.T) {
	for name, tt := range map[string]Env{
		"read fails":       {Forge: opens(&fakeForge{pullsErr: errors.New("500")})},
		"cannot be opened": {Forge: func() (forge.Forge, error) { return nil, errors.New("no token") }},
		"no forge":         {},
	} {
		t.Run(name, func(t *testing.T) {
			rows := collectRows(t, tt)
			for _, r := range rows {
				if r.PRLookupOK || r.PRState != nil {
					t.Errorf("row %s: lookup ok %v, state %v; want an unknown PR", r.Name, r.PRLookupOK, r.PRState)
				}
			}
		})
	}
}

// A PR the forge reports in a state the audit does not know fails the whole lookup: skipping it
// could let a merged PR of the same head win.
func TestCollectTreatsAPullInAnUnknownStateAsAFailedLookup(t *testing.T) {
	f := &fakeForge{pulls: []forge.PullHead{
		{Number: 1, State: "MERGED", MergedAt: "2026-01-01T00:00:00Z", HeadRef: "feat-x"},
		{Number: 2, State: "DRAFT", HeadRef: "feat-x"},
	}}
	env := Env{Forge: opens(f)}
	rows, stderr := collectRowsStderr(t, env)
	if !strings.Contains(stderr, "PR lookup failed") {
		t.Errorf("stderr %q does not say the lookup failed", stderr)
	}
	for _, r := range rows {
		if r.PRLookupOK || r.PRState != nil {
			t.Errorf("row %s: lookup ok %v, state %v; want an unknown PR", r.Name, r.PRLookupOK, r.PRState)
		}
	}
}

// The heaviest state per head wins and a merged PR carries its merge time; open and closed come
// from the forge as the GitHub REST names them.
func TestCollectReadsThePullsOfTheForge(t *testing.T) {
	f := &fakeForge{pulls: []forge.PullHead{
		{Number: 1, State: "CLOSED", HeadRef: "feat-x"},
		{Number: 2, State: "OPEN", HeadRef: "feat-y"},
		{Number: 3, State: "MERGED", MergedAt: "2026-01-01T00:00:00Z", HeadRef: "feat-z"},
	}}
	rows, _ := collectRowsStderr(t, Env{Forge: opens(f)})
	got := map[string]string{}
	for _, r := range rows {
		if r.PRState != nil {
			got[r.Name] = *r.PRState + "#" + itoa(*r.PRNumber)
		}
	}
	want := map[string]string{"feat-x": "closed#1", "feat-y": "open#2", "feat-z": "merged#3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PR states %v, want %v", got, want)
	}
}

func collectRows(t *testing.T, env Env) []Row {
	t.Helper()
	rows, _ := collectRowsStderr(t, env)
	return rows
}

// collectRowsStderr collects a repo with the branches main, feat-x, feat-y and feat-z against env's
// forge.
func collectRowsStderr(t *testing.T, forgeEnv Env) ([]Row, string) {
	t.Helper()
	f := &gitFake{answer: func(args []string) *string {
		if args[0] == "branch" && len(args) == 2 {
			return ptr("main\nfeat-x\nfeat-y\nfeat-z\n")
		}
		return mainOnly(args)
	}}
	env, stderr := collectEnv(t, f, "[forge]\nrepo = \"o/r\"\n")
	env.Forge = forgeEnv.Forge
	audit, err := Collect(context.Background(), env, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(audit.Rows) == 0 {
		t.Fatal("no rows to check")
	}
	return audit.Rows, stderr.String()
}

// Red when findOrphans goes back to joining the const `.claude/worktrees` onto the repo: a root
// outside the repo is then never looked in, and the orphan is not found.
func TestFindOrphansReadsARootOutsideTheRepo(t *testing.T) {
	root := filepath.Join(t.TempDir(), "my worktrees")
	for _, d := range []string{"known", "orphan"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "orphan", "f.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got := findOrphans(root, []WorktreeInfo{{Path: filepath.Join(root, "known")}})
	want := []OrphanDir{{Name: "orphan", Path: filepath.Join(root, "orphan"), DirEntries: []string{"f.txt"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// Red when KPI goes back to strings.Contains on the relative string: it then counts nothing under
// an outside root, and counts `<root>-old/.claude/worktrees/x`.
func TestKPICountsWorktreesUnderTheResolvedRoot(t *testing.T) {
	row := func(p string) Row {
		r := NewRow("b")
		r.Worktree = &p
		return r
	}
	v := View{Policy: mainPolicy, Alive: PidAlive, Root: "/data/wt"}
	got := v.KPI([]Row{row("/data/wt/a"), row("/data/wt/b"), row("/data/wt-old/.claude/worktrees/x"), row("/data/wt")})
	if !strings.HasPrefix(got, "worktrees=2 ") {
		t.Fatalf("got %q, want worktrees=2", got)
	}
}

// Red when an empty Root goes back to a substring match on `.claude/worktrees`: then
// `/data/wt-old/.claude/worktrees/x` counts, and a View without a Root counts nothing otherwise.
func TestKPIWithoutARootCountsNoWorktree(t *testing.T) {
	p := "/data/wt-old/.claude/worktrees/x"
	r := NewRow("b")
	r.Worktree = &p
	got := View{Policy: mainPolicy, Alive: PidAlive}.KPI([]Row{r})
	if !strings.HasPrefix(got, "worktrees=0 ") {
		t.Fatalf("got %q, want worktrees=0", got)
	}
}

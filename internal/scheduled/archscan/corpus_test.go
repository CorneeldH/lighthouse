package archscan

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Classification: io (real git with a bare origin, the real forge client against the fake
// GitHub of forgetest). The regression surface is what the gate decides from origin and the
// open scan issue, which files the hotspot count sees, and what the corpus holds and cuts.
// Each test names the production change that makes it fail.

const (
	jobLogin   = "lh-bot"
	scanLabel  = "3-needs-decision" // deliberately not the key needs_decision
	defaultTOM = "[project]\nlanguage = \"nl\"\n"
)

func gitIn(t *testing.T, dir string, extraEnv []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com"), extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func put(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// rig is a clone of a bare origin on main, and a fake forge.
type rig struct {
	t            *testing.T
	origin, repo string
	srv          *forgetest.Server
	noForge      bool
}

func newRig(t *testing.T) *rig {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origin, repo := filepath.Join(root, "origin.git"), filepath.Join(root, "repo")
	gitIn(t, root, nil, "init", "-q", "--bare", "-b", "main", origin)
	gitIn(t, root, nil, "clone", "-q", origin, repo)
	gitIn(t, repo, nil, "checkout", "-q", "-b", "main")
	put(t, repo, "lighthouse.toml", defaultTOM)
	r := &rig{t: t, origin: origin, repo: repo, srv: forgetest.New(t)}
	r.srv.Login = jobLogin
	r.srv.Issues = map[int]map[string]any{}
	r.commit()
	return r
}

// commit commits the work tree and pushes it to origin; it returns the new HEAD.
func (r *rig) commit(extraEnv ...string) string {
	r.t.Helper()
	gitIn(r.t, r.repo, extraEnv, "add", "-A")
	gitIn(r.t, r.repo, extraEnv, "commit", "-q", "--allow-empty", "-m", "c")
	gitIn(r.t, r.repo, nil, "push", "-q", "origin", "main")
	return gitIn(r.t, r.repo, nil, "rev-parse", "HEAD")
}

// issue seeds an issue on the forge.
func (r *rig) issue(n int, author, state, created, closed, body string, labels ...string) {
	ls := []any{}
	for _, l := range labels {
		ls = append(ls, map[string]any{"name": l})
	}
	it := map[string]any{"number": n, "title": fmt.Sprintf("issue %d", n), "state": state, "body": body, "labels": ls,
		"created_at": created, "user": map[string]any{"login": author}}
	if closed != "" {
		it["closed_at"] = closed
	}
	r.srv.Issues[n] = it
}

// scanIssue seeds a scan issue of the job by the token's login, with base in its marker.
func (r *rig) scanIssue(n int, state, created, closed, base, content string) string {
	m := scheduled.Marker{Kind: Name, Version: 1, Run: "scan-old", Base: base, First: "2026-08-01", Scans: 1}
	body := m.String() + "\nwarning\n\n" + content
	r.issue(n, jobLogin, state, created, closed, body, scanLabel)
	return body
}

func (r *rig) comment(n int, body, created string) {
	if r.srv.Comments == nil {
		r.srv.Comments = map[int][]map[string]any{}
	}
	r.srv.Comments[n] = append(r.srv.Comments[n], map[string]any{
		"id": len(r.srv.Comments[n]) + 1000*n, "body": body, "created_at": created,
		"user": map[string]any{"login": jobLogin},
	})
}

func (r *rig) env() scheduled.Env {
	env := scheduled.Env{
		Repo: repoconfig.Repo{Dir: r.repo, DefaultBranch: "main", Slug: "o/r", Kind: "github", Language: "nl",
			Labels: repoconfig.Labels{NeedsDecision: scanLabel}},
		Dir: r.repo,
		Now: time.Date(2026, 11, 22, 5, 0, 0, 0, time.UTC),
		Run: workplace.ExecRunner(),
	}
	if !r.noForge {
		c := forge.New("o/r", "secret-token")
		c.BaseURL = r.srv.URL()
		env.Forge = c
	}
	return env
}

func bind(t *testing.T) *Job {
	t.Helper()
	j, err := scheduled.Bind(New(), repoconfig.Schedule{Name: Name, Job: Name})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	return j.(*Job)
}

func TestGateSkipsWhenNoNewCommits(t *testing.T) {
	// Fails if Gate does not compare the base of the open scan issue with origin/<default>,
	// or reads the base from somewhere other than the marker.
	r := newRig(t)
	head := gitIn(t, r.repo, nil, "rev-parse", "HEAD")
	r.scanIssue(7, "open", "2026-08-01T05:00:00Z", "", head, "body")
	v, err := bind(t).Gate(context.Background(), r.env())
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if v.Skip != "no commits since last scan" {
		t.Fatalf("Skip = %q, want no commits since last scan", v.Skip)
	}
}

func TestGateRunsWithNewCommits(t *testing.T) {
	// Fails if Gate skips with new commits on origin, compares against the local HEAD of the
	// repo instead of fetching origin, or sets no RunID.
	const wantRun = "scan-20261122T0500"
	t.Run("new commit on origin", func(t *testing.T) {
		r := newRig(t)
		old := gitIn(t, r.repo, nil, "rev-parse", "HEAD")
		r.scanIssue(7, "open", "2026-08-01T05:00:00Z", "", old, "body")
		// A commit pushed from another clone: the repo learns of it only by a fetch.
		other := filepath.Join(filepath.Dir(r.repo), "other")
		gitIn(t, filepath.Dir(r.repo), nil, "clone", "-q", r.origin, other)
		put(t, other, "new.go", "package x\n")
		gitIn(t, other, nil, "add", "-A")
		gitIn(t, other, nil, "commit", "-q", "-m", "new")
		gitIn(t, other, nil, "push", "-q", "origin", "main")
		v, err := bind(t).Gate(context.Background(), r.env())
		if err != nil {
			t.Fatalf("Gate: %v", err)
		}
		if v.Skip != "" || v.RunID != wantRun {
			t.Fatalf("Verdict = %+v, want no skip and RunID %s", v, wantRun)
		}
	})
	t.Run("no scan issue", func(t *testing.T) {
		r := newRig(t)
		v, err := bind(t).Gate(context.Background(), r.env())
		if err != nil {
			t.Fatalf("Gate: %v", err)
		}
		if v.Skip != "" || v.RunID != wantRun {
			t.Fatalf("Verdict = %+v, want no skip and RunID %s", v, wantRun)
		}
	})
}

func TestGateNoForgeIsError(t *testing.T) {
	// Fails if Gate without a forge skips or runs instead of failing (the tick must keep the slot).
	r := newRig(t)
	r.noForge = true
	v, err := bind(t).Gate(context.Background(), r.env())
	if err == nil {
		t.Fatalf("Gate = %+v, nil; want an error", v)
	}
}

func TestGateForgeErrorIsError(t *testing.T) {
	// Fails if a forge error while reading the scan issue becomes a skip or a run.
	for _, key := range []string{"GET /user", "GET /repos/o/r/issues"} {
		t.Run(key, func(t *testing.T) {
			r := newRig(t)
			r.srv.Fail = map[string]int{key: 500}
			v, err := bind(t).Gate(context.Background(), r.env())
			if err == nil {
				t.Fatalf("Gate = %+v, nil; want an error", v)
			}
		})
	}
}

func TestHotspotsCountsPerPathWithinWindow(t *testing.T) {
	// Fails if hotspots counts a commit outside the window, a file outside the paths, or does not
	// sort by changes, most first.
	r := newRig(t)
	old := []string{"GIT_AUTHOR_DATE=2020-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2020-01-01T00:00:00Z"}
	put(t, r.repo, "a/old.go", "1\n")
	put(t, r.repo, "a/x.go", "0\n")
	r.commit(old...)
	put(t, r.repo, "a/x.go", "1\n")
	put(t, r.repo, "a/y.go", "1\n")
	put(t, r.repo, "b/z.go", "1\n")
	r.commit()
	put(t, r.repo, "a/x.go", "2\n")
	put(t, r.repo, "b/z.go", "2\n")
	r.commit()
	got, err := hotspots(context.Background(), r.env(), r.repo, "3 months", []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Hotspot{{Path: "a/x.go", Changes: 2}, {Path: "a/y.go", Changes: 1}}
	if !slices.Equal(got, want) {
		t.Fatalf("hotspots = %+v, want %+v", got, want)
	}
}

func TestHotspotsKeepsNonASCIIPaths(t *testing.T) {
	// Fails if hotspots reads git's quoted form ("a/caf\303\251.go") instead of the real path.
	r := newRig(t)
	put(t, r.repo, "a/café ü.go", "1\n")
	r.commit()
	got, err := hotspots(context.Background(), r.env(), r.repo, "3 months", []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Hotspot{{Path: "a/café ü.go", Changes: 1}}
	if !slices.Equal(got, want) {
		t.Fatalf("hotspots = %+v, want %+v", got, want)
	}
}

func TestHotspotsCappedHighestFirst(t *testing.T) {
	// Fails if hotspots returns more than maxHotspots, or drops the busiest file instead of the
	// quietest.
	r := newRig(t)
	for i := range maxHotspots + 5 {
		put(t, r.repo, fmt.Sprintf("a/f%03d.go", i), "1\n")
	}
	r.commit()
	put(t, r.repo, "a/f204.go", "2\n")
	r.commit()
	got, err := hotspots(context.Background(), r.env(), r.repo, "3 months", []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != maxHotspots {
		t.Fatalf("len = %d, want %d", len(got), maxHotspots)
	}
	if got[0] != (Hotspot{Path: "a/f204.go", Changes: 2}) {
		t.Fatalf("first = %+v, want the busiest file", got[0])
	}
}

// stage runs Stage on the rig and decodes the corpus.
func stage(t *testing.T, r *rig) (corpus, []byte) {
	t.Helper()
	if err := bind(t).Stage(context.Background(), r.env(), scheduled.Verdict{RunID: "scan-x"}); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(r.repo, filepath.FromSlash(corpusPath)))
	if err != nil {
		t.Fatal(err)
	}
	var c corpus
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("corpus is no JSON: %v", err)
	}
	return c, b
}

func TestStageCorpusHasOpenAndLastClosedScan(t *testing.T) {
	// Fails if the corpus misses the open scan issue or its comments, picks a closed scan issue
	// other than the newest closed one, or drops the base, the language or search_dirs.
	r := newRig(t)
	put(t, r.repo, "lighthouse.toml", defaultTOM+"\n[paths]\nsearch_dirs = [\"internal\"]\n")
	put(t, r.repo, "internal/a.go", "package a\n")
	put(t, r.repo, "other/b.go", "package b\n")
	head := r.commit()
	open := r.scanIssue(7, "open", "2026-11-01T05:00:00Z", "", "abc", "- [x] Afgewezen")
	r.comment(7, "previous scan", "2026-11-02T05:00:00Z")
	r.scanIssue(3, "closed", "2026-08-01T05:00:00Z", "2026-10-01T05:00:00Z", "def", "closed newest")
	r.comment(3, "old comment", "2026-09-01T05:00:00Z")
	r.scanIssue(2, "closed", "2026-05-01T05:00:00Z", "2026-07-01T05:00:00Z", "ghi", "closed oldest")
	c, _ := stage(t, r)
	if c.Open == nil || c.Open.Number != 7 || c.Open.Body != open || !slices.Equal(c.Open.Comments, []string{"previous scan"}) {
		t.Fatalf("Open = %+v, want #7 with its body and comment", c.Open)
	}
	if c.LastClosed == nil || c.LastClosed.Number != 3 || !slices.Equal(c.LastClosed.Comments, []string{"old comment"}) {
		t.Fatalf("LastClosed = %+v, want #3 with its comment", c.LastClosed)
	}
	if c.Base != head || c.Language != "nl" {
		t.Fatalf("Base = %q, Language = %q; want %q, nl", c.Base, c.Language, head)
	}
	if !slices.Equal(c.SearchDirs, []string{"internal"}) {
		t.Fatalf("SearchDirs = %v, want [internal]", c.SearchDirs)
	}
	for _, h := range c.Hotspots {
		if !strings.HasPrefix(h.Path, "internal/") {
			t.Fatalf("hotspot %s is outside search_dirs", h.Path)
		}
	}
	if len(c.Hotspots) == 0 {
		t.Fatal("no hotspots from search_dirs")
	}
}

func TestStageTruncatesIssueBodies(t *testing.T) {
	// Fails if an issue body reaches the corpus uncut, or if the cut also hits the scan issue.
	r := newRig(t)
	long := strings.Repeat("é", 5000)
	r.issue(12, "someone", "open", "2026-11-01T05:00:00Z", "", long)
	open := r.scanIssue(7, "open", "2026-11-01T05:00:00Z", "", "abc", long)
	c, _ := stage(t, r)
	if len(c.Issues) != 1 || c.Issues[0].Number != 12 {
		t.Fatalf("Issues = %+v, want #12", c.Issues)
	}
	if n := utf8.RuneCountInString(c.Issues[0].Body); n > maxIssueBody || n < maxIssueBody/2 {
		t.Fatalf("issue body has %d characters, want at most %d", n, maxIssueBody)
	}
	if c.Open == nil || c.Open.Body != open {
		t.Fatal("the scan issue body was cut")
	}
}

func TestStageCorpusExcludesScanIssueFromIssueList(t *testing.T) {
	// Fails if the open scan issue shows up among the open issues (the skill would then count it
	// as an issue a candidate falls under).
	r := newRig(t)
	r.issue(12, "someone", "open", "2026-11-01T05:00:00Z", "", "work", "bug")
	r.scanIssue(7, "open", "2026-11-01T05:00:00Z", "", "abc", "scan")
	c, _ := stage(t, r)
	var nums []int
	for _, is := range c.Issues {
		nums = append(nums, is.Number)
	}
	if !slices.Equal(nums, []int{12}) {
		t.Fatalf("issue numbers = %v, want [12]", nums)
	}
	if !slices.Equal(c.Issues[0].Labels, []string{"bug"}) || c.Issues[0].Title != "issue 12" {
		t.Fatalf("issue = %+v, want title and labels", c.Issues[0])
	}
}

func TestStageOverCapKeepsScanBody(t *testing.T) {
	// Fails if the corpus exceeds maxCorpus, if the scan issue body is cut, if scan comments
	// drop newest first or before they must, or if other issue bodies are not shortened further
	// once the comments are gone.
	t.Run("comments drop oldest first", func(t *testing.T) {
		r := newRig(t)
		open := r.scanIssue(7, "open", "2026-11-01T05:00:00Z", "", "abc", strings.Repeat("s", 50000))
		for i := range 9 {
			r.comment(7, fmt.Sprintf("c%d ", i)+strings.Repeat("c", 50000), fmt.Sprintf("2026-11-%02dT05:00:00Z", i+2))
		}
		r.issue(12, "someone", "open", "2026-11-01T05:00:00Z", "", strings.Repeat("b", 3000))
		c, b := stage(t, r)
		if len(b) > maxCorpus {
			t.Fatalf("corpus is %d bytes, want at most %d", len(b), maxCorpus)
		}
		if c.Open == nil || c.Open.Body != open {
			t.Fatal("the scan issue body was cut")
		}
		var heads []string
		for _, cm := range c.Open.Comments {
			heads = append(heads, cm[:3])
		}
		if len(heads) == 0 || len(heads) == 9 || heads[0] == "c0 " || heads[len(heads)-1] != "c8 " {
			t.Fatalf("kept comments %v, want the newest ones and not all", heads)
		}
		if n := utf8.RuneCountInString(c.Issues[0].Body); n != maxIssueBody {
			t.Fatalf("issue body has %d characters, want %d while comments can still go", n, maxIssueBody)
		}
	})
	t.Run("issue bodies shorten after the comments", func(t *testing.T) {
		r := newRig(t)
		open := r.scanIssue(7, "open", "2026-11-01T05:00:00Z", "", "abc", strings.Repeat("s", 50000))
		r.comment(7, "c0 "+strings.Repeat("c", 50000), "2026-11-02T05:00:00Z")
		for n := 100; n < 320; n++ {
			r.issue(n, "someone", "open", "2026-11-01T05:00:00Z", "", strings.Repeat("b", 3000))
		}
		c, b := stage(t, r)
		if len(b) > maxCorpus {
			t.Fatalf("corpus is %d bytes, want at most %d", len(b), maxCorpus)
		}
		if c.Open == nil || c.Open.Body != open || len(c.Open.Comments) != 0 {
			t.Fatalf("scan issue cut or comments kept: body ok %v, %d comments", c.Open != nil && c.Open.Body == open, len(c.Open.Comments))
		}
		if len(c.Issues) != 220 {
			t.Fatalf("%d issues, want 220", len(c.Issues))
		}
		if n := utf8.RuneCountInString(c.Issues[0].Body); n >= maxIssueBody || n == 0 {
			t.Fatalf("issue body has %d characters, want it shortened below %d", n, maxIssueBody)
		}
	})
}

func TestLaunchCommandAndTools(t *testing.T) {
	// Fails if the command loses its corpus or out argument, or the tools are not the params'.
	j := bind(t)
	p, err := j.Launch(scheduled.Verdict{RunID: "scan-x"}, scheduled.Env{})
	if err != nil {
		t.Fatal(err)
	}
	want := "/lighthouse-architecture-scan --corpus .lighthouse/run/architecture-scan-corpus.json --out architecture-scan-result.md"
	if p.Command != want {
		t.Fatalf("Command = %q, want %q", p.Command, want)
	}
	if !slices.Equal(p.Tools, []string{"Read", "Glob", "Grep", "Write"}) || p.MaxRuntime != 60*time.Minute {
		t.Fatalf("plan = %+v", p)
	}
	for _, s := range []string{"ask none", "architecture-scan-result.md", "do NOT push"} {
		if !strings.Contains(p.Instructions, s) {
			t.Errorf("Instructions miss %q", s)
		}
	}
}

package mapplanned

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Classification: io (real git with a bare origin, the real forge client against the fake
// GitHub of forgetest). The regression surface is which issues the gate lets through: the rows
// it reads (from origin, never the work tree), the documents it reads, the milestone rule, and
// the job comments that settle or hold an issue. Each test names the production change that
// makes it fail.

const labelsPath = "docs/labels.md"

const loopTable = "# Labels\n\n| Label | Document | Planned |\n|---|---|---|\n| `loop` | `docs/site/data/loop.json` | always |\n"

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
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
	t                  *testing.T
	root, origin, repo string
	srv                *forgetest.Server
	labels             string // Paths.Labels
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
	gitIn(t, root, "init", "-q", "--bare", "-b", "main", origin)
	gitIn(t, root, "clone", "-q", origin, repo)
	gitIn(t, repo, "checkout", "-q", "-b", "main")
	put(t, repo, "README.md", "x\n")
	r := &rig{t: t, root: root, origin: origin, repo: repo, srv: forgetest.New(t), labels: labelsPath}
	r.srv.Login = jobLogin
	r.srv.Issues = map[int]map[string]any{}
	r.commit()
	return r
}

// commit commits the work tree and pushes it to origin.
func (r *rig) commit() {
	r.t.Helper()
	gitIn(r.t, r.repo, "add", "-A")
	gitIn(r.t, r.repo, "commit", "-q", "--allow-empty", "-m", "c")
	gitIn(r.t, r.repo, "push", "-q", "origin", "main")
}

// issue adds open issue n with label to the forge; milestone "" sets none.
func (r *rig) issue(n int, label, milestone string) {
	it := map[string]any{"number": n, "title": "t", "state": "open", "labels": []any{map[string]any{"name": label}}}
	if milestone != "" {
		it["milestone"] = map[string]any{"number": 1, "title": milestone}
	}
	r.srv.Issues[n] = it
}

// comment adds a comment by author to issue n.
func (r *rig) comment(n int, author, body string) {
	if r.srv.Comments == nil {
		r.srv.Comments = map[int][]map[string]any{}
	}
	r.srv.Comments[n] = append(r.srv.Comments[n], map[string]any{
		"id": len(r.srv.Comments[n]) + 100*n, "body": body, "created_at": "2026-11-20T10:00:00Z",
		"user": map[string]any{"login": author},
	})
}

func (r *rig) env() scheduled.Env {
	c := forge.New("o/r", "secret-token")
	c.BaseURL = r.srv.URL()
	return scheduled.Env{
		Repo: repoconfig.Repo{Dir: r.repo, DefaultBranch: "main", Slug: "o/r", Kind: "github", Language: "nl",
			Paths: repoconfig.Paths{Labels: r.labels}, Labels: repoconfig.Labels{NeedsDecision: "needs-decision"}},
		Now:     time.Date(2026, 11, 22, 5, 0, 0, 0, time.UTC),
		Run:     workplace.ExecRunner(),
		Forge:   c,
		Trusted: trustedLogins,
	}
}

func bind(t *testing.T) *Job {
	t.Helper()
	j, err := scheduled.Bind(New(), repoconfig.Schedule{Name: Name, Job: Name})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	return j.(*Job)
}

func gate(t *testing.T, r *rig) scheduled.Verdict {
	t.Helper()
	v, err := bind(t).Gate(context.Background(), r.env())
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	return v
}

// loopRig is a rig with the loop row on origin, an empty loop document, and open issue 12.
func loopRig(t *testing.T, doc string) *rig {
	t.Helper()
	r := newRig(t)
	put(t, r.repo, labelsPath, loopTable)
	put(t, r.repo, "docs/site/data/loop.json", doc)
	r.commit()
	r.issue(12, "loop", "")
	return r
}

func TestGateSkipsWithoutTaxonomy(t *testing.T) {
	// Fails if the gate reads rows without Paths.Labels, treats a file without a Planned row as
	// work, or fails (instead of skipping) when the labels file is missing on origin.
	const want = "no planned documents"
	t.Run("no paths.labels", func(t *testing.T) {
		r := loopRig(t, "[]")
		r.labels = ""
		if v := gate(t, r); v.Skip != want {
			t.Fatalf("Skip = %q, want %q", v.Skip, want)
		}
	})
	t.Run("no row with Planned", func(t *testing.T) {
		r := newRig(t)
		put(t, r.repo, labelsPath, "| Label | Document | Planned |\n|---|---|---|\n| `loop` | `docs/site/data/loop.json` |  |\n")
		r.commit()
		r.issue(12, "loop", "")
		if v := gate(t, r); v.Skip != want {
			t.Fatalf("Skip = %q, want %q", v.Skip, want)
		}
	})
	t.Run("file missing on origin", func(t *testing.T) {
		r := newRig(t)
		r.issue(12, "loop", "")
		if v := gate(t, r); v.Skip != want {
			t.Fatalf("Skip = %q, want %q", v.Skip, want)
		}
	})
}

func TestGateFailsWhenOriginHasNoBranch(t *testing.T) {
	// Fails if a ref that does not resolve is read as a missing labels file (a skip) instead of
	// an error.
	r := loopRig(t, "[]")
	env := r.env()
	env.Repo.DefaultBranch = "nope"
	if _, err := bind(t).Gate(context.Background(), env); err == nil {
		t.Fatal("Gate on a branch origin lacks: no error")
	}
}

func TestGateLetsThroughAnIssueMissingFromItsDocument(t *testing.T) {
	// Fails if the gate does not list the label's open issues, or counts an issue as on the map
	// when the document does not name it.
	r := loopRig(t, `{"items": [{"issue": 3, "min_lines": 12}]}`)
	v := gate(t, r)
	if v.Skip != "" || v.Score != 1 {
		t.Fatalf("Verdict = %+v, want a run with Score 1", v)
	}
}

func TestGateSkipsWhenEveryIssueIsOnItsMap(t *testing.T) {
	// Fails if the gate ignores the issue numbers of the document.
	r := loopRig(t, `{"items": [{"issue": 12}]}`)
	if v := gate(t, r); v.Skip != "every planned issue is on its map" {
		t.Fatalf("Skip = %q, want every planned issue is on its map", v.Skip)
	}
}

func TestGateSkipsARowWithoutLabel(t *testing.T) {
	// Fails if a row with an empty Label lists every open issue (ListIssues without a label).
	r := newRig(t)
	put(t, r.repo, labelsPath, "| Label | Document | Planned |\n|---|---|---|\n|  | `docs/site/data/loop.json` | always |\n")
	put(t, r.repo, "docs/site/data/loop.json", "[]")
	r.commit()
	r.issue(12, "loop", "")
	if v := gate(t, r); v.Skip == "" {
		t.Fatalf("Verdict = %+v, want a skip", v)
	}
}

func TestGateMilestoneRowNeedsAMilestone(t *testing.T) {
	// Review Focus 5. Fails if Planned = milestone lets an issue without a milestone through.
	for _, tc := range []struct {
		milestone string
		run       bool
	}{{"", false}, {"v1", true}} {
		t.Run("milestone="+tc.milestone, func(t *testing.T) {
			r := newRig(t)
			put(t, r.repo, labelsPath, strings.Replace(loopTable, "always", "milestone", 1))
			put(t, r.repo, "docs/site/data/loop.json", "[]")
			r.commit()
			r.issue(12, "loop", tc.milestone)
			if v := gate(t, r); (v.Skip == "") != tc.run {
				t.Fatalf("Verdict = %+v, want run %v", v, tc.run)
			}
		})
	}
}

func TestGateLeavesOutOpenAndSettledIssues(t *testing.T) {
	// Fails if an issue with an unanswered job question, or with a job decision, goes through
	// the gate again; or if an answered question (other than off) stays out.
	q := RenderQuestion(Question{Issue: 12, Topic: TopicPlace, Text: "Waar?", Options: []string{"hier", "nieuw", "niet"},
		Recommended: "a", Off: "c"}, "nl")
	t.Run("open question and decision", func(t *testing.T) {
		r := loopRig(t, "[]")
		r.comment(12, jobLogin, q)
		r.issue(13, "loop", "")
		r.comment(13, jobLogin, RenderDecision(13, "nl"))
		if v := gate(t, r); v.Skip != "every planned issue is on its map" {
			t.Fatalf("Verdict = %+v, want a skip", v)
		}
	})
	t.Run("answered question", func(t *testing.T) {
		r := loopRig(t, "[]")
		r.comment(12, jobLogin, q)
		r.comment(12, "owner", "a")
		if v := gate(t, r); v.Skip != "" || v.Score != 1 {
			t.Fatalf("Verdict = %+v, want a run with Score 1", v)
		}
		// Stage reads the answer from due: fails if due drops it.
		due, err := bind(t).due(context.Background(), r.env(), r.repo, "origin/main")
		if err != nil {
			t.Fatal(err)
		}
		if len(due) != 1 || due[0].Issue.Number != 12 || due[0].Row.Label != "loop" || due[0].Answer == nil || due[0].Answer.Letter != "a" {
			t.Fatalf("due = %+v, want #12 under loop with answer a", due)
		}
	})
}

func TestDueFailsOnARefThatDoesNotResolve(t *testing.T) {
	// Fails if a ref that does not resolve is read as a missing labels file (no rows, no error).
	r := loopRig(t, "[]")
	if _, err := bind(t).due(context.Background(), r.env(), r.repo, "origin/nope"); err == nil {
		t.Fatal("due on a ref that does not resolve: no error")
	}
}

func TestGateReadsOrigin(t *testing.T) {
	// Fails if the gate reads labels.md or a document from the work tree instead of origin.
	r := loopRig(t, `[{"issue": 12}]`)
	put(t, r.repo, labelsPath, loopTable+"| `kaart` | `docs/kaart.yaml` | always |\n")
	put(t, r.repo, "docs/site/data/loop.json", "[]")
	r.issue(14, "kaart", "")
	if v := gate(t, r); v.Skip != "every planned issue is on its map" {
		t.Fatalf("Verdict = %+v, want a skip: only origin counts", v)
	}
}

func TestGateFailsOnABrokenDocument(t *testing.T) {
	// Fails if a document that does not parse is skipped or read as empty.
	r := loopRig(t, `{"items": [`)
	_, err := bind(t).Gate(context.Background(), r.env())
	if err == nil || !strings.Contains(err.Error(), "docs/site/data/loop.json") {
		t.Fatalf("Gate err = %v, want an error naming the document", err)
	}
}

func TestGateFailsWithoutAForge(t *testing.T) {
	// Fails if the gate dereferences a nil Forge (a repo the tick has no forge for).
	r := loopRig(t, "[]")
	env := r.env()
	env.Forge, env.Trusted = nil, nil
	if _, err := bind(t).Gate(context.Background(), env); err == nil {
		t.Fatal("Gate without a forge: no error")
	}
}

func TestConfigureRefusesParams(t *testing.T) {
	// Fails if Configure accepts a param the job does not read.
	if _, err := New().(*Job).Configure(repoconfig.Schedule{Name: Name, Job: Name, Params: map[string]any{"x": 1}}); err == nil {
		t.Fatal("Configure with params: no error")
	}
	if _, err := New().(*Job).Configure(repoconfig.Schedule{Name: Name, Job: Name}); err != nil {
		t.Fatalf("Configure without params: %v", err)
	}
}

func TestLaunchPlan(t *testing.T) {
	// Fails if the command, the toolset (no Bash), the limit, or the unattended rules change.
	p, err := bind(t).Launch(scheduled.Verdict{Score: 2}, scheduled.Env{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Command != "/lighthouse-map-planned --corpus .lighthouse/run/map-planned-corpus.json --result .lighthouse/run/map-planned-result.json" {
		t.Errorf("Command = %q", p.Command)
	}
	if strings.Join(p.Tools, " ") != "Read Write Edit Glob Grep" {
		t.Errorf("Tools = %v", p.Tools)
	}
	if p.MaxRuntime != 30*time.Minute {
		t.Errorf("MaxRuntime = %v", p.MaxRuntime)
	}
	for _, s := range []string{"unattended", "Do NOT create a git commit", "do NOT push", "result file"} {
		if !strings.Contains(p.Instructions, s) {
			t.Errorf("Instructions lack %q:\n%s", s, p.Instructions)
		}
	}
}

func TestAnUnboundJobRefusesToRun(t *testing.T) {
	// Fails if the registered instance runs without Bind.
	if _, err := New().Gate(context.Background(), scheduled.Env{}); err == nil {
		t.Error("Gate on the unbound job: no error")
	}
	if _, err := New().Launch(scheduled.Verdict{}, scheduled.Env{}); err == nil {
		t.Error("Launch on the unbound job: no error")
	}
}

// gitOut runs git in dir and returns its trimmed output.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

const runBranch = "scheduled/map-planned-20261122T0500-1"

// workplace adds the run's work tree on runBranch at origin/main and returns it with the start.
func (r *rig) workplace() (dir, start string) {
	r.t.Helper()
	dir = filepath.Join(r.root, "work")
	gitIn(r.t, r.repo, "worktree", "add", "-q", "-b", runBranch, dir, "origin/main")
	return dir, gitOut(r.t, dir, "rev-parse", "HEAD")
}

// posts are the comment bodies POSTed to issue n.
func (r *rig) posts(n int) []string {
	var bodies []string
	for _, p := range r.srv.Posts {
		if p.Method == "POST" && p.Path == "/repos/o/r/issues/"+strconv.Itoa(n)+"/comments" {
			bodies = append(bodies, p.Body["body"].(string))
		}
	}
	return bodies
}

// writes counts every write request the forge received.
func (r *rig) writes() int { return len(r.srv.Posts) }

// stagedEnv is a loop rig's env with a work tree, after Stage.
func stagedEnv(t *testing.T, r *rig, j *Job) scheduled.Env {
	t.Helper()
	dir, _ := r.workplace()
	env := r.env()
	env.Dir = dir
	if err := j.Stage(context.Background(), env, scheduled.Verdict{Score: 1}); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	return env
}

func readCorpus(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".lighthouse/run/map-planned-corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("corpus: %v\n%s", err, b)
	}
	return c
}

func TestStageWritesTheCorpus(t *testing.T) {
	// Fails if Stage leaves out the due issue, its row's document, the taxonomy path or the
	// language, writes an answer where there is none, or drops a trusted answer.
	t.Run("no answer", func(t *testing.T) {
		r := loopRig(t, "[]")
		r.srv.Issues[12]["body"] = "Het lichaam"
		c := readCorpus(t, stagedEnv(t, r, bind(t)).Dir)
		if c["language"] != "nl" || c["taxonomy"] != labelsPath {
			t.Errorf("language %v, taxonomy %v; want nl and %s", c["language"], c["taxonomy"], labelsPath)
		}
		docs := c["documents"].([]any)
		if len(docs) != 1 || docs[0].(map[string]any)["document"] != "docs/site/data/loop.json" || docs[0].(map[string]any)["planned"] != "always" {
			t.Errorf("documents = %v", docs)
		}
		issues := c["issues"].([]any)
		if len(issues) != 1 {
			t.Fatalf("issues = %v, want #12 only", issues)
		}
		is := issues[0].(map[string]any)
		if is["number"] != 12.0 || is["body"] != "Het lichaam" || is["label"] != "loop" || is["document"] != "docs/site/data/loop.json" {
			t.Errorf("issue = %v", is)
		}
		if a, ok := is["answer"]; !ok || a != nil {
			t.Errorf("answer = %v (present %v), want null", a, ok)
		}
	})
	t.Run("answered", func(t *testing.T) {
		r := loopRig(t, "[]")
		r.comment(12, jobLogin, RenderQuestion(Question{Issue: 12, Topic: TopicPlace, Text: "Waar?",
			Options: []string{"hier", "nieuw", "niet"}, Recommended: "a", Off: "c"}, "nl"))
		r.comment(12, "owner", "b, onder intake")
		c := readCorpus(t, stagedEnv(t, r, bind(t)).Dir)
		a, _ := c["issues"].([]any)[0].(map[string]any)["answer"].(map[string]any)
		if a["letter"] != "b" || a["topic"] != "place" || a["text"] != "b, onder intake" {
			t.Errorf("answer = %v, want letter b on place", a)
		}
	})
}

// writeResult puts the result file into the work tree.
func writeResult(t *testing.T, dir, body string) {
	t.Helper()
	put(t, dir, ".lighthouse/run/map-planned-result.json", body)
}

const placeQ12 = `{"issue":12,"topic":"place","text":"Waar?","options":["onder intake","nieuw onderdeel op de kaart","niet op de kaart"],"recommended":"a","off":"c"}`

func TestCleanupPostsQuestionsAndDecisions(t *testing.T) {
	// Fails if Cleanup does not post the question or the decision, posts without the job
	// heading or machine line, labels a place question, or leaves the staging files behind.
	r := loopRig(t, "[]")
	r.issue(13, "loop", "")
	j := bind(t)
	env := stagedEnv(t, r, j)
	writeResult(t, env.Dir, `{"questions":[`+placeQ12+`],"decisions":[{"issue":13,"value":"no-item"}]}`)
	if err := j.Cleanup(context.Background(), env, env.Dir); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	q, d := r.posts(12), r.posts(13)
	if len(q) != 1 || !strings.HasPrefix(q[0], "🗺 Kaart · map-planned\n<!-- lighthouse:map-planned kind=question topic=place off=c -->") {
		t.Errorf("posts on #12 = %q, want one place question", q)
	}
	if len(d) != 1 || !strings.Contains(d[0], "kind=decision") {
		t.Errorf("posts on #13 = %q, want one decision", d)
	}
	for _, p := range r.srv.Posts {
		if strings.HasSuffix(p.Path, "/labels") || p.Method == "PATCH" {
			t.Errorf("label write %s %s: a place question sets no label", p.Method, p.Path)
		}
	}
	for _, f := range []string{".lighthouse/run/map-planned-corpus.json", ".lighthouse/run/map-planned-result.json", ".lighthouse"} {
		if _, err := os.Lstat(filepath.Join(env.Dir, f)); !os.IsNotExist(err) {
			t.Errorf("%s left behind (err %v)", f, err)
		}
	}
}

func TestCleanupLabelsAWishQuestion(t *testing.T) {
	// Fails if a wish question does not set labels.needs_decision on its issue.
	r := loopRig(t, "[]")
	j := bind(t)
	env := stagedEnv(t, r, j)
	writeResult(t, env.Dir, `{"questions":[{"issue":12,"topic":"wish","text":"Willen we dit?","options":["ja, op de kaart","nee, niet op de kaart"],"recommended":"a","off":"b"}]}`)
	if err := j.Cleanup(context.Background(), env, env.Dir); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if q := r.posts(12); len(q) != 1 || !strings.Contains(q[0], "topic=wish") {
		t.Errorf("posts on #12 = %q, want one wish question", q)
	}
	is, err := env.Forge.Issue(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(is.Labels, "needs-decision") {
		t.Errorf("labels of #12 = %v, want needs-decision", is.Labels)
	}
}

const wishQ12 = `{"issue":12,"topic":"wish","text":"Willen we dit?","options":["ja, op de kaart","nee, niet op de kaart"],"recommended":"a","off":"b"}`

func TestCleanupLabelsAWishBeforeItsQuestion(t *testing.T) {
	// Fails if Cleanup posts the wish question before labels.needs_decision is set: a label
	// that then fails leaves an open question without the label, which nothing ever repairs.
	t.Run("label fails", func(t *testing.T) {
		r := loopRig(t, "[]")
		r.srv.Fail = map[string]int{"POST /repos/o/r/issues/12/labels": 500}
		j := bind(t)
		env := stagedEnv(t, r, j)
		writeResult(t, env.Dir, `{"questions":[`+wishQ12+`]}`)
		if err := j.Cleanup(context.Background(), env, env.Dir); err == nil {
			t.Fatal("Cleanup: no error")
		}
		if q := r.posts(12); len(q) != 0 {
			t.Errorf("posts on #12 = %q, want none after a failed label", q)
		}
	})
	t.Run("comment fails after the label", func(t *testing.T) {
		// The issue keeps no job comment, so the gate lets it through and a next run asks again.
		r := loopRig(t, "[]")
		r.srv.FailFirst = map[string]int{"POST /repos/o/r/issues/12/comments": 500}
		j := bind(t)
		env := stagedEnv(t, r, j)
		writeResult(t, env.Dir, `{"questions":[`+wishQ12+`]}`)
		if err := j.Cleanup(context.Background(), env, env.Dir); err == nil {
			t.Fatal("Cleanup: no error")
		}
		if v := gate(t, r); v.Score != 1 {
			t.Errorf("Gate after a failed comment = %+v, want #12 due", v)
		}
		writeResult(t, env.Dir, `{"questions":[`+wishQ12+`]}`)
		if err := j.Cleanup(context.Background(), env, env.Dir); err != nil {
			t.Fatalf("second Cleanup: %v", err)
		}
		is, err := env.Forge.Issue(context.Background(), 12)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(is.Labels, "needs-decision") {
			t.Errorf("labels of #12 = %v, want needs-decision", is.Labels)
		}
		if v := gate(t, r); v.Skip == "" {
			t.Errorf("Gate after the question = %+v, want a skip", v)
		}
	})
}

func TestCleanupPostsOncePerIssue(t *testing.T) {
	// Review Focus 3. Fails if a repeated Cleanup (a postcheck after a transient error) posts
	// a second question for an issue that already has an open one.
	r := loopRig(t, "[]")
	j := bind(t)
	env := stagedEnv(t, r, j)
	result := `{"questions":[` + placeQ12 + `]}`
	for range 2 {
		writeResult(t, env.Dir, result)
		if err := j.Cleanup(context.Background(), env, env.Dir); err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
	}
	if q := r.posts(12); len(q) != 1 {
		t.Errorf("posts on #12 = %d, want 1", len(q))
	}
}

// refuseResult runs Cleanup on result in a loop rig with #40 (another label) and #42 (closed)
// on the forge, and wants an error and no forge write at all.
func refuseResult(t *testing.T, result string) {
	t.Helper()
	r := loopRig(t, "[]")
	r.issue(40, "other", "")
	r.issue(42, "loop", "")
	r.srv.Issues[42]["state"] = "closed"
	j := bind(t)
	env := stagedEnv(t, r, j)
	writeResult(t, env.Dir, result)
	if err := j.Cleanup(context.Background(), env, env.Dir); err == nil {
		t.Fatal("Cleanup: no error")
	}
	if n := r.writes(); n != 0 {
		t.Errorf("%d forge writes, want none: %+v", n, r.srv.Posts)
	}
}

func TestCleanupRefusesAPlaceQuestionWithoutOff(t *testing.T) {
	// Fails if Cleanup posts a question whose off letter is empty, missing or outside its
	// options (the gate could never settle it), or a question that is malformed otherwise.
	off := func(v string) string { return strings.Replace(placeQ12, `"off":"c"`, v, 1) }
	for name, result := range map[string]string{
		"off empty":                       `{"questions":[` + off(`"off":""`) + `]}`,
		"off missing":                     `{"questions":[` + off(`"x":1`) + `]}`,
		"off outside the options":         `{"questions":[` + off(`"off":"d"`) + `]}`,
		"recommended outside the options": `{"questions":[` + strings.Replace(placeQ12, `"recommended":"a"`, `"recommended":"f"`, 1) + `]}`,
		"unknown topic":                   `{"questions":[` + strings.Replace(placeQ12, `"place"`, `"where"`, 1) + `]}`,
		"one option":                      `{"questions":[{"issue":12,"topic":"place","text":"Waar?","options":["a"],"recommended":"a","off":"a"}]}`,
		"no text":                         `{"questions":[` + strings.Replace(placeQ12, `"Waar?"`, `" "`, 1) + `]}`,
		"not json":                        `{"questions":[`,
	} {
		t.Run(name, func(t *testing.T) { refuseResult(t, result) })
	}
}

func TestCleanupRefusesAMalformedWishQuestion(t *testing.T) {
	// Fails if Cleanup posts a wish question with other than two options, or whose off letter
	// is not b (the gate would settle a yes or never settle a no).
	for name, result := range map[string]string{
		"three options": `{"questions":[` + strings.Replace(wishQ12, `"nee, niet op de kaart"]`, `"nee, niet op de kaart","later"]`, 1) + `]}`,
		"off a":         `{"questions":[` + strings.Replace(wishQ12, `"off":"b"`, `"off":"a"`, 1) + `]}`,
	} {
		t.Run(name, func(t *testing.T) { refuseResult(t, result) })
	}
}

func TestCleanupRefusesAResultOutsideThePlannedIssues(t *testing.T) {
	// Fails if Cleanup posts for an issue that carries no label of a planned row, is closed or
	// missing on the forge, or appears twice, or for a decision value the job does not know;
	// each case has a valid entry for #12 first, so nothing may be posted before validation ends.
	for name, result := range map[string]string{
		"issue without a planned label": `{"decisions":[{"issue":12,"value":"no-item"},{"issue":40,"value":"no-item"}]}`,
		"issue not on the forge":        `{"decisions":[{"issue":12,"value":"no-item"},{"issue":41,"value":"no-item"}]}`,
		"closed issue":                  `{"decisions":[{"issue":12,"value":"no-item"},{"issue":42,"value":"no-item"}]}`,
		"same issue twice":              `{"questions":[` + placeQ12 + `],"decisions":[{"issue":12,"value":"no-item"}]}`,
		"unknown decision value":        `{"decisions":[{"issue":12,"value":"maybe"}]}`,
	} {
		t.Run(name, func(t *testing.T) { refuseResult(t, result) })
	}
}

func TestCleanupAcceptsAMissingResult(t *testing.T) {
	// Fails if a run without a result file (nothing to ask) fails or writes to the forge.
	r := loopRig(t, "[]")
	j := bind(t)
	env := stagedEnv(t, r, j)
	if err := j.Cleanup(context.Background(), env, env.Dir); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if n := r.writes(); n != 0 {
		t.Errorf("%d forge writes, want none", n)
	}
}

func TestAllowedIsASyntacticFilter(t *testing.T) {
	// Fails if Allowed depends on state of the instance (a resumed run gets a fresh one), lets
	// through a staging or git path, or refuses a document or the run's fragment.
	for _, j := range []scheduled.Job{New(), bind(t)} {
		for p, want := range map[string]bool{
			"docs/site/data/loop.json":                   true,
			"docs/kaart.yaml":                            true,
			"docs/kaart.yml":                             true,
			"changelog.d/scheduled-map-planned-x.md":     true,
			"changelog.d/other.md":                       false,
			"changelog.d/sub/scheduled-map-planned-x.md": false,
			".lighthouse/run/x":                          false,
			".lighthouse/run/map-planned-result.json":    false,
			".git/config":                                false,
			".claude/settings.json":                      false,
			".agents/x.json":                             false,
			"main.go":                                    false,
		} {
			if got := j.Allowed(p); got != want {
				t.Errorf("Allowed(%q) = %v, want %v", p, got, want)
			}
		}
	}
}

// checkRig is a loop rig with web/package.json and changelog.d/ on origin, and a work tree.
func checkRig(t *testing.T, doc string) (*rig, scheduled.Env) {
	t.Helper()
	r := loopRig(t, doc)
	put(t, r.repo, "web/package.json", `{"name":"web"}`)
	put(t, r.repo, "changelog.d/keep.md", "---\ntype: internal\n---\nx\n")
	r.commit()
	dir, _ := r.workplace()
	env := r.env()
	env.Dir = dir
	return r, env
}

func TestCheckRefusesANewUntrackedFile(t *testing.T) {
	// Fails if Check misses a file the agent added (untracked at the first Postcheck) that the
	// syntactic filter lets through.
	for _, p := range []string{"web/new.json", ".github/workflows/x.yml"} {
		t.Run(p, func(t *testing.T) {
			_, env := checkRig(t, "[]")
			put(t, env.Dir, p, "{}\n")
			err := bind(t).Check(context.Background(), env, env.Dir)
			if err == nil || !strings.Contains(err.Error(), p) {
				t.Fatalf("Check = %v, want an error naming %s", err, p)
			}
		})
	}
}

func TestCheckRefusesAChangedFileThatIsNoPlannedDocument(t *testing.T) {
	// Fails if Check lets through a tracked file that is no planned document, a change to the
	// labels file itself, or a fragment where the repo has no changelog.d/ at the base.
	t.Run("web/package.json", func(t *testing.T) {
		_, env := checkRig(t, "[]")
		put(t, env.Dir, "web/package.json", `{"name":"evil"}`)
		if err := bind(t).Check(context.Background(), env, env.Dir); err == nil || !strings.Contains(err.Error(), "web/package.json") {
			t.Fatalf("Check = %v, want an error naming web/package.json", err)
		}
	})
	t.Run("labels.md", func(t *testing.T) {
		_, env := checkRig(t, "[]")
		put(t, env.Dir, labelsPath, loopTable+"| `web` | `web/package.json` | always |\n")
		if err := bind(t).Check(context.Background(), env, env.Dir); err == nil || !strings.Contains(err.Error(), labelsPath) {
			t.Fatalf("Check = %v, want an error naming %s", err, labelsPath)
		}
	})
	t.Run("fragment without changelog.d at the base", func(t *testing.T) {
		r := loopRig(t, "[]")
		dir, _ := r.workplace()
		env := r.env()
		env.Dir = dir
		put(t, dir, "changelog.d/scheduled-map-planned-x.md", fragmentBody("nl"))
		if err := bind(t).Check(context.Background(), env, dir); err == nil || !strings.Contains(err.Error(), "changelog.d") {
			t.Fatalf("Check = %v, want an error naming changelog.d", err)
		}
	})
	t.Run("second fragment", func(t *testing.T) {
		_, env := checkRig(t, "[]")
		put(t, env.Dir, "changelog.d/scheduled-map-planned-x.md", fragmentBody("nl"))
		put(t, env.Dir, "changelog.d/scheduled-map-planned-y.md", fragmentBody("nl"))
		if err := bind(t).Check(context.Background(), env, env.Dir); err == nil || !strings.Contains(err.Error(), "more than one fragment") {
			t.Fatalf("Check = %v, want an error for the second fragment", err)
		}
	})
	t.Run("nothing changed", func(t *testing.T) {
		// Postcheck never checks an empty change set: Check on one means its base is wrong.
		_, env := checkRig(t, "[]")
		if err := bind(t).Check(context.Background(), env, env.Dir); err == nil {
			t.Fatal("Check on a clean work tree: no error")
		}
	})
	t.Run("fragment with other content", func(t *testing.T) {
		_, env := checkRig(t, "[]")
		put(t, env.Dir, "changelog.d/scheduled-map-planned-x.md", "---\ntype: added\n---\nIets anders.\n")
		if err := bind(t).Check(context.Background(), env, env.Dir); err == nil || !strings.Contains(err.Error(), "changelog.d/scheduled-map-planned-x.md") {
			t.Fatalf("Check = %v, want an error naming the fragment", err)
		}
	})
}

func TestCheckRefusesAnItemForAnIssueWithoutTheRowLabel(t *testing.T) {
	// Fails if Check accepts a new issue number on a document without asking the forge whether
	// the issue carries the row's label, or accepts a document that no longer parses.
	t.Run("issue without the label", func(t *testing.T) {
		r, env := checkRig(t, `[{"issue": 3}]`)
		r.issue(99, "other", "")
		put(t, env.Dir, "docs/site/data/loop.json", `[{"issue": 3}, {"issue": 99}]`)
		if err := bind(t).Check(context.Background(), env, env.Dir); err == nil || !strings.Contains(err.Error(), "#99") {
			t.Fatalf("Check = %v, want an error naming #99", err)
		}
	})
	t.Run("issue with the label", func(t *testing.T) {
		r, env := checkRig(t, `[{"issue": 3}]`)
		r.issue(99, "loop", "")
		put(t, env.Dir, "docs/site/data/loop.json", `[{"issue": 3}, {"issue": 99}]`)
		if err := bind(t).Check(context.Background(), env, env.Dir); err != nil {
			t.Fatalf("Check = %v, want nil", err)
		}
	})
	t.Run("broken document", func(t *testing.T) {
		_, env := checkRig(t, "[]")
		put(t, env.Dir, "docs/site/data/loop.json", `[{"issue": `)
		if err := bind(t).Check(context.Background(), env, env.Dir); err == nil || !strings.Contains(err.Error(), "loop.json") {
			t.Fatalf("Check = %v, want an error naming the document", err)
		}
	})
	t.Run("deleted document", func(t *testing.T) {
		_, env := checkRig(t, "[]")
		must(t, os.Remove(filepath.Join(env.Dir, "docs/site/data/loop.json")))
		if err := bind(t).Check(context.Background(), env, env.Dir); err == nil || !strings.Contains(err.Error(), "loop.json") {
			t.Fatalf("Check = %v, want an error naming the document", err)
		}
	})
}

func TestCheckRefusesAnItemTheGateWouldNotLetThrough(t *testing.T) {
	// Fails if Check accepts a new item for a closed issue, or, under a row with Planned
	// milestone, for an issue without a milestone: the gate lets neither through.
	milestoneRig := func(t *testing.T) (*rig, scheduled.Env) {
		t.Helper()
		r := newRig(t)
		put(t, r.repo, labelsPath, strings.Replace(loopTable, "| always |", "| milestone |", 1))
		put(t, r.repo, "docs/site/data/loop.json", "[]")
		r.commit()
		dir, _ := r.workplace()
		env := r.env()
		env.Dir = dir
		return r, env
	}
	t.Run("closed issue", func(t *testing.T) {
		r, env := checkRig(t, "[]")
		r.issue(99, "loop", "")
		r.srv.Issues[99]["state"] = "closed"
		put(t, env.Dir, "docs/site/data/loop.json", `[{"issue": 99}]`)
		if err := bind(t).Check(context.Background(), env, env.Dir); err == nil || !strings.Contains(err.Error(), "#99") {
			t.Fatalf("Check = %v, want an error naming #99", err)
		}
	})
	t.Run("milestone row, issue without milestone", func(t *testing.T) {
		r, env := milestoneRig(t)
		r.issue(99, "loop", "")
		put(t, env.Dir, "docs/site/data/loop.json", `[{"issue": 99}]`)
		if err := bind(t).Check(context.Background(), env, env.Dir); err == nil || !strings.Contains(err.Error(), "#99") {
			t.Fatalf("Check = %v, want an error naming #99", err)
		}
	})
	t.Run("milestone row, issue with milestone", func(t *testing.T) {
		r, env := milestoneRig(t)
		r.issue(99, "loop", "v1")
		put(t, env.Dir, "docs/site/data/loop.json", `[{"issue": 99}]`)
		if err := bind(t).Check(context.Background(), env, env.Dir); err != nil {
			t.Fatalf("Check = %v, want nil", err)
		}
	})
}

func TestFinishWritesAnInternalFragment(t *testing.T) {
	// Fails if Finish writes no fragment where the repo keeps changelog.d/, one of another
	// type or name, one Check refuses, or one in a repo without changelog.d/.
	t.Run("with changelog.d", func(t *testing.T) {
		_, env := checkRig(t, "[]")
		j := bind(t)
		must(t, j.Finish(context.Background(), env, env.Dir, scheduled.Verdict{}))
		b, err := os.ReadFile(filepath.Join(env.Dir, "changelog.d/scheduled-map-planned-20261122T0500-1.md"))
		if err != nil {
			t.Fatalf("fragment: %v", err)
		}
		if !strings.HasPrefix(string(b), "---\ntype: internal\n---\n") {
			t.Errorf("fragment = %q, want type internal", b)
		}
		if err := bind(t).Check(context.Background(), env, env.Dir); err != nil {
			t.Errorf("Check after Finish = %v, want nil", err)
		}
	})
	t.Run("without changelog.d", func(t *testing.T) {
		r := loopRig(t, "[]")
		dir, _ := r.workplace()
		env := r.env()
		env.Dir = dir
		must(t, bind(t).Finish(context.Background(), env, dir, scheduled.Verdict{}))
		if _, err := os.Lstat(filepath.Join(dir, "changelog.d")); !os.IsNotExist(err) {
			t.Errorf("changelog.d made (err %v), want nothing written", err)
		}
	})
}

func TestGateRefusesADocumentOutsideTheRepo(t *testing.T) {
	// Fails if a Document path that is not a clean relative path inside the repo reaches git
	// or the corpus.
	for _, doc := range []string{"../x.json", "/etc/x.json", "docs/../x.json", "./x.json"} {
		t.Run(doc, func(t *testing.T) {
			r := newRig(t)
			put(t, r.repo, labelsPath, "| Label | Document | Planned |\n|---|---|---|\n| `loop` | `"+doc+"` | always |\n")
			r.commit()
			r.issue(12, "loop", "")
			if _, err := bind(t).Gate(context.Background(), r.env()); err == nil || !strings.Contains(err.Error(), "loop") {
				t.Fatalf("Gate = %v, want an error naming the row", err)
			}
		})
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

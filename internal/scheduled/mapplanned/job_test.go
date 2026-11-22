package mapplanned

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
	t      *testing.T
	repo   string
	srv    *forgetest.Server
	labels string // Paths.Labels
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
	r := &rig{t: t, repo: repo, srv: forgetest.New(t), labels: labelsPath}
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
			Paths: repoconfig.Paths{Labels: r.labels}},
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

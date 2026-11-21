package postartefact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/forge/gitlabtest"
	"github.com/CorneeldH/lighthouse/internal/procrun"
)

const testRun = "0f8e2c1a-1b2c-4d5e-8f90-123456789abc"

// artefactRepo is a clone of a bare origin with main pushed and a local branch issue-1-x that
// commits a spec and, when workflow is set, a workflow file.
type artefactRepo struct {
	t      *testing.T
	dir    string
	origin string
	spec   string
}

func newArtefactRepo(t *testing.T, workflow bool) *artefactRepo {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{
		"HOME": tmp, "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com",
		"GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com",
	} {
		t.Setenv(k, v)
	}
	r := &artefactRepo{t: t, dir: filepath.Join(tmp, "work"), origin: filepath.Join(tmp, "origin.git")}
	r.git(tmp, "init", "-q", "--bare", "-b", "main", r.origin)
	r.git(tmp, "clone", "-q", r.origin, r.dir)
	r.git(r.dir, "checkout", "-q", "-b", "main")
	r.commit("README", "readme\n")
	r.git(r.dir, "push", "-q", "origin", "main")
	r.git(r.dir, "checkout", "-q", "-b", "issue-1-x")
	if workflow {
		r.commit(".github/workflows/ci.yml", "on: push\n")
	}
	r.spec = r.commit("specs/s.md", "# Spec\n")
	return r
}

func (r *artefactRepo) git(dir string, args ...string) string {
	r.t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...).CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit writes name, commits it and returns its absolute path.
func (r *artefactRepo) commit(name, text string) string {
	r.t.Helper()
	path := filepath.Join(r.dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		r.t.Fatal(err)
	}
	r.git(r.dir, "add", "--", name)
	r.git(r.dir, "commit", "-q", "-m", "add "+name)
	return path
}

// onOrigin reports whether origin has the branch.
func (r *artefactRepo) onOrigin(branch string) bool {
	r.t.Helper()
	return r.git(r.origin, "for-each-ref", "refs/heads/"+branch) != ""
}

// head is the commit of the spec.
func (r *artefactRepo) head() string { return r.git(r.dir, "rev-parse", "HEAD") }

const githubTOML = "[project]\nlanguage = \"en\"\n[forge]\nrepo = \"o/r\"\n[paths]\nspecs = \"specs\"\n"

const gitlabTOML = "[project]\nlanguage = \"en\"\n[forge]\nkind = \"gitlab\"\nhost = \"git.example.org\"\nrepo = \"group/project\"\n[paths]\nspecs = \"specs\"\n"

// env runs git for real and gets its forge from open; toml is the lighthouse.toml.
func (r *artefactRepo) env(getenv func(string) string, toml string, open func(string) (forge.Forge, error)) Env {
	return Env{
		Run:   procrun.Exec(time.Minute),
		Forge: open,
		Config: func() (*config.Config, error) {
			path := filepath.Join(r.t.TempDir(), "lighthouse.toml")
			if err := os.WriteFile(path, []byte(toml), 0o600); err != nil {
				return nil, err
			}
			return config.Load(path)
		},
		Getenv: getenv,
	}
}

// forgeFixture is one fake forge of either kind, with the few things these tests set on it.
type forgeFixture struct {
	toml         string
	open         func(string) (forge.Forge, error)
	addCommit    func(sha string)
	failCommit   func(sha string)
	addPR        func(branch, state string)
	failComments func()
	posted       func() []string
}

func githubFixture(t *testing.T) *forgeFixture {
	t.Helper()
	srv := forgetest.New(t)
	f, err := forge.Open(forge.Config{Kind: "github", Repo: "o/r", Token: "t", BaseURL: srv.URL()})
	if err != nil {
		t.Fatal(err)
	}
	return &forgeFixture{
		toml:       githubTOML,
		open:       func(string) (forge.Forge, error) { return f, nil },
		addCommit:  func(sha string) { srv.Commits = append(srv.Commits, sha) },
		failCommit: func(sha string) { srv.Fail = map[string]int{"GET /repos/o/r/commits/" + sha: 500} },
		addPR: func(branch, state string) {
			node := map[string]any{"number": 5, "state": state, "headRefName": branch, "headRefOid": "abc", "url": "u"}
			if state == "MERGED" {
				node["mergedAt"] = "2026-01-01T00:00:00Z"
			}
			if srv.Branches == nil {
				srv.Branches = map[string][]map[string]any{}
			}
			srv.Branches[branch] = append(srv.Branches[branch], node)
		},
		failComments: func() { srv.Fail = map[string]int{"GET /repos/o/r/issues/1/comments": 500} },
		posted: func() []string {
			var out []string
			for _, c := range srv.IssueComments(1) {
				out = append(out, c["body"].(string))
			}
			return out
		},
	}
}

func gitlabFixture(t *testing.T) *forgeFixture {
	t.Helper()
	srv := gitlabtest.New(t)
	f, err := forge.Open(forge.Config{Kind: "gitlab", Repo: "group/project", Host: "git.example.org", Token: "t", BaseURL: srv.URL()})
	if err != nil {
		t.Fatal(err)
	}
	return &forgeFixture{
		toml:       gitlabTOML,
		open:       func(string) (forge.Forge, error) { return f, nil },
		addCommit:  srv.AddCommit,
		failCommit: srv.FailCommit,
		addPR: func(branch, state string) {
			m := map[string]any{"iid": 5, "state": map[string]string{"OPEN": "opened", "MERGED": "merged"}[state],
				"source_branch": branch, "source_project_id": 1, "target_project_id": 1}
			if state == "MERGED" {
				m["merged_at"] = "2026-01-01T00:00:00Z"
			}
			srv.AddMR(m)
		},
		posted: func() []string {
			cs, err := f.Comments(context.Background(), 1)
			if err != nil {
				t.Fatal(err)
			}
			var out []string
			for _, c := range cs {
				out = append(out, c.Body)
			}
			return out
		},
	}
}

var forges = map[string]func(*testing.T) *forgeFixture{"github": githubFixture, "gitlab": gitlabFixture}

func controllerRun(key string) string {
	if key == "LIGHTHOUSE_CONTROLLER_RUN" {
		return testRun
	}
	return ""
}

func noRun(string) string { return "" }

func TestPostArtefactRefusesWorkflowRange(t *testing.T) {
	r := newArtefactRepo(t, true)
	fx := githubFixture(t)

	_, err := Run(context.Background(), r.env(controllerRun, fx.toml, fx.open), 1, r.spec)

	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want an *Error", err)
	}
	for _, want := range []string{"artefact: ", ".github/workflows/ci.yml", "V2b", "next: push-workflows"} {
		if !strings.Contains(e.Line, want) {
			t.Errorf("error %q lacks %q", e.Line, want)
		}
	}
	if lower := strings.ToLower(e.Line); strings.Contains(lower, "scope") || strings.Contains(lower, "token") {
		t.Errorf("error advises on credentials: %q", e.Line)
	}
	if r.onOrigin("issue-1-x") {
		t.Error("the branch was pushed")
	}
}

func TestPostArtefactPushesWithoutWorkflows(t *testing.T) {
	r := newArtefactRepo(t, false)
	fx := githubFixture(t)
	fx.failComments()

	_, err := Run(context.Background(), r.env(controllerRun, fx.toml, fx.open), 1, r.spec)

	if err == nil || !strings.Contains(err.Error(), "forge failed") {
		t.Fatalf("err = %v, want the comment read after the push to fail", err)
	}
	if !r.onOrigin("issue-1-x") {
		t.Error("the branch was not pushed")
	}
}

func TestPostArtefactPushesWorkflowsOutsideControllerRun(t *testing.T) {
	r := newArtefactRepo(t, true)
	fx := githubFixture(t)
	fx.failComments()

	_, err := Run(context.Background(), r.env(noRun, fx.toml, fx.open), 1, r.spec)

	if err == nil || !strings.Contains(err.Error(), "forge failed") {
		t.Fatalf("err = %v, want the comment read after the push to fail", err)
	}
	if !r.onOrigin("issue-1-x") {
		t.Error("the branch was not pushed")
	}
}

// Without origin/main and with the branch not on origin the range is unknown: no push.
func TestPostArtefactCouldNotCheckRefuses(t *testing.T) {
	r := newArtefactRepo(t, false)
	r.git(r.dir, "update-ref", "-d", "refs/remotes/origin/main")
	fx := githubFixture(t)

	_, err := Run(context.Background(), r.env(controllerRun, fx.toml, fx.open), 1, r.spec)

	if err == nil || !strings.Contains(err.Error(), "COULD NOT CHECK") {
		t.Fatalf("err = %v, want COULD NOT CHECK", err)
	}
	if r.onOrigin("issue-1-x") {
		t.Error("the branch was pushed")
	}
}

// On both forges: an unknown commit is pushed and then posted; a known one is not pushed; a
// branch with an open PR is never pushed; a branch with only a merged PR is.
func TestPostArtefactPushDecisionPerForge(t *testing.T) {
	for name, mk := range forges {
		for _, tc := range []struct {
			name       string
			known      bool
			prState    string
			wantPush   bool
			wantErr    string
			wantPosted int
		}{
			{"unknown commit pushes and posts", false, "", true, "", 1},
			{"known commit does not push", true, "", false, "", 1},
			{"open PR does not push", false, "OPEN", false, "not on the forge", 0},
			{"only a merged PR pushes", false, "MERGED", true, "", 1},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				r := newArtefactRepo(t, false)
				fx := mk(t)
				if tc.known {
					fx.addCommit(r.head())
				}
				if tc.prState != "" {
					fx.addPR("issue-1-x", tc.prState)
				}

				line, err := Run(context.Background(), r.env(noRun, fx.toml, fx.open), 1, r.spec)

				if tc.wantErr == "" && err != nil {
					t.Fatalf("err = %v", err)
				}
				if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				if tc.wantErr == "" && !strings.Contains(line, "posted") {
					t.Errorf("line = %q, want a posted line", line)
				}
				if got := r.onOrigin("issue-1-x"); got != tc.wantPush {
					t.Errorf("pushed = %v, want %v", got, tc.wantPush)
				}
				if got := len(fx.posted()); got != tc.wantPosted {
					t.Errorf("posted %d comments, want %d", got, tc.wantPosted)
				}
			})
		}
	}
}

// A commit check that fails with anything but 404 is a forge failure; nothing is pushed or posted.
func TestPostArtefactCommitCheckFailureIsAForgeFailure(t *testing.T) {
	for name, mk := range forges {
		t.Run(name, func(t *testing.T) {
			r := newArtefactRepo(t, false)
			fx := mk(t)
			fx.failCommit(r.head())

			_, err := Run(context.Background(), r.env(noRun, fx.toml, fx.open), 1, r.spec)

			var e *Error
			if !errors.As(err, &e) || !strings.HasPrefix(e.Line, "artefact: forge failed: ") {
				t.Fatalf("err = %v, want an artefact: forge failed: line", err)
			}
			if r.onOrigin("issue-1-x") || len(fx.posted()) != 0 {
				t.Error("something was pushed or posted")
			}
		})
	}
}

// The header links to the file on the forge of the repo: GitHub's /blob/, GitLab's /-/blob/ on
// the configured host.
func TestPostArtefactSourceLinkFollowsTheForge(t *testing.T) {
	for name, want := range map[string]string{
		"github": "https://github.com/o/r/blob/%s/specs/s.md",
		"gitlab": "https://git.example.org/group/project/-/blob/%s/specs/s.md",
	} {
		t.Run(name, func(t *testing.T) {
			r := newArtefactRepo(t, false)
			fx := forges[name](t)
			fx.addCommit(r.head())

			if _, err := Run(context.Background(), r.env(noRun, fx.toml, fx.open), 1, r.spec); err != nil {
				t.Fatal(err)
			}

			posted := fx.posted()
			if len(posted) != 1 || !strings.Contains(posted[0], fmt.Sprintf(want, r.head())) {
				t.Errorf("posted %q, want a link %s", posted, fmt.Sprintf(want, r.head()))
			}
		})
	}
}

// No token is an artefact line that names the cause, not a stack of forge text.
func TestPostArtefactWithoutTokenSaysSo(t *testing.T) {
	r := newArtefactRepo(t, false)
	open := func(string) (forge.Forge, error) {
		return nil, &forge.TokenError{Kind: "github", Vars: []string{"GH_TOKEN", "GITHUB_TOKEN"}}
	}

	_, err := Run(context.Background(), r.env(noRun, githubTOML, open), 1, r.spec)

	var e *Error
	if !errors.As(err, &e) || !strings.HasPrefix(e.Line, "artefact: no token for forge") || !strings.Contains(e.Line, "GH_TOKEN") {
		t.Fatalf("err = %v, want artefact: no token for forge naming GH_TOKEN", err)
	}
}

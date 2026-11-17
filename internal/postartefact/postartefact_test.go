package postartefact

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
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

// env runs git for real and answers gh: the commit is unknown, there is no open PR, and reading
// the comments fails (the test ends at the push).
func (r *artefactRepo) env(getenv func(string) string) Env {
	real := procrun.Exec(time.Minute)
	return Env{
		Run: func(ctx context.Context, dir string, stdin *string, name string, args ...string) (procrun.Result, error) {
			if name == "git" {
				return real(ctx, dir, stdin, name, args...)
			}
			joined := strings.Join(args, " ")
			switch {
			case strings.HasPrefix(joined, "api repos/o/r/commits/"):
				return procrun.Result{Code: 1, Stderr: "gh: Not Found (HTTP 404)"}, nil
			case strings.HasPrefix(joined, "pr list"):
				return procrun.Result{Stdout: "0\n"}, nil
			}
			return procrun.Result{Code: 1, Stderr: "no comments in this test"}, nil
		},
		Config: func() (*config.Config, error) {
			path := filepath.Join(r.t.TempDir(), "lighthouse.toml")
			if err := os.WriteFile(path, []byte("[project]\nlanguage = \"en\"\n[forge]\nrepo = \"o/r\"\n[paths]\nspecs = \"specs\"\n"), 0o600); err != nil {
				return nil, err
			}
			return config.Load(path)
		},
		Getenv: getenv,
	}
}

func controllerRun(key string) string {
	if key == "LIGHTHOUSE_CONTROLLER_RUN" {
		return testRun
	}
	return ""
}

func noRun(string) string { return "" }

func TestPostArtefactRefusesWorkflowRange(t *testing.T) {
	r := newArtefactRepo(t, true)

	_, err := Run(context.Background(), r.env(controllerRun), 1, r.spec)

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

	_, err := Run(context.Background(), r.env(controllerRun), 1, r.spec)

	if err == nil || !strings.Contains(err.Error(), "no comments in this test") {
		t.Fatalf("err = %v, want the comment read after the push to fail", err)
	}
	if !r.onOrigin("issue-1-x") {
		t.Error("the branch was not pushed")
	}
}

func TestPostArtefactPushesWorkflowsOutsideControllerRun(t *testing.T) {
	r := newArtefactRepo(t, true)

	_, err := Run(context.Background(), r.env(noRun), 1, r.spec)

	if err == nil || !strings.Contains(err.Error(), "no comments in this test") {
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

	_, err := Run(context.Background(), r.env(controllerRun), 1, r.spec)

	if err == nil || !strings.Contains(err.Error(), "COULD NOT CHECK") {
		t.Fatalf("err = %v, want COULD NOT CHECK", err)
	}
	if r.onOrigin("issue-1-x") {
		t.Error("the branch was pushed")
	}
}

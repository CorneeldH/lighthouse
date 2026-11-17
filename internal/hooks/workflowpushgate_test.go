package hooks

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/hook"
)

const testRun = "0f8e2c1a-1b2c-4d5e-8f90-123456789abc"

// pushRepo is a clone of a bare origin with one commit on main (pushed) and a checkout of
// branch issue-1-x. It points the config at a lighthouse.toml without a default branch (main).
type pushRepo struct {
	t   *testing.T
	dir string
}

func newPushRepo(t *testing.T) *pushRepo {
	t.Helper()
	tmp := realDir(t, t.TempDir())
	toml := filepath.Join(tmp, "lighthouse.toml")
	if err := os.WriteFile(toml, []byte("[project]\nlanguage = \"en\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", toml)
	t.Setenv("HOME", tmp)
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	r := &pushRepo{t: t, dir: filepath.Join(tmp, "work")}
	bare := filepath.Join(tmp, "origin.git")
	r.git(tmp, "init", "-q", "--bare", "-b", "main", bare)
	r.git(tmp, "clone", "-q", bare, r.dir)
	r.git(r.dir, "checkout", "-q", "-b", "main")
	r.commit("README", "readme\n")
	r.git(r.dir, "push", "-q", "origin", "main")
	r.git(r.dir, "checkout", "-q", "-b", "issue-1-x")
	return r
}

func (r *pushRepo) git(dir string, args ...string) string {
	r.t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...).CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit writes name (directories included) and commits it.
func (r *pushRepo) commit(name, text string) {
	r.t.Helper()
	writeFile(r.t, filepath.Join(r.dir, name), text)
	r.git(r.dir, "add", "--", name)
	r.git(r.dir, "commit", "-q", "-m", "add "+name)
}

func (r *pushRepo) push() { r.t.Helper(); r.git(r.dir, "push", "-q", "-u", "origin", "HEAD") }

func pushGate(t *testing.T, run, command, cwd string) hook.Result {
	t.Helper()
	t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", run)
	raw, err := json.Marshal(map[string]any{"tool_input": map[string]any{"command": command}, "cwd": cwd})
	if err != nil {
		t.Fatal(err)
	}
	return WorkflowPushGate(context.Background(), "", "", hook.Payload{Raw: raw})
}

func wantPushBlocked(t *testing.T, res hook.Result, want ...string) {
	t.Helper()
	if res.Code != 2 {
		t.Fatalf("code %d, stderr %q; want a block (2)", res.Code, res.Stderr)
	}
	for _, w := range want {
		if !strings.Contains(res.Stderr, w) {
			t.Errorf("stderr %q lacks %q", res.Stderr, w)
		}
	}
}

func wantPushPass(t *testing.T, res hook.Result) {
	t.Helper()
	if res.Code != 0 || res.Stderr != "" || res.Output != nil {
		t.Fatalf("code %d, stderr %q, output %v; want a silent pass", res.Code, res.Stderr, res.Output)
	}
}

func TestWorkflowPushGateBlocksInControllerRun(t *testing.T) {
	r := newPushRepo(t)
	r.commit("a.txt", "a\n")
	r.push()
	r.commit(".github/workflows/ci.yml", "on: push\n")

	res := pushGate(t, testRun, "git push", r.dir)

	wantPushBlocked(t, res, "Workflow-push gate: blocked", ".github/workflows/ci.yml", "V2b",
		"next: push-workflows", "roborev-gate gate --push")
}

func TestWorkflowPushGateAllowsOutsideControllerRun(t *testing.T) {
	r := newPushRepo(t)
	r.commit(".github/workflows/ci.yml", "on: push\n")
	for _, run := range []string{"", "not a run id", "../x"} {
		wantPushPass(t, pushGate(t, run, "git push -u origin HEAD", r.dir))
	}
}

// The workflow commit is already on origin/<branch>: only the new commit counts.
func TestWorkflowPushGateAllowsNonWorkflowRange(t *testing.T) {
	r := newPushRepo(t)
	r.commit(".github/workflows/ci.yml", "on: push\n")
	r.push()
	r.commit("notes.md", "notes\n")

	for _, command := range []string{"git push", "git push origin HEAD", "git push origin HEAD:refs/heads/issue-1-x", "git status && git push --force-with-lease"} {
		t.Run(command, func(t *testing.T) { wantPushPass(t, pushGate(t, testRun, command, r.dir)) })
	}
}

func TestWorkflowPushGateUsesMergeBaseWhenBranchNotOnOrigin(t *testing.T) {
	t.Run("workflow on the branch, behind a later commit", func(t *testing.T) {
		r := newPushRepo(t)
		r.commit(".github/workflows/ci.yml", "on: push\n")
		r.commit("b.txt", "b\n")
		wantPushBlocked(t, pushGate(t, testRun, "git push -u origin HEAD", r.dir), ".github/workflows/ci.yml")
	})
	t.Run("workflow only on origin/main before the branch point", func(t *testing.T) {
		r := newPushRepo(t)
		r.git(r.dir, "checkout", "-q", "main")
		r.commit(".github/workflows/ci.yml", "on: push\n")
		r.git(r.dir, "push", "-q", "origin", "main")
		r.git(r.dir, "checkout", "-q", "-B", "issue-1-x")
		r.commit("b.txt", "b\n")
		wantPushPass(t, pushGate(t, testRun, "git push -u origin HEAD", r.dir))
	})
}

func TestWorkflowPushGateCouldNotCheckBlocks(t *testing.T) {
	t.Run("no origin/main and branch not on origin", func(t *testing.T) {
		r := newPushRepo(t)
		r.commit("b.txt", "b\n")
		r.git(r.dir, "update-ref", "-d", "refs/remotes/origin/main")
		wantPushBlocked(t, pushGate(t, testRun, "git push -u origin HEAD", r.dir), "COULD NOT CHECK")
	})
	t.Run("unresolvable path", func(t *testing.T) {
		r := newPushRepo(t)
		wantPushBlocked(t, pushGate(t, testRun, `git -C "$(pwd)" push`, r.dir), "COULD NOT CHECK")
	})
	t.Run("open quote", func(t *testing.T) {
		r := newPushRepo(t)
		wantPushBlocked(t, pushGate(t, testRun, "echo 'x && git push", r.dir), "COULD NOT CHECK")
	})
	t.Run("every branch", func(t *testing.T) {
		r := newPushRepo(t)
		for _, command := range []string{"git push --all origin", "git push --mirror origin", "git push --tags", "git push --follow-tags"} {
			wantPushBlocked(t, pushGate(t, testRun, command, r.dir), "COULD NOT CHECK")
		}
	})
	t.Run("refspec from a variable", func(t *testing.T) {
		r := newPushRepo(t)
		wantPushBlocked(t, pushGate(t, testRun, `git push origin "$REF"`, r.dir), "COULD NOT CHECK")
	})
	t.Run("unknown source ref", func(t *testing.T) {
		r := newPushRepo(t)
		wantPushBlocked(t, pushGate(t, testRun, "git push origin nosuch:refs/heads/x", r.dir), "COULD NOT CHECK")
	})
	t.Run("git missing", func(t *testing.T) {
		r := newPushRepo(t)
		emptyPath(t)
		wantPushBlocked(t, pushGate(t, testRun, "git push", r.dir), "COULD NOT CHECK")
	})
	t.Run("payload not JSON", func(t *testing.T) {
		t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", testRun)
		res := WorkflowPushGate(context.Background(), "", "", hook.Payload{Raw: []byte("{")})
		wantPushBlocked(t, res, "COULD NOT CHECK")
	})
}

func TestWorkflowPushGateMessageHasNoScopeAdvice(t *testing.T) {
	r := newPushRepo(t)
	r.commit(".github/workflows/ci.yml", "on: push\n")
	results := []hook.Result{
		pushGate(t, testRun, "git push", r.dir),
		pushGate(t, testRun, "git push --all origin", r.dir),
		pushGate(t, testRun, "echo 'x && git push", r.dir),
	}
	for _, res := range results {
		if res.Code != 2 {
			t.Fatalf("code %d, stderr %q; want a block", res.Code, res.Stderr)
		}
		lower := strings.ToLower(res.Stderr)
		for _, word := range []string{"scope", "token", "pat ", "permission"} {
			if strings.Contains(lower, word) {
				t.Errorf("message advises on %q:\n%s", word, res.Stderr)
			}
		}
	}
}

// Ways to spell the same push, and ways to touch a workflow that are not an added file.
func TestWorkflowPushGateSeesThroughSpellings(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(r *pushRepo)
		command func(r *pushRepo) string
		want    string
	}{
		{"git -C", wfCommit, func(r *pushRepo) string { return "git -C '" + r.dir + "' push" }, ".github/workflows/ci.yml"},
		{"cd then push", wfCommit, func(r *pushRepo) string { return "cd '" + r.dir + "' && git push origin HEAD" }, ".github/workflows/ci.yml"},
		{"cd into a subdirectory", wfCommit, func(r *pushRepo) string { return "cd .github && git push" }, ".github/workflows/ci.yml"},
		{"refspec to another branch", wfCommit, func(*pushRepo) string { return "git push origin HEAD:refs/heads/other" }, ".github/workflows/ci.yml"},
		{"forced refspec", wfCommit, func(*pushRepo) string { return "git push --force origin +HEAD:issue-1-x" }, ".github/workflows/ci.yml"},
		{"bash -c", wfCommit, func(*pushRepo) string { return "bash -c 'git push'" }, ".github/workflows/ci.yml"},
		{"push option with a value", wfCommit, func(*pushRepo) string { return "git push -o ci.skip origin HEAD" }, ".github/workflows/ci.yml"},
		{"another local branch", func(r *pushRepo) {
			r.git(r.dir, "checkout", "-q", "-b", "side")
			r.commit(".github/workflows/ci.yml", "on: push\n")
			r.git(r.dir, "checkout", "-q", "issue-1-x")
		}, func(*pushRepo) string { return "git push origin side" }, ".github/workflows/ci.yml"},
		{"path with spaces", func(r *pushRepo) {
			r.commit(".github/workflows/my ci.yml", "on: push\n")
		}, func(*pushRepo) string { return "git push" }, ".github/workflows/my ci.yml"},
		{"renamed out of workflows", func(r *pushRepo) {
			r.commit(".github/workflows/ci.yml", "on: push\n")
			r.push()
			r.git(r.dir, "mv", ".github/workflows/ci.yml", "ci.yml")
			r.git(r.dir, "commit", "-q", "-m", "move")
		}, func(*pushRepo) string { return "git push" }, ".github/workflows/ci.yml"},
		{"deleted workflow", func(r *pushRepo) {
			r.commit(".github/workflows/ci.yml", "on: push\n")
			r.push()
			r.git(r.dir, "rm", "-q", ".github/workflows/ci.yml")
			r.git(r.dir, "commit", "-q", "-m", "drop")
		}, func(*pushRepo) string { return "git push" }, ".github/workflows/ci.yml"},
		{"added and removed again", func(r *pushRepo) {
			r.commit(".github/workflows/ci.yml", "on: push\n")
			r.git(r.dir, "rm", "-q", ".github/workflows/ci.yml")
			r.git(r.dir, "commit", "-q", "-m", "drop")
		}, func(*pushRepo) string { return "git push -u origin HEAD" }, ".github/workflows/ci.yml"},
		{"merge of main that changed a workflow", func(r *pushRepo) {
			r.commit("a.txt", "a\n")
			r.push()
			r.git(r.dir, "checkout", "-q", "main")
			r.commit(".github/workflows/ci.yml", "on: push\n")
			r.git(r.dir, "push", "-q", "origin", "main")
			r.git(r.dir, "checkout", "-q", "issue-1-x")
			r.git(r.dir, "merge", "-q", "--no-edit", "main")
		}, func(*pushRepo) string { return "git push" }, ".github/workflows/ci.yml"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newPushRepo(t)
			c.setup(r)
			wantPushBlocked(t, pushGate(t, testRun, c.command(r), r.dir), c.want)
		})
	}
}

func wfCommit(r *pushRepo) { r.commit(".github/workflows/ci.yml", "on: push\n") }

func TestWorkflowPushGatePassesWhatPushesNoWorkflow(t *testing.T) {
	r := newPushRepo(t)
	r.commit(".github/workflows/ci.yml", "on: push\n")
	r.push()
	r.commit("b.txt", "b\n")
	for _, command := range []string{
		"ls -la",
		"git status",
		"echo 'we push later'",
		"git push origin --delete old-branch",
		"git push origin :old-branch",
		"git -C /nonexistent/dir push",
	} {
		t.Run(command, func(t *testing.T) { wantPushPass(t, pushGate(t, testRun, command, r.dir)) })
	}
}

// stageWorkflow writes a workflow and stages it, without committing: the command under test does.
func stageWorkflow(r *pushRepo) {
	r.t.Helper()
	writeFile(r.t, filepath.Join(r.dir, ".github/workflows/ci.yml"), "on: push\n")
	r.git(r.dir, "add", "--", ".github/workflows/ci.yml")
}

// The hook runs before the whole command: a commit earlier in it moves HEAD after the
// measurement, so a push behind a step that may move refs is not measurable.
func TestWorkflowPushGateBlocksPushAfterACommitInTheSameCommand(t *testing.T) {
	for _, command := range []string{
		"git commit -qm wf && git push origin issue-1-x",
		"git commit -qm wf; git push",
		"git commit -qam wf && git push -u origin HEAD",
		"git add . && git commit -qm wf && git push",
		"git -C . merge --ff-only main && git push",
		"git reset --soft HEAD && git push",
	} {
		t.Run(command, func(t *testing.T) {
			r := newPushRepo(t)
			r.commit("a.txt", "a\n")
			r.push()
			stageWorkflow(r)
			wantPushBlocked(t, pushGate(t, testRun, command, r.dir), "COULD NOT CHECK", "as its own command",
				"next: push-workflows")
		})
	}
}

func TestWorkflowPushGateReadOnlyStepsBeforeAPushPass(t *testing.T) {
	for _, command := range []string{
		"git status && git push",
		"git fetch origin && git push",
		"git log --oneline -1; git diff --stat && git push origin HEAD",
		"git add -- notes.md && git push",
	} {
		t.Run(command, func(t *testing.T) {
			r := newPushRepo(t)
			r.commit("a.txt", "a\n")
			r.push()
			r.commit("b.txt", "b\n")
			wantPushPass(t, pushGate(t, testRun, command, r.dir))
		})
	}
}

// Without a refspec git pushes what the configuration says: a configured refspec or
// push.default=matching can send other branches, which the hook does not measure.
func TestWorkflowPushGateConfiguredPushIsNotMeasured(t *testing.T) {
	for name, config := range map[string][]string{
		"remote push refspec":   {"remote.origin.push", "refs/heads/*:refs/heads/*"},
		"push.default matching": {"push.default", "matching"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newPushRepo(t)
			r.commit("a.txt", "a\n")
			r.push()
			r.git(r.dir, "config", config[0], config[1])
			for _, command := range []string{"git push", "git push origin"} {
				wantPushBlocked(t, pushGate(t, testRun, command, r.dir), "COULD NOT CHECK")
			}
			// An explicit refspec says what is pushed: the configuration does not matter then.
			wantPushPass(t, pushGate(t, testRun, "git push origin HEAD", r.dir))
		})
	}
	t.Run("push.default simple", func(t *testing.T) {
		r := newPushRepo(t)
		r.commit("a.txt", "a\n")
		r.push()
		r.git(r.dir, "config", "push.default", "simple")
		wantPushPass(t, pushGate(t, testRun, "git push", r.dir))
	})
}

package commitgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests here run Pre on a `gh pr create` against a real repository with an origin: the gate
// then tests the branch diff against the merge-base with origin/main.

// branchRepo is a project repository with an origin, a branch with one commit above main, and
// the harness for it. base is the merge-base with origin/main.
func branchRepo(t *testing.T, gate string) (h *harness, repo, base string) {
	t.Helper()
	h, repo = projectHarness(t, gate)
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitIn(t, repo, "init", "-q", "--bare", origin)
	gitIn(t, repo, "remote", "add", "origin", origin)
	gitIn(t, repo, "push", "-q", "origin", "main")
	gitIn(t, repo, "fetch", "-q", "origin")
	base = gitIn(t, repo, "rev-parse", "HEAD")
	gitIn(t, repo, "checkout", "-q", "-b", "issue-1-x")
	write(t, filepath.Join(repo, "feature.txt"), "x\n")
	gitIn(t, repo, "add", "feature.txt")
	gitIn(t, repo, "commit", "-qm", "feature")
	return h, repo, base
}

// envOf is the value of key in the env of the first test run.
func envOf(t *testing.T, h *harness, key string) string {
	t.Helper()
	if len(h.calls) == 0 {
		t.Fatal("the test command did not run")
	}
	return getenv(h.calls[0].opts.Env, key)
}

func TestPrOnPRRunsOnBranchDiff(t *testing.T) {
	h, repo, base := branchRepo(t, testGate+"on = \"pr\"\n")
	h.reply(runReply{0, "3 passed\n", nil})
	res := h.pre("gh pr create --title x", repo)
	if res.Code != 0 {
		t.Fatalf("got %d\n%s", res.Code, res.Stderr)
	}
	if got := h.ranIn(); len(got) != 1 || got[0] != repo {
		t.Fatalf("the test command ran in %q, want only %q", got, repo)
	}
	if got := envOf(t, h, "LIGHTHOUSE_GATE_BASE"); got != base {
		t.Errorf("LIGHTHOUSE_GATE_BASE = %q, want the merge-base %q", got, base)
	}
	if got, want := envOf(t, h, "LIGHTHOUSE_GATE_HEAD"), gitIn(t, repo, "rev-parse", "HEAD"); got != want {
		t.Errorf("LIGHTHOUSE_GATE_HEAD = %q, want %q", got, want)
	}
}

func TestPrOnPRListsBranchFiles(t *testing.T) {
	h, repo, _ := branchRepo(t, testGate+"on = \"pr\"\n")
	h.reply(runReply{0, "3 passed\n", nil})
	h.during = func(_ int, c runCall) {
		raw, err := os.ReadFile(getenv(c.opts.Env, "LIGHTHOUSE_GATE_FILES"))
		if err != nil {
			t.Error(err)
		}
		if string(raw) != "feature.txt\n" {
			t.Errorf("files = %q, want only the branch file", raw)
		}
	}
	if res := h.pre("gh pr create --title x", repo); res.Code != 0 {
		t.Fatalf("got %d\n%s", res.Code, res.Stderr)
	}
	if len(h.calls) != 1 {
		t.Fatalf("%d runs, want 1", len(h.calls))
	}
}

func TestPrOnBothRunsAtPR(t *testing.T) {
	h, repo, _ := branchRepo(t, testGate+"on = \"both\"\n")
	h.reply(runReply{0, "3 passed\n", nil})
	if res := h.pre("gh pr create --title x", repo); res.Code != 0 {
		t.Fatalf("got %d\n%s", res.Code, res.Stderr)
	}
	if len(h.calls) != 1 {
		t.Fatalf("%d runs, want 1", len(h.calls))
	}
}

func TestPrDefaultOnDoesNothing(t *testing.T) {
	h, repo, _ := branchRepo(t, testGate)
	res := h.pre("gh pr create --title x", repo)
	if res.Code != 0 || res.Stderr != "" || len(h.calls) != 0 {
		t.Fatalf("got %d %q with %d runs, want a silent pass", res.Code, res.Stderr, len(h.calls))
	}
}

func TestPrRedBlocksWithFirstErrorLine(t *testing.T) {
	h, repo, _ := branchRepo(t, testGate+"on = \"pr\"\n")
	h.reply(runReply{1, "ok pkg/a\nFAIL TestX\nmore\n", nil})
	res := h.pre("gh pr create --title x", repo)
	if res.Code != 2 {
		t.Fatalf("got %d, want 2", res.Code)
	}
	for _, want := range []string{
		"commit-test-gate: tests failed on this branch. Fix them before creating the PR.",
		"First error: FAIL TestX",
		"ok pkg/a\nFAIL TestX\nmore",
	} {
		if !strings.Contains(res.Stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, res.Stderr)
		}
	}
}

func TestPrRedWithoutErrorWordNamesFirstLine(t *testing.T) {
	h, repo, _ := branchRepo(t, testGate+"on = \"pr\"\n")
	h.reply(runReply{1, "\n\nsomething odd\nelse\n", nil})
	res := h.pre("gh pr create --title x", repo)
	if res.Code != 2 || !strings.Contains(res.Stderr, "First error: something odd\n") {
		t.Fatalf("got %d\n%s", res.Code, res.Stderr)
	}
}

func TestPrNoCommitsAboveBase(t *testing.T) {
	h, repo, _ := branchRepo(t, testGate+"on = \"pr\"\n")
	gitIn(t, repo, "checkout", "-q", "main")
	res := h.pre("gh pr create --title x", repo)
	if res.Code != 0 || len(h.calls) != 0 {
		t.Fatalf("got %d with %d runs, want a pass without a run", res.Code, len(h.calls))
	}
	if got := message(t, res, "PreToolUse"); !strings.Contains(got, "no commits above origin/main, nothing was tested") {
		t.Fatalf("message:\n%s", got)
	}
}

func TestPrMissingOriginRefBlocks(t *testing.T) {
	h, repo := projectHarness(t, testGate+"on = \"pr\"\n")
	res := h.pre("gh pr create --title x", repo)
	if res.Code != 2 || !strings.Contains(res.Stderr, "COULD NOT CHECK") || len(h.calls) != 0 {
		t.Fatalf("got %d with %d runs\n%s", res.Code, len(h.calls), res.Stderr)
	}
}

func TestPrBlockMessageNamesPR(t *testing.T) {
	h, repo := projectHarness(t, testGate+"on = \"pr\"\n")
	res := h.pre("gh pr create --title x", repo)
	if res.Code != 2 || strings.Contains(res.Stderr, "git commit") || !strings.Contains(res.Stderr, "PR create") {
		t.Fatalf("got %d\n%s, want a block that names the PR create and not git commit", res.Code, res.Stderr)
	}
}

func TestPrNotACreate(t *testing.T) {
	h, repo, _ := branchRepo(t, testGate+"on = \"pr\"\n")
	write(t, h.toml, "this is [not toml")
	res := h.pre("gh pr view 3", repo)
	if res.Code != 0 || res.Stderr != "" || len(h.calls) != 0 {
		t.Fatalf("got %d %q with %d runs, want a silent pass", res.Code, res.Stderr, len(h.calls))
	}
}

func TestPrCommandEmptyWithOnPR(t *testing.T) {
	h, repo, _ := branchRepo(t, "[test_gate]\ncommand = \"\"\non = \"pr\"\n")
	res := h.pre("gh pr create --title x", repo)
	if res.Code != 0 || len(h.calls) != 0 {
		t.Fatalf("got %d with %d runs, want a pass without a run", res.Code, len(h.calls))
	}
	if got := message(t, res, "PreToolUse"); !strings.Contains(got, "no test command configured, nothing was tested") {
		t.Fatalf("message:\n%s", got)
	}
}

func TestPrForgeAndGlabForms(t *testing.T) {
	for _, command := range []string{"lighthouse forge pr create --title x", "glab mr create --title x"} {
		t.Run(command, func(t *testing.T) {
			h, repo, _ := branchRepo(t, testGate+"on = \"pr\"\n")
			h.reply(runReply{0, "3 passed\n", nil})
			if res := h.pre(command, repo); res.Code != 0 {
				t.Fatalf("got %d\n%s", res.Code, res.Stderr)
			}
			if len(h.calls) != 1 {
				t.Fatalf("%d runs, want 1", len(h.calls))
			}
		})
	}
}

func TestPrCombinedCommitAndCreateBlocks(t *testing.T) {
	const split = "split the commit and the PR create into separate Bash calls: the PR run needs the new commit as HEAD"
	for _, on := range []string{"pr", "both"} {
		t.Run(on, func(t *testing.T) {
			h, repo, _ := branchRepo(t, testGate+"on = \""+on+"\"\n")
			res := h.pre("git commit -m x && gh pr create --title x", repo)
			if res.Code != 2 || !strings.Contains(res.Stderr, split) || len(h.calls) != 0 {
				t.Fatalf("got %d with %d runs\n%s", res.Code, len(h.calls), res.Stderr)
			}
		})
	}
	t.Run("commit", func(t *testing.T) {
		h, repo, _ := branchRepo(t, testGate)
		res := h.pre("git commit -m x && gh pr create --title x", repo)
		if res.Code != 0 || strings.Contains(res.Stderr, split) {
			t.Fatalf("got %d\n%s, want the commit path", res.Code, res.Stderr)
		}
	})
}

func TestPrUnparseableCreateBlocksOnPR(t *testing.T) {
	h, repo, _ := branchRepo(t, testGate+"on = \"pr\"\n")
	res := h.pre("gh pr create --title 'x", repo)
	if res.Code != 2 || !strings.Contains(res.Stderr, "cannot be parsed") {
		t.Fatalf("got %d\n%s", res.Code, res.Stderr)
	}
}

func TestPrUnparseableCreateIgnoredByDefault(t *testing.T) {
	h, repo, _ := branchRepo(t, testGate)
	if res := h.pre("gh pr create --title 'x", repo); res.Code != 0 {
		t.Fatalf("got %d\n%s, want a pass", res.Code, res.Stderr)
	}
}

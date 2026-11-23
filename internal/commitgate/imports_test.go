package commitgate

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const finding = "pkg/a.py:3: unlisted-dependency requests — not declared in pyproject.toml"

type importsCall struct {
	root  string
	files []string
}

type importsReply struct {
	stdout, stderr string
	code           int
	err            error
}

// withImports installs a fake ImportsCheck that answers reply and records its calls.
func withImports(h *harness, reply importsReply) *[]importsCall {
	var calls []importsCall
	h.g.ImportsCheck = func(root string, files []string, _ time.Duration) (string, string, int, error) {
		calls = append(calls, importsCall{root, files})
		return reply.stdout, reply.stderr, reply.code, reply.err
	}
	return &calls
}

// installPacks gives root a manifest, the sign that lighthouse packs are installed.
func installPacks(t *testing.T, root string) {
	t.Helper()
	write(t, filepath.Join(root, ".lighthouse", "manifest.toml"), "version = \"1.0.0\"\n")
}

// stagedRepo is a git repo with a.txt committed, new.py staged and gone.py staged as a deletion.
func stagedRepo(t *testing.T) string {
	t.Helper()
	root, _, _ := gitRepo(t)
	write(t, filepath.Join(root, "gone.py"), "x")
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("add", ".")
	git("commit", "-qm", "gone")
	git("rm", "-q", "gone.py")
	write(t, filepath.Join(root, "new.py"), "import requests\n")
	git("add", "new.py")
	return root
}

func TestImportsFindingIsRed(t *testing.T) {
	h := newHarness(t, "pre", testGate, "")
	root := stagedRepo(t)
	installPacks(t, root)
	calls := withImports(h, importsReply{stdout: finding + "\n", code: 1})
	res := h.g.RunRoots([]Row{{Root: root}}, "PreToolUse")
	if res.Code != 2 || res.Output != nil {
		t.Fatalf("got %d %v\n%s", res.Code, res.Output, res.Stderr)
	}
	for _, want := range []string{finding, "imports not declared in a manifest"} {
		if !strings.Contains(res.Stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, res.Stderr)
		}
	}
	if strings.Contains(res.Stderr, "tests failed") || strings.Contains(res.Stderr, "COULD NOT CHECK") {
		t.Errorf("a finding must not read as a red test or an unchecked run:\n%s", res.Stderr)
	}
	if len(*calls) != 1 || len(h.calls) != 0 {
		t.Fatalf("imports calls %d, test runs %d; want 1 and 0 (a finding stops before the tests)", len(*calls), len(h.calls))
	}
}

func TestImportsUncheckedBlocks(t *testing.T) {
	for name, reply := range map[string]importsReply{
		"exit 3":                     {stderr: "imports: COULD NOT CHECK — no go\n", code: 3},
		"exit 1 without finding":     {stdout: "something else\n", code: 1},
		"exit 3 with a finding line": {stdout: finding + "\n", code: 3},
		"old binary":                 {stderr: "Error: unknown command \"imports\"\n", code: 1},
		"runner error":               {err: os.ErrPermission},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "pre", testGate, "")
			root := stagedRepo(t)
			installPacks(t, root)
			withImports(h, reply)
			res := h.g.RunRoots([]Row{{Root: root}}, "PreToolUse")
			if res.Code != 2 || !strings.Contains(res.Stderr, "COULD NOT CHECK") || !strings.Contains(res.Stderr, "import check") {
				t.Fatalf("got %d\n%s", res.Code, res.Stderr)
			}
			if len(h.calls) != 0 {
				t.Fatal("the tests ran after an unchecked import step")
			}
		})
	}
}

func TestImportsRunsWithoutTestCommand(t *testing.T) {
	for name, gate := range map[string]string{
		"not set": "",
		"empty":   "[test_gate]\ncommand = \"\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "pre", gate, "")
			root := stagedRepo(t)
			installPacks(t, root)
			calls := withImports(h, importsReply{stdout: finding + "\n", code: 1})
			res := h.g.RunRoots([]Row{{Root: root}}, "PreToolUse")
			if res.Code != 2 || !strings.Contains(res.Stderr, finding) || len(*calls) != 1 {
				t.Fatalf("got %d, %d calls\n%s", res.Code, len(*calls), res.Stderr)
			}
			// Green imports: the note about the missing test command stays beside it.
			h = newHarness(t, "pre", gate, "")
			calls = withImports(h, importsReply{})
			res = h.g.RunRoots([]Row{{Root: root}}, "PreToolUse")
			if m := message(t, res, "PreToolUse"); m != "commit-test-gate: no test command configured, nothing was tested" || len(*calls) != 1 {
				t.Fatalf("message %q, %d calls", m, len(*calls))
			}
		})
	}
}

func TestImportsOptOut(t *testing.T) {
	h := newHarness(t, "pre", "[test_gate]\ncommand = \"\"\n\n[imports]\ncheck = false\n", "")
	root := stagedRepo(t)
	installPacks(t, root)
	calls := withImports(h, importsReply{stdout: finding + "\n", code: 1})
	res := h.g.RunRoots([]Row{{Root: root}}, "PreToolUse")
	message(t, res, "PreToolUse")
	if len(*calls) != 0 {
		t.Fatal("the check ran with [imports] check = false")
	}
	// An empty test command alone does not turn the import step off.
	h = newHarness(t, "pre", "[test_gate]\ncommand = \"\"\n", "")
	calls = withImports(h, importsReply{})
	h.g.RunRoots([]Row{{Root: root}}, "PreToolUse")
	if len(*calls) != 1 {
		t.Fatalf("%d calls, want 1", len(*calls))
	}
}

func TestImportsWithoutPacksIsSkipped(t *testing.T) {
	h := newHarness(t, "pre", "", "")
	root := stagedRepo(t) // no manifest
	calls := withImports(h, importsReply{stdout: finding + "\n", code: 1})
	message(t, h.g.RunRoots([]Row{{Root: root}}, "PreToolUse"), "PreToolUse")
	if len(*calls) != 0 {
		t.Fatal("the check ran in a root without installed packs")
	}
}

func TestImportsEmptyFileListSkips(t *testing.T) {
	t.Run("pre: nothing staged", func(t *testing.T) {
		h := newHarness(t, "pre", "", "")
		root, _, _ := gitRepo(t)
		installPacks(t, root)
		calls := withImports(h, importsReply{stdout: finding + "\n", code: 1})
		m := message(t, h.g.RunRoots([]Row{{Root: root}}, "PreToolUse"), "PreToolUse")
		if !strings.Contains(m, "commit-test-gate: imports not checked: nothing staged yet") || len(*calls) != 0 {
			t.Fatalf("message %q, %d calls", m, len(*calls))
		}
	})
	t.Run("post: only deletions", func(t *testing.T) {
		h := newHarness(t, "post", "", "")
		root := stagedRepo(t)
		installPacks(t, root)
		// Only the deletion of gone.py is committed; new.py stays untracked.
		gitOut(t, root, "reset", "-q", "new.py")
		gitOut(t, root, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-qm", "rm")
		head := gitOut(t, root, "rev-parse", "HEAD")
		base := gitOut(t, root, "rev-parse", "HEAD~1")
		calls := withImports(h, importsReply{stdout: finding + "\n", code: 1})
		res := h.g.RunRoots([]Row{{Root: root, Base: base, Head: head}}, "PostToolUse")
		if res.Code != 0 || len(*calls) != 0 {
			t.Fatalf("got %d, %d calls\n%s", res.Code, len(*calls), res.Stderr)
		}
	})
}

func gitOut(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestImportsPreUsesStagedFiles(t *testing.T) {
	h := newHarness(t, "pre", "", "")
	root := stagedRepo(t)
	installPacks(t, root)
	write(t, filepath.Join(root, "unstaged.py"), "import os\n")
	calls := withImports(h, importsReply{})
	h.g.RunRoots([]Row{{Root: root}}, "PreToolUse")
	want := []importsCall{{root, []string{"new.py"}}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %v, want %v (staged only, deletion dropped)", *calls, want)
	}
}

func TestImportsPostUsesChangedFiles(t *testing.T) {
	root, base, head := gitRepo(t)
	installPacks(t, root)
	h := newHarness(t, "post", "", "")
	calls := withImports(h, importsReply{})
	h.g.RunRoots([]Row{{Root: root, Base: base, Head: head}}, "PostToolUse")
	want := []importsCall{{root, []string{"b é.txt", "z.txt"}}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %v, want %v", *calls, want)
	}
}

func TestImportsPostFindingSaysFixInANewCommit(t *testing.T) {
	root, base, head := gitRepo(t)
	installPacks(t, root)
	h := newHarness(t, "post", "", "")
	withImports(h, importsReply{stdout: finding + "\n", code: 1})
	res := h.g.RunRoots([]Row{{Root: root, Base: base, Head: head}}, "PostToolUse")
	if res.Code != 2 || !strings.Contains(res.Stderr, "Declare them in a new commit.") || !strings.Contains(res.Stderr, finding) {
		t.Fatalf("got %d\n%s", res.Code, res.Stderr)
	}
}

func TestImportsDefaultRunnerUsesOwnExecutable(t *testing.T) {
	h := newHarness(t, "pre", "", "")
	root := stagedRepo(t)
	installPacks(t, root)
	h.g.Executable = func() (string, error) { return "/own/lighthouse", nil }
	var gotDir string
	var gotArgv []string
	h.g.Exec = func(dir string, _, argv []string) (string, string, int, error) {
		gotDir, gotArgv = dir, argv
		return "", "", 0, nil
	}
	message(t, h.g.RunRoots([]Row{{Root: root}}, "PreToolUse"), "PreToolUse")
	want := []string{"/own/lighthouse", "imports", "check", "--files", "new.py"}
	if gotDir != root || !reflect.DeepEqual(gotArgv, want) {
		t.Fatalf("ran %v in %q, want %v in %q (os.Executable, not a PATH lookup)", gotArgv, gotDir, want, root)
	}
}

func TestImportsDefaultExecutableIsOsExecutable(t *testing.T) {
	h := newHarness(t, "pre", "", "")
	root := stagedRepo(t)
	installPacks(t, root)
	var gotArgv []string
	h.g.Exec = func(_ string, _, argv []string) (string, string, int, error) {
		gotArgv = argv
		return "", "", 0, nil
	}
	h.g.RunRoots([]Row{{Root: root}}, "PreToolUse")
	exe, _ := os.Executable()
	if len(gotArgv) == 0 || gotArgv[0] != exe {
		t.Fatalf("argv = %v, want %s first", gotArgv, exe)
	}
}

// A manifest that exists but cannot be read is not "no packs": the step could not check.
func TestImportsUnreadableManifestBlocks(t *testing.T) {
	h := newHarness(t, "pre", "", "")
	root := stagedRepo(t)
	write(t, filepath.Join(root, ".lighthouse", "manifest.toml"), "version = [\n")
	calls := withImports(h, importsReply{})
	res := h.g.RunRoots([]Row{{Root: root}}, "PreToolUse")
	if res.Code != 2 || !strings.Contains(res.Stderr, "COULD NOT CHECK — the pack manifest of "+root+" cannot be read") || len(*calls) != 0 {
		t.Fatalf("got %d, %d calls\n%s", res.Code, len(*calls), res.Stderr)
	}
}

// The import step counts towards the one deadline of the gate: the clock starts before it, the
// test deadline is measured from that start, and a run gets at most what MaxDeadline leaves.
func TestImportsCountTowardsTheDeadline(t *testing.T) {
	h := newHarness(t, "pre", testGate, "").reply(runReply{0, "a\n", nil})
	root := stagedRepo(t)
	installPacks(t, root)
	now := time.Unix(startEpoch, 0)
	h.g.Now = func() time.Time { return now }
	var limits []time.Duration
	h.g.ImportsCheck = func(_ string, _ []string, limit time.Duration) (string, string, int, error) {
		limits = append(limits, limit)
		now = now.Add(100 * time.Second) // knip took 100 s
		return "", "", 0, nil
	}
	message(t, h.g.RunRoots([]Row{{Root: root}}, "PreToolUse"), "PreToolUse")
	if len(limits) != 1 || limits[0] != importsDeadline {
		t.Fatalf("limits %v, want [%v]", limits, importsDeadline)
	}
	if want := time.Unix(startEpoch+270, 0); !h.calls[0].deadline.Equal(want) {
		t.Fatalf("test deadline %v, want %v: measured from before the import step", h.calls[0].deadline, want)
	}
}

func TestImportsRunIsBoundedByWhatIsLeft(t *testing.T) {
	h := newHarness(t, "pre", testGate, "").reply(runReply{0, "a\n", nil}, runReply{0, "a\n", nil})
	root := stagedRepo(t)
	installPacks(t, root)
	// Two rows: the first leaves 30 s of MaxDeadline for the second.
	now := time.Unix(startEpoch, 0)
	h.g.Now = func() time.Time { return now }
	var limits []time.Duration
	h.g.ImportsCheck = func(_ string, _ []string, limit time.Duration) (string, string, int, error) {
		limits = append(limits, limit)
		now = now.Add(MaxDeadline*time.Second - 30*time.Second)
		return "", "", 0, nil
	}
	h.g.RunRoots([]Row{{Root: root}, {Root: root}}, "PreToolUse")
	if len(limits) != 2 || limits[0] != importsDeadline || limits[1] != 30*time.Second {
		t.Fatalf("limits %v, want [%v 30s]", limits, importsDeadline)
	}
}

// An exhausted deadline blocks (COULD NOT CHECK), never fail-open, and tests nothing.
func TestImportsExhaustedDeadlineBlocks(t *testing.T) {
	h := newHarness(t, "pre", testGate, "").reply(runReply{0, "a\n", nil})
	root := stagedRepo(t)
	installPacks(t, root)
	now := time.Unix(startEpoch, 0)
	h.g.Now = func() time.Time { return now }
	calls := 0
	h.g.ImportsCheck = func(string, []string, time.Duration) (string, string, int, error) {
		calls++
		now = now.Add(MaxDeadline * time.Second)
		return "", "", 0, nil
	}
	res := h.g.RunRoots([]Row{{Root: root}, {Root: root}}, "PreToolUse")
	if res.Code != 2 || !strings.Contains(res.Stderr, "COULD NOT CHECK") || !strings.Contains(res.Stderr, "deadline of the gate is used up") {
		t.Fatalf("got %d\n%s", res.Code, res.Stderr)
	}
	if calls != 1 || len(h.calls) != 0 {
		t.Fatalf("imports runs %d, test runs %d; want 1 and 0", calls, len(h.calls))
	}
}

package lane

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/deps"
	"github.com/CorneeldH/lighthouse/internal/initcmd"
	"github.com/CorneeldH/lighthouse/internal/pins"
)

// laneRepo is a lighthouse-like repository (initialized with its own pack/) with a bare
// origin, and the upstream its pins come from.
type laneRepo struct {
	root, origin string
	up           *upstream
	pin, head    string
	sources      string // pack/SOURCES.toml as committed
}

const (
	demoSkill = "pack/core/skills/demo/SKILL.md"
	toolFile  = "pack/core/lib/tool.py"
	ciFile    = "pack/core/workflows/ci.yml"
	localBody = "\nintro local\none\ntwo\nthree\nfour\nfive\n"
)

func sourcesTOML(pin string) string {
	return `# provenance
[[file]]
path = "pack/core/hooks/g.sh"
new = true

[[file]]
path = "pack/core/skills/demo/SKILL.md"
pin = "core/skills/demo"

[[file]]
path = "pack/core/skills/demo/ref.md"
pin = "core/skills/demo"

[[file]]
path = "pack/core/lib/tool.py"
source_repo = "o/r"
source_sha = "` + pin + `"
source_path = "lib/tool.py"

[[file]]
path = "pack/core/workflows/ci.yml"
source_repo = "o/r"
source_sha = "` + pin + `"
source_path = "workflows/ci.yml"
`
}

func newLaneRepo(t *testing.T) *laneRepo {
	t.Helper()
	isolateGit(t)
	r := &laneRepo{up: newUpstream(t)}
	r.pin = r.up.commit(map[string]string{
		"skills/demo/SKILL.md": upSkillBase,
		"skills/demo/ref.md":   "a\nb\nc\n",
		"lib/tool.py":          "x = 1\ny = 2\nz = 3\nv = 4\nw = 5\n",
		"workflows/ci.yml":     "on: push\n",
	})
	r.head = r.up.commit(map[string]string{
		"skills/demo/SKILL.md": upSkillHead,
		"skills/demo/ref.md":   "a\nb\nc upstream\n",
		"skills/demo/new.md":   "new\n",
		"lib/tool.py":          "x = 10\ny = 2\nz = 3\nv = 4\nw = 5\n",
		"workflows/ci.yml":     "on: pull_request\n",
	})

	r.origin = filepath.Join(t.TempDir(), "origin.git")
	gitT(t, t.TempDir(), "init", "-q", "--bare", "-b", "main", r.origin)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.root = dir
	gitT(t, r.root, "init", "-q", "-b", "main")
	gitT(t, r.root, "remote", "add", "origin", r.origin)
	r.sources = sourcesTOML(r.pin)
	for rel, content := range map[string]string{
		"pack/SOURCES.toml":                r.sources,
		"pack/core/hooks/g.sh":             "#!/bin/bash\necho g\n",
		"pack/core/hooks/hooks.json":       `{"hooks": {}}`,
		"pack/core/instructions/AGENTS.md": "## core\n\n- rule\n",
		demoSkill:                          localSkill(r.pin, localBody),
		"pack/core/skills/demo/ref.md":     "a\nb\nc\n",
		toolFile:                           "x = 1\ny = 2\nz = 3\nv = 4\nw = 5 local\n",
		ciFile:                             "on: push\n",
	} {
		writeT(t, r.root, rel, content)
	}
	var out, errOut bytes.Buffer
	code := initcmd.Run(initcmd.Options{
		Dir: r.root, Yes: true, Version: "dev", Packs: os.DirFS(filepath.Join(r.root, "pack")),
		Deps: deps.Checker{
			LookPath: func(name string) (string, error) { return "/usr/bin/" + name, nil },
			Run:      func(string, ...string) error { return nil },
		},
		HostTOML:    filepath.Join(t.TempDir(), "host.toml"),
		TailscaleIP: func() (string, error) { return "", errors.New("no tailscale in tests") },
		In:          strings.NewReader(""), Out: &out, Err: &errOut,
	})
	if code != 0 {
		t.Fatalf("init: rc %d\n%s\n%s", code, out.String(), errOut.String())
	}
	gitT(t, r.root, "add", "-A")
	gitT(t, r.root, "commit", "-q", "-m", "init")
	gitT(t, r.root, "push", "-q", "origin", "main")
	return r
}

func (r *laneRepo) demoPin() pins.SourcePin {
	return skillPin("core/skills/demo", r.pin, "skills/demo", demoSkill)
}

func (r *laneRepo) toolPin() pins.SourcePin { return filePin(toolFile, r.pin, "lib/tool.py") }

func (r *laneRepo) ciPin() pins.SourcePin { return filePin(ciFile, r.pin, "workflows/ci.yml") }

func (r *laneRepo) merge(t *testing.T, p pins.SourcePin) MergeResult {
	t.Helper()
	mr, err := MergeSource(context.Background(), r.root, p, r.up.clone(t))
	if err != nil {
		t.Fatal(err)
	}
	if !mr.Clean {
		t.Fatalf("merge of %s not clean: %v", p.Key, mr.Conflicts)
	}
	return mr
}

func (r *laneRepo) prepare(t *testing.T, p pins.SourcePin) string {
	t.Helper()
	branch, err := PrepareBranch(context.Background(), r.root, p, r.merge(t, p), r.head)
	if err != nil {
		t.Fatal(err)
	}
	return branch
}

// show returns a file of a branch on origin, untrimmed.
func (r *laneRepo) show(t *testing.T, branch, rel string) string {
	t.Helper()
	cmd := exec.Command("git", "show", "refs/heads/"+branch+":"+rel)
	cmd.Dir = r.origin
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git show %s:%s: %v", branch, rel, err)
	}
	return string(out)
}

func (r *laneRepo) changed(t *testing.T, branch string) []string {
	t.Helper()
	return strings.Fields(gitT(t, r.origin, "diff", "--name-only", "main", "refs/heads/"+branch))
}

func (r *laneRepo) pinBranches(t *testing.T) []string {
	t.Helper()
	return strings.Fields(gitT(t, r.origin, "for-each-ref", "--format=%(refname)", "refs/heads/pins/"))
}

// assertRootUntouched fails when a run left a worktree behind or changed the main checkout.
func (r *laneRepo) assertRootUntouched(t *testing.T) {
	t.Helper()
	if s := gitT(t, r.root, "status", "--porcelain"); s != "" {
		t.Errorf("main checkout changed:\n%s", s)
	}
	if wt := strings.Count(gitT(t, r.root, "worktree", "list", "--porcelain"), "worktree "); wt != 1 {
		t.Errorf("%d worktrees, want only the main checkout", wt)
	}
}

// installedSkill is a SKILL.md as init installs it: the lighthouse marker first in metadata.
func installedSkill(commit, body string) string {
	return strings.Replace(localSkill(commit, body), "metadata:\n", "metadata:\n  lighthouse: \"core@dev\"\n", 1)
}

// Fails when the branch lacks the merge, the raised upstream-commit, the new SOURCES.toml entry,
// the changelog fragment or the copies init --upgrade installs from the merged pack/.
func TestPrepareBranch_bumpsPinAndSourcesToml(t *testing.T) {
	r := newLaneRepo(t)
	branch := r.prepare(t, r.demoPin())
	if branch != "pins/core-skills-demo" {
		t.Fatalf("branch %q", branch)
	}
	merged := localSkill(r.head, "\nintro local\none\ntwo\nthree\nfour\nfive upstream\n")
	for rel, want := range map[string]string{
		demoSkill:                                 merged,
		"pack/core/skills/demo/ref.md":            "a\nb\nc upstream\n",
		"pack/core/skills/demo/new.md":            "new\n",
		"pack/SOURCES.toml":                       r.sources + "\n[[file]]\npath = \"pack/core/skills/demo/new.md\"\npin = \"core/skills/demo\"\n",
		"changelog.d/pins-core-skills-demo.md":    "---\ntype: changed\n---\n`core/skills/demo` follows upstream `o/r` up to commit `" + r.head[:7] + "`.\n",
		".claude/skills/lighthouse-demo/new.md":   "new\n",
		".agents/skills/lighthouse-demo/new.md":   "new\n",
		".claude/skills/lighthouse-demo/ref.md":   "a\nb\nc upstream\n",
		".agents/skills/lighthouse-demo/SKILL.md": installedSkill(r.head, "\nintro local\none\ntwo\nthree\nfour\nfive upstream\n"),
	} {
		if got := r.show(t, branch, rel); got != want {
			t.Errorf("%s:\n got %q\nwant %q", rel, got, want)
		}
	}
	r.assertRootUntouched(t)

	t.Run("file pin raises its own source_sha only", func(t *testing.T) {
		branch := r.prepare(t, r.toolPin())
		want := strings.Replace(r.sources,
			"path = \"pack/core/lib/tool.py\"\nsource_repo = \"o/r\"\nsource_sha = \""+r.pin,
			"path = \"pack/core/lib/tool.py\"\nsource_repo = \"o/r\"\nsource_sha = \""+r.head, 1)
		if got := r.show(t, branch, "pack/SOURCES.toml"); got != want {
			t.Errorf("SOURCES.toml:\n got %q\nwant %q", got, want)
		}
		if got := r.show(t, branch, toolFile); got != "x = 10\ny = 2\nz = 3\nv = 4\nw = 5 local\n" {
			t.Errorf("tool.py %q", got)
		}
	})

	t.Run("removed file loses its SOURCES.toml entry", func(t *testing.T) {
		mr := MergeResult{Clean: true, Files: map[string][]byte{}, Removed: []string{"pack/core/skills/demo/ref.md"}}
		branch, err := PrepareBranch(context.Background(), r.root, r.demoPin(), mr, r.head)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Replace(r.sources, "\n[[file]]\npath = \"pack/core/skills/demo/ref.md\"\npin = \"core/skills/demo\"\n", "", 1)
		if got := r.show(t, branch, "pack/SOURCES.toml"); got != want {
			t.Errorf("SOURCES.toml:\n got %q\nwant %q", got, want)
		}
		for _, rel := range []string{"pack/core/skills/demo/ref.md", ".claude/skills/lighthouse-demo/ref.md"} {
			if strings.Contains(gitT(t, r.origin, "ls-tree", "-r", "--name-only", "refs/heads/"+branch), rel) {
				t.Errorf("%s still on the branch", rel)
			}
		}
	})
}

// Fails when the lane commit takes its author or committer from the git config or the
// environment, or when a configured signing program or hook stops it.
func TestPrepareBranch_commitIdentityIsGithubActionsBot(t *testing.T) {
	r := newLaneRepo(t)
	mr := r.merge(t, r.demoPin())

	hooks := t.TempDir()
	for _, h := range []string{"pre-commit", "commit-msg", "pre-push"} {
		writeT(t, hooks, h, "#!/bin/sh\nexit 1\n")
		if err := os.Chmod(filepath.Join(hooks, h), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	global := filepath.Join(t.TempDir(), "gitconfig")
	writeT(t, filepath.Dir(global), "gitconfig",
		"[user]\n\tname = Mallory\n\temail = mallory@example.com\n[commit]\n\tgpgsign = true\n[gpg]\n\tprogram = false\n[core]\n\thooksPath = "+hooks+"\n")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_AUTHOR_NAME", "Mallory")
	t.Setenv("GIT_COMMITTER_EMAIL", "mallory@example.com")

	branch, err := PrepareBranch(context.Background(), r.root, r.demoPin(), mr, r.head)
	if err != nil {
		t.Fatal(err)
	}
	got := gitT(t, r.origin, "log", "-1", "--format=%an|%ae|%cn|%ce", "refs/heads/"+branch)
	want := "github-actions[bot]|41898282+github-actions[bot]@users.noreply.github.com|" +
		"github-actions[bot]|41898282+github-actions[bot]@users.noreply.github.com"
	if got != want {
		t.Errorf("identity %q, want %q", got, want)
	}
}

// Fails when a branch left on origin by an earlier run is not replaced by the new commit on
// top of the default branch, or when the run makes a second branch.
func TestPrepareBranch_existingRemoteBranchIsUpdated(t *testing.T) {
	r := newLaneRepo(t)
	stale := gitT(t, r.root, "commit-tree", "main^{tree}", "-p", "main", "-m", "stale run")
	gitT(t, r.root, "push", "-q", "origin", stale+":refs/heads/pins/core-skills-demo")

	branch := r.prepare(t, r.demoPin())
	if got := r.pinBranches(t); !reflect.DeepEqual(got, []string{"refs/heads/pins/core-skills-demo"}) {
		t.Fatalf("pin branches %v", got)
	}
	main := gitT(t, r.origin, "rev-parse", "main")
	if parent := gitT(t, r.origin, "rev-parse", "refs/heads/"+branch+"^"); parent != main {
		t.Errorf("parent %s, want main %s", parent, main)
	}
	if got := r.show(t, branch, "pack/core/skills/demo/new.md"); got != "new\n" {
		t.Errorf("new.md %q", got)
	}
}

// Fails when a result that reaches .github/workflows/ (through init --upgrade or the merge
// itself) is pushed, or leaves a file behind that a later pin inherits.
func TestPrepareBranch_workflowsPathPushesNothing(t *testing.T) {
	r := newLaneRepo(t)
	branch, err := PrepareBranch(context.Background(), r.root, r.ciPin(), r.merge(t, r.ciPin()), r.head)
	if !errors.Is(err, ErrTouchesWorkflows) || branch != "" {
		t.Fatalf("branch %q err %v, want ErrTouchesWorkflows", branch, err)
	}
	if !strings.Contains(err.Error(), ".github/workflows/lighthouse-ci.yml") {
		t.Errorf("error does not name the workflow: %v", err)
	}
	if got := r.pinBranches(t); len(got) != 0 {
		t.Errorf("pushed %v", got)
	}
	r.assertRootUntouched(t)

	flagged := MergeResult{Clean: true, TouchesWorkflows: true, Files: map[string][]byte{".github/workflows/x.yml": []byte("on: push\n")}}
	if _, err := PrepareBranch(context.Background(), r.root, r.toolPin(), flagged, r.head); !errors.Is(err, ErrTouchesWorkflows) {
		t.Errorf("flagged merge: err %v, want ErrTouchesWorkflows", err)
	}
	if got := r.pinBranches(t); len(got) != 0 {
		t.Errorf("pushed %v", got)
	}

	next := r.prepare(t, r.toolPin())
	want := []string{"changelog.d/pins-pack-core-lib-tool.py.md", ".lighthouse/lib/lighthouse_lib/tool.py", ".lighthouse/manifest.toml", "pack/SOURCES.toml", toolFile}
	if got := r.changed(t, next); !sameSet(got, want) {
		t.Errorf("next pin changed %v, want %v", got, want)
	}
}

// Fails when a second pin's branch carries the first pin's changes.
func TestPrepareBranch_twoPinsEachBranchHoldsOnlyOwnChanges(t *testing.T) {
	r := newLaneRepo(t)
	demo := r.prepare(t, r.demoPin())
	tool := r.prepare(t, r.toolPin())

	wantDemo := []string{
		".agents/skills/lighthouse-demo/SKILL.md", ".agents/skills/lighthouse-demo/new.md", ".agents/skills/lighthouse-demo/ref.md",
		".claude/skills/lighthouse-demo/SKILL.md", ".claude/skills/lighthouse-demo/new.md", ".claude/skills/lighthouse-demo/ref.md",
		".lighthouse/manifest.toml", "changelog.d/pins-core-skills-demo.md", "pack/SOURCES.toml",
		demoSkill, "pack/core/skills/demo/new.md", "pack/core/skills/demo/ref.md",
	}
	wantTool := []string{"changelog.d/pins-pack-core-lib-tool.py.md", ".lighthouse/lib/lighthouse_lib/tool.py", ".lighthouse/manifest.toml", "pack/SOURCES.toml", toolFile}
	if got := r.changed(t, demo); !sameSet(got, wantDemo) {
		t.Errorf("%s changed\n got %v\nwant %v", demo, got, wantDemo)
	}
	if got := r.changed(t, tool); !sameSet(got, wantTool) {
		t.Errorf("%s changed\n got %v\nwant %v", tool, got, wantTool)
	}
	r.assertRootUntouched(t)
}

// Fails when init --upgrade installs from the embedded packages (the old text) instead of the
// merged pack/ on the branch.
func TestPrepareBranch_installedCopyEqualsMergedPackFile(t *testing.T) {
	r := newLaneRepo(t)
	branch := r.prepare(t, r.demoPin())
	pack := r.show(t, branch, "pack/core/skills/demo/ref.md")
	if pack != "a\nb\nc upstream\n" {
		t.Fatalf("pack ref.md %q", pack)
	}
	for _, rel := range []string{".claude/skills/lighthouse-demo/ref.md", ".agents/skills/lighthouse-demo/ref.md"} {
		if got := r.show(t, branch, rel); got != pack {
			t.Errorf("%s %q, want the merged pack file %q", rel, got, pack)
		}
	}
	if got, want := r.show(t, branch, ".claude/skills/lighthouse-demo/SKILL.md"), installedSkill(r.head, "\nintro local\none\ntwo\nthree\nfour\nfive upstream\n"); got != want {
		t.Errorf("installed SKILL.md:\n got %q\nwant %q", got, want)
	}
}

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]bool{}
	for _, g := range got {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			return false
		}
	}
	return true
}

// Fails when PrepareBranch writes or removes a path of mr outside its worktree.
func TestPrepareBranch_refusesPathOutsideRepo(t *testing.T) {
	r := newLaneRepo(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp) // the worktree is <tmp>/lighthouse-pins-wt-*/wt
	victim := filepath.Join(tmp, "victim")
	writeT(t, tmp, "victim", "keep\n")
	for name, mr := range map[string]MergeResult{
		"write":  {Clean: true, Files: map[string][]byte{"../../escape": []byte("pwned\n")}},
		"remove": {Clean: true, Files: map[string][]byte{}, Removed: []string{"../../victim"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := PrepareBranch(context.Background(), r.root, r.demoPin(), mr, r.head); err == nil || !strings.Contains(err.Error(), "leaves the repository") {
				t.Errorf("err %v, want a refusal", err)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(tmp, "escape")); err == nil {
		t.Error("escape written outside the worktree")
	}
	if b, err := os.ReadFile(victim); err != nil || string(b) != "keep\n" {
		t.Errorf("victim %q, %v", b, err)
	}
	if got := r.pinBranches(t); len(got) != 0 {
		t.Errorf("pushed %v", got)
	}
}

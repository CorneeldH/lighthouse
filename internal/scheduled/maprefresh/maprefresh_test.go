package maprefresh

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Classification: io/filesystem (real git repositories with a bare origin, real files). The
// regression surface is the routing of the gate on the drift score and its boundaries, what the
// containment and the completeness check accept, that nothing ever reads or writes through a
// symlink, and what Finish leaves in the work tree. Each test names the production change it
// guards.

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=t@example.com")
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

// rig is a clone of a bare origin, on main with one source file.
type rig struct {
	t            *testing.T
	root, origin string
	repo         string
	seq          int
}

func newRig(t *testing.T) *rig {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, root: root, origin: filepath.Join(root, "origin.git"), repo: filepath.Join(root, "repo")}
	gitIn(t, root, "init", "-q", "--bare", "-b", "main", r.origin)
	gitIn(t, root, "clone", "-q", r.origin, r.repo)
	gitIn(t, r.repo, "checkout", "-q", "-b", "main")
	put(t, r.repo, "src/main.go", "package main\n")
	r.commit("init", true)
	return r
}

func (r *rig) commit(msg string, push bool) string {
	r.t.Helper()
	gitIn(r.t, r.repo, "add", "-A")
	gitIn(r.t, r.repo, "commit", "-q", "-m", msg)
	if push {
		gitIn(r.t, r.repo, "push", "-q", "origin", "main")
	}
	return gitIn(r.t, r.repo, "rev-parse", "HEAD")
}

// mapIt commits the seven documents under map_dir (the base of the drift).
func (r *rig) mapIt() string {
	for _, n := range testDocs {
		put(r.t, r.repo, ".planning/codebase/"+n+".md", doc(n))
	}
	return r.commit("map", true)
}

// churn commits n new files in src/ (an existing directory).
func (r *rig) churn(n int) {
	for i := 0; i < n; i++ {
		r.seq++
		put(r.t, r.repo, fmt.Sprintf("src/f%d.go", r.seq), "package main\n")
	}
	r.commit("churn", true)
}

var testDocs = []string{"STACK", "INTEGRATIONS", "ARCHITECTURE", "STRUCTURE", "CONVENTIONS", "TESTING", "CONCERNS"}

func doc(name string) string {
	return "# " + name + "\n" + strings.Repeat("line\n", 25)
}

func (r *rig) env() scheduled.Env {
	return scheduled.Env{
		Repo: repoconfig.Repo{Dir: r.repo, DefaultBranch: "main", Slug: "o/r", Language: "en"},
		Now:  time.Date(2026, 11, 18, 4, 0, 0, 0, time.UTC),
		Run:  workplace.ExecRunner(),
	}
}

// bind is the job configured with params, failing the test on a config error.
func bind(t *testing.T, params map[string]any) *Job {
	t.Helper()
	j, err := scheduled.Bind(New(), repoconfig.Schedule{Name: "map", Job: Name, Params: params})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	return j.(*Job)
}

// small thresholds keep the table cheap: skip below 3, fast up to and including 6, full above.
var small = map[string]any{"drift_low": int64(3), "drift_high": int64(6), "new_dir_weight": int64(4), "publish_dir": "docs/map"}

func TestGateRoutesOnTheDriftScore(t *testing.T) {
	// Guards the thresholds: `< drift_low` skips, `<= drift_high` is fast, above is full. Each
	// boundary has a case on both sides, so a `<` that becomes `<=` (or the reverse) fails one.
	for _, tc := range []struct {
		changed int
		skip    bool
		mode    string
	}{
		{2, true, ""},
		{3, false, "fast"},
		{6, false, "fast"},
		{7, false, "full"},
	} {
		t.Run(fmt.Sprintf("%d changed", tc.changed), func(t *testing.T) {
			r := newRig(t)
			r.mapIt()
			r.churn(tc.changed)
			v, err := bind(t, small).Gate(context.Background(), r.env())
			if err != nil {
				t.Fatalf("Gate: %v", err)
			}
			if (v.Skip != "") != tc.skip || v.Mode != tc.mode || v.Score != tc.changed {
				t.Fatalf("verdict = %+v, want skip %v, mode %q, score %d", v, tc.skip, tc.mode, tc.changed)
			}
		})
	}
}

func TestGateWeighsANewDirectory(t *testing.T) {
	// Guards new_dir_weight: one file in a new directory scores 1 + 4 = 5 (fast), where a gate
	// that counts only the file scores 1 (skip), and one that counts the directory without its
	// weight scores 2.
	r := newRig(t)
	r.mapIt()
	put(t, r.repo, "src/newpkg/x.go", "package newpkg\n")
	r.commit("new dir", true)
	v, err := bind(t, small).Gate(context.Background(), r.env())
	if err != nil {
		t.Fatal(err)
	}
	if v.Skip != "" || v.Mode != "fast" || v.Score != 5 {
		t.Fatalf("verdict = %+v, want fast with score 5 (1 file + 4 for the new directory)", v)
	}
}

func TestGateIgnoresChangesOutsideDriftPaths(t *testing.T) {
	// Guards drift_paths: churn in docs/ says nothing about the architecture.
	r := newRig(t)
	r.mapIt()
	for i := 0; i < 10; i++ {
		put(t, r.repo, fmt.Sprintf("docs/n%d.md", i), "x\n")
	}
	r.commit("docs churn", true)
	v, err := bind(t, small).Gate(context.Background(), r.env())
	if err != nil || v.Skip == "" {
		t.Fatalf("verdict = %+v, %v; want a skip", v, err)
	}
}

func TestGateNeverMappedIsFull(t *testing.T) {
	// Guards the "never mapped" branch: no commit touched map_dir, so the run is the full map
	// whatever the drift.
	r := newRig(t)
	v, err := bind(t, small).Gate(context.Background(), r.env())
	if err != nil {
		t.Fatal(err)
	}
	if v.Skip != "" || v.Mode != "full" {
		t.Fatalf("verdict = %+v, want full", v)
	}
}

func TestGateMeasuresOriginNotTheLocalCheckout(t *testing.T) {
	// Guards the measure on origin/<default>: ten unpushed local commits do not count, and a
	// remote commit the checkout has not fetched does (the gate fetches).
	r := newRig(t)
	r.mapIt()
	for i := 0; i < 10; i++ {
		put(t, r.repo, fmt.Sprintf("src/local%d.go", i), "x\n")
	}
	r.commit("local only", false)
	v, err := bind(t, small).Gate(context.Background(), r.env())
	if err != nil || v.Skip == "" {
		t.Fatalf("local churn: verdict = %+v, %v; want a skip", v, err)
	}

	other := filepath.Join(r.root, "other")
	gitIn(t, r.root, "clone", "-q", r.origin, other)
	for i := 0; i < 4; i++ {
		put(t, other, fmt.Sprintf("src/remote%d.go", i), "x\n")
	}
	gitIn(t, other, "add", "-A")
	gitIn(t, other, "commit", "-q", "-m", "remote churn")
	gitIn(t, other, "push", "-q", "origin", "HEAD:main")
	v, err = bind(t, small).Gate(context.Background(), r.env())
	if err != nil || v.Mode != "fast" || v.Score != 4 {
		t.Fatalf("remote churn: verdict = %+v, %v; want fast with score 4", v, err)
	}
}

func TestGateErrorsDoNotLeakTheHostPath(t *testing.T) {
	// Guards the error text of the gate: a repo that is not a repository fails with the
	// subcommand and its exit code, and the host path of the repo is not in it.
	dir := t.TempDir()
	env := scheduled.Env{Repo: repoconfig.Repo{Dir: dir, DefaultBranch: "main"}, Run: workplace.ExecRunner()}
	_, err := bind(t, small).Gate(context.Background(), env)
	if err == nil {
		t.Fatal("Gate on a directory that is no repository succeeded")
	}
	if strings.Contains(err.Error(), dir) {
		t.Errorf("error %q holds the host path %s", err, dir)
	}
}

func TestLaunch(t *testing.T) {
	// Guards the launch plan: fast carries fast_args and the 45m limit, full the bare command and
	// 120m, the default tools are the spec's, and the instructions name the writable paths.
	j := bind(t, small)
	fast, err := j.Launch(scheduled.Verdict{Mode: "fast", Args: "--fast --focus tech+arch"}, scheduled.Env{})
	if err != nil {
		t.Fatal(err)
	}
	full, err := j.Launch(scheduled.Verdict{Mode: "full"}, scheduled.Env{})
	if err != nil {
		t.Fatal(err)
	}
	if fast.Command != "/gsd-map-codebase --fast --focus tech+arch" || fast.MaxRuntime != 45*time.Minute {
		t.Errorf("fast plan = %q, %v", fast.Command, fast.MaxRuntime)
	}
	if full.Command != "/gsd-map-codebase" || full.MaxRuntime != 120*time.Minute {
		t.Errorf("full plan = %q, %v", full.Command, full.MaxRuntime)
	}
	if want := []string{"Read", "Write", "Glob", "Grep", "Bash", "Agent"}; !slices.Equal(full.Tools, want) {
		t.Errorf("tools = %v, want %v", full.Tools, want)
	}
	for _, w := range []string{".planning/codebase/", "docs/map/", "Do NOT create a git commit"} {
		if !strings.Contains(full.Instructions, w) {
			t.Errorf("instructions lack %q:\n%s", w, full.Instructions)
		}
	}
	custom := bind(t, map[string]any{"tools": "Read Write", "max_runtime_fast": "10m", "max_runtime_full": "20m", "fast_args": "--x"})
	p, _ := custom.Launch(scheduled.Verdict{Mode: "fast", Args: "--x"}, scheduled.Env{})
	if !slices.Equal(p.Tools, []string{"Read", "Write"}) || p.MaxRuntime != 10*time.Minute || p.Command != "/gsd-map-codebase --x" {
		t.Errorf("custom plan = %+v", p)
	}
}

func TestAllowedOnlyMapDirAndPublishDir(t *testing.T) {
	// Guards the containment: only files under map_dir/ and publish_dir/. The directory itself,
	// a sibling with the same prefix and a path with a dot-dot are refused.
	j := bind(t, small)
	for p, want := range map[string]bool{
		".planning/codebase/STACK.md": true,
		"docs/map/index.md":           true,
		"docs/map/sub/x.md":           true,
		".planning/codebase":          false,
		".planning/codebase2/x.md":    false,
		"docs/mapx/x.md":              false,
		"docs/other.md":               false,
		"src/main.go":                 false,
		".planning/STATE.md":          false,
	} {
		if got := j.Allowed(p); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", p, got, want)
		}
	}
	if bind(t, nil).Allowed("docs/developer/codebase/x.md") {
		t.Error("without publish_dir nothing outside map_dir is allowed")
	}
}

func TestParamsAreValidated(t *testing.T) {
	// Guards the config errors: an empty documents list, a typo in a key, thresholds the wrong
	// way round, a path that leaves the repo and a map_dir that holds the publish_dir.
	for name, tc := range map[string]struct {
		params map[string]any
		want   string
	}{
		"empty documents":       {map[string]any{"documents": []any{}}, "documents: must not be empty"},
		"document with a slash": {map[string]any{"documents": []any{"../x"}}, "documents entry"},
		"duplicate document":    {map[string]any{"documents": []any{"A", "A"}}, "listed twice"},
		"unknown key":           {map[string]any{"drift_lowe": int64(3)}, "unknown key"},
		"low above high":        {map[string]any{"drift_low": int64(9), "drift_high": int64(3)}, "above drift_high"},
		"map_dir escapes":       {map[string]any{"map_dir": "../x"}, "map_dir"},
		"map_dir is .git":       {map[string]any{"map_dir": ".git/x"}, "map_dir"},
		"nested dirs":           {map[string]any{"map_dir": "a", "publish_dir": "a/b"}, "inside one another"},
		"empty drift_paths":     {map[string]any{"drift_paths": []any{}}, "drift_paths: must not be empty"},
		"min_lines zero":        {map[string]any{"min_lines": int64(0)}, "min_lines"},
		"bad duration":          {map[string]any{"max_runtime_fast": "soon"}, "max_runtime_fast"},
		"command not a string":  {map[string]any{"command": int64(3)}, "command"},
		"newline in command":    {map[string]any{"command": "/a\n/b"}, "command"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := scheduled.Bind(New(), repoconfig.Schedule{Name: "map", Job: Name, Params: tc.params})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Bind error = %v, want one containing %q", err, tc.want)
			}
		})
	}
	if _, err := scheduled.Bind(New(), repoconfig.Schedule{Name: "map", Job: Name}); err != nil {
		t.Errorf("no params: %v, want the defaults", err)
	}
}

func TestAnUnboundJobRefusesToRun(t *testing.T) {
	// Guards the registry instance: it has no params, so it must not run with defaults while the
	// schedule says otherwise.
	j := New()
	if _, err := j.Gate(context.Background(), scheduled.Env{}); err == nil {
		t.Error("unbound Gate succeeded")
	}
	if _, err := j.Launch(scheduled.Verdict{}, scheduled.Env{}); err == nil {
		t.Error("unbound Launch succeeded")
	}
	if j.Allowed(".planning/codebase/STACK.md") {
		t.Error("unbound Allowed accepted a path")
	}
	if err := j.Cleanup(context.Background(), scheduled.Env{}, t.TempDir()); err == nil {
		t.Error("unbound Cleanup succeeded")
	}
	if err := j.Check(context.Background(), scheduled.Env{}, t.TempDir()); err == nil {
		t.Error("unbound Check succeeded")
	}
	if err := j.Finish(context.Background(), scheduled.Env{}, t.TempDir(), scheduled.Verdict{}); err == nil {
		t.Error("unbound Finish succeeded")
	}
	if got, ok := scheduled.Lookup(Name); !ok || got.Name() != Name {
		t.Errorf("Lookup(%q) = %v, %v; want the registered job", Name, got, ok)
	}
}

func TestCheck(t *testing.T) {
	// Guards the completeness check: a missing, a short, a deleted and a symlinked document each
	// fail, and a full set passes.
	j := bind(t, small)
	ctx := context.Background()
	full := func(t *testing.T) string {
		dir := t.TempDir()
		for _, n := range testDocs {
			put(t, dir, ".planning/codebase/"+n+".md", doc(n))
		}
		return dir
	}
	if err := j.Check(ctx, scheduled.Env{}, full(t)); err != nil {
		t.Fatalf("complete set: %v", err)
	}
	t.Run("missing", func(t *testing.T) {
		dir := full(t)
		must(t, os.Remove(filepath.Join(dir, ".planning/codebase/TESTING.md")))
		wantErr(t, j.Check(ctx, scheduled.Env{}, dir), "TESTING (missing)")
	})
	t.Run("short", func(t *testing.T) {
		dir := full(t)
		put(t, dir, ".planning/codebase/STACK.md", strings.Repeat("l\n", 19))
		wantErr(t, j.Check(ctx, scheduled.Env{}, dir), "STACK (19 lines, want at least 20)")
	})
	t.Run("exactly min_lines passes", func(t *testing.T) {
		dir := full(t)
		put(t, dir, ".planning/codebase/STACK.md", strings.Repeat("l\n", 20))
		if err := j.Check(ctx, scheduled.Env{}, dir); err != nil {
			t.Fatalf("20 lines: %v", err)
		}
	})
	t.Run("deleted from a tracked set", func(t *testing.T) {
		r := newRig(t)
		r.mapIt()
		must(t, os.Remove(filepath.Join(r.repo, ".planning/codebase/CONCERNS.md")))
		wantErr(t, j.Check(ctx, scheduled.Env{}, r.repo), "CONCERNS (missing)")
	})
	t.Run("symlinked document", func(t *testing.T) {
		dir := full(t)
		outside := filepath.Join(t.TempDir(), "elsewhere.md")
		put(t, filepath.Dir(outside), "elsewhere.md", strings.Repeat("l\n", 40))
		p := filepath.Join(dir, ".planning/codebase/STACK.md")
		must(t, os.Remove(p))
		must(t, os.Symlink(outside, p))
		err := j.Check(ctx, scheduled.Env{}, dir)
		wantErr(t, err, "is a symlink")
		if strings.Contains(err.Error(), outside) {
			t.Errorf("error %q holds the host path", err)
		}
	})
	t.Run("map_dir is a symlink", func(t *testing.T) {
		dir := full(t)
		real := filepath.Join(dir, "real")
		must(t, os.Rename(filepath.Join(dir, ".planning/codebase"), real))
		must(t, os.Symlink(real, filepath.Join(dir, ".planning/codebase")))
		wantErr(t, j.Check(ctx, scheduled.Env{}, dir), "is a symlink")
	})
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func wantErr(t *testing.T, err error, fragment string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), fragment) {
		t.Fatalf("error = %v, want one containing %q", err, fragment)
	}
}

func TestCleanupNeverFollowsASymlink(t *testing.T) {
	// Guards Cleanup: a link in map_dir or publish_dir (a file, a directory, the directory
	// itself) is unlinked, and what it points at, outside the work tree, is left as it was.
	j := bind(t, small)
	outside := t.TempDir()
	put(t, outside, "secret.txt", "keep me\n")
	put(t, outside, "dir/inner.txt", "keep me too\n")
	untouched := func(t *testing.T) {
		t.Helper()
		for p, want := range map[string]string{"secret.txt": "keep me\n", "dir/inner.txt": "keep me too\n"} {
			if b, err := os.ReadFile(filepath.Join(outside, p)); err != nil || string(b) != want {
				t.Errorf("%s = %q, %v; want it untouched", p, b, err)
			}
		}
	}

	t.Run("links inside the directories", func(t *testing.T) {
		dir := t.TempDir()
		put(t, dir, ".planning/codebase/REAL.md", "real\n")
		must(t, os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, ".planning/codebase/STACK.md")))
		must(t, os.Symlink(filepath.Join(outside, "dir"), filepath.Join(dir, ".planning/codebase/sub")))
		put(t, dir, "docs/map/keep.md", "keep\n")
		must(t, os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "docs/map/index.md")))
		must(t, j.Cleanup(context.Background(), scheduled.Env{}, dir))
		untouched(t)
		for _, p := range []string{".planning/codebase/STACK.md", ".planning/codebase/sub", "docs/map/index.md"} {
			if _, err := os.Lstat(filepath.Join(dir, p)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("%s after Cleanup: %v, want the link gone", p, err)
			}
		}
		for _, p := range []string{".planning/codebase/REAL.md", "docs/map/keep.md"} {
			if _, err := os.Lstat(filepath.Join(dir, p)); err != nil {
				t.Errorf("%s after Cleanup: %v, want the real file kept", p, err)
			}
		}
	})
	t.Run("the directory itself is a link", func(t *testing.T) {
		dir := t.TempDir()
		put(t, dir, ".planning/x", "")
		must(t, os.Remove(filepath.Join(dir, ".planning/x")))
		must(t, os.Symlink(filepath.Join(outside, "dir"), filepath.Join(dir, ".planning/codebase")))
		must(t, j.Cleanup(context.Background(), scheduled.Env{}, dir))
		untouched(t)
		if _, err := os.Lstat(filepath.Join(dir, ".planning/codebase")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("map_dir link after Cleanup: %v, want it gone", err)
		}
	})
	t.Run("a parent of publish_dir is a link", func(t *testing.T) {
		dir := t.TempDir()
		must(t, os.Symlink(outside, filepath.Join(dir, "docs")))
		must(t, j.Cleanup(context.Background(), scheduled.Env{}, dir))
		untouched(t)
		if _, err := os.Lstat(filepath.Join(dir, "docs")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("docs link after Cleanup: %v, want it gone", err)
		}
	})
	t.Run("nothing there", func(t *testing.T) {
		must(t, j.Cleanup(context.Background(), scheduled.Env{}, t.TempDir()))
	})
}

// run is a workplace for Finish: a worktree of the rig on a run branch, at the start commit.
func (r *rig) workplace() (dir, start string) {
	r.t.Helper()
	dir = filepath.Join(r.root, "work")
	gitIn(r.t, r.repo, "worktree", "add", "-q", "-b", "scheduled/map-20261118T0400-1", dir, "origin/main")
	return dir, gitIn(r.t, dir, "rev-parse", "HEAD")
}

func TestFinishMirrorsACopyAndWritesTheIndex(t *testing.T) {
	// Guards Finish: the documents are copied into publish_dir as regular files (not links),
	// byte-equal, and index.md carries mode, score, the base commit and the date.
	r := newRig(t)
	base := r.mapIt()
	r.churn(4)
	dir, _ := r.workplace()
	for _, n := range testDocs {
		put(t, dir, ".planning/codebase/"+n+".md", doc(n)+"refreshed\n")
	}
	j := bind(t, small)
	must(t, j.Finish(context.Background(), r.env(), dir, scheduled.Verdict{Mode: "fast", Score: 4}))
	for _, n := range testDocs {
		p := filepath.Join(dir, "docs/map", n+".md")
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("%s: %v, mode %v; want a regular file", n, err, info)
		}
		if b, _ := os.ReadFile(p); string(b) != doc(n)+"refreshed\n" {
			t.Errorf("%s copy differs from the source", n)
		}
	}
	idx, err := os.ReadFile(filepath.Join(dir, "docs/map/index.md"))
	must(t, err)
	for _, w := range []string{"2026-11-18", "**fast**", "**4**", "`" + base[:8] + "`", "[STACK](STACK.md)"} {
		if !strings.Contains(string(idx), w) {
			t.Errorf("index.md lacks %q:\n%s", w, idx)
		}
	}
}

func TestFinishDoesNotWriteThroughALink(t *testing.T) {
	// Guards the mirror: a link at the target of a copy, or at index.md, is replaced by a file,
	// and the file it pointed at, outside the work tree, is not written.
	r := newRig(t)
	r.mapIt()
	dir, _ := r.workplace()
	for _, n := range testDocs {
		put(t, dir, ".planning/codebase/"+n+".md", doc(n)+"refreshed\n")
	}
	outside := filepath.Join(t.TempDir(), "victim.txt")
	put(t, filepath.Dir(outside), "victim.txt", "victim\n")
	must(t, os.MkdirAll(filepath.Join(dir, "docs/map"), 0o755))
	must(t, os.Symlink(outside, filepath.Join(dir, "docs/map/STACK.md")))
	must(t, os.Symlink(outside, filepath.Join(dir, "docs/map/index.md")))
	must(t, bind(t, small).Finish(context.Background(), r.env(), dir, scheduled.Verdict{Mode: "full", Score: 9}))
	if b, _ := os.ReadFile(outside); string(b) != "victim\n" {
		t.Fatalf("the file behind the link is now %q", b)
	}
	for _, n := range []string{"STACK.md", "index.md"} {
		if info, err := os.Lstat(filepath.Join(dir, "docs/map", n)); err != nil || !info.Mode().IsRegular() {
			t.Errorf("%s: %v, %v; want a regular file", n, err, info)
		}
	}
}

func TestFinishRefusesALinkedPublishDir(t *testing.T) {
	// Guards the mirror target: a publish_dir that is a link to a directory is an error, and
	// nothing is written behind it.
	r := newRig(t)
	r.mapIt()
	dir, _ := r.workplace()
	for _, n := range testDocs {
		put(t, dir, ".planning/codebase/"+n+".md", doc(n)+"refreshed\n")
	}
	outside := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "docs"), 0o755))
	must(t, os.Symlink(outside, filepath.Join(dir, "docs/map")))
	err := bind(t, small).Finish(context.Background(), r.env(), dir, scheduled.Verdict{Mode: "full"})
	wantErr(t, err, "is a symlink")
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("%d entries written behind the link", len(entries))
	}
}

func TestFinishUnchangedMapLeavesNothingToCommit(t *testing.T) {
	// Guards the no-op run: map_dir byte-equal to HEAD gives no index.md and no mirror, and
	// whatever the agent wrote into publish_dir is put back, so the work tree is clean.
	r := newRig(t)
	r.mapIt()
	j := bind(t, small)
	dir, _ := r.workplace()
	for _, n := range testDocs { // a first, real refresh, committed with its mirror
		put(t, dir, ".planning/codebase/"+n+".md", doc(n)+"v2\n")
	}
	must(t, j.Finish(context.Background(), r.env(), dir, scheduled.Verdict{Mode: "full"}))
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "publish")
	for _, n := range testDocs { // the next run rewrites the same bytes and pollutes publish_dir
		put(t, dir, ".planning/codebase/"+n+".md", doc(n)+"v2\n")
	}
	put(t, dir, "docs/map/STACK.md", "tampered\n")
	put(t, dir, "docs/map/stray.md", "stray\n")
	must(t, j.Finish(context.Background(), r.env(), dir, scheduled.Verdict{Mode: "full", Score: 9}))
	if st := gitIn(t, dir, "status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Fatalf("work tree after an unchanged run:\n%s\nwant clean", st)
	}
}

func TestFinishUnchangedMapWithoutAPublishedTree(t *testing.T) {
	// Guards the first-ever run shape: publish_dir is not in HEAD, the map is unchanged and the
	// agent left a stray file there; it is removed instead of failing the restore.
	r := newRig(t)
	r.mapIt()
	dir, _ := r.workplace()
	put(t, dir, "docs/map/stray.md", "stray\n")
	must(t, bind(t, small).Finish(context.Background(), r.env(), dir, scheduled.Verdict{Mode: "full"}))
	if st := gitIn(t, dir, "status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Fatalf("work tree:\n%s\nwant clean", st)
	}
}

func TestFinishWithoutAPublishDirDoesNotMirror(t *testing.T) {
	// Guards publish_dir "": the default has no mirror, so a refreshed map writes nothing else.
	r := newRig(t)
	r.mapIt()
	dir, _ := r.workplace()
	for _, n := range testDocs {
		put(t, dir, ".planning/codebase/"+n+".md", doc(n)+"refreshed\n")
	}
	must(t, bind(t, nil).Finish(context.Background(), r.env(), dir, scheduled.Verdict{Mode: "full"}))
	if st := gitIn(t, dir, "status", "--porcelain", "--untracked-files=all"); strings.Contains(st, "docs") || !strings.Contains(st, ".planning/codebase/STACK.md") {
		t.Fatalf("status:\n%s", st)
	}
}

func TestRoute(t *testing.T) {
	// Guards the boundaries of the pure router directly.
	for _, tc := range []struct {
		score int
		want  string
	}{{0, ""}, {24, ""}, {25, "fast"}, {150, "fast"}, {151, "full"}} {
		if got := route(tc.score, 25, 150); got != tc.want {
			t.Errorf("route(%d) = %q, want %q", tc.score, got, tc.want)
		}
	}
}

func TestFinishRefusesASymlinkedParentOfPublishDir(t *testing.T) {
	// Guards Finish without Cleanup: a link among the existing parents of publish_dir is
	// refused before anything is created, and nothing is written behind it.
	r := newRig(t)
	r.mapIt()
	dir, _ := r.workplace()
	for _, n := range testDocs {
		put(t, dir, ".planning/codebase/"+n+".md", doc(n)+"refreshed\n")
	}
	outside := t.TempDir()
	must(t, os.Symlink(outside, filepath.Join(dir, "docs")))
	j := bind(t, map[string]any{"publish_dir": "docs/deep/map"})
	wantErr(t, j.Finish(context.Background(), r.env(), dir, scheduled.Verdict{Mode: "full"}), "is a symlink")
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("%d entries created behind the link", len(entries))
	}
}

func TestGitErrorsNameTheSubcommandNotTheOptions(t *testing.T) {
	// Guards the error label of Finish's hardened git calls: `git status`, not `git -c`.
	failing := func(context.Context, time.Duration, string, ...string) (string, int, error) { return "", 7, nil }
	env := scheduled.Env{Run: failing}
	err := bind(t, small).Finish(context.Background(), env, t.TempDir(), scheduled.Verdict{})
	wantErr(t, err, "git status: exit code 7")
}

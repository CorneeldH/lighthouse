package pins

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// upstreamRepo is a local bare repository standing in for github.com/<owner>/<repo>.
type upstreamRepo struct {
	t    *testing.T
	root string // parent of owner/repo
	repo string // owner/repo
	bare string
	work string
}

func gitEnvTest() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnvTest()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newUpstream(t *testing.T, branch string) *upstreamRepo {
	t.Helper()
	root := t.TempDir()
	u := &upstreamRepo{t: t, root: root, repo: "o/r"}
	u.bare = filepath.Join(root, "o", "r")
	u.work = filepath.Join(t.TempDir(), "work")
	for _, d := range []string{u.bare, u.work} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, u.bare, "init", "--bare", "-b", branch)
	// A local file:// remote only serves partial clones when it allows filters.
	runGit(t, u.bare, "config", "uploadpack.allowFilter", "true")
	runGit(t, u.bare, "config", "uploadpack.allowAnySHA1InWant", "true")
	runGit(t, u.work, "init", "-b", branch)
	runGit(t, u.work, "remote", "add", "origin", u.bare)
	return u
}

// commit writes files, commits them and pushes; it returns the commit sha.
func (u *upstreamRepo) commit(subject string, files map[string]string) string {
	u.t.Helper()
	for rel, content := range files {
		writeFile(u.t, u.work, rel, content)
	}
	runGit(u.t, u.work, "add", "-A")
	runGit(u.t, u.work, "commit", "-m", subject)
	runGit(u.t, u.work, "push", "origin", "HEAD")
	return runGit(u.t, u.work, "rev-parse", "HEAD")
}

func (u *upstreamRepo) remove(subject, rel string) string {
	u.t.Helper()
	runGit(u.t, u.work, "rm", "-q", rel)
	runGit(u.t, u.work, "commit", "-m", subject)
	runGit(u.t, u.work, "push", "origin", "HEAD")
	return runGit(u.t, u.work, "rev-parse", "HEAD")
}

func (u *upstreamRepo) source() GitSource {
	return &ghSource{base: func(repo string) string {
		return "file://" + filepath.Join(u.root, filepath.FromSlash(repo))
	}}
}

func (u *upstreamRepo) pin(commit, path string) SourcePin {
	return SourcePin{Key: "k", Repo: u.repo, Commit: commit, Path: path}
}

func TestMeasureSource_currentWhenOnlyOtherPathMoved(t *testing.T) {
	// Class: behaviour. At risk: a moved upstream HEAD outside the pinned path flips to behind.
	u := newUpstream(t, "main")
	pinned := u.commit("add", map[string]string{"x/a.txt": "a\n", "other/b.txt": "b\n"})
	head := u.commit("touch other", map[string]string{"other/b.txt": "b2\n"})

	got := MeasureSource(context.Background(), u.source(), u.pin(pinned, "x"))

	if got.Status != StatusCurrent {
		t.Fatalf("status = %q (%s), want current", got.Status, got.Detail)
	}
	if got.Target != head {
		t.Errorf("target = %q, want upstream HEAD %q", got.Target, head)
	}
}

func TestMeasureSource_behindListsCommits(t *testing.T) {
	// Class: behaviour. At risk: behind without the commits that touched the path, or with
	// commits that did not.
	u := newUpstream(t, "main")
	pinned := u.commit("add", map[string]string{"x/a.txt": "a\n", "other/b.txt": "b\n"})
	c2 := u.commit("change x", map[string]string{"x/a.txt": "a2\n"})
	u.commit("change other", map[string]string{"other/b.txt": "b2\n"})
	c4 := u.commit("change x again\n\nbody line", map[string]string{"x/a.txt": "a3\n"})

	got := MeasureSource(context.Background(), u.source(), u.pin(pinned, "x"))

	if got.Status != StatusBehind {
		t.Fatalf("status = %q (%s), want behind", got.Status, got.Detail)
	}
	want := []CommitInfo{
		{SHA: c4, Subject: "change x again", Body: "body line"},
		{SHA: c2, Subject: "change x"},
	}
	if !reflect.DeepEqual(got.Commits, want) {
		t.Errorf("commits = %+v, want %+v", got.Commits, want)
	}
	for _, s := range []string{"change x", "change x again"} {
		if !strings.Contains(got.Detail, s) {
			t.Errorf("detail %q lacks %q", got.Detail, s)
		}
	}
	if strings.Contains(got.Detail, "change other") {
		t.Errorf("detail %q names a commit outside the path", got.Detail)
	}
}

func TestMeasureSource_errors(t *testing.T) {
	// Class: behaviour. At risk: an unmeasurable pin reported as current or behind.
	u := newUpstream(t, "main")
	pinned := u.commit("add", map[string]string{"x/a.txt": "a\n", "gone/g.txt": "g\n"})
	u.remove("drop gone", "gone/g.txt")

	cases := []struct {
		name string
		pin  SourcePin
		src  GitSource
	}{
		{"unknown pin commit", u.pin(strings.Repeat("a", 40), "x"), u.source()},
		{"path missing on the pin", u.pin(pinned, "nope"), u.source()},
		{"path gone on HEAD", u.pin(pinned, "gone"), u.source()},
		{"unknown repo", SourcePin{Key: "k", Repo: "o/missing", Commit: pinned, Path: "x"}, u.source()},
		{"no upstream-commit", u.pin("", "x"), u.source()},
		{"pin that did not load", SourcePin{Key: "k", Err: errors.New("bad frontmatter")}, u.source()},
		{"commit that is not a sha", u.pin("--upload-pack=touch x", "x"), u.source()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MeasureSource(context.Background(), c.src, c.pin)
			if got.Status != StatusError {
				t.Errorf("status = %q (%s), want error", got.Status, got.Detail)
			}
			if got.Detail == "" {
				t.Error("an error carries a detail")
			}
		})
	}
}

func TestMeasureSource_defaultBranchNotMain(t *testing.T) {
	// Class: behaviour. At risk: a hardcoded `main` instead of the remote's HEAD.
	u := newUpstream(t, "master")
	pinned := u.commit("add", map[string]string{"x/a.txt": "a\n"})
	head := u.commit("change x", map[string]string{"x/a.txt": "a2\n"})

	got := MeasureSource(context.Background(), u.source(), u.pin(pinned, "x"))

	if got.Status != StatusBehind || got.Target != head {
		t.Errorf("got %q target %q (%s), want behind at %q", got.Status, got.Target, got.Detail, head)
	}
}

func TestMeasureSource_neverRunsUpstreamHooks(t *testing.T) {
	// Class: safety. At risk: a global hooks path runs code during the measurement.
	u := newUpstream(t, "main")
	pinned := u.commit("add", map[string]string{"x/a.txt": "a\n"})
	u.commit("change x", map[string]string{"x/a.txt": "a2\n"})

	hooks, marker := t.TempDir(), filepath.Join(t.TempDir(), "ran")
	for _, h := range []string{"post-checkout", "post-merge", "reference-transaction"} {
		script := "#!/bin/sh\ntouch " + marker + "\n"
		if err := os.WriteFile(filepath.Join(hooks, h), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte("[core]\n\thooksPath = "+hooks+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)

	// Sanity: the planted hook does fire for a plain git call that honours the config.
	probe := exec.Command("git", "clone", "-q", "--bare", u.bare, filepath.Join(t.TempDir(), "p"))
	if out, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("probe: %v %s", err, out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the planted hooks do not fire for a plain clone, so this test proves nothing: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}

	cl, err := u.source().Clone(context.Background(), u.repo)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cl.Close() }()
	g := cl.(*ghClone)
	if _, err := cl.CommitTree(pinned, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.ReadFile(pinned, "x/a.txt"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(marker); err == nil {
		t.Error("an upstream hook ran during the measurement")
	}
	if _, err := os.Stat(filepath.Join(g.dir, "HEAD")); err != nil {
		t.Errorf("clone is not bare: %v", err)
	}
	if _, err := os.Stat(filepath.Join(g.dir, "x")); err == nil {
		t.Error("clone has a working tree")
	}
	if _, err := os.Stat(filepath.Join(g.dir, ".git")); err == nil {
		t.Error("clone has a .git, so a working tree")
	}
}

func TestGhClone_readFileAndListTree(t *testing.T) {
	// Class: behaviour. At risk: content or listing taken from HEAD instead of the sha.
	u := newUpstream(t, "main")
	old := u.commit("add", map[string]string{"x/b.txt": "old\n", "x/sub/a.txt": "a\n", "y/c.txt": "c\n"})
	u.commit("change", map[string]string{"x/b.txt": "new\n", "x/z.txt": "z\n"})

	cl, err := u.source().Clone(context.Background(), u.repo)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cl.Close() }()

	data, err := cl.ReadFile(old, "x/b.txt")
	if err != nil || string(data) != "old\n" {
		t.Errorf("ReadFile = %q, %v; want old content", data, err)
	}
	if _, err := cl.ReadFile(old, "x/missing.txt"); err == nil {
		t.Error("ReadFile of a missing file returned no error")
	}
	got, err := cl.ListTree(old, "x")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"x/b.txt", "x/sub/a.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ListTree(old) = %v, want %v", got, want)
	}
	head, err := cl.Head()
	if err != nil {
		t.Fatal(err)
	}
	got, err = cl.ListTree(head, "x")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"x/b.txt", "x/sub/a.txt", "x/z.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ListTree(head) = %v, want %v", got, want)
	}
	if _, err := cl.ListTree(old, "nope"); err == nil {
		t.Error("ListTree of a missing path returned no error")
	}
}

func TestGhClone_closeRemovesDir(t *testing.T) {
	// Class: behaviour. At risk: temp clones that pile up.
	u := newUpstream(t, "main")
	u.commit("add", map[string]string{"x/a.txt": "a\n"})
	cl, err := u.source().Clone(context.Background(), u.repo)
	if err != nil {
		t.Fatal(err)
	}
	dir := cl.(*ghClone).dir
	if err := cl.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("clone dir still exists: %v", err)
	}
}

func TestGitHubSource_urlIsHTTPS(t *testing.T) {
	// Class: behaviour. At risk: the default source pointing somewhere else than GitHub.
	src := NewGitSource().(*ghSource)
	if got := src.base("o/r"); got != "https://github.com/o/r" {
		t.Errorf("base = %q", got)
	}
}

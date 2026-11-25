package repopins

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const uiTag = "v1.2.3"

// uiEnv is the environment of every git and script call: the user's git configuration is switched
// off (a global `commit.gpgsign` must not steer the tests) and GIT_* of an outer hook is dropped.
func uiEnv(repo string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") && !strings.HasPrefix(kv, "HOME=") {
			env = append(env, kv)
		}
	}
	return append(env, "HOME="+repo, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
}

func needTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"git", "bash"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("COULD NOT CHECK: %s is not on the PATH: %v", tool, err)
		}
	}
}

func uiGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = uiEnv(repo)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func uiWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// uiRepo is a repository with one commit on main.
func uiRepo(t *testing.T) string {
	t.Helper()
	needTools(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	uiGit(t, repo, "init", "-q", "-b", "main")
	uiGit(t, repo, "config", "user.name", "Test")
	uiGit(t, repo, "config", "user.email", "test@example.com")
	uiWrite(t, filepath.Join(repo, "README.md"), "hello\n")
	uiGit(t, repo, "add", "README.md")
	uiGit(t, repo, "commit", "-q", "-m", "init")
	return repo
}

// uiSrc is a built SPA outside the repository; withIndex false leaves out index.html.
func uiSrc(t *testing.T, withIndex bool) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "src")
	if withIndex {
		uiWrite(t, filepath.Join(src, "index.html"), "<html></html>\n")
	}
	uiWrite(t, filepath.Join(src, "assets", "app.js"), "console.log(1)\n")
	return src
}

// uiRun runs ci/tag-with-ui.sh in repo and returns its output and its exit error (nil on success).
func uiRun(t *testing.T, repo, src string) (string, error) {
	t.Helper()
	script := filepath.Join(repoRoot(t), "ci", "tag-with-ui.sh")
	cmd := exec.Command(script, uiTag, src)
	if runtime.GOOS == "windows" {
		// Windows cannot exec a shell script; Git Bash runs it there (#559).
		cmd = exec.Command("bash", filepath.ToSlash(script), uiTag, src)
	}
	cmd.Dir = repo
	cmd.Env = uiEnv(repo)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func uiMustRun(t *testing.T, repo, src string) {
	t.Helper()
	if out, err := uiRun(t, repo, src); err != nil {
		t.Fatalf("tag-with-ui.sh: %v\n%s", err, out)
	}
}

func uiFiles(t *testing.T, repo, rev string) []string {
	t.Helper()
	return strings.Split(uiGit(t, repo, "ls-tree", "-r", rev, "--name-only"), "\n")
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// assertTagHasDist checks the annotated tag, its commit message and the SPA files in its tree.
func assertTagHasDist(t *testing.T, repo string) {
	t.Helper()
	files := uiFiles(t, repo, uiTag)
	for _, want := range []string{"internal/web/dist/index.html", "internal/web/dist/assets/app.js"} {
		if !contains(files, want) {
			t.Errorf("tag %s lacks %s, has %q", uiTag, want, files)
		}
	}
	if got := uiGit(t, repo, "cat-file", "-t", uiTag); got != "tag" {
		t.Errorf("%s is a %q, want an annotated tag", uiTag, got)
	}
	if got, want := uiGit(t, repo, "log", "-1", "--format=%s", uiTag+"^{commit}"), "chore(release): "+uiTag+" web-UI"; got != want {
		t.Errorf("tag commit subject = %q, want %q", got, want)
	}
}

// A production change that fails these tests: ci/tag-with-ui.sh stops putting the built SPA in a release commit under
// internal/web/dist, or tags another commit.
func TestTagWithUI(t *testing.T) {
	t.Run("tag points to commit with dist", func(t *testing.T) {
		repo := uiRepo(t)
		uiMustRun(t, repo, uiSrc(t, true))
		assertTagHasDist(t, repo)
	})

	t.Run("parent has no dist", func(t *testing.T) {
		repo := uiRepo(t)
		uiMustRun(t, repo, uiSrc(t, true))
		for _, f := range uiFiles(t, repo, uiTag+"^") {
			if strings.HasPrefix(f, "internal/web/dist") {
				t.Errorf("the parent of the tag commit has %s", f)
			}
		}
	})

	t.Run("fails without index.html", func(t *testing.T) {
		repo := uiRepo(t)
		head := uiGit(t, repo, "rev-parse", "HEAD")
		if _, err := uiRun(t, repo, uiSrc(t, false)); err == nil {
			t.Error("the script succeeded on a source without index.html")
		}
		if tags := uiGit(t, repo, "tag", "--list"); tags != "" {
			t.Errorf("a tag was created: %q", tags)
		}
		if got := uiGit(t, repo, "rev-parse", "HEAD"); got != head {
			t.Errorf("HEAD moved from %s to %s", head, got)
		}
	})

	t.Run("second run after reset works", func(t *testing.T) {
		repo := uiRepo(t)
		src := uiSrc(t, true)
		uiMustRun(t, repo, src)
		uiGit(t, repo, "tag", "-d", uiTag)
		uiGit(t, repo, "reset", "-q", "--hard", "HEAD~1")
		uiMustRun(t, repo, src)
		assertTagHasDist(t, repo)
	})

	t.Run("ignored dist is forced", func(t *testing.T) {
		repo := uiRepo(t)
		uiWrite(t, filepath.Join(repo, ".gitignore"), "/internal/web/dist/*\n")
		uiGit(t, repo, "add", ".gitignore")
		uiGit(t, repo, "commit", "-q", "-m", "ignore dist")
		uiMustRun(t, repo, uiSrc(t, true))
		assertTagHasDist(t, repo)
	})

	t.Run("script is tracked executable", func(t *testing.T) {
		assertTrackedExecutable(t, "ci/tag-with-ui.sh")
	})

	t.Run("stale tracked dist file is dropped", func(t *testing.T) {
		repo := uiRepo(t)
		uiWrite(t, filepath.Join(repo, "internal", "web", "dist", "stale.js"), "old\n")
		uiGit(t, repo, "add", "-f", "internal/web/dist/stale.js")
		uiGit(t, repo, "commit", "-q", "-m", "earlier dist")
		uiMustRun(t, repo, uiSrc(t, true))
		if contains(uiFiles(t, repo, uiTag), "internal/web/dist/stale.js") {
			t.Error("the stale dist file is still in the tag")
		}
		assertTagHasDist(t, repo)
	})

	t.Run("prestaged unrelated file stays out of the tag commit", func(t *testing.T) {
		repo := uiRepo(t)
		uiWrite(t, filepath.Join(repo, "other.txt"), "unrelated\n")
		uiGit(t, repo, "add", "other.txt")
		uiMustRun(t, repo, uiSrc(t, true))
		if contains(uiFiles(t, repo, uiTag), "other.txt") {
			t.Error("the staged unrelated file landed in the tag commit")
		}
		assertTagHasDist(t, repo)
	})
}

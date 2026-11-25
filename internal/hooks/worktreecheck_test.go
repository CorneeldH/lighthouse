package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/hook"
	"github.com/CorneeldH/lighthouse/internal/stubbin"
)

func check(t *testing.T, filePath, cwd string) hook.Result {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"tool_input": map[string]any{"file_path": filePath}, "cwd": cwd})
	if err != nil {
		t.Fatal(err)
	}
	return WorktreeCheck(context.Background(), "", "", hook.Payload{Raw: raw})
}

// physical is dir with its symbolic links resolved: t.TempDir may sit behind one.
func physical(t *testing.T, dir string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWorktreeCheckWarnsOnTheMainCheckoutOnMain(t *testing.T) {
	dir := physical(t, repo(t, "main"))

	res := check(t, "src/nieuw.py", dir)

	want := "Worktree check: this writes into the main checkout while it is on 'main'.\n" +
		"\n" +
		"  " + dir + "/src/nieuw.py\n" +
		"\n" +
		"Convention: work that produces a commit starts in a worktree on its own\n" +
		"branch (git worktree add .claude/worktrees/<name> -b issue-<nr>-<slug>).\n" +
		"Exception: read-only work, or an explicit 'on main' from the user.\n" +
		"\n" +
		"Note: committing on 'main' is blocked (main-commit-gate). Move\n" +
		"this work now, not later."
	if res.Code != 0 || res.Stderr != "" || res.Output["systemMessage"] != want {
		t.Fatalf("got code %d, stderr %q, output %q; want 0 and systemMessage %q", res.Code, res.Stderr, res.Output, want)
	}
}

func TestWorktreeCheckIsSilentInALinkedWorktreeSubdirectory(t *testing.T) {
	// The worktree branch is the protected `main`: only the worktree detection can be silent.
	dir := repo(t, "trunk")
	wt := filepath.Join(dir, "wt")
	if out, err := exec.Command("git", "-C", dir, "worktree", "add", "-q", "-b", "main", wt).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	sub := filepath.Join(wt, "a", "b")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{wt + "/x.py", sub + "/missing/x.py"} {
		if res := check(t, target, sub); res.Code != 0 || res.Output != nil {
			t.Errorf("%s: got code %d, output %v; want silence", target, res.Code, res.Output)
		}
	}
}

func TestWorktreeCheckPayloadShapes(t *testing.T) {
	cases := []struct {
		raw  string
		want any
	}{
		{"[]", "Worktree check: COULD NOT CHECK — the payload is not valid JSON.\n" +
			"\n" +
			"Nothing was checked, so this is no sign that the write is in a worktree.\n" +
			"Convention: work that produces a commit starts in a worktree on its own branch\n" +
			"(git worktree add .claude/worktrees/<name> -b issue-<nr>-<slug>)."},
		{`{"tool_input": "x"}`, "Worktree check: COULD NOT CHECK — the payload is not valid JSON.\n" +
			"\n" +
			"Nothing was checked, so this is no sign that the write is in a worktree.\n" +
			"Convention: work that produces a commit starts in a worktree on its own branch\n" +
			"(git worktree add .claude/worktrees/<name> -b issue-<nr>-<slug>)."},
		{"null", nil},
		{"", nil},
		{`{"tool_input": {}}`, nil},
		{`{"tool_input": {"file_path": null}}`, nil},
	}
	for _, c := range cases {
		res := WorktreeCheck(context.Background(), "", "", hook.Payload{Raw: []byte(c.raw)})
		if res.Code != 0 || res.Output["systemMessage"] != c.want {
			t.Errorf("%q: got code %d, output %q; want 0 and systemMessage %v", c.raw, res.Code, res.Output, c.want)
		}
	}
}

func TestCanonMissing(t *testing.T) {
	root := physical(t, t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, "real", "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		"link":     filepath.Join(root, "real"),
		"rel":      "real/sub",
		"dangling": "nowhere/deeper",
	} {
		if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct{ in, want string }{
		{root + "/real/sub/x", root + "/real/sub/x"},
		{root + "/link/sub/x", root + "/real/sub/x"},
		{root + "/rel/x", root + "/real/sub/x"},
		{root + "/missing/a/b", root + "/missing/a/b"},
		{root + "/missing/../link/x", root + "/real/x"},
		{root + "/link/../x", root + "/x"},
		{root + "/dangling/x", root + "/nowhere/deeper/x"},
		{root + "//real/./sub/", root + "/real/sub"},
		{"/", "/"},
	}
	for _, c := range cases {
		got, err := canonMissing(c.in)
		if err != nil || got != c.want {
			t.Errorf("canonMissing(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

// The want values are what GNU coreutils 9.7 `realpath -m` prints for the same tree.
func TestCanonMissingKeepsALoopingLinkAsGNUDoes(t *testing.T) {
	root := physical(t, t.TempDir())
	links := map[string]string{
		"a": "b", "b": "a", // a cycle of two
		"c": "a/sub",                 // into the cycle
		"x": "y", "y": "z", "z": "x", // a cycle of three
		"c0": "c1", "c1": "c2", "c2": "c3", "c3": "c4", "c4": "c0", // a cycle of five
		"self": "self",
	}
	for i := 0; i < 60; i++ { // a chain longer than any link limit, without a loop
		links[fmt.Sprintf("l%d", i)] = fmt.Sprintf("l%d", i+1)
	}
	for link, target := range links {
		if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct{ in, want string }{
		{"/a/x", "/a/x"},
		{"/b/x", "/b/x"},
		{"/c/z", "/b/sub/z"},
		{"/x/q", "/z/q"},
		{"/c0/q", "/c0/q"},
		{"/self/y", "/self/y"},
		{"/a/../q", "/q"},
		{"/l0/x", "/l60/x"},
	}
	for _, c := range cases {
		got, err := canonMissing(root + c.in)
		if err != nil || got != root+c.want {
			t.Errorf("canonMissing(%q) = %q, %v; want %q", c.in, got, err, root+c.want)
		}
	}
}

// worktreeUncheckable is the systemMessage of a check that could not be made.
func worktreeUncheckable(reason string) string {
	return "Worktree check: COULD NOT CHECK — " + reason + "\n" +
		"\n" +
		"Nothing was checked, so this is no sign that the write is in a worktree.\n" +
		"Convention: work that produces a commit starts in a worktree on its own branch\n" +
		"(git worktree add .claude/worktrees/<name> -b issue-<nr>-<slug>)."
}

// mainWarning is the systemMessage of a write to file in the main checkout on branch.
func mainWarning(branch, file string) string {
	return "Worktree check: this writes into the main checkout while it is on '" + branch + "'.\n" +
		"\n" +
		"  " + file + "\n" +
		"\n" +
		"Convention: work that produces a commit starts in a worktree on its own\n" +
		"branch (git worktree add .claude/worktrees/<name> -b issue-<nr>-<slug>).\n" +
		"Exception: read-only work, or an explicit 'on main' from the user.\n" +
		"\n" +
		"Note: committing on '" + branch + "' is blocked (main-commit-gate). Move\n" +
		"this work now, not later."
}

// wantSilence fails unless res is no output at all.
func wantSilence(t *testing.T, res hook.Result) {
	t.Helper()
	if res.Code != 0 || res.Stderr != "" || res.Output != nil {
		t.Errorf("got %#v, want silence", res)
	}
}

// wantMessage fails unless res is exit 0 with exactly this systemMessage.
func wantMessage(t *testing.T, res hook.Result, want string) {
	t.Helper()
	if res.Code != 0 || res.Stderr != "" || res.Output["systemMessage"] != want || len(res.Output) != 1 {
		t.Errorf("got %#v\nwant systemMessage %q", res, want)
	}
}

// shimGit puts a git on PATH, and only that, that runs script (a sh script).
func shimGit(t *testing.T, script string) {
	t.Helper()
	stubbin.RequirePOSIX(t, "the shim git is a sh script (locale branching, exec of the real git)")
	bin := t.TempDir()
	stubbin.MakeScript(t, bin, "git", script)
	stubbin.Only(t, bin)
}

func TestWorktreeCheckIsSilentOnAFeatureBranchInTheMainCheckout(t *testing.T) {
	dir := physical(t, repo(t, "main"))
	gitIn(t, dir, "checkout", "-q", "-b", "fix-iets")

	wantSilence(t, check(t, dir+"/README.md", dir))
}

func TestWorktreeCheckWarnsWhenTheCwdIsAWorktreeButTheTargetIsTheMainCheckout(t *testing.T) {
	dir := physical(t, repo(t, "main"))
	wt := filepath.Join(dir, ".claude", "worktrees", "issue-1-iets")
	gitIn(t, dir, "worktree", "add", "-q", "-b", "issue-1-iets", wt)

	wantSilence(t, check(t, wt+"/src/nieuw.py", wt))
	wantMessage(t, check(t, dir+"/README.md", wt), mainWarning("main", dir+"/README.md"))
}

func TestWorktreeCheckIsSilentOutsideAGitRepository(t *testing.T) {
	repo(t, "main") // the config of the test
	outside := physical(t, t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(outside))

	wantSilence(t, check(t, outside+"/x.txt", outside))
}

// git speaks its own language outside the repo: the hook asks for the C locale to recognise the
// message. The shim answers in German unless it is asked for C.
func TestWorktreeCheckIsSilentOutsideAGitRepositoryUnderAnotherLocale(t *testing.T) {
	outside := physical(t, t.TempDir())
	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "de_DE.UTF-8")
	shimGit(t, `if [ "$LC_ALL" = C ]; then
  echo "fatal: not a git repository (or any of the parent directories): .git"
else
  echo "fatal: kein Git-Repository (oder eines der Elternverzeichnisse): .git"
fi >&2
exit 128
`)

	wantSilence(t, check(t, outside+"/x.txt", outside))
}

func TestWorktreeCheckWithoutGit(t *testing.T) {
	dir := physical(t, repo(t, "main"))
	emptyPath(t)

	wantMessage(t, check(t, dir+"/x.py", dir),
		"Worktree check: COULD NOT CHECK — missing tooling: git. "+
			"Nothing was checked, so this is no sign that the write is in a worktree.")
}

func TestWorktreeCheckSpeaksWhenGitBreaks(t *testing.T) {
	dir := physical(t, repo(t, "main"))
	shimGit(t, "echo 'fatal: broken' >&2\nexit 128\n")

	wantMessage(t, check(t, dir+"/src/nieuw.py", dir),
		worktreeUncheckable("git could not read '"+dir+"': fatal: broken."))
}

func TestWorktreeCheckSpeaksWhenTheBranchCannotBeRead(t *testing.T) {
	dir := physical(t, repo(t, "main"))
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	shimGit(t, "case \"$*\" in\n  branch*) echo 'fatal: broken' >&2; exit 128;;\nesac\nexec "+real+" \"$@\"\n")

	wantMessage(t, check(t, dir+"/src/nieuw.py", dir),
		worktreeUncheckable("git could not determine the branch of '"+dir+"': fatal: broken."))
}

func TestWorktreeCheckFollowsTheDefaultBranch(t *testing.T) {
	trunk := physical(t, repo(t, "trunk"))
	onMain := physical(t, repo(t, "main"))
	toml := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(toml, []byte("[forge]\ndefault_branch = \"trunk\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", toml)

	wantMessage(t, check(t, trunk+"/src/nieuw.py", trunk), mainWarning("trunk", trunk+"/src/nieuw.py"))
	wantSilence(t, check(t, onMain+"/src/nieuw.py", onMain))
}

func TestWorktreeCheckSpeaksWhenTheConfigIsBroken(t *testing.T) {
	dir := physical(t, repo(t, "main"))
	toml := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(toml, []byte("[forge\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", toml)

	res := check(t, dir+"/src/nieuw.py", dir)

	// Only our own prefix is pinned: the tail is the TOML parser's wording, which is not ours.
	head := "Worktree check: COULD NOT CHECK — the protected branch could not be read: config: " + toml +
		": unreadable or not valid TOML: "
	msg, _ := res.Output["systemMessage"].(string)
	if res.Code != 0 || res.Stderr != "" || len(res.Output) != 1 || !strings.HasPrefix(msg, head) {
		t.Errorf("got %#v\nwant a systemMessage starting with %q", res, head)
	}
}

// outsideWorktree is a linked worktree of dir in a folder outside the repo, with a space in its path.
func outsideWorktree(t *testing.T, dir string) string {
	t.Helper()
	wt := filepath.Join(physical(t, t.TempDir()), "my worktrees", "issue-1-x")
	if out, err := exec.Command("git", "-C", dir, "worktree", "add", "-q", "-b", "issue-1-x", wt).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	return wt
}

// Detection is git's, not a path's: this stays green, and is red only when worktree-check starts
// to look for `.claude/worktrees` in the path of the write.
func TestWorktreeCheckIsSilentInAWorktreeOutsideTheRepo(t *testing.T) {
	dir := repo(t, "main")
	wt := outsideWorktree(t, dir)

	if res := check(t, filepath.Join(wt, "x.go"), dir); res.Output != nil || res.Code != 0 {
		t.Fatalf("a write in a worktree outside the repo warned: %+v", res)
	}
	if res := check(t, filepath.Join(physical(t, dir), "x.go"), wt); res.Output == nil {
		t.Fatal("a write in the main checkout on main did not warn")
	}
}

// Red when the message goes back to the fixed `.claude/worktrees/<name>`.
func TestWorktreeCheckNamesTheConfiguredRoot(t *testing.T) {
	dir := repo(t, "main")
	setConfig(t, "[project]\nlanguage = \"en\"\n[worktree]\nroot = \"/data/my wt\"\n")

	msg, _ := check(t, "x.go", dir).Output["systemMessage"].(string)

	if !strings.Contains(msg, "git worktree add '/data/my wt'/<name> -b") || strings.Contains(msg, ".claude/worktrees") {
		t.Fatalf("the message does not name the configured root: %q", msg)
	}
}

// Red when isAbsPath does not exist, or counts only a leading `/`.
func TestIsAbsPath(t *testing.T) {
	for p, want := range map[string]bool{
		`C:\x`: true, "C:/x": true, "/x": true, `c:\Users\A B\x.go`: true,
		"x": false, "./x": false, "C:x": false, "": false, `:\x`: false,
	} {
		if got := isAbsPath(p); got != want {
			t.Errorf("isAbsPath(%q) = %v, want %v", p, got, want)
		}
	}
}

// Red when canonMissing walks the volume as a component (`C:/C:/...`) or starts from a plain "/".
func TestCanonMissingKeepsTheVolume(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a drive letter is a volume only on Windows")
	}
	got, err := canonMissing(`C:\Users\A B\wt\x.go`)
	if err != nil {
		t.Fatal(err)
	}
	if want := `C:\Users\A B\wt\x.go`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

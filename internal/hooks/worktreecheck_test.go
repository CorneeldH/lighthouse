package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/hook"
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
		"Note: committing on 'main' is blocked (main-commit-gate.sh). Move\n" +
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

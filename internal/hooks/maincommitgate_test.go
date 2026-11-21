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

// repo makes a git repository on branch with one commit, and points the config at a
// lighthouse.toml without a default branch.
func repo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	toml := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(toml, []byte("[project]\nlanguage = \"en\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", toml)
	// A controller run sets these; the gate and the config must not read them from the host.
	t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", "")
	t.Setenv("LIGHTHOUSE_REPO_ROOT", "")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	for _, args := range [][]string{
		{"init", "-q", "-b", branch},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func gate(t *testing.T, command, cwd string) hook.Result {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"tool_input": map[string]any{"command": command}, "cwd": cwd})
	if err != nil {
		t.Fatal(err)
	}
	return gateRaw(raw)
}

func gateRaw(raw []byte) hook.Result {
	return MainCommitGate(context.Background(), "", "", hook.Payload{Raw: raw})
}

// setConfig points the config at a lighthouse.toml with this text.
func setConfig(t *testing.T, text string) {
	t.Helper()
	toml := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(toml, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", toml)
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// realPath is dir as git reports it: without symlinks.
func realPath(t *testing.T, dir string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// allowMainIn sets the escape hatch of the repository at dir.
func allowMainIn(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude", "allow-main"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

// checkResult fails t unless res has code and a stderr that starts with prefix; an empty prefix
// means a silent stderr.
func checkResult(t *testing.T, res hook.Result, code int, prefix string) {
	t.Helper()
	if res.Code != code || (prefix == "" && res.Stderr != "") || !strings.HasPrefix(res.Stderr, prefix) {
		t.Errorf("got code %d, stderr %q; want %d and a stderr starting with %q", res.Code, res.Stderr, code, prefix)
	}
}

func TestMainCommitGateBlocksACommitOnMain(t *testing.T) {
	dir := repo(t, "main")
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	res := gate(t, "git commit -m x", dir)

	want := "[main-commit-gate/commit-to-main] Main-commit gate: blocked — '" + root + "' is on branch 'main' (commit).\n"
	if res.Code != 2 || !strings.HasPrefix(res.Stderr, want) {
		t.Fatalf("got code %d, stderr %q; want 2 and a stderr starting with %q", res.Code, res.Stderr, want)
	}
	if res.Output != nil {
		t.Errorf("output %v; a block writes nothing to stdout", res.Output)
	}
}

func TestMainCommitGateAllowsACommitOnAFeatureBranch(t *testing.T) {
	dir := repo(t, "issue-1-x")

	res := gate(t, "git commit -m x", dir)

	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("got code %d, stderr %q; want a silent pass", res.Code, res.Stderr)
	}
}

func TestMainCommitGateUnparseableBlocksOnlyWhenACommitMayBeInIt(t *testing.T) {
	dir := repo(t, "issue-1-x")
	cases := []struct {
		command string
		code    int
		stderr  string
	}{
		{"echo 'open && git commit -m x", 2, gateUnparseable},
		{"echo 'open && git push", 2, gateUnparseable},
		{"echo 'we push this", 0, ""},
		{"git status && echo 'open", 0, ""},
	}
	for _, c := range cases {
		t.Run(c.command, func(t *testing.T) {
			res := gate(t, c.command, dir)
			if res.Code != c.code || res.Stderr != c.stderr {
				t.Errorf("got code %d, stderr %q; want %d, %q", res.Code, res.Stderr, c.code, c.stderr)
			}
		})
	}
}

func TestMainCommitGateWithoutGitCouldNotCheck(t *testing.T) {
	dir := repo(t, "main")
	t.Setenv("PATH", t.TempDir())

	res := gate(t, "git commit -m x", dir)

	want := "Main-commit gate: COULD NOT CHECK — missing tooling: git\n" +
		"\n" +
		"This hook makes sure nothing lands directly on the default branch.\n" +
		"Without that tooling nothing was checked; that is not a green light.\n"
	if res.Code != 2 || res.Stderr != want {
		t.Fatalf("got code %d, stderr %q; want 2, %q", res.Code, res.Stderr, want)
	}
}

func TestReadGatePayload(t *testing.T) {
	cases := []struct {
		raw, command, cwd string
		ok                bool
	}{
		{`{"tool_input": {"command": "git commit"}, "cwd": "/r"}`, "git commit", "/r", true},
		{`{"tool_input": {"command": 5}}`, "5", "", true},
		{`{"tool_input": {"command": false}, "cwd": null}`, "", "", true},
		{`{"tool_input": null}`, "", "", true},
		{`null`, "", "", true},
		{` `, "", "", true},
		{`[]`, "", "", false},
		{`{"tool_input": "git commit"}`, "", "", false},
		{`not json`, "", "", false},
		// jq indexes by exact key; a key in another case is another field.
		{`{"tool_input": {"command": "git commit -m x"}, "Tool_Input": null}`, "git commit -m x", "", true},
		{`{"tool_input": {"command": "git commit -m x", "Command": null}}`, "git commit -m x", "", true},
		{`{"cwd": "/r", "CWD": "/other"}`, "", "/r", true},
		// A duplicate key: the last one wins, as in jq.
		{`{"tool_input": {"command": "ls", "command": "git push"}}`, "git push", "", true},
		{`{"tool_input": false}`, "", "", false},
	}
	for _, c := range cases {
		command, cwd, ok := readGatePayload([]byte(c.raw))
		if command != c.command || cwd != c.cwd || ok != c.ok {
			t.Errorf("readGatePayload(%s) = %q, %q, %v; want %q, %q, %v", c.raw, command, cwd, ok, c.command, c.cwd, c.ok)
		}
	}
}

func TestMainCommitGateEscapeHatch(t *testing.T) {
	cases := []struct {
		name, branch, command string
		hatch                 bool
		code                  int
		stderr                string
	}{
		{"commit on main without the hatch", "main", "git commit -m x", false, 2,
			"[main-commit-gate/commit-to-main] Main-commit gate: blocked — 'ROOT' is on branch 'main' (commit).\n"},
		{"commit on main with the hatch", "main", "git commit -m x", true, 0, ""},
		{"unresolvable -C path without the hatch", "issue-1-x", `git -C "$HOOFDBOOM" commit -m x`, false, 2,
			"Main-commit gate: COULD NOT CHECK — the path '$HOOFDBOOM' cannot be resolved.\n"},
		{"unresolvable -C path with the hatch", "main", `git -C "$HOOFDBOOM" commit -m x`, true, 0, ""},
		{"unresolvable push destination without the hatch", "issue-1-x", "git push origin $BR", false, 2,
			"Main-commit gate: COULD NOT CHECK — the destination of this push cannot be resolved.\n"},
		{"unresolvable push destination with the hatch", "issue-1-x", "git push origin $BR", true, 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := repo(t, c.branch)
			if c.hatch {
				allowMainIn(t, dir)
			}

			res := gate(t, c.command, dir)

			checkResult(t, res, c.code, strings.ReplaceAll(c.stderr, "ROOT", realPath(t, dir)))
		})
	}
}

func TestMainCommitGateProtectedBranch(t *testing.T) {
	const trunk = "[project]\nlanguage = \"en\"\n\n[forge]\ndefault_branch = \"trunk\"\n"
	cases := []struct {
		name, config, branch, extraBranch, command string
		code                                       int
		stderr                                     string
	}{
		{"commit on the configured default branch", trunk, "trunk", "", "git commit -m x", 2,
			"[main-commit-gate/commit-to-main] Main-commit gate: blocked — 'ROOT' is on branch 'trunk' (commit).\n"},
		{"push to the configured default branch", trunk, "issue-1-x", "", "git push origin HEAD:trunk", 2,
			"[main-commit-gate/push-to-main] Main-commit gate: blocked — this push explicitly targets 'trunk' (push from branch 'issue-1-x').\n"},
		{"commit on main when the default is trunk", trunk, "main", "", "git commit -m x", 0, ""},
		{"push to main when the default is trunk", trunk, "issue-1-x", "", "git push origin HEAD:main", 0, ""},
		{"push --all with a local default branch trunk", trunk, "issue-1-x", "trunk", "git push --all origin", 2,
			"[main-commit-gate/push-to-main] Main-commit gate: blocked — this push explicitly targets 'trunk' (push from branch 'issue-1-x').\n"},
		{"push to main from a feature branch", "", "issue-1-x", "", "git push origin HEAD:main", 2,
			"[main-commit-gate/push-to-main] Main-commit gate: blocked — this push explicitly targets 'main' (push from branch 'issue-1-x').\n"},
		{"push --all with a local main", "", "issue-1-x", "main", "git push --all origin", 2,
			"[main-commit-gate/push-to-main] Main-commit gate: blocked — this push explicitly targets 'main' (push from branch 'issue-1-x').\n"},
		{"push --mirror with a local main", "", "issue-1-x", "main", "git push --mirror origin", 2,
			"[main-commit-gate/push-to-main] Main-commit gate: blocked — this push explicitly targets 'main' (push from branch 'issue-1-x').\n"},
		{"push --all without a local main", "", "issue-1-x", "", "git push --all origin", 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := repo(t, c.branch)
			if c.config != "" {
				setConfig(t, c.config)
			}
			if c.extraBranch != "" {
				gitIn(t, dir, "branch", c.extraBranch)
			}

			res := gate(t, c.command, dir)

			checkResult(t, res, c.code, strings.ReplaceAll(c.stderr, "ROOT", realPath(t, dir)))
		})
	}
}

func TestMainCommitGateFromAWorktree(t *testing.T) {
	cases := []struct {
		name, command string
		code          int
		stderr        string
	}{
		{"commit through -C into the main checkout", "git -C ROOT commit -m x", 2,
			"[main-commit-gate/commit-to-main] Main-commit gate: blocked — 'ROOT' is on branch 'main' (commit).\n"},
		{"commit after a cd into the main checkout", "cd ROOT && git commit -m x", 2,
			"[main-commit-gate/commit-to-main] Main-commit gate: blocked — 'ROOT' is on branch 'main' (commit).\n"},
		{"commit in the worktree itself", "git commit -m x", 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := repo(t, "main")
			root := realPath(t, dir)
			worktree := filepath.Join(root, ".claude", "worktrees", "issue-1-x")
			gitIn(t, root, "worktree", "add", "-q", "-b", "issue-1-x", worktree)

			res := gate(t, strings.ReplaceAll(c.command, "ROOT", root), worktree)

			checkResult(t, res, c.code, strings.ReplaceAll(c.stderr, "ROOT", root))
		})
	}
}

func TestMainCommitGateBrokenConfigBlocksOnlyACommit(t *testing.T) {
	cases := []struct {
		name, command string
		hatch         bool
		code          int
		stderr        string
	}{
		{"a commit", "git commit -m x", false, 2,
			"Main-commit gate: COULD NOT CHECK — the protected branch could not be read.\n"},
		{"a plain command", "ls -la", false, 0, ""},
		{"a commit with the hatch", "git commit -m x", true, 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := repo(t, "issue-1-x")
			setConfig(t, "[forge\n")
			if c.hatch {
				allowMainIn(t, dir)
			}

			res := gate(t, c.command, dir)

			checkResult(t, res, c.code, c.stderr)
		})
	}
}

func TestMainCommitGateUnreadablePayloadBlocks(t *testing.T) {
	for name, payload := range map[string]string{"an array has no tool_input": `[]`, "not json": `dit is geen json`} {
		t.Run(name, func(t *testing.T) {
			res := gateRaw([]byte(payload))

			checkResult(t, res, 2, gateInvalidJSON)
		})
	}
}

// stubGit puts a `git` that runs script first on the PATH.
func stubGit(t *testing.T, script string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\n"+script), 0o755); err != nil { //nolint:gosec // an executable stub
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestMainCommitGateTargetWithoutARepository(t *testing.T) {
	// Git speaks the language of the locale: the gate must ask for the C locale, or a German
	// 'kein Git-Repository' would read as a git that breaks. The stub speaks German unless it is
	// asked for LC_ALL=C.
	const localeStub = `if [ "$LC_ALL" = C ]; then echo 'fatal: not a git repository' >&2; else echo 'fatal: kein Git-Repository' >&2; fi
exit 128
`
	cases := []struct {
		name, stub string
		code       int
		stderr     string
	}{
		{"no repository at all", "", 0, ""},
		{"git answers in another language", localeStub, 0, ""},
		{"git breaks down", "echo 'fatal: kapot object store' >&2\nexit 1\n", 2,
			"Main-commit gate: COULD NOT CHECK — git could not read 'DIR': fatal: kapot object store.\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := repo(t, "main")
			target := t.TempDir()
			if c.stub != "" {
				stubGit(t, c.stub)
			}

			res := gate(t, "git -C "+target+" commit -m x", dir)

			checkResult(t, res, c.code, strings.ReplaceAll(c.stderr, "DIR", target))
		})
	}
}

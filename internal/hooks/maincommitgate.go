package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/gitinvocations"
	"github.com/CorneeldH/lighthouse/internal/hook"
)

// The messages of main-commit-gate, word for word those of the bash hook it replaces.
const (
	gateMissingGit = "Main-commit gate: COULD NOT CHECK — missing tooling: git\n" +
		"\n" +
		"This hook makes sure nothing lands directly on the default branch.\n" +
		"Without that tooling nothing was checked; that is not a green light.\n"
	gateInvalidJSON = "Main-commit gate: COULD NOT CHECK — the payload is not valid JSON.\n" +
		"\n" +
		"Without the command nothing was checked; that is not a green light.\n"
	gateUnparseable = "Main-commit gate: COULD NOT CHECK — the command cannot be parsed\n" +
		"(open quote), so no commit check was done.\n" +
		"\n" +
		"That is not a green light: 'no commit found' and 'could not look'\n" +
		"must not both pass. Split the command, or — deliberately and once —\n" +
		"'touch <repo-root>/.claude/allow-main'.\n"
)

// notARepo are the git messages that mean "this target is no repository": a real outcome that
// may pass, unlike a git that breaks.
var notARepo = []string{"not a git repository", "Not a git repository", "No such file or directory", "cannot change to"}

// MainCommitGate blocks `git commit` and `git push` on the default branch: `forge.default_branch`,
// `main` when it is not set. It reads `git -C <path>` and a preceding `cd`. Escape hatch:
// `.claude/allow-main` in the repo root. Not fail-open: a check it cannot make blocks with
// COULD NOT CHECK, except for an unparseable command that cannot hold a commit or push at all.
func MainCommitGate(ctx context.Context, _, _ string, p hook.Payload) hook.Result {
	if _, err := exec.LookPath("git"); err != nil {
		return blocked(gateMissingGit)
	}
	command, cwd, ok := readGatePayload(p.Raw)
	if !ok {
		return blocked(gateInvalidJSON)
	}
	if command == "" {
		return hook.Result{}
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// Only read when the text can hold a commit or push at all: a broken config must not block
	// `ls`. A config that cannot be read blocks the commit — guarding 'main' while the repo
	// protects another branch is no check.
	protected := "main"
	if mayCommit(command) {
		branch, errText, ok := defaultBranch()
		if !ok {
			if allowMain(repoRoot(ctx, cwd)) {
				return hook.Result{}
			}
			return blocked("Main-commit gate: COULD NOT CHECK — the protected branch could not be read.\n\n" +
				errText + "\n" +
				"Fix the cause above, or — deliberately and once —\n" +
				"'touch <repo-root>/.claude/allow-main'.\n")
		}
		protected = branch
	}

	found, err := gitinvocations.Find(command, cwd, gitinvocations.Options{DefaultBranch: protected})
	if errors.Is(err, gitinvocations.ErrUnparseable) {
		// 'Cannot parse' is not 'cannot look at a commit': without `git` and `commit`/`push` in
		// the raw text there is nothing to protect, and one stray quote must not lock up every
		// following command.
		if !mayCommit(command) {
			return hook.Result{}
		}
		return blocked(gateUnparseable)
	}
	if err != nil {
		return blocked("Main-commit gate: COULD NOT CHECK — the command parser failed.\n\n" + err.Error() + "\n")
	}
	if len(found) == 0 {
		return hook.Result{}
	}

	// Repo root of the cwd: only the escape hatch for a target that cannot be checked.
	cwdRoot := repoRoot(ctx, cwd)
	for _, inv := range found {
		if res, stop := checkInvocation(ctx, inv, protected, cwdRoot); stop {
			return res
		}
	}
	return hook.Result{}
}

// checkInvocation judges one commit or push; stop is true when it blocks.
func checkInvocation(ctx context.Context, inv gitinvocations.Invocation, protected, cwdRoot string) (hook.Result, bool) {
	if inv.Target == gitinvocations.Unresolved {
		if allowMain(cwdRoot) {
			return hook.Result{}, false
		}
		return uncheckable("the path '"+inv.Raw+"' cannot be resolved.", inv.Action), true
	}

	root, gitMessage, err := runGit(ctx, inv.Target, true, "rev-parse", "--show-toplevel")
	if err != nil {
		root = ""
	}
	if root == "" {
		// '<target> is not a repo' is a real outcome and may pass; a git that breaks is not.
		if containsAny(gitMessage, notARepo) || allowMain(cwdRoot) {
			return hook.Result{}, false
		}
		return uncheckable("git could not read '"+inv.Target+"': "+orNoMessage(gitMessage)+".", inv.Action), true
	}
	if allowMain(root) {
		return hook.Result{}, false
	}

	branch, gitMessage, err := runGit(ctx, root, false, "branch", "--show-current")
	if err != nil {
		return uncheckable("git could not determine the branch of '"+root+"': "+orNoMessage(gitMessage)+".", inv.Action), true
	}

	toMain := inv.ToMain
	if toMain == "all" {
		// `--all` / `--mirror` pushes every local branch: only relevant with a local default branch.
		toMain = "no"
		if _, _, err := runGit(ctx, root, false, "show-ref", "--verify", "--quiet", "refs/heads/"+protected); err == nil {
			toMain = "yes"
		}
	}
	// A destination that cannot be resolved blocks, unless the repo itself is on the default
	// branch: the message below is clearer then.
	if toMain == "unresolved" && branch != protected {
		return uncheckable("the destination of this push cannot be resolved.", inv.Action), true
	}
	if branch != protected && toMain != "yes" {
		return hook.Result{}, false
	}

	reason := "'" + root + "' is on branch '" + protected + "' (" + inv.Action + ")"
	if toMain == "yes" && branch != protected {
		reason = "this push explicitly targets '" + protected + "' (" + inv.Action + " from branch '" + branch + "')"
	}
	return blocked("Main-commit gate: blocked — " + reason + ".\n" +
		"\n" +
		"This repo has no branch protection on GitHub (free private repo),\n" +
		"so the line is drawn here. Nothing lands directly on '" + protected + "'.\n" +
		"\n" +
		"What you can do:\n" +
		"  1. Work in a worktree on its own branch:\n" +
		"     git worktree add .claude/worktrees/<name> -b issue-<nr>-<slug>\n" +
		"  2. Is the work already in the main checkout? Move it:\n" +
		"     git -C '" + root + "' stash push -u -m move-<tag>\n" +
		"  3. If it really has to go on '" + protected + "', deliberately and once:\n" +
		"     touch '" + root + "/.claude/allow-main'   # and remove it afterwards\n"), true
}

// readGatePayload reads `.tool_input.command` and `.cwd` the way `jq -r '.x // empty'` does: null
// and false are empty, a string is its text, any other value its JSON text. ok is false when the
// payload has no such fields to read (not JSON, or not an object where one is needed); an empty
// payload gives jq nothing to read and so an empty command.
func readGatePayload(raw []byte) (command, cwd string, ok bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", "", true
	}
	var in struct {
		ToolInput *struct {
			Command json.RawMessage `json:"command"`
		} `json:"tool_input"`
		Cwd json.RawMessage `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", "", false
	}
	if in.ToolInput != nil {
		command = jqText(in.ToolInput.Command)
	}
	return command, jqText(in.Cwd), true
}

// jqText is the output of `jq -r '.x // empty'` for one value, without the trailing newline.
func jqText(v json.RawMessage) string {
	text := string(bytes.TrimSpace(v))
	switch text {
	case "", "null", "false":
		return ""
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	return text
}

// mayCommit reports whether the raw text holds `git` together with `commit` or `push`.
func mayCommit(command string) bool {
	return strings.Contains(command, "git") &&
		(strings.Contains(command, "commit") || strings.Contains(command, "push"))
}

// repoRoot is the top level of the repository at dir, or "" when there is none.
func repoRoot(ctx context.Context, dir string) string {
	root, _, err := runGit(ctx, dir, false, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return root
}

// allowMain reports whether the escape hatch is set in the repo at root.
func allowMain(root string) bool {
	if root == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(root, ".claude", "allow-main"))
	return err == nil
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func orNoMessage(s string) string {
	if s == "" {
		return "no message"
	}
	return s
}

// uncheckable blocks an invocation the gate could not look at.
func uncheckable(reason, action string) hook.Result {
	return blocked("Main-commit gate: COULD NOT CHECK — " + reason + "\n" +
		"\n" +
		"This command does contain a '" + action + "'. 'Could not look' is not a\n" +
		"negative result, so this does not pass silently.\n" +
		"\n" +
		"What you can do:\n" +
		"  1. Spell the path out, without a variable or substitution.\n" +
		"  2. If it really has to be this way, deliberately and once:\n" +
		"     touch <repo-root>/.claude/allow-main   # and remove it afterwards\n")
}

func blocked(stderr string) hook.Result {
	return hook.Result{Code: 2, Stderr: stderr}
}

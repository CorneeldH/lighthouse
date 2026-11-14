package commitgate

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/gitinvocations"
	"github.com/CorneeldH/lighthouse/internal/hook"
	"github.com/CorneeldH/lighthouse/internal/repoidentity"
)

// noVerifyBlock is the refusal of --no-verify.
const noVerifyBlock = HookName + ": --no-verify is not allowed in this repo.\n" +
	"\n" +
	"The test gate is a Claude Code PreToolUse hook, not a git hook, so the\n" +
	"flag skips nothing today — but it will as soon as git hooks are added.\n" +
	"If something fails: fix it. If it really has to be skipped, ask.\n"

// notARepo are the git messages that mean "this target is no repository": a real outcome with
// nothing to test, unlike a git that breaks.
var notARepo = []string{"not a git repository", "Not a git repository", "No such file or directory", "cannot change to"}

// gitRun is hook.RunGit with the output as `$(…)` leaves it.
func gitRun(ctx context.Context, dir string, cLocale bool, args ...string) (stdout, stderr string, err error) {
	out, errOut, err := hook.RunGit(ctx, dir, cLocale, args...)
	return captured(out), captured(errOut), err
}

func orNoMessage(s string) string {
	if s == "" {
		return "no message"
	}
	return s
}

// Pre is the gate before a Bash call: it blocks --no-verify, tests a commit to the default branch
// before it lands, and records the HEAD of every other root of this project for Post. cwd is the
// directory the command runs in.
func Pre(ctx context.Context, g *Gate, raw []byte, cwd string) Result {
	command, _, ok := ReadPayload(raw)
	if !ok {
		return Result{Code: 2, Stderr: InvalidPayload}
	}
	if command == "" {
		return Result{}
	}
	found, err := gitinvocations.Find(command, cwd, gitinvocations.Options{ReportNewDirs: true})
	if errors.Is(err, gitinvocations.ErrUnparseable) {
		// The guard's two-step rule, here on `git` and `commit`: without them in the raw text
		// there is nothing to test, and one stray apostrophe locks nothing up.
		if !mayCommit(command) {
			return Result{}
		}
		return g.Block("the command cannot be parsed (open quote). Split the command.")
	}
	if err != nil {
		return Result{Code: 2, Stderr: HookName + ": COULD NOT CHECK — the command parser failed.\n\n" + err.Error() + "\n"}
	}
	var commits []gitinvocations.Invocation
	for _, inv := range found {
		if inv.Action == "commit" {
			commits = append(commits, inv)
		}
	}
	if len(commits) == 0 {
		return Result{}
	}
	// no_verify.py walked the command from its own working directory, not from cwd.
	here, _ := os.Getwd()
	if CommandHasNoVerify(command, here) {
		return Result{Code: 2, Stderr: noVerifyBlock}
	}

	roots, block := g.commitRoots(ctx, commits)
	if block != nil {
		return *block
	}
	if len(roots) == 0 {
		return Result{}
	}
	project, block := g.projectRoots(ctx, roots)
	if block != nil {
		return *block
	}
	// No project root (only another project): nothing to test or record, so no configuration,
	// no slot and no wait.
	if len(project) == 0 {
		return g.Emit("PreToolUse")
	}
	return g.preProject(ctx, raw, project)
}

// commitRoots are the repo roots of the commits, unique and in order. A target that is no
// repository has nothing to test; one that cannot be read blocks.
func (g *Gate) commitRoots(ctx context.Context, commits []gitinvocations.Invocation) ([]string, *Result) {
	var roots []string
	seen := map[string]bool{}
	for _, inv := range commits {
		var reason string
		switch inv.Target {
		case gitinvocations.Unresolved:
			reason = "the path '" + inv.Raw + "' cannot be resolved. Spell the path out."
		case gitinvocations.NewDir:
			reason = "'" + inv.Raw + "' does not exist yet. Commit in a separate call, once the directory exists."
		}
		if reason != "" {
			res := g.Block(reason)
			return nil, &res
		}
		// LC_ALL=C: git translates the text matched below.
		root, message, err := gitRun(ctx, inv.Target, true, "rev-parse", "--show-toplevel")
		if err != nil {
			root = ""
		}
		if root == "" {
			// '<target> is not a repo' is a real outcome: nothing to test. A git that breaks
			// (dubious ownership, broken installation) is not.
			if containsAny(message, notARepo) {
				continue
			}
			res := g.Block("git could not read '" + inv.Target + "': " + orNoMessage(message) + ".")
			return nil, &res
		}
		if !seen[root] {
			seen[root] = true
			roots = append(roots, root)
		}
	}
	return roots, nil
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// commonDir is `--git-common-dir` of dir, absolute; the same error contract for reference and
// target.
func (g *Gate) commonDir(ctx context.Context, dir string) (string, *Result) {
	out, message, err := gitRun(ctx, dir, false, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		out = ""
	}
	if out == "" {
		res := g.Block("git could not determine the common git dir of '" + dir + "': " + orNoMessage(message) + ".")
		return "", &res
	}
	return out, nil
}

// projectRoots are the roots of this project: the same `--git-common-dir` as CLAUDE_PROJECT_DIR
// (else the lighthouse root), or the same origin (a worktree of a separate clone, #36). Every
// other root is skipped with a note.
func (g *Gate) projectRoots(ctx context.Context, roots []string) ([]string, *Result) {
	ref := getenv(g.Env, "CLAUDE_PROJECT_DIR")
	if ref == "" {
		ref = g.Root
	}
	refCommon, block := g.commonDir(ctx, ref)
	if block != nil {
		return nil, block
	}
	var project []string
	for _, root := range roots {
		common, block := g.commonDir(ctx, root)
		if block != nil {
			return nil, block
		}
		if common == refCommon {
			project = append(project, root)
			continue
		}
		same, reason, err := repoidentity.SameOrigin(ctx, ref, root)
		switch {
		case err != nil:
			res := g.Block("git could not read the origin of '" + root + "': " + err.Error())
			return nil, &res
		case same:
			project = append(project, root)
		default:
			g.Note(HookName + ": " + root + " skipped — other project (git-common-dir " + common + " vs " + refCommon + "; " + reason + ")")
		}
	}
	return project, nil
}

// preProject splits the project roots by branch: a root on the default branch is tested before
// the commit, every other root gets its HEAD recorded for Post. A detached HEAD is not the
// default branch.
func (g *Gate) preProject(ctx context.Context, raw []byte, project []string) Result {
	branch, set, block := g.readConfig("forge.default_branch")
	if block != nil {
		return *block
	}
	if !set || branch == "" {
		return g.Block("the default branch cannot be read (forge.default_branch is not set).")
	}
	var mainRows []Row
	var others []string
	for _, root := range project {
		current, message, err := gitRun(ctx, root, false, "branch", "--show-current")
		if err != nil {
			return g.Block("git could not read the branch of '" + root + "': " + message)
		}
		if current != "" && current == branch {
			mainRows = append(mainRows, Row{Root: root})
		} else {
			others = append(others, root)
		}
	}
	// A background call returns before the commit lands, so Post would see nothing: no record,
	// but a visible notice instead of a silent pass.
	if len(others) > 0 {
		if payloadBackground(raw) {
			for _, root := range others {
				g.Note(HookName + ": " + root + " — commit in a background call is not tested; commit in the foreground.")
			}
		} else if err := Record(raw, others); err != nil {
			return g.Block("could not record HEAD: " + err.Error())
		}
	}
	// A commit to the default branch: test it before the commit, as before #80.
	if len(mainRows) > 0 {
		return g.RunRoots(mainRows, "PreToolUse")
	}
	return g.Emit("PreToolUse")
}

// payloadBackground is `jq -r '.tool_input.run_in_background // false'` = "true".
func payloadBackground(raw []byte) bool {
	top, _ := hook.JQFields(raw)
	toolInput, _ := hook.JQFields(top["tool_input"])
	return hook.JQText(toolInput["run_in_background"]) == "true"
}

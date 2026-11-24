package commitgate

import (
	"context"
	"errors"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/gitinvocations"
	"github.com/CorneeldH/lighthouse/internal/prbody"
)

// splitReason is the block for a commit and a PR create in one call: the hook runs before the
// call, so HEAD is still the old commit.
const splitReason = "split the commit and the PR create into separate Bash calls: the PR run needs the new commit as HEAD"

// mayCreate is the two-step rule on the raw text, as mayCommit is for a commit: a forge tool
// (`gh`, `glab` or `forge`) and `pr` or `mr` both in it.
func mayCreate(command string) bool {
	tool := strings.Contains(command, "gh") || strings.Contains(command, "glab") || strings.Contains(command, "forge")
	return tool && (strings.Contains(command, "pr") || strings.Contains(command, "mr"))
}

// Pr is the gate before a pull or merge request create, when the on setting says "pr" or "both":
// it tests the branch (merge-base with origin/<default branch> up to HEAD) of every root of this
// project that a create runs in. A command that creates nothing, or a project that tests at the
// commit only, passes untouched. A --repo or GH_REPO is not read: a create in this project runs
// the gate.
func Pr(ctx context.Context, g *Gate, command, cwd string) Result {
	g.Mode = "pr"
	if !mayCreate(command) {
		return Result{}
	}
	creates, err := prbody.FindPrCreates(command, cwd)
	if errors.Is(err, gitinvocations.ErrUnparseable) {
		on, blk := g.readOn()
		if blk != nil {
			return *blk
		}
		if on == "commit" {
			return Result{}
		}
		return g.Block("the command cannot be parsed (open quote). Split the command.")
	}
	if err != nil {
		return couldNotCheckText(HookName + ": COULD NOT CHECK — the command parser failed.\n\n" + err.Error() + "\n")
	}
	if len(creates) == 0 {
		return Result{}
	}
	on, blk := g.readOn()
	if blk != nil {
		return *blk
	}
	if on == "commit" {
		return Result{}
	}
	roots, blk := g.createRoots(ctx, creates, cwd)
	if blk != nil {
		return *blk
	}
	if len(roots) == 0 {
		return Result{}
	}
	project, blk := g.projectRoots(ctx, roots)
	if blk != nil {
		return *blk
	}
	if len(project) == 0 {
		return g.Emit("PreToolUse")
	}
	return g.runBranches(ctx, project)
}

// createRoots are the repo roots the creates run in, unique and in order. A directory that is no
// repository has nothing to test; one that cannot be read blocks.
func (g *Gate) createRoots(ctx context.Context, creates []prbody.PrCreate, cwd string) ([]string, *Result) {
	var roots []string
	seen := map[string]bool{}
	for _, pc := range creates {
		dir := cwd
		if pc.Cwd != nil {
			dir = *pc.Cwd
		}
		root, message, err := gitRun(ctx, dir, true, "rev-parse", "--show-toplevel")
		if err != nil {
			root = ""
		}
		if root == "" {
			if containsAny(message, notARepo) {
				continue
			}
			res := g.Block("git could not read '" + dir + "': " + orNoMessage(message) + ".")
			return nil, &res
		}
		if !seen[root] {
			seen[root] = true
			roots = append(roots, root)
		}
	}
	return roots, nil
}

// runBranches tests the branch of every project root: its commits above origin/<default branch>.
// A root without such a commit has nothing to test and gets a note.
func (g *Gate) runBranches(ctx context.Context, project []string) Result {
	branch, set, blk := g.readConfig("forge.default_branch")
	if blk != nil {
		return *blk
	}
	if !set || branch == "" {
		return g.Block("the default branch cannot be read (forge.default_branch is not set).")
	}
	var rows []Row
	for _, root := range project {
		base, message, err := gitRun(ctx, root, false, "merge-base", "origin/"+branch, "HEAD")
		if err != nil || base == "" {
			return g.Block("git could not find the base of the branch in '" + root + "' (merge-base origin/" + branch + " HEAD): " + orNoMessage(message) + ".")
		}
		head, message, err := gitRun(ctx, root, false, "rev-parse", "HEAD")
		if err != nil || head == "" {
			return g.Block("git could not read HEAD in '" + root + "': " + orNoMessage(message) + ".")
		}
		if base == head {
			g.Note(HookName + ": " + root + " — no commits above origin/" + branch + ", nothing was tested")
			continue
		}
		rows = append(rows, Row{Root: root, Base: base, Head: head})
	}
	if len(rows) == 0 {
		return g.Emit("PreToolUse")
	}
	return g.RunRoots(rows, "PreToolUse")
}

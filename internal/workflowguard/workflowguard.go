// Package workflowguard measures whether a push touches .github/workflows/, the one change a
// controller run never pushes itself: the workflow pusher does, after its checks. The hook
// workflow-push-gate, `lighthouse post-artefact` and `lighthouse roborev-gate gate --push` share
// the range it computes, so the run reviews exactly what the gate would refuse.
package workflowguard

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Dir is the directory whose changes a run does not push.
const Dir = ".github/workflows/"

// Git runs git in one repository and returns its stdout; an error means it did not succeed.
type Git func(args ...string) (string, error)

// Base is the start of the pushed range of tip onto branch dest: origin/<dest> when that
// remote-tracking branch exists, else merge-base(origin/<defaultBranch>, tip). An empty dest is a
// destination that is no branch on origin.
func Base(git Git, tip, dest, defaultBranch string) (string, error) {
	if dest != "" {
		sha, err := remoteBranch(git, dest)
		if err != nil {
			return "", err
		}
		if sha != "" {
			return sha, nil
		}
	}
	mainSHA, err := remoteBranch(git, defaultBranch)
	if err != nil {
		return "", err
	}
	if mainSHA == "" {
		return "", fmt.Errorf("origin/%s does not exist, so the branch point is unknown", defaultBranch)
	}
	out, err := git("merge-base", mainSHA, tip)
	if err != nil {
		return "", fmt.Errorf("no merge-base of origin/%s and %s: %w", defaultBranch, tip, err)
	}
	return strings.TrimSpace(out), nil
}

// remoteBranch is the sha of refs/remotes/origin/<branch>, or "" when it does not exist.
// for-each-ref also matches a prefix up to a slash, so only the exact refname counts.
func remoteBranch(git Git, branch string) (string, error) {
	if branch == "" || strings.ContainsAny(branch, "*?[\\ \t\n") {
		return "", fmt.Errorf("%q is no branch name", branch)
	}
	ref := "refs/remotes/origin/" + branch
	out, err := git("for-each-ref", "--format=%(objectname) %(refname)", ref)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", ref, err)
	}
	for _, line := range strings.Split(out, "\n") {
		if sha, name, ok := strings.Cut(strings.TrimSpace(line), " "); ok && name == ref {
			return sha, nil
		}
	}
	return "", nil
}

// Touched are the paths under Dir that base..tip touches, sorted: every path a non-merge commit
// in the range changes, plus the net diff of base and tip (which holds what a merge brings in).
// A file that a commit adds and a later one removes counts: GitHub sees every pushed commit.
// Renames count as a delete and an add.
func Touched(git Git, base, tip string) ([]string, error) {
	perCommit, err := git("log", "--no-ext-diff", "--no-textconv", "--no-renames", "--format=", "--name-only", "-z", base+".."+tip)
	if err != nil {
		return nil, fmt.Errorf("cannot list the commits of %s..%s: %w", base, tip, err)
	}
	net, err := git("diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-only", "-z", base, tip)
	if err != nil {
		return nil, fmt.Errorf("cannot diff %s and %s: %w", base, tip, err)
	}
	seen := map[string]bool{}
	for _, field := range strings.Split(perCommit+"\x00"+net, "\x00") {
		path := strings.TrimLeft(field, "\n")
		if strings.HasPrefix(path, Dir) {
			seen[path] = true
		}
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}

// Check is Base and Touched in one: the range as `<base>..<tip>` and the workflow paths in it.
func Check(git Git, tip, dest, defaultBranch string) (rng string, paths []string, err error) {
	base, err := Base(git, tip, dest, defaultBranch)
	if err != nil {
		return "", nil, err
	}
	paths, err = Touched(git, base, tip)
	if err != nil {
		return "", nil, err
	}
	return base + ".." + tip, paths, nil
}

// maxListed is how many paths a refusal names.
const maxListed = 5

// Refusal is the text of a refused push: what it touches and what the run does instead. It
// never advises on credentials: the run is not meant to push this, whatever it holds.
func Refusal(rng string, paths []string) string {
	listed := make([]string, 0, maxListed+1)
	for i, p := range paths {
		if i == maxListed {
			listed = append(listed, fmt.Sprintf("and %d more", len(paths)-maxListed))
			break
		}
		listed = append(listed, strconv.Quote(p))
	}
	return "this push touches " + Dir + " (" + oneLine(rng) + "): " + strings.Join(listed, ", ") + ".\n" +
		"\n" +
		"A controller run does not push workflow changes itself; the workflow pusher does.\n" +
		"Instead:\n" +
		steps
}

// Uncheckable is the text of a push whose range could not be measured: it does not pass.
func Uncheckable(reason string) string {
	return "COULD NOT CHECK — " + oneLine(reason) + "\n" +
		"\n" +
		"In a controller run a push that may touch " + Dir + " does not pass unmeasured.\n" +
		"Fix the cause above, or, when the branch touches " + Dir + ":\n" +
		steps
}

// steps is what a run does instead of pushing workflow changes.
const steps = "  1. Run V2b of /lighthouse-write-pr on the unpushed range with\n" +
	"     `lighthouse roborev-gate gate --push` (no line threshold; a skip blocks the pusher).\n" +
	"  2. Post the handoff with `next: push-workflows` (/lighthouse-write-handoff).\n"

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

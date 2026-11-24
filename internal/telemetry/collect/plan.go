package collect

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/telemetry/transcript"
)

var (
	taskHeadingRe = regexp.MustCompile(`^ {0,3}#{2,3}[ \t]+(?:Task|Taak)[ \t]+\d+\b`)
	fenceRe       = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
)

// CountPlanTasks counts the headings "## Task <n>", "### Task <n>", "## Taak <n>" and
// "### Taak <n>" outside fenced code blocks (``` or ~~~).
func CountPlanTasks(markdown string) int {
	n := 0
	var fence string // the opening fence; empty outside a block
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			switch {
			case fence == "":
				fence = m[1]
				continue
			case m[1][0] == fence[0] && len(m[1]) >= len(fence) && strings.TrimSpace(line) == m[1]:
				fence = ""
				continue
			}
		}
		if fence == "" && taskHeadingRe.MatchString(line) {
			n++
		}
	}
	return n
}

// planFile is where the plan of a trajectory was found: Rel is the repo-relative path that is
// stored, Text its current content.
type planFile struct {
	Rel  string
	Text string
}

// repoRoots are the directories a plan written by the session may live in: the repo dir and
// its worktrees. A file under any other directory belongs to another repository.
func repoRoots(ctx context.Context, opt Options) []string {
	if opt.RepoDir == "" {
		return nil
	}
	roots := []string{filepath.Clean(opt.RepoDir)}
	if out, ok, err := gitOut(ctx, opt, "worktree", "list", "--porcelain"); err == nil && ok {
		for _, ln := range strings.Split(out, "\n") {
			if p, found := strings.CutPrefix(ln, "worktree "); found {
				roots = append(roots, filepath.Clean(strings.TrimSpace(p)))
			}
		}
	}
	return roots
}

// relUnder returns the repo-relative path ("docs/plans/x.md", slashes) of abs when abs lies
// under plansDir of one of the roots; the deepest root wins, so a worktree inside the repo dir
// is read as its own root.
func relUnder(abs string, roots []string, plansDir string) (string, bool) {
	dir := path.Clean(filepath.ToSlash(plansDir))
	best, bestRel := -1, ""
	for _, root := range roots {
		r, err := filepath.Rel(root, filepath.Clean(abs))
		if err != nil || r == "." || strings.HasPrefix(r, "..") || filepath.IsAbs(r) {
			continue
		}
		if len(root) > best {
			best, bestRel = len(root), filepath.ToSlash(r)
		}
	}
	if best < 0 || !strings.HasPrefix(bestRel, dir+"/") || len(bestRel) <= len(dir)+1 {
		return "", false
	}
	return bestRel, true
}

// findWrittenPlan returns the last file the transcript wrote under plansDir that can still be
// read. The content comes from the file as written, else from the working tree, else from
// origin/<default>. A nil result is no written plan.
func findWrittenPlan(ctx context.Context, opt Options, written []transcript.WrittenFile) (*planFile, error) {
	roots := repoRoots(ctx, opt)
	for i := len(written) - 1; i >= 0; i-- {
		rel, ok := relUnder(written[i].Path, roots, opt.PlansDir)
		if !ok {
			continue
		}
		if text, ok, err := readPlan(ctx, opt, written[i].Path, rel); err != nil {
			return nil, err
		} else if ok {
			return &planFile{Rel: rel, Text: text}, nil
		}
	}
	return nil, nil
}

// findNamedPlan returns the last plan file whose name holds issue-<nr>-, or nil.
func findNamedPlan(ctx context.Context, opt Options, issue int) (*planFile, error) {
	names, err := planNames(ctx, opt)
	if err != nil {
		return nil, err
	}
	needle := fmt.Sprintf("issue-%d-", issue)
	for i := len(names) - 1; i >= 0; i-- {
		if !strings.Contains(path.Base(names[i]), needle) {
			continue
		}
		if text, ok, err := readPlan(ctx, opt, "", names[i]); err != nil {
			return nil, err
		} else if ok {
			return &planFile{Rel: names[i], Text: text}, nil
		}
	}
	return nil, nil
}

// readPlan reads a plan from the written path, the working tree or the default branch, in that order.
func readPlan(ctx context.Context, opt Options, abs, rel string) (string, bool, error) {
	candidates := []string{abs}
	if opt.RepoDir != "" {
		candidates = append(candidates, filepath.Join(opt.RepoDir, filepath.FromSlash(rel)))
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		b, err := os.ReadFile(p)
		if err == nil {
			return string(b), true, nil
		}
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrInvalid) {
			return "", false, fmt.Errorf("plan %s: %w", p, err)
		}
	}
	out, ok, err := gitOut(ctx, opt, "show", "origin/"+opt.DefaultBranch+":"+rel)
	return out, ok, err
}

// planNames lists the files under plansDir, in the working tree and on the default branch, sorted.
func planNames(ctx context.Context, opt Options) ([]string, error) {
	seen := map[string]bool{}
	dir := path.Clean(filepath.ToSlash(opt.PlansDir))
	if opt.RepoDir != "" {
		entries, err := os.ReadDir(filepath.Join(opt.RepoDir, filepath.FromSlash(dir)))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("plans dir: %w", err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				seen[dir+"/"+e.Name()] = true
			}
		}
	}
	if out, ok, err := gitOut(ctx, opt, "ls-tree", "--name-only", "origin/"+opt.DefaultBranch, dir+"/"); err != nil {
		return nil, err
	} else if ok {
		for _, n := range strings.Split(out, "\n") {
			if n = strings.TrimSpace(n); n != "" {
				seen[n] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// gitOut runs git in the repo dir. A git failure (no such ref or path, no clone) is "not found",
// not an error: a plan that cannot be read is no plan.
func gitOut(ctx context.Context, opt Options, args ...string) (string, bool, error) {
	if opt.RepoDir == "" || opt.DefaultBranch == "" {
		return "", false, nil
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", opt.RepoDir}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		return "", false, nil
	}
	return out.String(), true, nil
}

package scheduled

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// PostcheckResult is the outcome of Postcheck. Failure is non-empty when the run failed for
// real, with the reason; nothing is committed or pushed then. Changed is false when the run left
// nothing to commit (a done run without a PR).
type PostcheckResult struct {
	Changed bool
	Failure string
}

const postcheckGitTimeout = 2 * time.Minute

// Fallback identity for a repository that has none configured.
const (
	fallbackName  = "Lighthouse"
	fallbackEmail = "lighthouse@localhost"
)

// hardening goes before every git subcommand Postcheck runs. The agent could write into the git
// dir of its workplace, so nothing from there may execute in the tick: no hook
// (core.hooksPath=/dev/null; a hook-based check of the target repo is skipped on purpose, its CI
// on the PR is the gate), no fsmonitor command, no signing program. --literal-pathspecs makes a
// file named like a glob or `:(magic)` mean only itself. Residual risk: filter drivers and other
// config the agent might plant in .git/config are not disabled; see the task report.
var hardening = []string{
	"-c", "core.hooksPath=" + os.DevNull,
	"-c", "core.fsmonitor=false",
	"-c", "commit.gpgSign=false",
	"--literal-pathspecs",
	"--no-optional-locks",
}

var shaPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// Postcheck contains, verifies and commits the result of a scheduled run in its workplace
// (env.Dir), then pushes it with push. The order is the spec's (§4): resume when branch is
// already on origin at HEAD; the HEAD-guard (git reset --mixed startSHA, so commits of the agent
// become working-tree changes and are never trusted as such); job.Cleanup; the containment
// (every changed path, deletions and both sides of a rename included, is clean, satisfies
// job.Allowed and has no symlink in it that leaves the work tree or resolves to a path Allowed
// refuses); job.Check; job.Finish; the containment again on the final set; staging that set
// into a tree; the object check and secret scan on that tree as git stores it; a commit of that
// tree on start; push. Checking the written tree, not the work tree, means a conversion on
// `git add` (a .gitattributes encoding, an eol rule) cannot carry content past the scan, and the
// commit holds exactly what was checked.
// An error is transient (git could not run, the push failed): the run stays open and the next
// tick calls Postcheck again.
func Postcheck(ctx context.Context, run workplace.Runner, job Job, env Env, v Verdict, startSHA, branch string, push func(context.Context) error) (PostcheckResult, error) {
	if !shaPattern.MatchString(startSHA) {
		return failed("invalid start SHA %q", startSHA), nil
	}
	g := gitIn{run: run, dir: env.Dir}
	if code, err := g.code(ctx, "check-ref-format", "--branch", branch); err != nil {
		return PostcheckResult{}, err
	} else if code != 0 {
		return failed("invalid branch %q", branch), nil
	}
	pushed, err := g.remoteBranch(ctx, branch)
	if err != nil {
		return PostcheckResult{}, err
	}
	if pushed != "" {
		return resume(ctx, g, job, env, startSHA, branch, pushed)
	}

	// HEAD-guard: back on the run's branch, at the start, with the agent's work as changes.
	if _, err := g.ok(ctx, "symbolic-ref", "HEAD", "refs/heads/"+branch); err != nil {
		return PostcheckResult{}, err
	}
	if code, err := g.code(ctx, "reset", "--quiet", "--mixed", startSHA); err != nil {
		return PostcheckResult{}, err
	} else if code != 0 {
		return failed("HEAD-guard: cannot reset to start %s (exit code %d)", startSHA, code), nil
	}

	if err := job.Cleanup(ctx, env, env.Dir); err != nil {
		return failed("cleanup: %v", err), nil
	}
	paths, reason, err := g.contained(ctx, job, env.Dir)
	if err != nil || reason != "" || len(paths) == 0 {
		return PostcheckResult{Failure: reason}, err
	}
	if err := job.Check(ctx, env, env.Dir); err != nil {
		return failed("check: %v", err), nil
	}
	if err := job.Finish(ctx, env, env.Dir, v); err != nil {
		return failed("finish: %v", err), nil
	}
	// Finish writes files of its own; only the set validated after it is committed.
	paths, reason, err = g.contained(ctx, job, env.Dir)
	if err != nil || reason != "" || len(paths) == 0 {
		return PostcheckResult{Failure: reason}, err
	}
	if _, err := g.ok(ctx, append([]string{"add", "--"}, paths...)...); err != nil {
		return PostcheckResult{}, err
	}
	tree, err := g.ok(ctx, "write-tree")
	if err != nil {
		return PostcheckResult{}, err
	}
	changes, err := g.diffTree(ctx, startSHA, tree)
	if err != nil {
		return PostcheckResult{}, err
	}
	if extra := difference(changePaths(changes), paths); len(extra) > 0 {
		return failed("staged paths differ from the checked set: %s", showPaths(extra)), nil
	}
	if reason, err := g.verifyObjects(ctx, job, changes); err != nil || reason != "" {
		return PostcheckResult{Failure: reason}, err
	}
	if err := g.commit(ctx, branch, tree, startSHA, commitMessage(job, env)); err != nil {
		return PostcheckResult{}, err
	}
	if err := push(ctx); err != nil {
		return PostcheckResult{}, fmt.Errorf("push %s: %w", branch, err)
	}
	return PostcheckResult{Changed: true}, nil
}

// resume handles a branch that is already on origin, at pushed. Only the commit Postcheck makes
// itself should be there, but the agent may have pushed one of its own, so the commit on origin
// must be HEAD, one commit on top of start, no index entry may hide the work tree from git
// (assume-unchanged, skip-worktree), and the commit itself, read from git's objects and never
// from the work tree, must meet the object check, the secret scan, and Check on a fresh checkout
// of it. Cleanup and Finish do not run again.
func resume(ctx context.Context, g gitIn, job Job, env Env, startSHA, branch, pushed string) (PostcheckResult, error) {
	head, err := g.ok(ctx, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return PostcheckResult{}, err
	}
	if head != pushed {
		return failed("branch %s is already on origin at another commit (%s) than HEAD (%s)", branch, pushed, head), nil
	}
	parents, err := g.ok(ctx, "rev-list", "--parents", "-n", "1", head)
	if err != nil {
		return PostcheckResult{}, err
	}
	if f := strings.Fields(parents); len(f) != 2 || f[1] != startSHA {
		return failed("branch %s on origin is not one commit on start %s", branch, startSHA), nil
	}
	if hidden, err := g.hiddenEntries(ctx); err != nil {
		return PostcheckResult{}, err
	} else if len(hidden) > 0 {
		return failed("resume: %s marked assume-unchanged or skip-worktree", showPaths(hidden)), nil
	}
	changes, err := g.diffTree(ctx, startSHA, head)
	if err != nil {
		return PostcheckResult{}, err
	}
	if len(changes) == 0 {
		return failed("commit of %s on origin changes nothing", branch), nil
	}
	if reason, err := g.verifyObjects(ctx, job, changes); err != nil || reason != "" {
		return PostcheckResult{Failure: reason}, err
	}
	return g.checkCommit(ctx, job, env, head)
}

// checkCommit runs job.Check on a fresh, detached checkout of commit, so Check sees the commit and
// not a work tree the agent may have edited after it.
func (g gitIn) checkCommit(ctx context.Context, job Job, env Env, commit string) (PostcheckResult, error) {
	tmp, err := os.MkdirTemp("", "lighthouse-postcheck-")
	if err != nil {
		return PostcheckResult{}, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	tree := filepath.Join(tmp, "tree")
	if _, err := g.ok(ctx, "worktree", "add", "--detach", "--quiet", tree, commit); err != nil {
		return PostcheckResult{}, err
	}
	defer func() { _, _, _ = g.raw(context.WithoutCancel(ctx), "worktree", "remove", "--force", tree) }()
	checkEnv := env
	checkEnv.Dir = tree
	if err := job.Check(ctx, checkEnv, tree); err != nil {
		return failed("check: %v", err), nil
	}
	return PostcheckResult{Changed: true}, nil
}

// hiddenEntries lists index entries marked assume-unchanged (a lowercase tag in ls-files -v) or
// skip-worktree (tag S): git status does not compare those with the work tree.
func (g gitIn) hiddenEntries(ctx context.Context) ([]string, error) {
	out, err := g.ok(ctx, "ls-files", "-v", "-z")
	if err != nil {
		return nil, err
	}
	var hidden []string
	for _, e := range strings.Split(strings.TrimSuffix(out, "\x00"), "\x00") {
		if len(e) < 3 {
			continue
		}
		if tag := e[0]; tag == 'S' || (tag >= 'a' && tag <= 'z') {
			hidden = append(hidden, e[2:])
		}
	}
	return hidden, nil
}

func failed(format string, args ...any) PostcheckResult {
	return PostcheckResult{Failure: fmt.Sprintf(format, args...)}
}

func commitMessage(job Job, env Env) string {
	name := env.Schedule.Name
	if name == "" {
		name = job.Name()
	}
	return "chore: geplande run " + name
}

// gitIn runs hardened git commands in one work tree.
type gitIn struct {
	run workplace.Runner
	dir string
}

func (g gitIn) code(ctx context.Context, args ...string) (int, error) {
	_, code, err := g.raw(ctx, args...)
	return code, err
}

func (g gitIn) raw(ctx context.Context, args ...string) (string, int, error) {
	full := append(append([]string{"-C", g.dir}, hardening...), args...)
	out, code, err := g.run(ctx, postcheckGitTimeout, "git", full...)
	if err != nil {
		return "", code, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, code, nil
}

// ok runs git and turns a non-zero exit into an error; stdout comes back without its trailing
// newline (not trimmed further: -z output may end in a meaningful space).
func (g gitIn) ok(ctx context.Context, args ...string) (string, error) {
	out, code, err := g.raw(ctx, args...)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("git %s: exit code %d", args[0], code)
	}
	return strings.TrimSuffix(out, "\n"), nil
}

// remoteBranch is the SHA of refs/heads/<branch> on origin, "" when origin lacks it.
func (g gitIn) remoteBranch(ctx context.Context, branch string) (string, error) {
	ref := "refs/heads/" + branch
	out, err := g.ok(ctx, "ls-remote", "origin", ref)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		if sha, name, ok := strings.Cut(line, "\t"); ok && name == ref {
			return sha, nil
		}
	}
	return "", nil
}

// status lists every changed path of the work tree against the index, untracked files one by
// one, deletions and both sides of a rename included.
func (g gitIn) status(ctx context.Context) ([]string, error) {
	out, err := g.ok(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return nil, err
	}
	return parsePorcelain(out)
}

// contained lists the changed paths and checks them. reason is non-empty for a violation.
func (g gitIn) contained(ctx context.Context, job Job, dir string) (paths []string, reason string, err error) {
	paths, err = g.status(ctx)
	if err != nil {
		return nil, "", err
	}
	return paths, containment(job, dir, paths), nil
}

// commit makes a commit of tree with parent start and moves branch onto it, only when branch is
// still at start. The commit holds tree and nothing else, whatever the index holds by then.
func (g gitIn) commit(ctx context.Context, branch, tree, start, message string) error {
	args := []string{"commit-tree", tree, "-p", start, "-m", message}
	if code, err := g.code(ctx, "config", "--get", "user.email"); err != nil {
		return err
	} else if code != 0 {
		args = append([]string{"-c", "user.name=" + fallbackName, "-c", "user.email=" + fallbackEmail}, args...)
	}
	commit, err := g.ok(ctx, args...)
	if err != nil {
		return err
	}
	_, err = g.ok(ctx, "update-ref", "-m", "lighthouse postcheck", "refs/heads/"+branch, commit, start)
	return err
}

// difference lists the entries of a that b lacks and of b that a lacks, each once.
func difference(a, b []string) []string {
	inA, inB := map[string]bool{}, map[string]bool{}
	for _, s := range a {
		inA[s] = true
	}
	for _, s := range b {
		inB[s] = true
	}
	var diff []string
	for _, list := range [][]string{a, b} {
		for _, s := range list {
			if inA[s] != inB[s] {
				diff = append(diff, s)
				inA[s], inB[s] = true, true
			}
		}
	}
	return diff
}

func showPaths(paths []string) string {
	shown := make([]string, len(paths))
	for i, p := range paths {
		shown[i] = showPath(p)
	}
	return strings.Join(shown, ", ")
}

// treeChange is one entry of `git diff-tree -r -z --no-renames`: the path, and its new mode and
// object (mode 000000 for a deletion).
type treeChange struct {
	path, mode, object string
}

func changePaths(changes []treeChange) []string {
	paths := make([]string, len(changes))
	for i, c := range changes {
		paths[i] = c.path
	}
	return paths
}

func (g gitIn) diffTree(ctx context.Context, from, to string) ([]treeChange, error) {
	out, err := g.ok(ctx, "diff-tree", "-r", "-z", "--no-renames", from, to)
	if err != nil {
		return nil, err
	}
	return parseDiffTree(out)
}

// verifyObjects checks changes as git stores them: the path form and job.Allowed; no gitlink
// (a submodule is content the containment cannot see); a symlink's target text must stay inside
// the tree, out of .git, and name a path Allowed accepts (resolved lexically: the object has no
// file system); then the secret scan on the blob content and the path names.
func (g gitIn) verifyObjects(ctx context.Context, job Job, changes []treeChange) (string, error) {
	var findings []string
	for _, c := range changes {
		if pathEscapes(c.path) {
			return fmt.Sprintf("containment: path %s escapes the work tree", showPath(c.path)), nil
		}
		if !job.Allowed(c.path) {
			return fmt.Sprintf("containment: path %s is not allowed for job %s", showPath(c.path), job.Name()), nil
		}
		var body []byte
		switch c.mode {
		case "000000":
		case "100644", "100755", "120000":
			out, err := g.blob(ctx, c.object)
			if err != nil {
				return "", err
			}
			body = []byte(out)
			if c.mode == "120000" {
				if reason := symlinkTargetViolation(job, c.path, out); reason != "" {
					return reason, nil
				}
			}
		case "160000":
			return fmt.Sprintf("containment: %s is a submodule", showPath(c.path)), nil
		default:
			return fmt.Sprintf("containment: %s has unexpected mode %s", showPath(c.path), c.mode), nil
		}
		findings = append(findings, scanBody(c.path, body)...)
	}
	if len(findings) > 0 {
		return "secret scan: " + strings.Join(findings, "; "), nil
	}
	return "", nil
}

// blob is the exact content of a blob, without the trimming ok does.
func (g gitIn) blob(ctx context.Context, object string) (string, error) {
	out, code, err := g.raw(ctx, "cat-file", "blob", object)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("git cat-file blob %s: exit code %d", object, code)
	}
	return out, nil
}

// symlinkTargetViolation checks the target text of the symlink at p.
func symlinkTargetViolation(job Job, p, target string) string {
	resolved := path.Clean(path.Join(path.Dir(p), target))
	switch {
	case path.IsAbs(target) || !filepath.IsLocal(resolved):
		return fmt.Sprintf("containment: symlink %s targets a path outside the work tree", showPath(p))
	case resolved == ".git" || strings.HasPrefix(resolved, ".git/"):
		return fmt.Sprintf("containment: symlink %s targets .git", showPath(p))
	case !job.Allowed(resolved):
		return fmt.Sprintf("containment: symlink %s targets %s, which is not allowed", showPath(p), showPath(resolved))
	}
	return ""
}

// containment returns the first violation among paths, "" when there is none: a path that
// escapes the work tree or is not in clean form, a path job.Allowed refuses, or a path through a
// symlink that leaves the work tree, enters .git or cannot be resolved.
func containment(job Job, dir string, paths []string) string {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Sprintf("containment: cannot resolve the work tree: %v", err)
	}
	for _, p := range paths {
		if pathEscapes(p) {
			return fmt.Sprintf("containment: path %s escapes the work tree", showPath(p))
		}
		if !job.Allowed(p) {
			return fmt.Sprintf("containment: path %s is not allowed for job %s", showPath(p), job.Name())
		}
		if reason := symlinkViolation(job, root, dir, p); reason != "" {
			return reason
		}
	}
	return ""
}

// pathEscapes reports whether p is not a clean relative path inside the work tree.
func pathEscapes(p string) bool {
	return p == "" || p == "." || !filepath.IsLocal(p) || path.Clean(p) != p
}

// symlinkViolation checks every component of p (relative to dir, whose resolved form is root):
// a symlink among them must resolve to a target inside root, outside its .git, at a path
// job.Allowed accepts. A component that does not exist ends the walk: a deletion.
func symlinkViolation(job Job, root, dir, p string) string {
	parts := strings.Split(p, "/")
	for i := range parts {
		rel := strings.Join(parts[:i+1], "/")
		full := filepath.Join(dir, rel)
		info, err := os.Lstat(full)
		if errors.Is(err, fs.ErrNotExist) {
			return ""
		}
		if err != nil {
			return fmt.Sprintf("containment: cannot inspect %s: %v", showPath(rel), err)
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			continue
		}
		target, err := filepath.EvalSymlinks(full)
		if err != nil {
			return fmt.Sprintf("containment: symlink %s cannot be resolved", showPath(rel))
		}
		inside, err := filepath.Rel(root, target)
		if err != nil || !filepath.IsLocal(inside) {
			return fmt.Sprintf("containment: symlink %s points outside the work tree", showPath(rel))
		}
		inside = filepath.ToSlash(inside)
		if inside == ".git" || strings.HasPrefix(inside, ".git/") {
			return fmt.Sprintf("containment: symlink %s points into .git", showPath(rel))
		}
		if !job.Allowed(inside) {
			return fmt.Sprintf("containment: symlink %s resolves to %s, which is not allowed", showPath(rel), showPath(inside))
		}
	}
	return ""
}

// parsePorcelain reads `git status --porcelain=v1 -z`: "XY <path>" per NUL-terminated entry,
// followed by the original path as a field of its own for a rename or copy. Every path counts.
func parsePorcelain(out string) ([]string, error) {
	if out == "" {
		return nil, nil
	}
	if !strings.HasSuffix(out, "\x00") {
		return nil, fmt.Errorf("git status: output not NUL-terminated")
	}
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	var paths []string
	for i := 0; i < len(fields); i++ {
		e := fields[i]
		if len(e) < 4 || e[2] != ' ' {
			return nil, fmt.Errorf("git status: malformed entry %q", e)
		}
		paths = append(paths, e[3:])
		if strings.ContainsAny(e[:2], "RC") {
			i++
			if i == len(fields) {
				return nil, fmt.Errorf("git status: rename %q without its original path", e)
			}
			paths = append(paths, fields[i])
		}
	}
	return paths, nil
}

// parseDiffTree reads `git diff-tree -r -z --no-renames`: per change a field
// ":<old mode> <new mode> <old object> <new object> <status>" and then the path.
func parseDiffTree(out string) ([]treeChange, error) {
	out = strings.TrimSuffix(out, "\x00")
	if out == "" {
		return nil, nil
	}
	fields := strings.Split(out, "\x00")
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("git diff-tree: odd number of fields")
	}
	changes := make([]treeChange, 0, len(fields)/2)
	for i := 0; i < len(fields); i += 2 {
		meta := strings.Fields(strings.TrimPrefix(fields[i], ":"))
		if !strings.HasPrefix(fields[i], ":") || len(meta) != 5 || fields[i+1] == "" {
			return nil, fmt.Errorf("git diff-tree: malformed entry %q", fields[i])
		}
		changes = append(changes, treeChange{path: fields[i+1], mode: meta[1], object: meta[3]})
	}
	return changes, nil
}

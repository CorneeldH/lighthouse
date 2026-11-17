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
// job.Allowed and has no symlink in it that leaves the work tree); job.Check; job.Finish; the
// containment again on the final set; the secret scan; a commit of exactly that set; push.
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
	if findings := ScanSecrets(env.Dir, paths); len(findings) > 0 {
		return failed("secret scan: %s", strings.Join(findings, "; ")), nil
	}
	if err := g.commit(ctx, paths, commitMessage(job, env)); err != nil {
		return PostcheckResult{}, err
	}
	if err := push(ctx); err != nil {
		return PostcheckResult{}, fmt.Errorf("push %s: %w", branch, err)
	}
	return PostcheckResult{Changed: true}, nil
}

// resume handles a branch that is already on origin, at pushed. Only the commit Postcheck makes
// itself should be there, but the agent may have pushed one of its own, so the commit on origin
// must be HEAD, one commit on top of start, with a clean work tree, and meet the containment,
// Check and the secret scan. Cleanup and Finish do not run again.
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
	dirty, err := g.status(ctx)
	if err != nil {
		return PostcheckResult{}, err
	}
	if len(dirty) > 0 {
		return failed("work tree differs from the commit of %s on origin: %s", branch, strings.Join(dirty, ", ")), nil
	}
	out, err := g.ok(ctx, "diff", "--name-status", "-z", "--no-renames", "--no-ext-diff", startSHA, head)
	if err != nil {
		return PostcheckResult{}, err
	}
	paths, err := parseNameStatus(out)
	if err != nil {
		return PostcheckResult{}, err
	}
	if len(paths) == 0 {
		return failed("commit of %s on origin changes nothing", branch), nil
	}
	if reason := containment(job, env.Dir, paths); reason != "" {
		return PostcheckResult{Failure: reason}, nil
	}
	if err := job.Check(ctx, env, env.Dir); err != nil {
		return failed("check: %v", err), nil
	}
	if findings := ScanSecrets(env.Dir, paths); len(findings) > 0 {
		return failed("secret scan: %s", strings.Join(findings, "; ")), nil
	}
	return PostcheckResult{Changed: true}, nil
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

// commit stages exactly paths, verifies the index holds exactly those changes, and commits.
func (g gitIn) commit(ctx context.Context, paths []string, message string) error {
	if _, err := g.ok(ctx, append([]string{"add", "--"}, paths...)...); err != nil {
		return err
	}
	out, err := g.ok(ctx, "diff", "--cached", "--name-only", "-z", "--no-renames", "--no-ext-diff")
	if err != nil {
		return err
	}
	staged := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	if !sameSet(staged, paths) {
		return fmt.Errorf("staged paths %q differ from the checked paths %q", staged, paths)
	}
	args := []string{"commit", "--quiet", "-m", message}
	if code, err := g.code(ctx, "config", "--get", "user.email"); err != nil {
		return err
	} else if code != 0 {
		args = append([]string{"-c", "user.name=" + fallbackName, "-c", "user.email=" + fallbackEmail}, args...)
	}
	_, err = g.ok(ctx, args...)
	return err
}

func sameSet(a, b []string) bool {
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[s] = true
	}
	if len(set) != len(a) {
		return false
	}
	for _, s := range b {
		if !set[s] {
			return false
		}
		delete(set, s)
	}
	return len(set) == 0
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
			return fmt.Sprintf("containment: path %q escapes the work tree", p)
		}
		if !job.Allowed(p) {
			return fmt.Sprintf("containment: path %q is not allowed for job %s", p, job.Name())
		}
		if reason := symlinkViolation(root, dir, p); reason != "" {
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
// a symlink among them must resolve to a target inside root and outside its .git. A component
// that does not exist ends the walk: a deletion.
func symlinkViolation(root, dir, p string) string {
	parts := strings.Split(p, "/")
	for i := range parts {
		rel := strings.Join(parts[:i+1], "/")
		full := filepath.Join(dir, rel)
		info, err := os.Lstat(full)
		if errors.Is(err, fs.ErrNotExist) {
			return ""
		}
		if err != nil {
			return fmt.Sprintf("containment: cannot inspect %q: %v", rel, err)
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			continue
		}
		target, err := filepath.EvalSymlinks(full)
		if err != nil {
			return fmt.Sprintf("containment: symlink %q cannot be resolved", rel)
		}
		inside, err := filepath.Rel(root, target)
		if err != nil || !filepath.IsLocal(inside) {
			return fmt.Sprintf("containment: symlink %q points outside the work tree", rel)
		}
		if inside == ".git" || strings.HasPrefix(inside, ".git/") {
			return fmt.Sprintf("containment: symlink %q points into .git", rel)
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

// parseNameStatus reads `git diff --name-status -z`: a status field, then one path, or two for
// a rename or copy.
func parseNameStatus(out string) ([]string, error) {
	out = strings.TrimSuffix(out, "\x00")
	if out == "" {
		return nil, nil
	}
	fields := strings.Split(out, "\x00")
	var paths []string
	for i := 0; i < len(fields); i++ {
		if fields[i] == "" {
			return nil, fmt.Errorf("git diff: empty status field")
		}
		n := 1
		if strings.ContainsAny(fields[i][:1], "RC") {
			n = 2
		}
		if i+n >= len(fields) {
			return nil, fmt.Errorf("git diff: status %q without its path", fields[i])
		}
		paths = append(paths, fields[i+1:i+1+n]...)
		i += n
	}
	return paths, nil
}

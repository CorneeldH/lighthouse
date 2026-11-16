package pins

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

// Status is the outcome of one measurement of one pin.
type Status string

const (
	StatusCurrent Status = "current"
	StatusBehind  Status = "behind"
	StatusAhead   Status = "ahead"
	StatusError   Status = "error"
)

// CommitInfo is one upstream commit that touched a pinned path.
type CommitInfo struct {
	SHA, Subject, Body string
}

// Result is the outcome of one measurement. Target is the upstream HEAD sha, or for a release
// measurement the newest version, when known.
type Result struct {
	Status  Status
	Detail  string
	Target  string
	Commits []CommitInfo
	// Releases are the upstream releases above the pin, newest first; only plugin and tool
	// measurements fill it.
	Releases []forge.Release
}

// Errors a Clone returns so that the measurement can say what is wrong with the pin.
var (
	ErrUnknownCommit = errors.New("commit not found upstream")
	ErrPathMissing   = errors.New("path does not exist at that commit")
)

// GitSource opens a read-only clone of an upstream repository.
type GitSource interface {
	Clone(ctx context.Context, repo string) (Clone, error)
}

// Clone reads from a bare, blob-less clone. Close removes it.
type Clone interface {
	// Head is the sha of the remote's default branch.
	Head() (string, error)
	// HeadTree and CommitTree give the object id of path at HEAD and at sha.
	HeadTree(path string) (string, error)
	CommitTree(sha, path string) (string, error)
	// PathLog lists the commits in sha..HEAD that touched path, newest first.
	PathLog(sha, path string) ([]CommitInfo, error)
	// ReadFile returns the content of path at sha.
	ReadFile(sha, path string) ([]byte, error)
	// ListTree returns the file paths under path at sha, relative to the repo root, sorted.
	ListTree(sha, path string) ([]string, error)
	Close() error
}

type ghSource struct {
	base func(repo string) string
}

// NewGitSource clones from https://github.com/<repo>.
func NewGitSource() GitSource {
	return NewGitSourceAt(func(repo string) string { return "https://github.com/" + repo })
}

// NewGitSourceAt clones from the URL base returns for owner/repo, such as a file:// path in a
// test or a forge other than github.com.
func NewGitSourceAt(base func(repo string) string) GitSource {
	return &ghSource{base: base}
}

type ghClone struct {
	ctx context.Context
	dir string
}

// gitEnv strips the caller's git environment and the user's and system's config, so no hook path
// or alias from there reaches the clone; hooks are also switched off per call.
func gitEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_") {
			env = append(env, e)
		}
	}
	return append(env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
}

func gitCmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	full := append([]string{"-c", "core.hooksPath=/dev/null", "-c", "protocol.ext.allow=never"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	return cmd
}

func runGitOut(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := gitCmd(ctx, dir, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// Clone makes a bare partial clone without checkout, hooks or submodules in a temporary directory.
func (s *ghSource) Clone(ctx context.Context, repo string) (Clone, error) {
	if !repoRe.MatchString(repo) {
		return nil, fmt.Errorf("invalid repo %q", repo)
	}
	dir, err := os.MkdirTemp("", "lighthouse-pins-*")
	if err != nil {
		return nil, err
	}
	if _, err := runGitOut(ctx, dir, "clone", "--bare", "--filter=blob:none", "--no-tags", "--quiet",
		"--", s.base(repo), dir); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("clone %s: %w", repo, err)
	}
	return &ghClone{ctx: ctx, dir: dir}, nil
}

func (c *ghClone) git(args ...string) ([]byte, error) {
	return runGitOut(c.ctx, c.dir, args...)
}

func (c *ghClone) Head() (string, error) {
	out, err := c.git("rev-parse", "--verify", "HEAD^{commit}")
	return strings.TrimSpace(string(out)), err
}

func (c *ghClone) HeadTree(path string) (string, error) {
	return c.tree("HEAD", path)
}

func (c *ghClone) CommitTree(sha, path string) (string, error) {
	if !shaRe.MatchString(sha) {
		return "", fmt.Errorf("%w: %q is not a 40-hex sha", ErrUnknownCommit, sha)
	}
	if _, err := c.git("cat-file", "-e", sha+"^{commit}"); err != nil {
		return "", fmt.Errorf("%w: %s", ErrUnknownCommit, sha)
	}
	return c.tree(sha, path)
}

func (c *ghClone) tree(rev, path string) (string, error) {
	out, err := c.git("rev-parse", "--verify", rev+":"+path)
	if err != nil {
		return "", fmt.Errorf("%w: %s:%s", ErrPathMissing, rev, path)
	}
	return strings.TrimSpace(string(out)), nil
}

func (c *ghClone) PathLog(sha, path string) ([]CommitInfo, error) {
	if !shaRe.MatchString(sha) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownCommit, sha)
	}
	out, err := c.git("log", "--format=%H%x00%s%x00%b%x1e", sha+"..HEAD", "--", path)
	if err != nil {
		return nil, err
	}
	var commits []CommitInfo
	for _, rec := range strings.Split(string(out), "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, "\x00", 3)
		if len(f) != 3 {
			return nil, fmt.Errorf("unexpected git log record %q", rec)
		}
		commits = append(commits, CommitInfo{SHA: f[0], Subject: f[1], Body: strings.TrimSpace(f[2])})
	}
	return commits, nil
}

// ReadFile fetches the blob lazily from the remote, as the clone has none.
func (c *ghClone) ReadFile(sha, path string) ([]byte, error) {
	if !shaRe.MatchString(sha) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownCommit, sha)
	}
	out, err := c.git("cat-file", "blob", sha+":"+path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s:%s: %w", ErrPathMissing, sha, path, err)
	}
	return out, nil
}

func (c *ghClone) ListTree(sha, path string) ([]string, error) {
	if !shaRe.MatchString(sha) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownCommit, sha)
	}
	if _, err := c.tree(sha, path); err != nil {
		return nil, err
	}
	out, err := c.git("ls-tree", "-r", "--name-only", "-z", sha, "--", path)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	return files, nil
}

func (c *ghClone) Close() error { return os.RemoveAll(c.dir) }

func errResult(format string, args ...any) Result {
	return Result{Status: StatusError, Detail: fmt.Sprintf(format, args...)}
}

// MeasureSource compares the tree of the pinned path at the pin commit with the same path at the
// upstream default branch. It runs no upstream code: see ghSource.Clone.
func MeasureSource(ctx context.Context, g GitSource, p SourcePin) Result {
	switch {
	case p.Err != nil:
		return errResult("%v", p.Err)
	case p.Commit == "":
		return errResult("no upstream-commit")
	case p.Path == "":
		return errResult("no upstream-path")
	}
	cl, err := g.Clone(ctx, p.Repo)
	if err != nil {
		return errResult("%v", err)
	}
	defer func() { _ = cl.Close() }()

	pinTree, err := cl.CommitTree(p.Commit, p.Path)
	switch {
	case errors.Is(err, ErrUnknownCommit):
		return errResult("pin commit %s not found in %s", p.Commit, p.Repo)
	case err != nil:
		return errResult("path %s does not exist at pin commit %s", p.Path, p.Commit)
	}
	head, err := cl.Head()
	if err != nil {
		return errResult("%v", err)
	}
	headTree, err := cl.HeadTree(p.Path)
	if err != nil {
		res := errResult("path %s no longer exists upstream", p.Path)
		res.Target = head
		return res
	}
	if headTree == pinTree {
		return Result{Status: StatusCurrent, Target: head}
	}
	commits, err := cl.PathLog(p.Commit, p.Path)
	if err != nil {
		res := errResult("%v", err)
		res.Target = head
		return res
	}
	var lines []string
	for _, c := range commits {
		lines = append(lines, c.SHA[:7]+" "+c.Subject)
	}
	detail := fmt.Sprintf("%d commit(s) touched %s", len(commits), p.Path)
	if len(lines) > 0 {
		detail += ": " + strings.Join(lines, "; ")
	}
	return Result{Status: StatusBehind, Detail: detail, Target: head, Commits: commits}
}

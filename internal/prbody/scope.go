package prbody

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/deadline"
	"github.com/CorneeldH/lighthouse/internal/gitinvocations"
	"github.com/CorneeldH/lighthouse/internal/repoidentity"
)

// The scope decisions of InScope: `toetsen`, `overslaan` and `blokkeren` in Python.
const (
	decisionCheck = "check"
	decisionSkip  = "skip"
	decisionBlock = "block"
)

// notARepo is the reason of commonDir for a directory outside every git repo.
const notARepo = "not a repo"

// The timeouts as variables, so a test can shorten them.
var (
	acTimeout    = ACTimeoutS * time.Second
	gitTimeout   = GitTimeoutS * time.Second
	gateDeadline = DeadlineS * time.Second
)

// waitDelay bounds the wait for the pipes of a child that the context ended.
const waitDelay = time.Second

var queryOrAnchorRe = regexp.MustCompile(`[?#]`)

// budget is min(own, the time left on ctx); <= 0 means the budget is spent.
func budget(ctx context.Context, own time.Duration) time.Duration {
	if dl, ok := ctx.Deadline(); ok {
		return min(own, time.Until(dl))
	}
	return own
}

// run runs name in its own process group, which goes as a whole when ctx ends: a grandchild that
// holds stdout open does not keep the wait going. env nil inherits this process's environment.
// err is ctx.Err() when ctx ended the child, an exec error when it did not start, and nil for any
// exit; code is then the exit code, or minus the signal, as Python's returncode.
func run(ctx context.Context, env []string, name string, args ...string) (stdout, stderr string, code int, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	deadline.InOwnGroup(cmd)
	cmd.Cancel = func() error { deadline.KillGroup(cmd); return nil }
	cmd.WaitDelay = waitDelay
	runErr := cmd.Run()
	if runErr != nil && ctx.Err() != nil {
		return "", "", 0, ctx.Err()
	}
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return "", "", 0, runErr
	}
	code = cmd.ProcessState.ExitCode()
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		code = -int(ws.Signal())
	}
	return out.String(), errOut.String(), code, nil
}

// commonDir is (git-common-dir, ""), ("", notARepo) or ("", <why git gave no answer>).
func commonDir(ctx context.Context, path string) (string, string) {
	timeout := budget(ctx, gitTimeout)
	if timeout <= 0 {
		return "", "COULD NOT CHECK: deadline reached"
	}
	gitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// The error text is matched below, and git translates it.
	env := append(os.Environ(), "LC_ALL=C")
	stdout, stderr, code, err := run(gitCtx, env, "git", "-C", path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	switch {
	case err != nil && gitCtx.Err() != nil:
		return "", "COULD NOT CHECK: git timeout or deadline reached"
	case err != nil:
		return "", "git did not run: " + err.Error()
	case code == 0:
		return realpath(pyStrip(stdout)), ""
	case strings.Contains(stderr, "not a git repository"):
		return "", notARepo
	}
	return "", fmt.Sprintf("git failed (rc %d): %s", code, pyStrip(stderr))
}

// realpath is os.path.realpath: the symlinks resolved; a path that cannot be resolved stays
// as it is, cleaned.
func realpath(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

// pyCasefold is str.casefold for the characters a repo name can bring: lower case, plus the full
// foldings that lower case lacks for Latin and Greek.
func pyCasefold(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case 'ß', 'ẞ':
			b.WriteString("ss")
		case 'ς':
			b.WriteRune('σ')
		case 'ſ':
			b.WriteRune('s')
		case 'µ':
			b.WriteRune('μ')
		case 'İ':
			b.WriteString("i̇")
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// pyStrRepr is Python's repr() of a str.
func pyStrRepr(s string) string {
	quote := byte('\'')
	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for i, r := range s {
		if _, size := utf8.DecodeRuneInString(s[i:]); r == utf8.RuneError && size == 1 {
			// An invalid byte: Python's surrogateescape makes it U+DC80–U+DCFF.
			fmt.Fprintf(&b, `\udc%02x`, s[i])
			continue
		}
		switch {
		case r == rune(quote) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case !unicode.IsPrint(r):
			switch {
			case r < 0x100:
				fmt.Fprintf(&b, `\x%02x`, r)
			case r < 0x10000:
				fmt.Fprintf(&b, `\u%04x`, r)
			default:
				fmt.Fprintf(&b, `\U%08x`, r)
			}
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// NormalizeRepo is the project path (case-folded) of a `--repo` value as the CLI of kind reads
// it, or false when that form is missing. Both take URLs (`https://…`, `ssh://git@…`,
// `git@host:o/r`), with or without `.git` or a trailing `/`. gh (kind "github" or "") reads
// `[HOST/]OWNER/REPO`: `owner/repo`. glab (kind "gitlab") reads `OWNER/REPO` or
// `GROUP/NAMESPACE/REPO`: without a scheme or a user no segment is a host, and the whole path
// is the project, subgroups included.
func NormalizeRepo(value, kind string) (string, bool) {
	rest := queryOrAnchorRe.Split(pyStrip(value), 2)[0] // drop query and anchor
	hasScheme := strings.Contains(rest, "://")
	if _, after, found := strings.Cut(rest, "://"); found {
		rest = after
	}
	first, _, _ := strings.Cut(rest, "/")
	user := strings.Contains(first, "@")
	if user {
		_, rest, _ = strings.Cut(rest, "@")
	}
	hasHost := hasScheme || user
	if user && !hasScheme {
		rest = strings.Replace(rest, ":", "/", 1) // scp form `git@host:owner/repo`
	} else if hasScheme {
		host, path, _ := strings.Cut(rest, "/")
		host, _, _ = strings.Cut(host, ":") // drop the port
		rest = host + "/" + path
	}
	var parts []string
	for _, d := range strings.Split(rest, "/") {
		if d != "" {
			parts = append(parts, d)
		}
	}
	if len(parts) > 2 && kind != "gitlab" {
		hasHost = true
	}
	if hasHost && len(parts) > 0 {
		parts = parts[1:]
	}
	if kind == "gitlab" {
		if len(parts) < 2 {
			return "", false
		}
		parts[len(parts)-1] = strings.TrimSuffix(parts[len(parts)-1], ".git")
		if parts[len(parts)-1] == "" {
			return "", false
		}
		return pyCasefold(strings.Join(parts, "/")), true
	}
	if len(parts) < 2 {
		return "", false
	}
	owner, name := parts[0], strings.TrimSuffix(parts[1], ".git")
	if owner == "" || name == "" {
		return "", false
	}
	return pyCasefold(owner + "/" + name), true
}

// repoFunc gives the forge of `forge.repo` (Slug "" when not set), or the error of a lighthouse.toml that cannot be
// read. Python reads the config only where it needs the repo; so does Main.
type repoFunc func() (Repo, error)

// cliKind is the forge kind whose `--repo` form the tool of pc reads.
func cliKind(pc PrCreate) string {
	if pc.Tool == "glab" {
		return "gitlab"
	}
	return "github"
}

// command is the command of pc as the messages name it.
func command(pc PrCreate) string {
	switch pc.Tool {
	case "glab":
		return "glab mr create"
	case "lighthouse":
		return "lighthouse forge pr create"
	}
	return "gh pr create"
}

// scopeRepo is the scope decision for a literal repo (`--repo`/`-R` or GH_REPO) of pc. Without
// forge.repo there is no telling whether it is another repo: then check.
func scopeRepo(pc PrCreate, value string, repo repoFunc) (string, string, error) {
	normalized, ok := NormalizeRepo(value, cliKind(pc))
	if !ok {
		return decisionBlock, "--repo " + pyStrRepr(value) + " does not have the form [HOST/]OWNER/REPO", nil
	}
	own, err := repo()
	if err != nil {
		return "", "", err
	}
	if own.Slug == "" || normalized == pyCasefold(own.Slug) {
		return decisionCheck, "", nil
	}
	return decisionSkip, "skipped — other repo " + value, nil
}

// withoutRepo is the scope of a create in this repo without --repo, -R or GH_REPO (#224). In a
// clone of a fork gh and glab then pick the upstream repo as the base, not forge.repo; with
// forge.repo set the repo has to be spelled out. `lighthouse forge pr create` always opens the
// PR in forge.repo: check it.
func withoutRepo(pc PrCreate, repo repoFunc) (string, string, error) {
	if pc.Tool == "lighthouse" {
		return decisionCheck, "", nil
	}
	own, err := repo()
	if err != nil {
		return "", "", err
	}
	if own.Slug == "" {
		return decisionCheck, "", nil
	}
	if pc.Tool == "glab" {
		return decisionBlock, "`glab mr create` without --repo: in a clone of a fork glab then opens the MR" +
			" in the upstream repo. Add `--repo " + own.Slug + "`.", nil
	}
	return decisionBlock, "`gh pr create` without --repo: in a clone of a fork gh then opens the PR" +
		" in the upstream repo. Add `--repo " + own.Slug + "`.", nil
}

// InScope decides for pc whether to check it, skip it or block it, with the reason. projectDir is
// the project's checkout; repo is forge.repo ("" when not set). A git call that cannot answer
// blocks: it is no proof of "inside" or "outside".
func InScope(ctx context.Context, pc PrCreate, projectDir string, repo Repo) (decision, reason string) {
	decision, reason, _ = inScope(ctx, pc, projectDir, func() (Repo, error) { return repo, nil })
	return decision, reason
}

func inScope(ctx context.Context, pc PrCreate, projectDir string, repo repoFunc) (string, string, error) {
	if pc.RepoProblem != "" {
		return decisionBlock, pc.RepoProblem, nil
	}
	if pc.Repo != nil {
		return scopeRepo(pc, *pc.Repo, repo)
	}
	if pc.Cwd == nil {
		return decisionBlock, "working directory of `" + command(pc) + "` cannot be resolved", nil
	}
	// A `cd` to a directory that the command itself still creates: the nearest existing ancestor
	// determines the repo.
	path, ok := gitinvocations.ExistingAncestor(*pc.Cwd)
	if !ok {
		return decisionBlock, "working directory " + *pc.Cwd + " cannot be resolved", nil
	}
	ownDir, why := commonDir(ctx, projectDir)
	if ownDir == "" {
		return decisionBlock, "repo of the project cannot be determined (" + why + ")", nil
	}
	dir, why := commonDir(ctx, path)
	if dir == "" {
		if why == notARepo {
			return decisionSkip, "skipped — outside this repo", nil
		}
		return decisionBlock, "repo of " + path + " cannot be determined (" + why + ")", nil
	}
	if dir == ownDir {
		return withoutRepo(pc, repo)
	}
	// Another common dir: a worktree of a separate clone (forge) with the same origin is this
	// repo after all (#36).
	timeout := budget(ctx, gitTimeout)
	if timeout <= 0 {
		return decisionBlock, "COULD NOT CHECK: deadline reached", nil
	}
	originCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	same, why, err := repoidentity.SameOrigin(originCtx, projectDir, path)
	if err != nil {
		return decisionBlock, "origin of " + path + " cannot be determined (" + err.Error() + ")", nil
	}
	if same {
		return withoutRepo(pc, repo)
	}
	return decisionSkip, "skipped — outside this repo (git-common-dir " + dir + " vs " + ownDir + "; " + why + ")", nil
}

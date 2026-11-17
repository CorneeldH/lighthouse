package pusher

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// hookGuard turns off the two ways a git command can run code named in config: hooks and the
// fsmonitor hook. Every command of the pusher carries it, in the worktree and in the clone.
var hookGuard = []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false"}

// envAllow are the only variables of the process environment a git command of the pusher sees:
// never a GIT_ variable (a GIT_DIR or GIT_CONFIG_COUNT from outside would steer it) and never
// the forge token.
var envAllow = []string{"PATH", "HOME", "LANG"}

// gitEnv is the environment of every git command of the pusher: envAllow, the global and system
// config turned off, no prompt, plus extra.
func gitEnv(extra ...string) []string {
	var env []string
	for _, k := range envAllow {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	env = append(env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	return append(env, extra...)
}

// tokenEnv carries the forge token as an http.extraHeader through GIT_CONFIG_COUNT, so it
// stays out of argv; it goes into the Cmd.Env of one command only. No token, no header.
func tokenEnv(token string) []string {
	if token == "" {
		return nil
	}
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: basic " + basic}
}

// gitError is a git command that ran and exited non-zero.
type gitError struct {
	args   []string
	code   int
	stderr string
}

func (e *gitError) Error() string {
	reason := e.stderr
	if reason == "" {
		reason = fmt.Sprintf("exit %d", e.code)
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.args, " "), reason)
}

// gitWaitDelay bounds the wait for git's pipes after its kill.
const gitWaitDelay = 2 * time.Second

// runGit runs `git -C dir <hookGuard> args` with gitEnv(extra...) and returns stdout. The error
// names the args, so a caller never passes a secret in them.
func runGit(ctx context.Context, dir string, extra []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append(append([]string{"-C", dir}, hookGuard...), args...)...)
	cmd.Env = gitEnv(extra...)
	// After ctx ends git is killed; a child it left holding the pipes (a transport, a hook) must
	// not keep the pusher waiting.
	cmd.WaitDelay = gitWaitDelay
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", &gitError{args: args, code: exit.ExitCode(), stderr: strings.TrimSpace(stderr.String())}
		}
		return "", fmt.Errorf("git %s did not run: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// isAncestor reports whether a is an ancestor of b in the clone; an unknown object is an error.
func isAncestor(ctx context.Context, clone, a, b string) (bool, error) {
	_, err := runGit(ctx, clone, nil, "merge-base", "--is-ancestor", a, b)
	var ge *gitError
	if errors.As(err, &ge) && ge.code == 1 {
		return false, nil
	}
	return err == nil, err
}

// maxRead caps a file the pusher reads from the run's git-dir.
const maxRead = 1 << 20

// readSmall reads a regular file of at most maxRead bytes. It never follows a symlink (Lstat,
// and O_NOFOLLOW against a swap in between), and opens non-blocking, so a FIFO cannot hang the
// pusher while it holds the clone lock.
func readSmall(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|oNoFollow, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	text, err := io.ReadAll(io.LimitReader(f, maxRead+1))
	if err != nil {
		return nil, err
	}
	if len(text) > maxRead {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, maxRead)
	}
	return text, nil
}

// RemoteURL is the https URL of the repository in forge.repo. Only github.com is supported:
// another forge or a GitHub Enterprise host is an error that names the reason.
func RemoteURL(r repoconfig.Repo) (string, error) {
	if r.Kind != "github" || (r.Host != "" && r.Host != "github.com") {
		return "", fmt.Errorf("the workflow pusher supports only github.com, not %s on %q", r.Kind, r.Host)
	}
	owner, name, ok := strings.Cut(r.Slug, "/")
	if !ok || !slugPart(owner) || !slugPart(name) {
		return "", fmt.Errorf("forge.repo %q is not <owner>/<name>", r.Slug)
	}
	return "https://github.com/" + r.Slug + ".git", nil
}

// slugPartRe is one side of owner/name: GitHub's characters, never a leading dash or dot.
var slugPartRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)

func slugPart(s string) bool { return slugPartRe.MatchString(s) }

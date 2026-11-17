package pusher

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// worktreeGuard turns off the two ways a read in the run's worktree can run code from that
// worktree's config: hooks and the fsmonitor hook.
var worktreeGuard = []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false"}

// gitEnv is the environment of every git command of the pusher: the process environment
// without any GIT_ variable (a GIT_DIR, GIT_CONFIG_PARAMETERS or GIT_CONFIG_COUNT from outside
// would steer it), with the global and system config turned off and no prompt, plus extra.
func gitEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
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

// runGit runs `git -C dir args` with gitEnv(extra...) and returns stdout. The error names the
// args, so a caller never passes a secret in them.
func runGit(ctx context.Context, dir string, extra []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = gitEnv(extra...)
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

// worktreeGit runs one of the three reads the pusher does in the run's worktree, with hooks
// and fsmonitor off and no token in the environment.
func worktreeGit(ctx context.Context, worktree string, args ...string) (string, error) {
	return runGit(ctx, worktree, nil, append(append([]string{}, worktreeGuard...), args...)...)
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

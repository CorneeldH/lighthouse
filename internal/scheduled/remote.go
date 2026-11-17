package scheduled

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Remote is where the branch of a scheduled run goes. Both calls name the remote by an explicit
// URL, never the workplace's origin: the agent could have pointed origin elsewhere.
type Remote interface {
	// Head is the commit refs/heads/<branch> is at on the remote, "" when the remote lacks it.
	Head(ctx context.Context, dir, branch string) (string, error)
	// Push sets refs/heads/<branch> on the remote to commit, from the repository in dir.
	Push(ctx context.Context, dir, commit, branch string) error
}

// remoteTimeout bounds one ls-remote or push.
const remoteTimeout = 5 * time.Minute

// tokenVarName is the shape of an environment variable name the askpass script may read.
var tokenVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// HTTPSRemote is a Remote over HTTP(S) that authenticates with a forge token. Git runs with
// nothing of the machine's git config: no system or global config, no credential helper, no
// extra header, no hook, only the URL's own protocol. The token reaches git only through
// GIT_ASKPASS: a script, written fresh to ScriptDir before each call, prints the variable
// TokenVar, which the child gets from Getenv. So the token is never in an argument, a URL, a
// file or an error.
//
// Git still reads the config of the repository in dir (the workplace). The caller compares the
// workplace's GitState with the one recorded at the start before it calls Head or Push: a config
// the agent changed is refused before it can redirect the URL or run a helper.
type HTTPSRemote struct {
	URL       string              // http(s)://<host>/<owner>/<repo>.git, without credentials
	User      string              // the user name the forge takes with a token
	TokenVar  string              // the environment variable that holds the token
	ScriptDir string              // where the askpass script goes (the state dir)
	Getenv    func(string) string // the tick's environment
}

var _ Remote = HTTPSRemote{}

// Head implements Remote.
func (r HTTPSRemote) Head(ctx context.Context, dir, branch string) (string, error) {
	ref := "refs/heads/" + branch
	out, err := r.git(ctx, dir, "ls-remote", "--refs", "--", "{url}", ref)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if sha, name, ok := strings.Cut(line, "\t"); ok && name == ref {
			if !shaPattern.MatchString(sha) {
				return "", fmt.Errorf("ls-remote: %q is no commit sha", sha)
			}
			return sha, nil
		}
	}
	return "", nil
}

// Push implements Remote. It never forces: a branch that is already on the remote at another
// commit refuses the push.
func (r HTTPSRemote) Push(ctx context.Context, dir, commit, branch string) error {
	if !shaPattern.MatchString(commit) {
		return fmt.Errorf("push: %q is no commit sha", commit)
	}
	_, err := r.git(ctx, dir, "push", "--quiet", "--no-verify", "--", "{url}", commit+":refs/heads/"+branch)
	return err
}

// git runs git in dir against r.URL ("{url}" in args) under a clean environment. An error carries
// at most the head of git's stderr, with the token withheld should it ever appear there.
func (r HTTPSRemote) git(ctx context.Context, dir string, args ...string) (string, error) {
	u, err := url.Parse(r.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return "", errors.New("remote: the URL must be http(s)://<host>/<path>, without credentials")
	}
	if !tokenVarName.MatchString(r.TokenVar) {
		return "", fmt.Errorf("remote: %q is no environment variable name", r.TokenVar)
	}
	token := r.Getenv(r.TokenVar)
	if token == "" {
		return "", fmt.Errorf("remote: token variable %s is not set", r.TokenVar)
	}
	askpass, err := r.writeAskpass()
	if err != nil {
		return "", err
	}
	withUser := *u
	withUser.User = url.User(r.User)
	for i, a := range args {
		if a == "{url}" {
			args[i] = withUser.String()
		}
	}
	full := append([]string{"-C", dir,
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "core.fsmonitor=false",
		"-c", "credential.helper=",
		"-c", "http.extraHeader=",
		"-c", "protocol.allow=never",
		"-c", "protocol." + u.Scheme + ".allow=always",
	}, args...)
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = r.environ(askpass, token)
	cmd.Dir = r.ScriptDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(strings.ReplaceAll(stderr.String(), token, "(withheld)"))
		if len(text) > 200 {
			text = text[:200]
		}
		cause := strings.ReplaceAll(err.Error(), token, "(withheld)")
		if text != "" {
			return "", fmt.Errorf("git %s: %s (stderr: %s)", args[0], cause, text)
		}
		return "", fmt.Errorf("git %s: %s", args[0], cause)
	}
	return stdout.String(), nil
}

// environ is the whole environment of the git child: nothing of the tick's GIT_* variables,
// no system or global config, no terminal prompt, the askpass script and the token variable.
func (r HTTPSRemote) environ(askpass, token string) []string {
	env := []string{
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=" + os.DevNull,
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=" + askpass,
		"LC_ALL=C",
		r.TokenVar + "=" + token,
	}
	for _, k := range []string{"PATH", "HOME", "https_proxy", "HTTPS_PROXY", "http_proxy", "HTTP_PROXY", "no_proxy", "NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if v := r.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// writeAskpass writes the askpass script for r.TokenVar to ScriptDir, fresh on every call so a
// script someone else changed is never run, and returns its path. The script holds the name of
// the variable, never the token.
func (r HTTPSRemote) writeAskpass() (string, error) {
	if err := os.MkdirAll(r.ScriptDir, 0o700); err != nil {
		return "", fmt.Errorf("remote: askpass: %w", err)
	}
	tmp, err := os.CreateTemp(r.ScriptDir, ".askpass-*")
	if err != nil {
		return "", fmt.Errorf("remote: askpass: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	script := "#!/bin/sh\nprintf '%s\\n' \"$" + r.TokenVar + "\"\n"
	if _, err := tmp.WriteString(script); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("remote: askpass: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("remote: askpass: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o700); err != nil {
		return "", fmt.Errorf("remote: askpass: %w", err)
	}
	path := filepath.Join(r.ScriptDir, "askpass-"+r.TokenVar)
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", fmt.Errorf("remote: askpass: %w", err)
	}
	return path, nil
}

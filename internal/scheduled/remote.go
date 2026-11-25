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

	"github.com/CorneeldH/lighthouse/internal/gitpath"
)

// Remote is where the branch of a scheduled run goes. Both calls name the remote by an explicit
// URL, never the workplace's origin: the agent could have pointed origin elsewhere.
type Remote interface {
	// Head is the commit refs/heads/<branch> is at on the remote, "" when the remote lacks it.
	Head(ctx context.Context, dir, branch string) (string, error)
	// Push sets refs/heads/<branch> on the remote to commit, from the repository in dir.
	Push(ctx context.Context, dir, commit, branch string) error
}

// remoteTimeout bounds one git call of the remote. It is a var only so a test can shorten it;
// nothing else assigns it.
var remoteTimeout = 5 * time.Minute

// gitHardening are the config options every git call of the remote starts with: certificate
// checks stay on whatever a config says. No empty http.sslCAInfo, http.sslCAPath or http.proxy:
// git hands "" to curl, which then has no CA bundle or no proxy. environ already cuts the system
// and global config, and the temporary repository is fresh.
var gitHardening = []string{"-c", "http.sslVerify=true"}

var (
	// tokenVarName is the shape of an environment variable name the credential helper reads.
	tokenVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	// userName is the shape of the user name the credential helper prints.
	userName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// HTTPSRemote is a Remote over HTTP(S) that authenticates with a forge token.
//
// Git never reads the workplace's config, nor the machine's: every call runs in a temporary bare
// repository made for it under TempDir (git init with no template), removed afterwards, with no
// system or global config. For a push its objects/info/alternates points at the object
// directory of the workplace, so it holds the commit without a fetch. A config another agent of
// the same repository writes after the caller's GitState check (a URL rewrite, a hook path, a
// helper) therefore cannot reach the push.
//
// The token reaches git only as the environment variable TokenVar of the git child, which an
// inline credential helper prints; argv holds only the variable's name, and the helper answers
// only for the URL's own scheme and host. So the token is never in an argument, a URL, a file or
// an error, and no file of it exists for anyone to swap.
type HTTPSRemote struct {
	URL      string              // http(s)://<host>/<owner>/<repo>.git, without credentials
	User     string              // the user name the forge takes with a token
	TokenVar string              // the environment variable that holds the token
	TempDir  string              // where the temporary repositories go (the state dir)
	Getenv   func(string) string // the tick's environment
}

var _ Remote = HTTPSRemote{}

// Head implements Remote.
func (r HTTPSRemote) Head(ctx context.Context, dir, branch string) (string, error) {
	ref := "refs/heads/" + branch
	out, err := r.git(ctx, "", "ls-remote", "--refs", "--", "{url}", ref)
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
	objects, err := r.objectDir(ctx, dir)
	if err != nil {
		return err
	}
	_, err = r.git(ctx, objects, "push", "--quiet", "--no-verify", "--", "{url}", commit+":refs/heads/"+branch)
	return err
}

// objectDir is the object directory of the repository in dir: <common dir>/objects. git rev-parse
// runs nothing the repository's config could name.
func (r HTTPSRemote) objectDir(ctx context.Context, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	args := append(append([]string{}, gitHardening...), "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = r.environ("")
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("push: common dir of the workplace: %w", err)
	}
	common := strings.TrimSuffix(string(out), "\n")
	if !filepath.IsAbs(common) || strings.ContainsAny(common, "\n\r") {
		return "", errors.New("push: the workplace has no usable common dir")
	}
	return filepath.Join(gitpath.Native(common), "objects"), nil
}

// git runs git against r.URL ("{url}" in args) in a fresh temporary bare repository whose
// alternates is objects ("" for none). An error carries at most the head of git's stderr, with
// the token withheld should it ever appear there.
func (r HTTPSRemote) git(ctx context.Context, objects string, args ...string) (string, error) {
	u, err := url.Parse(r.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return "", errors.New("remote: the URL must be http(s)://<host>/<path>, without credentials")
	}
	if !tokenVarName.MatchString(r.TokenVar) {
		return "", fmt.Errorf("remote: %q is no environment variable name", r.TokenVar)
	}
	if !userName.MatchString(r.User) {
		return "", fmt.Errorf("remote: %q is no user name", r.User)
	}
	token := r.Getenv(r.TokenVar)
	if token == "" {
		return "", fmt.Errorf("remote: token variable %s is not set", r.TokenVar)
	}
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	repo, err := r.scratch(ctx, objects)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(repo) }()

	for i, a := range args {
		if a == "{url}" {
			args[i] = u.String()
		}
	}
	origin := u.Scheme + "://" + u.Host
	helper := "!f() { test \"$1\" = get || exit 0; echo username=" + r.User + "; echo \"password=$" + r.TokenVar + "\"; }; f"
	full := append(append([]string{"--git-dir=" + repo}, gitHardening...), []string{
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "credential.helper=",
		"-c", "credential." + origin + ".helper=" + helper,
		"-c", "http.extraHeader=",
		"-c", "protocol.allow=never",
		"-c", "protocol." + u.Scheme + ".allow=always",
	}...)
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = r.environ(token)
	cmd.Dir = repo
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

// scratch makes an empty bare repository under TempDir (0700, git's defaults, no template) and,
// with objects set, points its alternates at objects. The caller removes it.
func (r HTTPSRemote) scratch(ctx context.Context, objects string) (string, error) {
	if err := os.MkdirAll(r.TempDir, 0o700); err != nil {
		return "", fmt.Errorf("remote: temporary repository: %w", err)
	}
	if err := checkStateDir(r.TempDir); err != nil {
		return "", err
	}
	repo, err := os.MkdirTemp(r.TempDir, "push-")
	if err != nil {
		return "", fmt.Errorf("remote: temporary repository: %w", err)
	}
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, gitHardening...), "init", "--quiet", "--bare", "--template=", repo)...)
	cmd.Env = r.environ("")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(repo)
		return "", fmt.Errorf("remote: temporary repository: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if objects != "" {
		info := filepath.Join(repo, "objects", "info")
		if err := os.MkdirAll(info, 0o700); err != nil {
			_ = os.RemoveAll(repo)
			return "", fmt.Errorf("remote: temporary repository: %w", err)
		}
		if err := os.WriteFile(filepath.Join(info, "alternates"), []byte(objects+"\n"), 0o600); err != nil {
			_ = os.RemoveAll(repo)
			return "", fmt.Errorf("remote: temporary repository: %w", err)
		}
	}
	return repo, nil
}

// environ is the whole environment of a git child: nothing of the tick's GIT_* variables, no
// system or global config, no terminal prompt, and with token the token variable.
func (r HTTPSRemote) environ(token string) []string {
	env := []string{
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=" + os.DevNull,
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
	}
	if token != "" {
		env = append(env, r.TokenVar+"="+token)
	}
	for _, k := range []string{"PATH", "HOME", "https_proxy", "HTTPS_PROXY", "http_proxy", "HTTP_PROXY", "no_proxy", "NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR", "GIT_SSL_CAINFO"} {
		if v := r.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// checkStateDir refuses a state directory that is a symlink, is not ours or can be written by
// anyone else: another user could swap a temporary repository under it for their own.
func checkStateDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("remote: state directory: %w", err)
	}
	if !fi.IsDir() {
		return errors.New("remote: the state directory is not a plain directory")
	}
	return checkOwner(fi, os.Getuid())
}

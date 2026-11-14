// Package repoidentity tells whether a tree is a checkout of this project's repo beyond
// `--git-common-dir`: the same `origin` host and path, whatever the form (https, ssh, scp), is the
// same repo. A local path or `file://` remote says nothing about the upstream and never matches:
// precision before recall. It ports pack/core/lib/repo_identity.py.
package repoidentity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// gitTimeout bounds one `git config` call (GIT_TIMEOUT_S of the Python).
const gitTimeout = 10 * time.Second

// scpRe is `[user@]host:path` without a scheme; a `/` before the `:` makes it a local path.
var scpRe = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+)$`)

// OriginError reports that `remote.origin.url` could not be read: git failed or timed out.
type OriginError struct{ msg string }

func (e *OriginError) Error() string { return e.msg }

// pyStrip is Python's str.strip(): it strips Unicode whitespace.
func pyStrip(s string) string { return strings.TrimSpace(s) }

// RemoteKey is `host/owner/repo` (lower case) of a remote URL; ok is false without host and path.
func RemoteKey(url string) (key string, ok bool) {
	rest := pyStrip(url)
	var host, path string
	if scheme, after, found := strings.Cut(rest, "://"); found {
		if strings.EqualFold(scheme, "file") {
			return "", false
		}
		authority, p, _ := strings.Cut(after, "/")
		path = p
		host, _, _ = strings.Cut(authority[strings.LastIndex(authority, "@")+1:], ":")
	} else {
		m := scpRe.FindStringSubmatch(rest)
		if m == nil {
			return "", false
		}
		host, path = m[1], m[2]
	}
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")
	if host == "" || path == "" {
		return "", false
	}
	return strings.ToLower(host + "/" + path), true
}

// Origin is `remote.origin.url` of root; set is false when it is not set. Any other failure is an
// *OriginError.
func Origin(ctx context.Context, root string) (url string, set bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "config", "--get", "remote.origin.url")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
		return pyStrip(stdout.String()), true, nil
	case ctx.Err() != nil:
		// Before the ExitError case: the context kills git, which then looks like a failed exit.
		return "", false, &OriginError{"git timeout reading the origin of " + root}
	case errors.As(runErr, &exitErr):
		msg := pyStrip(stderr.String())
		if exitErr.ExitCode() == 1 && msg == "" {
			return "", false, nil
		}
		return "", false, &OriginError{fmt.Sprintf("git failed (rc %d): %s", exitErr.ExitCode(), msg)}
	default:
		return "", false, &OriginError{runErr.Error()}
	}
}

// SameOrigin reports whether root has the same origin as projectDir. Otherwise reason is
// "origin <root's> vs <project's>", `none` for a missing one. An *OriginError comes back when git
// fails for either.
func SameOrigin(ctx context.Context, projectDir, root string) (same bool, reason string, err error) {
	ours, _, err := Origin(ctx, projectDir)
	if err != nil {
		return false, "", err
	}
	theirs, _, err := Origin(ctx, root)
	if err != nil {
		return false, "", err
	}
	if key, ok := RemoteKey(ours); ok && theirs != "" {
		if other, ok := RemoteKey(theirs); ok && other == key {
			return true, "", nil
		}
	}
	return false, "origin " + orNone(theirs) + " vs " + orNone(ours), nil
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// Package datadir says where the review records live. They carry session content, so the
// directory must never be inside a git work tree.
package datadir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrInRepo is returned when the data directory is inside a git work tree.
var ErrInRepo = errors.New("data directory is inside a git work tree")

// Dir is $LIGHTHOUSE_DATA, else ${XDG_DATA_HOME:-~/.local/share}/lighthouse/review-records. It
// creates the directory. A relative XDG_DATA_HOME is ignored, as the XDG base directory spec
// says. getenv is os.Getenv in production.
func Dir(getenv func(string) string) (string, error) {
	var path string
	if raw := getenv("LIGHTHOUSE_DATA"); raw != "" {
		expanded, err := expandUser(raw)
		if err != nil {
			return "", err
		}
		path = expanded
	} else {
		base := getenv("XDG_DATA_HOME")
		if !filepath.IsAbs(base) {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("home directory: %w", err)
			}
			base = filepath.Join(home, ".local", "share")
		}
		path = filepath.Join(base, "lighthouse", "review-records")
	}
	path, err := resolve(path)
	if err != nil {
		return "", err
	}
	if inGitTree(path) {
		return "", fmt.Errorf("%w: %s", ErrInRepo, path)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func expandUser(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home directory: %w", err)
	}
	return filepath.Join(home, path[1:]), nil
}

// resolve makes path absolute and follows symlinks in its longest existing prefix, so a path
// that does not exist yet still resolves like Path.resolve() does.
func resolve(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	existing, rest := abs, ""
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return abs, nil
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	return filepath.Join(real, rest), nil
}

func inGitTree(path string) bool {
	for p := path; ; p = filepath.Dir(p) {
		if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
			return true
		}
		if filepath.Dir(p) == p {
			return false
		}
	}
}

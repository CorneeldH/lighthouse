// Package levellog is the logbook of the stand `log`: one JSON line per report a component would
// have made, in a file per shared repository, outside the tracked files. Hooks write it without
// reading lighthouse.toml.
package levellog

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// entry is one line of the logbook.
type entry struct {
	Component string    `json:"component"`
	At        time.Time `json:"at"`
	Line      string    `json:"line"`
}

// Dir is $XDG_STATE_HOME/lighthouse/level-log, else %LOCALAPPDATA% on Windows, else
// $HOME/.local/state/lighthouse/level-log. A base that is not absolute is skipped; when none is
// absolute it is an error. It does not create the directory.
func Dir() (string, error) {
	var bases []string
	bases = append(bases, os.Getenv("XDG_STATE_HOME"))
	if runtime.GOOS == "windows" {
		bases = append(bases, os.Getenv("LOCALAPPDATA"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		bases = append(bases, filepath.Join(home, ".local", "state"))
	}
	for _, base := range bases {
		if filepath.IsAbs(base) {
			return filepath.Join(base, "lighthouse", "level-log"), nil
		}
	}
	return "", errors.New("no absolute state directory: set XDG_STATE_HOME")
}

// logPath is the logbook file of the repository root belongs to. The key is the first 16 hex
// characters of the sha256 of the absolute `git rev-parse --git-common-dir`, so a worktree and
// the main checkout share a file. A relative answer (`.git`) is made absolute against root.
func logPath(root string) (string, error) {
	out, err := exec.Command("git", "--no-optional-locks", "-C", root, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return "", fmt.Errorf("git common dir of %s: %w", root, err)
	}
	common := strings.TrimSpace(string(out))
	if common == "" {
		return "", fmt.Errorf("git common dir of %s: empty answer", root)
	}
	if !filepath.IsAbs(common) {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", err
		}
		common = filepath.Join(abs, common)
	}
	common = filepath.Clean(common)
	if resolved, err := filepath.EvalSymlinks(common); err == nil {
		common = resolved
	}
	sum := sha256.Sum256([]byte(common))
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, hex.EncodeToString(sum[:])[:16]+".jsonl"), nil
}

// Append writes one line to the logbook of the repository at root: the component, the time and
// the first line of its message. Any failure is returned; the caller decides what it means.
func Append(root, component, firstLine string, at time.Time) error {
	path, err := logPath(root)
	if err != nil {
		return err
	}
	data, err := json.Marshal(entry{Component: component, At: at, Line: firstLine})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	// One write of one short line: O_APPEND keeps concurrent hooks from interleaving.
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Count is how often each component logged at or after since. No logbook yet is an empty map.
// A line that does not parse is skipped: the logbook is only a measurement.
func Count(root string, since time.Time) (map[string]int, error) {
	counts := map[string]int{}
	path, err := logPath(root)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return counts, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e entry
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Component == "" || e.At.Before(since) {
			continue
		}
		counts[e.Component]++
	}
	return counts, sc.Err()
}

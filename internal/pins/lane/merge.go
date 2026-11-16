package lane

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/pins"
)

// workflowsDir is where a push needs the `workflows` permission, which the lane does not get.
const workflowsDir = ".github/workflows/"

// MergeResult is the outcome of the 3-way merge of one source pin. Paths are relative to the
// repository root, with forward slashes.
type MergeResult struct {
	// Clean is true when no file conflicts.
	Clean bool
	// Conflicts are the local files the merge cannot settle, sorted.
	Conflicts []string
	// TouchesWorkflows is true when Files or Removed hold a path under .github/workflows/.
	TouchesWorkflows bool
	// Files are the new contents of the local files that change, new upstream files included.
	Files map[string][]byte
	// Removed are the local files whose upstream counterpart vanished, sorted; each is also a
	// conflict.
	Removed []string
}

// MergeSource merges the upstream change of a source pin into the files under root, per file
// with `git merge-file`: base is the file at the pin, ours the local file, theirs the file at
// the upstream HEAD. For a skill the upstream SKILL.md goes to every variant SKILL.md and every
// other file to the same relative path in the skill directory; an `upstream-path` that is a
// directory is merged per file in it. A file new upstream is taken over, a file that vanished
// upstream is a conflict, and so is binary content or a merge-file that does not exit 0.
// Nothing is written; PrepareBranch writes the result.
func MergeSource(ctx context.Context, root string, p pins.SourcePin, c pins.Clone) (MergeResult, error) {
	head, err := c.Head()
	if err != nil {
		return MergeResult{}, err
	}
	baseFiles, err := c.ListTree(p.Commit, p.Path)
	if err != nil {
		return MergeResult{}, err
	}
	headFiles, err := c.ListTree(head, p.Path)
	if err != nil {
		return MergeResult{}, err
	}
	targets, err := targetMap(p, baseFiles, headFiles)
	if err != nil {
		return MergeResult{}, err
	}
	m := merger{ctx: ctx, root: root, res: MergeResult{Files: map[string][]byte{}}}
	ups := make([]string, 0, len(targets))
	for up := range targets {
		ups = append(ups, up)
	}
	sort.Strings(ups)
	for _, up := range ups {
		base, err := readUpstream(c, p.Commit, up, baseFiles)
		if err != nil {
			return MergeResult{}, err
		}
		theirs, err := readUpstream(c, head, up, headFiles)
		if err != nil {
			return MergeResult{}, err
		}
		for _, local := range targets[up] {
			if err := m.mergeOne(local, base, theirs); err != nil {
				return MergeResult{}, err
			}
		}
	}
	sort.Strings(m.res.Conflicts)
	sort.Strings(m.res.Removed)
	m.res.Clean = len(m.res.Conflicts) == 0
	for f := range m.res.Files {
		m.res.TouchesWorkflows = m.res.TouchesWorkflows || strings.HasPrefix(f, workflowsDir)
	}
	for _, f := range m.res.Removed {
		m.res.TouchesWorkflows = m.res.TouchesWorkflows || strings.HasPrefix(f, workflowsDir)
	}
	return m.res, nil
}

// isFilePin tells a file of SOURCES.toml (key = its path) from a skill directory.
func isFilePin(p pins.SourcePin) bool { return len(p.Files) == 1 && p.Files[0] == p.Key }

// targetMap maps every upstream file at the pin or at HEAD to the local files it is merged
// into.
func targetMap(p pins.SourcePin, baseFiles, headFiles []string) (map[string][]string, error) {
	isFile := len(baseFiles) == 1 && baseFiles[0] == p.Path
	out := map[string][]string{}
	if isFilePin(p) {
		if !isFile {
			return nil, fmt.Errorf("file pin %s: source_path %s is not a single file", p.Key, p.Path)
		}
		out[p.Path] = []string{p.Key}
		return out, nil
	}
	if isFile {
		out[p.Path] = p.Files
		return out, nil
	}
	skillDir := "pack/" + p.Key
	for _, up := range append(append([]string{}, baseFiles...), headFiles...) {
		rel := strings.TrimPrefix(up, p.Path+"/")
		if rel == "SKILL.md" {
			out[up] = p.Files
		} else {
			out[up] = []string{path.Join(skillDir, rel)}
		}
	}
	return out, nil
}

// readUpstream returns the file at sha, or nil when it is not in listed.
func readUpstream(c pins.Clone, sha, file string, listed []string) ([]byte, error) {
	i := sort.SearchStrings(listed, file)
	if i == len(listed) || listed[i] != file {
		return nil, nil
	}
	return c.ReadFile(sha, file)
}

type merger struct {
	ctx  context.Context
	root string
	res  MergeResult
}

func (m *merger) conflict(local string) { m.res.Conflicts = append(m.res.Conflicts, local) }

// mergeOne settles one local file; a nil base or theirs means the file is absent there.
func (m *merger) mergeOne(local string, base, theirs []byte) error {
	ours, err := os.ReadFile(filepath.Join(m.root, filepath.FromSlash(local)))
	oursMissing := errors.Is(err, fs.ErrNotExist)
	if err != nil && !oursMissing {
		return err
	}
	switch {
	case base != nil && theirs != nil && bytes.Equal(base, theirs):
		return nil
	case theirs == nil: // vanished upstream
		if !oursMissing {
			m.conflict(local)
			m.res.Removed = append(m.res.Removed, local)
		}
		return nil
	case oursMissing && base == nil: // new upstream
		if isBinary(theirs) {
			m.conflict(local)
		} else {
			m.res.Files[local] = theirs
		}
		return nil
	case oursMissing: // changed upstream, gone locally
		m.conflict(local)
		return nil
	case base == nil: // new upstream where a local file already is
		if !bytes.Equal(ours, theirs) {
			m.conflict(local)
		}
		return nil
	}
	if isBinary(base) || isBinary(ours) || isBinary(theirs) {
		m.conflict(local)
		return nil
	}
	merged, clean, err := mergeFile(m.ctx, ours, base, theirs)
	if err != nil {
		return fmt.Errorf("merge %s: %w", local, err)
	}
	if !clean {
		m.conflict(local)
		return nil
	}
	if !bytes.Equal(merged, ours) {
		m.res.Files[local] = merged
	}
	return nil
}

func isBinary(b []byte) bool { return bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b) }

// mergeFile runs `git merge-file -p`; clean is false on any non-zero exit (the number of
// conflicts, or a refusal).
func mergeFile(ctx context.Context, ours, base, theirs []byte) (merged []byte, clean bool, err error) {
	dir, err := os.MkdirTemp("", "lighthouse-merge-*")
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	names := []string{"ours", "base", "theirs"}
	for i, b := range [][]byte{ours, base, theirs} {
		if err := os.WriteFile(filepath.Join(dir, names[i]), b, 0o600); err != nil {
			return nil, false, err
		}
	}
	out, err := laneGit(ctx, dir, "merge-file", "-p", "ours", "base", "theirs")
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// Identity of every lane commit: the bot that cla-signatures.json lists for workflow commits.
const (
	botName  = "github-actions[bot]"
	botEmail = "41898282+github-actions[bot]@users.noreply.github.com"
)

// keptGitEnv are the GIT_ variables a lane git call keeps: where the config is and how to ask
// for credentials. Every other one (GIT_DIR, GIT_INDEX_FILE, an author, injected config) would
// point the call elsewhere.
var keptGitEnv = []string{"GIT_CONFIG_GLOBAL=", "GIT_CONFIG_NOSYSTEM=", "GIT_CONFIG_SYSTEM=", "GIT_ASKPASS=", "GIT_SSH=", "GIT_SSH_COMMAND="}

func laneEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_") || hasAnyPrefix(e, keptGitEnv) {
			env = append(env, e)
		}
	}
	return append(env, "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME="+botName, "GIT_AUTHOR_EMAIL="+botEmail,
		"GIT_COMMITTER_NAME="+botName, "GIT_COMMITTER_EMAIL="+botEmail)
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// laneGit runs git in dir as the bot, without hooks or signing from any config, and returns
// stdout. An exit error carries stderr in its message and stays an *exec.ExitError for As.
func laneGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	full := append([]string{
		"-c", "core.hooksPath=" + os.DevNull, "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false",
		"-c", "protocol.ext.allow=never",
	}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
	cmd.Env = laneEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, &gitError{args: args, err: err, stderr: strings.TrimSpace(stderr.String())}
	}
	return out, nil
}

type gitError struct {
	args   []string
	err    error
	stderr string
}

func (e *gitError) Error() string {
	return fmt.Sprintf("git %s: %v: %s", strings.Join(e.args, " "), e.err, e.stderr)
}

func (e *gitError) Unwrap() error { return e.err }

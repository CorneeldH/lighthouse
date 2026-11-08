package hooks

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/hook"
)

// The fixed messages of worktree-check, word for word those of the bash hook it replaces.
const (
	worktreeMissingGit = "Worktree check: COULD NOT CHECK — missing tooling: git. " +
		"Nothing was checked, so this is no sign that the write is in a worktree."
	worktreeUncheckableTail = "\n" +
		"\n" +
		"Nothing was checked, so this is no sign that the write is in a worktree.\n" +
		"Convention: work that produces a commit starts in a worktree on its own branch\n" +
		"(git worktree add .claude/worktrees/<name> -b issue-<nr>-<slug>)."
)

// loopCheckAfter is the number of symbolic links canonMissing follows before it checks for a
// loop, as gnulib's canonicalize does.
const loopCheckAfter = 20

// WorktreeCheck warns at a write into the main checkout while it is on the default branch:
// `forge.default_branch`, `main` when it is not set. It checks the target file of the write, not
// the cwd. It never blocks: a warning, and a check it cannot make, go out as a systemMessage with
// exit 0. Only "not a git repository" stays silent.
func WorktreeCheck(ctx context.Context, _, _ string, p hook.Payload) hook.Result {
	if _, err := exec.LookPath("git"); err != nil {
		return systemMessage(worktreeMissingGit)
	}
	filePath, cwd, ok := readWritePayload(p.Raw)
	if !ok {
		return warnUncheckable("the payload is not valid JSON.")
	}
	if filePath == "" {
		return hook.Result{}
	}

	abs := filePath
	if !strings.HasPrefix(filePath, "/") {
		if cwd == "" {
			// When Getwd fails cwd stays "", as an unset $PWD in bash.
			cwd, _ = os.Getwd()
		}
		abs = cwd + "/" + filePath
	}
	canon, err := canonMissing(abs)
	if err != nil {
		return warnUncheckable("cannot resolve '" + abs + "': " + err.Error() + ".")
	}
	abs = canon

	// The nearest directory that exists: the target of a Write does not have to.
	dir := filepath.Dir(abs)
	for !isDir(dir) && dir != "/" {
		dir = filepath.Dir(dir)
	}

	// A linked worktree has its own --git-dir but shares --git-common-dir.
	rawGitDir, err := gitCombined(ctx, dir, true, "rev-parse", "--git-dir")
	if err != nil {
		if strings.Contains(rawGitDir, "not a git repository") {
			return hook.Result{}
		}
		return warnUncheckable("git could not read '" + dir + "': " + orNoMessage(rawGitDir) + ".")
	}
	rawGitCommon, err := gitCombined(ctx, dir, true, "rev-parse", "--git-common-dir")
	if err != nil {
		return warnUncheckable("git could not read '" + dir + "': " + orNoMessage(rawGitCommon) + ".")
	}
	gitDir, okDir := physicalDir(dir, rawGitDir)
	gitCommon, okCommon := physicalDir(dir, rawGitCommon)
	if !okDir || !okCommon {
		return warnUncheckable("cannot resolve the git directories of '" + dir + "'.")
	}
	if gitDir != gitCommon {
		return hook.Result{}
	}

	branch, err := gitCombined(ctx, dir, false, "branch", "--show-current")
	if err != nil {
		return warnUncheckable("git could not determine the branch of '" + dir + "': " + orNoMessage(branch) + ".")
	}
	protected, errText, ok := defaultBranch()
	if !ok {
		return warnUncheckable("the protected branch could not be read: " + orNoMessage(strings.TrimRight(errText, "\n")) + ".")
	}
	if branch != protected {
		return hook.Result{}
	}

	return systemMessage("Worktree check: this writes into the main checkout while it is on '" + protected + "'.\n" +
		"\n" +
		"  " + abs + "\n" +
		"\n" +
		"Convention: work that produces a commit starts in a worktree on its own\n" +
		"branch (git worktree add .claude/worktrees/<name> -b issue-<nr>-<slug>).\n" +
		"Exception: read-only work, or an explicit 'on main' from the user.\n" +
		"\n" +
		"Note: committing on '" + protected + "' is blocked (main-commit-gate.sh). Move\n" +
		"this work now, not later.")
}

// readWritePayload reads `.tool_input.file_path` and `.cwd` the way `jq -r '.x // empty'` does.
// ok is false when jq could not index the payload.
func readWritePayload(raw []byte) (filePath, cwd string, ok bool) {
	top, ok := jqFields(raw)
	if !ok {
		return "", "", false
	}
	toolInput, ok := jqFields(top["tool_input"])
	if !ok {
		return "", "", false
	}
	return jqText(toolInput["file_path"]), jqText(top["cwd"]), true
}

// canonMissing is GNU `realpath -m path`: the components are walked from left to right; a
// symbolic link is replaced by its target, `..` takes the parent of what is resolved so far, and
// a component that does not exist is appended as it stands. A loop is no error either: as in
// gnulib, after loopCheckAfter links a link met twice with the same remaining path stays as it
// stands. The only error is a relative path without a working directory.
func canonMissing(path string) (string, error) {
	if !filepath.IsAbs(path) {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		path = wd + "/" + path
	}
	rest := strings.Split(path, "/")
	resolved := "/"
	links := 0
	seen := map[string][]os.FileInfo{}
	for len(rest) > 0 {
		part := rest[0]
		remaining := strings.Join(rest, "/")
		rest = rest[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			resolved = filepath.Dir(resolved)
			continue
		}
		next := filepath.Join(resolved, part)
		resolved = next
		info, err := os.Lstat(next)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		target, err := os.Readlink(next)
		if err != nil {
			continue
		}
		if links < loopCheckAfter {
			links++
		} else if seenBefore(seen, remaining, info) {
			continue
		}
		resolved = filepath.Dir(next)
		if filepath.IsAbs(target) {
			resolved = "/"
		}
		rest = append(strings.Split(target, "/"), rest...)
	}
	return resolved, nil
}

// seenBefore reports whether the link info was met before with the same remaining path, and
// records it when not: gnulib's loop check in canonicalize.c.
func seenBefore(seen map[string][]os.FileInfo, remaining string, info os.FileInfo) bool {
	for _, other := range seen[remaining] {
		if os.SameFile(other, info) {
			return true
		}
	}
	seen[remaining] = append(seen[remaining], info)
	return false
}

// gitCombined runs git in dir, with LC_ALL=C when cLocale is set, and returns stdout and stderr
// together without trailing newlines, as `$(cd dir && git … 2>&1)` does. A git that does not
// start (dir unreachable) has no message, as the failing `cd 2>/dev/null` in bash had none.
func gitCombined(ctx context.Context, dir string, cLocale bool, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if cLocale {
		cmd.Env = append(os.Environ(), "LC_ALL=C")
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return "", err
	}
	return strings.TrimRight(out.String(), "\n"), err
}

// physicalDir is `cd dir && cd raw && pwd -P`: raw as git printed it, relative to dir unless it
// is absolute, with every symbolic link resolved. ok is false when it is no directory.
func physicalDir(dir, raw string) (string, bool) {
	if !filepath.IsAbs(raw) {
		raw = filepath.Join(dir, raw)
	}
	phys, err := filepath.EvalSymlinks(raw)
	if err != nil || !isDir(phys) {
		return "", false
	}
	return phys, true
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func warnUncheckable(reason string) hook.Result {
	return systemMessage("Worktree check: COULD NOT CHECK — " + reason + worktreeUncheckableTail)
}

func systemMessage(msg string) hook.Result {
	return hook.Result{Output: map[string]any{"systemMessage": msg}}
}

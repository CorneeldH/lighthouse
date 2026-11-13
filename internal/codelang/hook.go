package codelang

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/hook"
)

// underGatesEnv is set when bash-write-check calls this hook for a file a Bash call wrote.
const underGatesEnv = "LIGHTHOUSE_UNDER_BASH_WRITE_CHECK"

func init() { hook.Register("code-language-check", runHook) }

// runHook is the PostToolUse hook code-language-check: code must be written in English.
// Findings block (2, the report on stderr). A notice alone never blocks: JSON for the user
// and the agent, or plain text under bash-write-check, which passes it on. It is not
// fail-open: a payload, path or git it cannot read is COULD NOT CHECK (2), never a silent
// pass. The config comes from the git root of the written file; only the layer 2 chain comes
// from the session's own lighthouse.toml (LIGHTHOUSE_TOML, else the one at root).
func runHook(ctx context.Context, root, _ string, p hook.Payload) hook.Result {
	file, cwd, ok := payloadPaths(p.Raw)
	if !ok {
		return blocked("the payload is not valid JSON.", "")
	}
	if file == "" {
		return hook.Result{}
	}
	abs := file
	if !strings.HasPrefix(file, "/") {
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		abs = cwd + "/" + file
	}
	path, err := canonical(abs)
	if err != nil {
		return blocked("cannot resolve '"+abs+"' (no 'realpath -m' and no python3).", "")
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return hook.Result{}
	}
	dir := filepath.Dir(path)
	repo, res, done := gitRoot(dir)
	if done {
		return res
	}
	sessionTOML := os.Getenv("LIGHTHOUSE_TOML")
	if sessionTOML == "" && root != "" {
		sessionTOML = filepath.Join(root, "lighthouse.toml")
	} else if sessionTOML == "" {
		if sessionTOML, err = config.Path(); err != nil {
			return checkerFailed(err)
		}
	}
	in, err := Check(DefaultScorer, path, repo, sessionTOML)
	if err != nil {
		return checkerFailed(err)
	}
	found, note := Resolve(ctx, in, path, os.Getenv(underGatesEnv) != "", DefaultStateDir())
	switch {
	case len(found) > 0:
		report := Report(path, found)
		if note != "" {
			report += "\n\n" + note
		}
		return hook.Result{Code: 2, Stderr: report + "\n"}
	case note == "":
		return hook.Result{}
	case os.Getenv(underGatesEnv) != "":
		return hook.Result{Text: note + "\n"}
	}
	return hook.Result{Output: map[string]any{
		"systemMessage": note,
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PostToolUse", "additionalContext": note,
		},
	}}
}

// blocked is cannot_check() of the shell hook: nothing was checked, with an optional detail.
func blocked(reason, detail string) hook.Result {
	s := "code-language-check: COULD NOT CHECK — " + reason + "\n\n" +
		"This hook checks that code is written in English.\n" +
		"Nothing was checked; that is not a green light.\n"
	if detail != "" {
		s += "\n" + detail + "\n"
	}
	return hook.Result{Code: 2, Stderr: s}
}

// checkerFailed is a COULD NOT CHECK of the checker: one line with the reason.
func checkerFailed(err error) hook.Result {
	return hook.Result{Code: 2, Stderr: "code-language-check: COULD NOT CHECK — " + err.Error() + "\n"}
}

// payloadPaths reads tool_input.file_path and cwd as `jq -r '.x // empty'` does: absent, null
// or false is "". ok is false where jq fails: no JSON, or a value that is no object where a
// field is read. An empty payload is no input at all, and so no path.
func payloadPaths(raw []byte) (file, cwd string, ok bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", "", true
	}
	var top any
	if err := json.Unmarshal(raw, &top); err != nil {
		return "", "", false
	}
	input, ok := field(top, "tool_input")
	if !ok {
		return "", "", false
	}
	fileValue, ok := field(input, "file_path")
	if !ok {
		return "", "", false
	}
	cwdValue, _ := field(top, "cwd")
	return text(fileValue), text(cwdValue), true
}

// field is .name of v: nil for a null v, false when v is no object.
func field(v any, name string) (any, bool) {
	switch o := v.(type) {
	case nil:
		return nil, true
	case map[string]any:
		return o[name], true
	}
	return nil, false
}

// text is v as `jq -r` prints it, "" for null and false.
func text(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case bool:
		if !s {
			return ""
		}
		return "true"
	case string:
		return s
	}
	out, _ := json.Marshal(v)
	return string(out)
}

// canonical is `realpath -m`: the absolute path with the symlinks of its longest existing
// part resolved, and the rest, which need not exist, after it.
func canonical(abs string) (string, error) {
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	head, rest := abs, ""
	for {
		resolved, err := filepath.EvalSymlinks(head)
		if err == nil {
			return filepath.Join(resolved, rest), nil
		}
		if !errors.Is(err, fs.ErrNotExist) || head == filepath.Dir(head) {
			return "", err
		}
		rest = filepath.Join(filepath.Base(head), rest)
		head = filepath.Dir(head)
	}
}

// gitRoot is the top level of the repository that holds dir. done is true when the hook ends
// here: outside a repository silently, after any other git failure with COULD NOT CHECK.
func gitRoot(dir string) (root string, res hook.Result, done bool) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	// LC_ALL=C: git translates the text matched here.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	raw, err := cmd.CombinedOutput()
	out := strings.TrimRight(string(raw), "\n")
	var exit *exec.ExitError
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return "", blocked("missing tooling: git", ""), true
	case err != nil && !errors.As(err, &exit):
		return "", blocked("git could not read '"+dir+"'.", err.Error()), true
	case err != nil && strings.Contains(out, "not a git repository"):
		return "", hook.Result{}, true
	case err != nil:
		return "", blocked("git could not read '"+dir+"'.", out), true
	case out == "":
		return "", blocked("git gave no repository root for '"+dir+"'.", ""), true
	}
	return out, hook.Result{}, false
}

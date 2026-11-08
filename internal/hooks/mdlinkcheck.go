package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/hook"
	"github.com/CorneeldH/lighthouse/internal/mdlinks"
)

func init() { hook.Register("md-link-check", mdLinkCheck) }

var problemReasons = map[string]string{
	"dead":     "does not exist, resolved against the directory of the file",
	"absolute": "an absolute path is dead on the rendered site and as soon as the worktree is gone; use a relative path",
}

// cannotCheck blocks with COULD NOT CHECK on the first line: nothing was checked, and a silent
// pass would read as "all links are fine". detail, when set, is a paragraph of its own.
func cannotCheck(prefix, reason, detail string) hook.Result {
	s := prefix + "Linkcheck: COULD NOT CHECK — " + reason + "\n\n" +
		"This hook checks that markdown links are relative and exist.\n" +
		"Nothing was checked; that is not a green light.\n"
	if detail != "" {
		s += "\n" + detail + "\n"
	}
	return hook.Result{Code: 2, Stderr: s}
}

// mdLinkCheck blocks (code 2) a written markdown file with a relative link that points nowhere or
// an absolute link, resolved against the root of the file's own git repository. A file outside
// every repository falls outside the check.
func mdLinkCheck(_ context.Context, _ string, _ string, p hook.Payload) hook.Result {
	git, err := exec.LookPath("git")
	if err != nil {
		return cannotCheck("", "missing tooling: git", "")
	}
	filePath, cwd, ok := linkTarget(p.Raw)
	if !ok {
		return cannotCheck("", "the payload is not valid JSON.", "")
	}
	if !strings.HasSuffix(filePath, ".md") && !strings.HasSuffix(filePath, ".markdown") {
		return hook.Result{}
	}

	abs := filePath
	if !filepath.IsAbs(abs) {
		base := cwd
		if base == "" {
			if base, err = os.Getwd(); err != nil {
				return cannotCheck("", "cannot resolve '"+filePath+"'.", err.Error())
			}
		}
		abs = base + string(filepath.Separator) + filePath
	}
	if fi, err := os.Stat(abs); err != nil || !fi.Mode().IsRegular() {
		return hook.Result{}
	}
	canon, err := filepath.EvalSymlinks(abs)
	if err == nil {
		canon, err = filepath.Abs(canon)
	}
	if err != nil {
		return cannotCheck("", "cannot resolve '"+abs+"'.", err.Error())
	}
	abs = canon

	// "Not a git repository" is a real outcome and stays silent; any other git failure is not.
	// LC_ALL=C: git translates the text matched here.
	dir := filepath.Dir(abs)
	cmd := exec.Command(git, "-C", dir, "rev-parse", "--show-toplevel")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()
	root := strings.TrimRight(out.String(), "\n")
	if runErr != nil {
		if strings.Contains(root, "not a git repository") {
			return hook.Result{}
		}
		if root == "" {
			root = runErr.Error()
		}
		return cannotCheck("", "git could not read '"+dir+"'.", root)
	}
	if root == "" {
		return cannotCheck("", "git gave no repository root for '"+dir+"'.", "")
	}

	// Proof that this path was reached: without it a green test does not show whether the hook
	// looked or stepped out early.
	trace := ""
	if os.Getenv("LIGHTHOUSE_LINKCHECK_TRACE") != "" {
		trace = "linkcheck: checking in " + root + "\n"
	}
	problems, err := mdlinks.FindProblems(abs, root)
	if err != nil {
		return cannotCheck(trace, "the link parser failed.", err.Error())
	}
	if len(problems) == 0 {
		return hook.Result{Stderr: trace}
	}
	var b strings.Builder
	b.WriteString(trace + "Linkcheck: markdown link(s) that are not relative-and-existing.\n\n")
	for _, pr := range problems {
		reason, ok := problemReasons[pr.Kind]
		if !ok {
			reason = "unknown kind of problem"
		}
		b.WriteString("  " + pr.Target + " — " + reason + "\n")
	}
	b.WriteString("In: " + abs + "\n")
	return hook.Result{Code: 2, Stderr: b.String()}
}

// linkTarget reads .tool_input.file_path and .cwd from the payload, as `jq -r '... // empty'`
// did: an empty payload or null has neither, and false is false only when the JSON cannot be
// read or a step on the way is no object.
func linkTarget(raw []byte) (filePath, cwd string, ok bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", "", true
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", "", false
	}
	if payload == nil {
		return "", "", true
	}
	data, isMap := payload.(map[string]any)
	if !isMap {
		return "", "", false
	}
	cwd, _ = data["cwd"].(string)
	switch input := data["tool_input"].(type) {
	case nil:
		return "", cwd, true
	case map[string]any:
		filePath, _ = input["file_path"].(string)
		return filePath, cwd, true
	}
	return "", "", false
}

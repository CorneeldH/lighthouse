package roborev

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
)

// CheckError means the gate could not check; the CLI turns it into `COULD NOT CHECK` and exit 3.
type CheckError struct{ Msg string }

func (e *CheckError) Error() string { return e.Msg }

// ValueError is a refusal of the input; the CLI prints `roborev_gate: <msg>` and exits 2.
type ValueError struct{ Msg string }

func (e *ValueError) Error() string { return e.Msg }

func checkf(format string, args ...any) error { return &CheckError{fmt.Sprintf(format, args...)} }
func valuef(format string, args ...any) error { return &ValueError{fmt.Sprintf(format, args...)} }

// git runs `git -C root args` and returns stdout; strip trims it.
func git(root string, strip bool, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return "", checkf("git did not run: %v", err)
		}
		reason := strings.TrimSpace(stderr.String())
		if reason == "" {
			reason = fmt.Sprint(exit.ExitCode())
		}
		return "", checkf("git %s: %s", strings.Join(args, " "), reason)
	}
	if strip {
		return strings.TrimSpace(string(out)), nil
	}
	return string(out), nil
}

// StateDir is `<git-dir>/lighthouse/roborev`, absolute and per worktree.
func StateDir(root string) (string, error) {
	gitDir, err := git(root, true, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	return filepath.Join(gitDir, "lighthouse", "roborev"), nil
}

// BranchStem is the branch with `/` turned into `-`.
func BranchStem(branch string) string { return strings.ReplaceAll(branch, "/", "-") }

// StatePaths are the state file and the raw review of the current branch.
func StatePaths(root string) (state, raw string, err error) {
	branch, err := git(root, true, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", "", err
	}
	dir, err := StateDir(root)
	if err != nil {
		return "", "", err
	}
	stem := BranchStem(branch)
	return filepath.Join(dir, stem+".json"), filepath.Join(dir, stem+".md"), nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// readState is the verdict a recorded state file stands for, or nil without a file.
func readState(state string) (*orderedjson.Object, error) {
	if !isFile(state) {
		return nil, nil
	}
	text, err := os.ReadFile(state)
	if err != nil {
		return nil, checkf("%s: unreadable: %v", state, err)
	}
	parsed, err := orderedjson.Parse(text)
	if err != nil {
		return nil, checkf("%s: unreadable: %v", state, err)
	}
	data, ok := parsed.(*orderedjson.Object)
	if !ok {
		return nil, checkf("%s: not a JSON object", state)
	}
	outcome, _ := data.Get("uitkomst")
	out := orderedjson.NewObject()
	switch outcome {
	case "done":
		sha, _ := data.Get("sha")
		out.Set("verdict", "done")
		out.Set("sha", sha)
	case "skip":
		reason, _ := data.Get("reden")
		out.Set("verdict", "skip")
		out.Set("reason", reason)
	default:
		return nil, checkf("%s: unknown uitkomst %s", state, pyRepr(outcome))
	}
	return out, nil
}

// writeState writes data as indented JSON, creating the directory.
func writeState(state string, data *orderedjson.Object) error {
	if err := os.MkdirAll(filepath.Dir(state), 0o755); err != nil {
		return checkf("%s: %v", state, err)
	}
	text, err := orderedjson.Marshal(data)
	if err != nil {
		return checkf("%s: %v", state, err)
	}
	if err := os.WriteFile(state, text, 0o644); err != nil {
		return checkf("%s: %v", state, err)
	}
	return nil
}

// pyRepr is the Python repr of a JSON scalar, for messages the skills show.
func pyRepr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case string:
		quote := "'"
		if strings.Contains(x, "'") && !strings.Contains(x, `"`) {
			quote = `"`
		}
		x = strings.ReplaceAll(x, `\`, `\\`)
		if quote == "'" {
			x = strings.ReplaceAll(x, "'", `\'`)
		}
		x = strings.NewReplacer("\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(x)
		return quote + x + quote
	case bool:
		if x {
			return "True"
		}
		return "False"
	}
	return fmt.Sprint(v)
}

// pyStr is str() of a JSON scalar, as an f-string prints it.
func pyStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return pyRepr(v)
}

func mkdir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return checkf("%s: %v", dir, err)
	}
	return nil
}

// Package hooks holds the hooks that run as `lighthouse hook <name>`. Each registers itself with
// internal/hook in init, so a blank import of this package makes them known to the binary.
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/hook"
)

func init() {
	hook.Register("bash-write-check", BashWriteCheck)
	hook.Register("main-commit-gate", MainCommitGate)
	hook.Register("worktree-check", WorktreeCheck)
}

// runGit runs `git -C dir args…`, with LC_ALL=C when cLocale is set: a message matched as text
// must not be translated. Trailing newlines go off both strings, as `$(…)` in shell takes them
// off. When git does not start at all, stderr carries the reason, as a shell would print it.
func runGit(ctx context.Context, dir string, cLocale bool, args ...string) (stdout, stderr string, err error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	if cLocale {
		cmd.Env = append(os.Environ(), "LC_ALL=C")
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	stdout = strings.TrimRight(out.String(), "\n")
	stderr = strings.TrimRight(errOut.String(), "\n")
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) && stderr == "" {
		stderr = err.Error()
	}
	return stdout, stderr, err
}

// jqFields is the fields of one JSON value as jq indexes them: by exact key, the last of a
// duplicate key winning. A missing value, empty input and null have no fields; ok is false for
// anything that is not JSON or not an object, where jq fails on `.key`. (encoding/json into a
// struct matches keys case-insensitively, so `Tool_Input` would shadow `tool_input`.)
func jqFields(raw []byte) (fields map[string]json.RawMessage, ok bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, true
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	return fields, true
}

// jqText is the output of `jq -r '.key // empty'` for one value, without the trailing newline:
// a missing value, null and false are empty, a string is its text, any other value its JSON text.
func jqText(v json.RawMessage) string {
	text := string(bytes.TrimSpace(v))
	switch text {
	case "", "null", "false":
		return ""
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	return text
}

// defaultBranch is `forge.default_branch` as `lighthouse config` reads it. When it cannot be
// read, errText is the message of the config command, trailing newline included.
func defaultBranch() (branch string, errText string, ok bool) {
	var out, errOut strings.Builder
	if config.Run([]string{"forge.default_branch"}, &out, &errOut) != 0 {
		return "", errOut.String(), false
	}
	return strings.TrimRight(out.String(), "\n"), "", true
}

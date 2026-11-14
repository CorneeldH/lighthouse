// Package hooks holds the hooks that run as `lighthouse hook <name>`. Each registers itself with
// internal/hook in init, so a blank import of this package makes them known to the binary.
package hooks

import (
	"context"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/hook"
)

func init() {
	hook.Register("bash-write-check", BashWriteCheck)
	hook.Register("commit-test-gate", CommitTestGate)
	hook.Register("main-commit-gate", MainCommitGate)
	hook.Register("worktree-check", WorktreeCheck)
}

// runGit is hook.RunGit with the trailing newlines off both strings, as `$(…)` in shell takes
// them off.
func runGit(ctx context.Context, dir string, cLocale bool, args ...string) (stdout, stderr string, err error) {
	out, errOut, err := hook.RunGit(ctx, dir, cLocale, args...)
	return strings.TrimRight(string(out), "\n"), strings.TrimRight(string(errOut), "\n"), err
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

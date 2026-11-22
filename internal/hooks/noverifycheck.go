package hooks

import (
	"context"
	"os"

	"github.com/CorneeldH/lighthouse/internal/commitgate"
	"github.com/CorneeldH/lighthouse/internal/hook"
)

func init() { hook.Register("no-verify-check", NoVerifyCheck) }

const (
	noVerifyName = "no-verify-check"

	// noVerifyBlock is the refusal of --no-verify, the text commit-test-gate used to give under
	// its own prefix.
	noVerifyBlock = noVerifyName + ": --no-verify is not allowed in this repo.\n" +
		"\n" +
		"The test gate is a Claude Code PreToolUse hook, not a git hook, so the\n" +
		"flag skips nothing today — but it will as soon as git hooks are added.\n" +
		"If something fails: fix it. If it really has to be skipped, ask.\n"

	noVerifyInvalidPayload = noVerifyName + ": COULD NOT CHECK — the payload is not valid JSON.\n" +
		"\n" +
		"Without the command nothing was checked; that is not a green light.\n"
)

// NoVerifyCheck blocks `git commit --no-verify` (and the short -n) on every branch. It is a
// base hook: no stand, no override, and a components.toml it cannot read changes nothing, so
// switching off main-commit-gate or commit-test-gate never lets the flag through. It is listed
// in hooks/blocking, so a crash blocks too.
func NoVerifyCheck(_ context.Context, _, _ string, p hook.Payload) hook.Result {
	command, _, ok := commitgate.ReadPayload(p.Raw)
	if !ok {
		return blocked(noVerifyInvalidPayload)
	}
	if command == "" {
		return hook.Result{}
	}
	// The command is walked from the hook's own working directory, as no_verify.py did.
	here, _ := os.Getwd()
	if commitgate.CommandHasNoVerify(command, here) {
		return blocked(noVerifyBlock)
	}
	return hook.Result{}
}

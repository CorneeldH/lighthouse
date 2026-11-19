package hooks

// Counterpart: /lighthouse-write-pr
// The skill carries the norm up front (build the body with Closes evidence); this hook enforces
// it at the moment of a PR or MR create (`gh`, `glab` or `lighthouse forge`): a body without `Closes #N` + Evidence table, `Refs #N —
// <reason>` or `No issue: <reason>` blocks. The check itself is internal/prbody.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/hook"
	"github.com/CorneeldH/lighthouse/internal/prbody"
)

const prGateName = "pr-create-gate"

// reasonPRBody is the block of a PR body that lacks the closing line with evidence.
var reasonPRBody = hook.Declare(prGateName, "pr-body")[0]

// The messages of pr-create-gate, word for word those of the bash hook it replaces.
const (
	prGateMissingGit = prGateName + ": COULD NOT CHECK — missing tooling: git\n" +
		"\n" +
		"This hook checks the body of every PR or MR create.\n" +
		"Without that tooling nothing was checked; that is not a green light.\n"
	prGateInvalidJSON = prGateName + ": COULD NOT CHECK — the payload is not valid JSON.\n" +
		"\n" +
		"Without the command nothing was checked; that is not a green light.\n"
)

// runGate is the body check; a test replaces it to reach the outcomes prbody never gives.
var runGate = prbody.Main

// PrCreateGate checks the body of every PR or MR create (`gh pr create`, `glab mr create`, `lighthouse forge pr create`) in a Bash command. Not fail-open: without
// git, with a payload that is not JSON, with a command that cannot be parsed or with a parser
// that breaks, nothing was checked, and that blocks with COULD NOT CHECK. The only loud pass is
// the AC count (no forge token, or a forge error): prbody reports it on stdout, and the hook passes the note
// on to user and model. root is the lighthouse root; the config reads it as LIGHTHOUSE_REPO_ROOT.
//
// The gate deadline (prbody.DeadlineS) must stay under the hook timeout in hooks.json: a hook
// that times out lets the call pass.
func PrCreateGate(ctx context.Context, root, _ string, p hook.Payload) hook.Result {
	if _, err := exec.LookPath("git"); err != nil {
		return hook.CouldNotCheck(prGateMissingGit)
	}
	command, cwd, ok := readGatePayload(p.Raw)
	if !ok {
		return hook.CouldNotCheck(prGateInvalidJSON)
	}
	if command == "" {
		return hook.Result{}
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if root != "" {
		// The hook process runs this one gate: the variable is the way prbody finds its config.
		_ = os.Setenv("LIGHTHOUSE_REPO_ROOT", root)
	}

	code, stdout, stderr, panicked := callGate(ctx, cwd, command)
	switch {
	case panicked != "":
		return hook.CouldNotCheck(stderr + prGateCouldNotCheck("the body parser failed (panic: "+panicked+")."))
	case code == 0:
		message := strings.TrimRight(stdout, "\n")
		if message == "" {
			return hook.Result{Stderr: stderr}
		}
		// additionalContext reaches the model, systemMessage the user.
		return hook.Result{Stderr: stderr, Output: map[string]any{
			"systemMessage": message,
			"hookSpecificOutput": map[string]any{
				"hookEventName":     "PreToolUse",
				"additionalContext": message,
			},
		}}
	case code == 2 && strings.Contains(stderr, "COULD NOT CHECK"):
		return hook.CouldNotCheck(stderr)
	case code == 2:
		return hook.Block(reasonPRBody, stderr)
	case code == 3:
		// Two-step rule: without a forge tool (`gh`, `glab`, `forge`) and `pr` or `mr` in the
		// raw text there is nothing to check, and one stray apostrophe locks nothing up.
		if !mentionsPrCreate(command) {
			return hook.Result{Stderr: stderr}
		}
		return hook.CouldNotCheck(stderr + prGateCouldNotCheck("the command cannot be parsed (open quote). Split the command."))
	default:
		return hook.CouldNotCheck(stderr + prGateCouldNotCheck(fmt.Sprintf("the body parser failed (rc %d).", code)))
	}
}

// mentionsPrCreate is the loose raw-text test of the two-step rule: a forge tool and a PR or MR
// word somewhere in the command.
func mentionsPrCreate(command string) bool {
	tool := strings.Contains(command, "gh") || strings.Contains(command, "glab") || strings.Contains(command, "forge")
	return tool && (strings.Contains(command, "pr") || strings.Contains(command, "mr"))
}

// callGate runs the body check on command and catches a panic in it: unrecovered, the runtime
// would make it exit 3, and run.sh would print another text than this hook's own block.
func callGate(ctx context.Context, cwd, command string) (code int, stdout, stderr, panicked string) {
	var out, errOut bytes.Buffer
	defer func() {
		if v := recover(); v != nil {
			code, stdout, stderr, panicked = 0, out.String(), errOut.String(), fmt.Sprint(v)
		}
	}()
	code = runGate(ctx, cwd, strings.NewReader(command), &out, &errOut, prbody.DefaultEnv())
	return code, out.String(), errOut.String(), ""
}

// prGateCouldNotCheck is the block text for a check that could not be made.
func prGateCouldNotCheck(reason string) string {
	return prGateName + ": COULD NOT CHECK — " + reason + "\n" +
		"\n" +
		"This command may contain a PR or MR create. 'Could not check' is\n" +
		"not a green light, so this does not pass silently.\n"
}

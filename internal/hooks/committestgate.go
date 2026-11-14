package hooks

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/commitgate"
	"github.com/CorneeldH/lighthouse/internal/hook"
)

const gateUnknownMode = commitgate.HookName + ": COULD NOT CHECK — unknown mode, expected pre or post\n" +
	"\n" +
	"The registration in .claude/settings.json names no known mode; nothing was tested.\n"

// CommitTestGate is the test gate of `git commit`: pre (PreToolUse `Bash`) blocks --no-verify,
// tests a commit to the default branch before it and records the HEAD of every other root; post
// (PostToolUse and PostToolUseFailure `Bash`) tests what those commits touched. root is the
// lighthouse root, used when LIGHTHOUSE_REPO_ROOT is not set.
func CommitTestGate(ctx context.Context, root, stand string, p hook.Payload) hook.Result {
	if stand != "pre" && stand != "post" {
		return blocked(gateUnknownMode)
	}
	bash, missing := gateTooling()
	if missing != "" {
		return blocked(commitgate.HookName + ": COULD NOT CHECK — missing tooling:" + missing + "\n" +
			"\n" +
			"This hook runs the test command of every commit.\n" +
			"Without that tooling nothing was tested; that is not a green light.\n")
	}
	command, cwd, ok := commitgate.ReadPayload(p.Raw)
	if !ok {
		return blocked(commitgate.InvalidPayload)
	}
	if command == "" {
		return hook.Result{}
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// The configuration reader takes the root from the process environment; the children get an
	// environment without it.
	repoRoot := os.Getenv("LIGHTHOUSE_REPO_ROOT")
	if repoRoot == "" && root != "" {
		repoRoot = root
		_ = os.Setenv("LIGHTHOUSE_REPO_ROOT", root)
	}
	g := &commitgate.Gate{
		Mode:  stand,
		Root:  repoRoot,
		Env:   withoutEnv(os.Environ(), "LIGHTHOUSE_REPO_ROOT"),
		Bash:  bash,
		Trace: os.Getenv("LIGHTHOUSE_GATE_TRACE"),
	}
	// A test seam: a value that is no whole number leaves the real clock in place.
	if start, err := strconv.ParseInt(os.Getenv("LIGHTHOUSE_GATE_START"), 10, 64); err == nil {
		g.Start = &start
	}

	var res commitgate.Result
	if stand == "pre" {
		res = commitgate.Pre(ctx, g, p.Raw, cwd)
	} else {
		res = commitgate.Post(ctx, g, p.Raw)
	}
	return hook.Result{Code: res.Code, Output: res.Output, Stderr: res.Stderr}
}

// gateTooling is the path of bash, and the names of the missing tools, each after a space.
func gateTooling() (bash, missing string) {
	if _, err := exec.LookPath("git"); err != nil {
		missing += " git"
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		missing += " bash"
	}
	return bash, missing
}

// withoutEnv is env without key.
func withoutEnv(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); k != key {
			out = append(out, kv)
		}
	}
	return out
}
